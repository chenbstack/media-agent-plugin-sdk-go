// Package fake 提供内存版 Provider 实现，供单元/集成测试使用，
// 避免测试依赖真实私站、下载器和媒体库（docs/test-strategy.md）。
package fake

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/chenbstack/media-agent-plugin-sdk-go/providers"
)

// ---- 下载器 ----

type Downloader struct {
	mu        sync.Mutex
	tasks     map[string]*providers.TorrentTask
	files     map[string][]providers.TorrentFile
	connErr   error
	nextState providers.DownloadState
	transfer  providers.TransferInfo
}

var _ providers.DownloaderProvider = (*Downloader)(nil)
var _ providers.DownloaderTagProvider = (*Downloader)(nil)

func NewDownloader() *Downloader {
	return &Downloader{
		tasks:     map[string]*providers.TorrentTask{},
		files:     map[string][]providers.TorrentFile{},
		nextState: providers.DownloadDownloading,
	}
}

// FailConnection 让 TestConnection 返回指定错误，模拟下载器不可用。
func (d *Downloader) FailConnection(err error) { d.connErr = err }

// CompleteTask 把任务标记为完成，模拟下载完成事件。
func (d *Downloader) CompleteTask(hash string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if t, ok := d.tasks[hash]; ok {
		t.State = providers.DownloadCompleted
		t.Progress = 1
		t.CompletedAt = time.Now()
	}
}

func (d *Downloader) SetTransferInfo(info providers.TransferInfo) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.transfer = info
}

func (d *Downloader) TransferInfo(context.Context) (providers.TransferInfo, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.transfer, d.connErr
}

func (d *Downloader) Kind() string { return "fake" }

func (d *Downloader) TestConnection(context.Context) error { return d.connErr }

func (d *Downloader) Add(_ context.Context, req providers.AddTorrentRequest) (providers.TorrentTask, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	source := req.Magnet
	if source == "" {
		source = req.TorrentURL
	}
	if source == "" {
		return providers.TorrentTask{}, fmt.Errorf("fake downloader: 缺少 torrent 来源")
	}
	sum := sha1.Sum([]byte(source))
	hash := hex.EncodeToString(sum[:])
	if existing, ok := d.tasks[hash]; ok {
		return *existing, fmt.Errorf("fake downloader: hash 已存在 %s", existing.Hash)
	}
	state := d.nextState
	if req.Paused {
		state = providers.DownloadPaused
	}
	task := &providers.TorrentTask{
		Hash: hash, Name: source, State: state,
		SavePath: req.SavePath, Category: req.Category, Tags: req.Tags,
		AddedAt: time.Now(),
	}
	d.tasks[hash] = task
	return *task, nil
}

func (d *Downloader) List(context.Context) ([]providers.TorrentTask, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]providers.TorrentTask, 0, len(d.tasks))
	for _, t := range d.tasks {
		out = append(out, *t)
	}
	return out, nil
}

func (d *Downloader) Pause(_ context.Context, hash string) error {
	return d.setState(hash, providers.DownloadPaused)
}

func (d *Downloader) Resume(_ context.Context, hash string) error {
	return d.setState(hash, providers.DownloadDownloading)
}

func (d *Downloader) Remove(_ context.Context, hash string, _ bool) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, ok := d.tasks[hash]; !ok {
		return fmt.Errorf("fake downloader: 任务不存在 %s", hash)
	}
	delete(d.tasks, hash)
	delete(d.files, hash)
	return nil
}

func (d *Downloader) Files(_ context.Context, hash string) ([]providers.TorrentFile, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, ok := d.tasks[hash]; !ok {
		return nil, fmt.Errorf("fake downloader: 任务不存在 %s", hash)
	}
	files := append([]providers.TorrentFile(nil), d.files[hash]...)
	return files, nil
}

func (d *Downloader) SetFileSelection(_ context.Context, hash string, files []providers.TorrentFile) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, ok := d.tasks[hash]; !ok {
		return fmt.Errorf("fake downloader: 任务不存在 %s", hash)
	}
	next := append([]providers.TorrentFile(nil), files...)
	for i := range next {
		if next[i].Selected && next[i].Priority <= 0 {
			next[i].Priority = 1
		}
		if !next[i].Selected {
			next[i].Priority = 0
		}
	}
	d.files[hash] = next
	return nil
}

func (d *Downloader) AddTags(_ context.Context, hash string, tags []string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	task, ok := d.tasks[hash]
	if !ok {
		return fmt.Errorf("fake downloader: 任务不存在 %s", hash)
	}
	seen := make(map[string]bool, len(task.Tags)+len(tags))
	for _, tag := range task.Tags {
		seen[tag] = true
	}
	for _, tag := range tags {
		if tag == "" || seen[tag] {
			continue
		}
		seen[tag] = true
		task.Tags = append(task.Tags, tag)
	}
	return nil
}

