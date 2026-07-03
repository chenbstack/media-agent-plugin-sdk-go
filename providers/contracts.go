// Package providers 定义业务层依赖的 Provider 统一接口（docs/architecture.md §2.1）。
// 业务层只依赖这些接口，不关心实现来自官方插件还是第三方 CLI 插件。
package providers

import (
	"context"
	"time"
)

// ---- 下载器 ----

type DownloadState string

const (
	DownloadQueued      DownloadState = "queued"
	DownloadDownloading DownloadState = "downloading"
	DownloadPaused      DownloadState = "paused"
	DownloadCompleted   DownloadState = "completed"
	DownloadFailed      DownloadState = "failed"
)

type AddTorrentRequest struct {
	// TorrentURL 与 Magnet 二选一。
	TorrentURL string
	Magnet     string
	SavePath   string
	Category   string
	Tags       []string
	Paused     bool
}

type TorrentTask struct {
	Hash         string
	Name         string
	State        DownloadState
	Progress     float64 // 0-1
	Ratio        float64
	SizeBytes    int64
	SavePath     string
	Category     string
	Tags         []string
	AddedAt      time.Time
	CompletedAt  time.Time
	StateMessage string
}

type TorrentFile struct {
	Index     int
	Path      string
	SizeBytes int64
	Completed int64
	Selected  bool
	Priority  int
}

// DownloaderProvider 屏蔽 qBittorrent、Transmission 等下载器差异。
type DownloaderProvider interface {
	Kind() string
	TestConnection(ctx context.Context) error
	Add(ctx context.Context, req AddTorrentRequest) (TorrentTask, error)
	List(ctx context.Context) ([]TorrentTask, error)
	Pause(ctx context.Context, hash string) error
	Resume(ctx context.Context, hash string) error
	Remove(ctx context.Context, hash string, deleteData bool) error
	Files(ctx context.Context, hash string) ([]TorrentFile, error)
}

// ---- 媒体库 ----

type Library struct {
	ExternalID string
	Name       string
	MediaType  string // movie / series / mixed
	Paths      []string
}

type LibraryItem struct {
	ExternalID       string
	LibraryID        string
	ParentExternalID string
	ItemType         string // movie / series / season / episode
	Title            string
	Year             int
	Path             string
	TMDBID           int64
	IMDBID           string
	SeasonNumber     int
	EpisodeNumber    int
}

type MediaRef struct {
	MediaType     string // movie / series
	TMDBID        int64
	IMDBID        string
	Title         string
	Year          int
	SeasonNumber  int // 0 表示不限定季
	EpisodeNumber int // 0 表示不限定集
}

// Existence 描述条目在媒体库中的存在状态，用于搜索展示和申请查重。
type Existence struct {
	Exists          bool
	Partial         bool
	Items           []LibraryItem
	PresentEpisodes map[int][]int // season -> 已存在的集
}

// MediaServerProvider 屏蔽 Emby、Jellyfin 等媒体库差异。
type MediaServerProvider interface {
	Kind() string
	TestConnection(ctx context.Context) error
	Libraries(ctx context.Context) ([]Library, error)
	Search(ctx context.Context, query string) ([]LibraryItem, error)
	Exists(ctx context.Context, ref MediaRef) (Existence, error)
	RefreshItem(ctx context.Context, externalID string) error
	RefreshLibrary(ctx context.Context, externalLibraryID string) error
	Latest(ctx context.Context, limit int) ([]LibraryItem, error)
}