// SetFiles 注入任务的文件列表，模拟整季包。
func (d *Downloader) SetFiles(hash string, files []providers.TorrentFile) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.files[hash] = append([]providers.TorrentFile(nil), files...)
}

func (d *Downloader) setState(hash string, state providers.DownloadState) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	t, ok := d.tasks[hash]
	if !ok {
		return fmt.Errorf("fake downloader: 任务不存在 %s", hash)
	}
	t.State = state
	return nil
}

// ---- 媒体库 ----

type MediaServer struct {
	mu        sync.Mutex
	libraries []providers.Library
	items     []providers.LibraryItem
	refreshed []string
	connErr   error
}

var _ providers.MediaServerProvider = (*MediaServer)(nil)
var _ providers.MediaServerIncrementalProvider = (*MediaServer)(nil)

func NewMediaServer() *MediaServer {
	return &MediaServer{}
}

func (m *MediaServer) FailConnection(err error) { m.connErr = err }

// AddLibrary / AddItem 注入测试数据。
func (m *MediaServer) AddLibrary(lib providers.Library) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.libraries = append(m.libraries, lib)
}

func (m *MediaServer) AddItem(item providers.LibraryItem) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.items = append(m.items, item)
}

// Refreshed 返回被刷新过的 external id 列表，供断言。
func (m *MediaServer) Refreshed() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.refreshed...)
}

func (m *MediaServer) Kind() string { return "fake" }

func (m *MediaServer) TestConnection(context.Context) error { return m.connErr }

func (m *MediaServer) Libraries(context.Context) ([]providers.Library, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]providers.Library(nil), m.libraries...), nil
}

func (m *MediaServer) Items(_ context.Context, libraryID string, startIndex, limit int) ([]providers.LibraryItem, int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var inLib []providers.LibraryItem
	for _, item := range m.items {
		if libraryID == "" || item.LibraryID == libraryID {
			inLib = append(inLib, item)
		}
	}
	total := len(inLib)
	if startIndex >= total {
		return nil, total, nil
	}
	end := min(startIndex+limit, total)
	return append([]providers.LibraryItem(nil), inLib[startIndex:end]...), total, nil
}

// ItemsChangedSince 的 fake 不模拟远端时间，只复用分页数据；宿主测试可以用它
// 验证增量 RPC 和 upsert 行为。
func (m *MediaServer) ItemsChangedSince(ctx context.Context, libraryID, _ string, startIndex, limit int) ([]providers.LibraryItem, int, error) {
	return m.Items(ctx, libraryID, startIndex, limit)
}

func (m *MediaServer) Search(_ context.Context, query string) ([]providers.LibraryItem, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []providers.LibraryItem
	for _, item := range m.items {
		if query == "" || containsFold(item.Title, query) {
			out = append(out, item)
		}
	}
	return out, nil
}

func (m *MediaServer) Exists(_ context.Context, ref providers.MediaRef) (providers.Existence, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := providers.Existence{PresentEpisodes: map[int][]int{}}
	for _, item := range m.items {
		match := (ref.TMDBID != 0 && item.TMDBID == ref.TMDBID) ||
			(ref.TMDBID == 0 && containsFold(item.Title, ref.Title))
		if !match {
			continue
		}
		result.Exists = true
		result.Items = append(result.Items, item)
		if item.ItemType == "episode" {
			result.PresentEpisodes[item.SeasonNumber] = append(result.PresentEpisodes[item.SeasonNumber], item.EpisodeNumber)
		}
	}
	// 剧集：有集但明确不完整时由调用方结合 TMDB 集数判断 partial，fake 只按季给基础信号
	result.Partial = result.Exists && len(result.PresentEpisodes) > 0
	return result, nil
}

func (m *MediaServer) RefreshItem(_ context.Context, externalID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.refreshed = append(m.refreshed, externalID)
	return nil
}

func (m *MediaServer) RefreshLibrary(_ context.Context, externalLibraryID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.refreshed = append(m.refreshed, "library:"+externalLibraryID)
	return nil
}

func (m *MediaServer) Latest(_ context.Context, limit int) ([]providers.LibraryItem, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if limit <= 0 || limit > len(m.items) {
		limit = len(m.items)
	}
	return append([]providers.LibraryItem(nil), m.items[len(m.items)-limit:]...), nil
}

func containsFold(haystack, needle string) bool {
	h, n := []rune(haystack), []rune(needle)
	if len(n) == 0 {
		return true
	}
outer:
	for i := 0; i+len(n) <= len(h); i++ {
		for j := range n {
			if lower(h[i+j]) != lower(n[j]) {
				continue outer
			}
		}
		return true
	}
	return false
}

func lower(r rune) rune {
	if r >= 'A' && r <= 'Z' {
		return r + 32
	}
	return r
}
