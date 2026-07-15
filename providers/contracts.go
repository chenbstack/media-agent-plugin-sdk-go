// Package providers 定义业务层依赖的 Provider 统一接口（docs/architecture.md §2.1）。
// 业务层只依赖这些接口，不关心实现来自官方插件还是第三方 CLI 插件。
package providers

import (
	"context"
	"errors"
	"io"
	"time"
)

// ---- 存储 ----

type StorageInfo struct {
	Kind         string
	RootPath     string
	Capabilities []string
	UsedBytes    int64 `json:"used_bytes,omitempty"`
	TotalBytes   int64 `json:"total_bytes,omitempty"`
}

// StorageProvider 屏蔽本地目录、SMB 和云盘等存储差异。
type StorageProvider interface {
	Kind() string
	TestConnection(ctx context.Context) error
	Info(ctx context.Context) (StorageInfo, error)
	EnsureMounted(ctx context.Context) error
	Unmount(ctx context.Context) error
}

type StorageFileInfo struct {
	Name    string
	Size    int64
	IsDir   bool
	ModTime time.Time
}

// StorageDirectoryLister 是支持按目录逐层浏览的可选能力。
// 插件需在 manifest 中声明 storage.browse，宿主才会向 UI 暴露目录选择入口。
type StorageDirectoryLister interface {
	ListDir(ctx context.Context, path string) ([]StorageFileInfo, error)
}

// FileStorageProvider 是整理执行需要的文件操作能力。
// 具体协议细节由插件内部维护，业务层只使用这里的通用语义。
type FileStorageProvider interface {
	StorageProvider
	Stat(ctx context.Context, name string) (StorageFileInfo, error)
	MkdirAll(ctx context.Context, path string) error
	Remove(ctx context.Context, name string) error
	OpenReader(ctx context.Context, name string) (io.ReadCloser, error)
	OpenWriter(ctx context.Context, name string) (io.WriteCloser, error)
	Rename(ctx context.Context, oldpath, newpath string) error
	Link(ctx context.Context, oldname, newname string) error
	Symlink(ctx context.Context, oldname, newname string) error
}

// UploadSource 是宿主为插件准备好的可重复读取上传源。
// 插件可以按需使用 Size/SHA1/OpenRange 做秒传、校验片段或分片上传。
type UploadSource interface {
	Name() string
	Size() int64
	SHA1(ctx context.Context) (string, error)
	Open(ctx context.Context) (io.ReadCloser, error)
	OpenRange(ctx context.Context, offset, length int64) (io.ReadCloser, error)
}

// UploadProvider 是云盘等非流式写入存储可选实现的上传能力。
// 宿主会先把来源准备为 UploadSource，再交给插件内部完成协议级上传。
type UploadProvider interface {
	Upload(ctx context.Context, name string, source UploadSource) error
}

// ServerSideCopyProvider 是云盘/网络存储可选实现的同存储复制能力。
// 未实现时，整理执行会继续使用 OpenReader/OpenWriter 做流式复制。
type ServerSideCopyProvider interface {
	Copy(ctx context.Context, oldname, newname string) error
}

// PlaybackURLInput 描述宿主播放网关请求某个存储文件的播放地址。
// Provider 通常只需要 Path；Metadata 预留给 115 pickcode、123 s3 key 等原生文件标识。
type PlaybackURLInput struct {
	Path     string         `json:"path"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// PlaybackURLResult 是存储插件换取到的临时播放 URL。
type PlaybackURLResult struct {
	URL       string            `json:"url"`
	ExpiresAt time.Time         `json:"expires_at,omitempty"`
	Headers   map[string]string `json:"headers,omitempty"`
}

// PlaybackURLProvider 是云盘等存储可选实现的播放 URL 解析能力。
// 宿主播放网关调用它并把 URL 作为 302 Location 返回。
type PlaybackURLProvider interface {
	ResolvePlaybackURL(ctx context.Context, input PlaybackURLInput) (PlaybackURLResult, error)
}

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
	Hash          string
	Name          string
	State         DownloadState
	Progress      float64 // 0-1
	Ratio         float64
	SizeBytes     int64
	DownloadSpeed int64 // bytes/s
	UploadSpeed   int64 // bytes/s
	SavePath      string
	Category      string
	Tags          []string
	AddedAt       time.Time
	CompletedAt   time.Time
	StateMessage  string
}

type TorrentFile struct {
	Index         int
	Path          string
	SizeBytes     int64
	Completed     int64
	Selected      bool
	Priority      int
	MediaKind     string
	SeasonNumber  int
	EpisodeNumber int
	Confidence    float64
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
	SetFileSelection(ctx context.Context, hash string, files []TorrentFile) error
	// TransferInfo 返回下载器当前的全局传输速度，用于连接卡片实时展示。
	TransferInfo(ctx context.Context) (TransferInfo, error)
}

// TransferInfo 是下载器全局传输状态快照。
type TransferInfo struct {
	DownloadSpeed int64 // bytes/s
	UploadSpeed   int64 // bytes/s
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

// ErrIncrementalSyncUnsupported 表示媒体服务器 Provider 没有可靠的按更新时间增量枚举能力。
// 宿主收到后应跳过增量任务，并保留周期性全量校验。
var ErrIncrementalSyncUnsupported = errors.New("media server incremental sync unsupported")

// MediaServerIncrementalProvider 是 MediaServerProvider 的可选扩展。
// since 必须是 RFC3339/RFC3339Nano 时间；返回值与 Items 一样按 startIndex/limit
// 分页。该接口只负责新增和更新，删除仍由宿主的周期性全量校验发现。
type MediaServerIncrementalProvider interface {
	ItemsChangedSince(ctx context.Context, libraryID, since string, startIndex, limit int) ([]LibraryItem, int, error)
}

// MediaServerProvider 屏蔽 Emby、Jellyfin 等媒体库差异。
type MediaServerProvider interface {
	Kind() string
	TestConnection(ctx context.Context) error
	Libraries(ctx context.Context) ([]Library, error)
	// Items 分页拉取库内条目（movie/series/season/episode），用于媒体库同步缓存。
	// startIndex 从 0 开始；返回 total 为库内条目总数，同步必须分页可恢复。
	Items(ctx context.Context, libraryID string, startIndex, limit int) ([]LibraryItem, int, error)
	Search(ctx context.Context, query string) ([]LibraryItem, error)
	Exists(ctx context.Context, ref MediaRef) (Existence, error)
	RefreshItem(ctx context.Context, externalID string) error
	RefreshLibrary(ctx context.Context, externalLibraryID string) error
	Latest(ctx context.Context, limit int) ([]LibraryItem, error)
}

// ---- 元数据（docs/metadata-provider.md）----

// MetaExternalIDs 是媒体在各外部数据源的 ID 映射；零值表示未知。
type MetaExternalIDs struct {
	TMDBID   int64
	IMDBID   string
	TVDBID   int64
	DoubanID string
}

type MetaSearchResult struct {
	Provider      string // 插件 id，如 "tmdb"
	ProviderID    string // 数据源内媒体 ID
	MediaType     string // movie / series
	Title         string
	OriginalTitle string
	Year          int
	Overview      string
	PosterURL     string
	Score         float64 // 数据源评分 0-10
	Popularity    float64
}

type MetaAlias struct {
	Name     string
	Language string // 语言或地区码，如 zh / US；可空
}

type MetaSeason struct {
	SeasonNumber     int
	Title            string
	AirDate          string // YYYY-MM-DD，可空
	EpisodeCount     int
	ProviderSeasonID string
}

type MetaDetail struct {
	Provider       string
	ProviderID     string
	MediaType      string // movie / series
	Title          string
	OriginalTitle  string
	Year           int
	Overview       string
	Status         string // released / upcoming / airing / ended
	Genres         []string
	RuntimeMinutes int
	PosterURL      string
	BackdropURL    string
	ExternalIDs    MetaExternalIDs
	Aliases        []MetaAlias
	Cast           []MetaCastMember
	Seasons        []MetaSeason // 仅剧集
	Raw            []byte       // 数据源原始响应，落 media.raw_json 兜底
}

type MetaCastMember struct {
	Name       string
	Character  string
	ProfileURL string // 头像缩略图
}

type MetaEpisode struct {
	SeasonNumber      int
	EpisodeNumber     int
	Title             string
	AirDate           string
	RuntimeMinutes    int
	Overview          string
	StillURL          string // 集剧照缩略图
	ProviderEpisodeID string
}

// MetadataProvider 屏蔽 TMDB、TVDB、豆瓣等元数据源差异。
// 元数据是申请、订阅、查重和命名的业务真源；媒体库只回答"是否已入库"。
type MetadataProvider interface {
	Kind() string
	TestConnection(ctx context.Context) error
	// Search 按标题搜索；mediaType 为 movie / series / 空（不限）；year 为 0 表示不限年份。
	Search(ctx context.Context, query, mediaType string, year int) ([]MetaSearchResult, error)
	Detail(ctx context.Context, mediaType, providerID string) (MetaDetail, error)
	SeasonEpisodes(ctx context.Context, providerID string, seasonNumber int) ([]MetaEpisode, error)
	// FindByExternalID 用外部 ID（如 IMDb）反查；找不到返回空切片，不算错误。
	FindByExternalID(ctx context.Context, ids MetaExternalIDs) ([]MetaSearchResult, error)
}

// ---- 模型提供方 ----

var (
	ErrModelProviderInvalidInput     = errors.New("模型参数无效")
	ErrModelProviderNotConfigured    = errors.New("模型未配置")
	ErrModelProviderRuntimeMissing   = errors.New("模型运行器不可用")
	ErrModelProviderDownloadFailed   = errors.New("模型下载失败")
	ErrModelProviderUninstallFailed  = errors.New("模型卸载失败")
	ErrModelProviderGenerationFailed = errors.New("模型生成失败")
)

type ModelConfig struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	Provider         string   `json:"provider"`
	Runtime          string   `json:"runtime"`
	Backend          string   `json:"backend,omitempty"`
	Command          string   `json:"command"`
	ModelPath        string   `json:"model_path"`
	ModelName        string   `json:"model_name,omitempty"`
	BaseURL          string   `json:"base_url,omitempty"`
	APIKey           string   `json:"api_key,omitempty"`
	APIKeyEnv        string   `json:"api_key_env,omitempty"`
	DownloadSite     string   `json:"download_site,omitempty"`
	DownloadURL      string   `json:"download_url,omitempty"`
	SHA256           string   `json:"sha256,omitempty"`
	Args             []string `json:"args"`
	Enabled          bool     `json:"enabled"`
	UseCPU           bool     `json:"use_cpu"`
	Threads          int      `json:"threads"`
	ContextTokens    int      `json:"context_tokens"`
	DefaultMaxTokens int      `json:"default_max_tokens"`
	Notes            string   `json:"notes,omitempty"`
}

type ModelGenerateRequest struct {
	Model     ModelConfig
	Prompt    string
	MaxTokens int
	Now       func() time.Time
}

type ModelGenerateResult struct {
	Output   string
	Stderr   string
	Started  time.Time
	Finished time.Time
}

type ModelDownloadRequest struct {
	Model          ModelConfig
	TimeoutSeconds int
	Now            func() time.Time
}

type ModelDownloadResult struct {
	ModelID             string `json:"model_id"`
	Name                string `json:"name"`
	URL                 string `json:"url"`
	ModelPath           string `json:"model_path"`
	Bytes               int64  `json:"bytes"`
	SHA256              string `json:"sha256"`
	StartedAt           string `json:"started_at"`
	FinishedAt          string `json:"finished_at"`
	ElapsedMilliseconds int64  `json:"elapsed_ms"`
}

type ModelUninstallRequest struct {
	Model          ModelConfig
	TimeoutSeconds int
	Now            func() time.Time
}

type ModelUninstallResult struct {
	ModelID             string `json:"model_id"`
	Name                string `json:"name"`
	ModelPath           string `json:"model_path"`
	StartedAt           string `json:"started_at"`
	FinishedAt          string `json:"finished_at"`
	ElapsedMilliseconds int64  `json:"elapsed_ms"`
}

// ModelProvider 屏蔽 llama.cpp、Ollama、OpenAI-compatible 等模型提供方差异。
type ModelProvider interface {
	Kind() string
	ValidateModel(model ModelConfig) error
	Generate(ctx context.Context, req ModelGenerateRequest) (ModelGenerateResult, error)
	Download(ctx context.Context, req ModelDownloadRequest) (ModelDownloadResult, error)
	Uninstall(ctx context.Context, req ModelUninstallRequest) (ModelUninstallResult, error)
	CommandDisplay(model ModelConfig) string
}

// ---- Cookie 来源 ----

// CookieSourceProvider 从 CookieCloud、浏览器导出或备份文件等来源返回结构化 Cookie。
// 插件只负责读取和解析来源；站点账号新增/更新由宿主统一校验、写 secret 和审计。
type CookieSourceProvider interface {
	Kind() string
	TestConnection(ctx context.Context) error
	Snapshot(ctx context.Context) (CookieSnapshot, error)
}

type CookieSnapshot struct {
	Source    string         `json:"source"`
	FetchedAt time.Time      `json:"fetched_at"`
	Domains   []CookieDomain `json:"domains"`
}

type CookieDomain struct {
	Domain  string       `json:"domain"`
	Cookies []HTTPCookie `json:"cookies"`
}

type HTTPCookie struct {
	Name     string    `json:"name"`
	Value    string    `json:"value"`
	Domain   string    `json:"domain,omitempty"`
	Path     string    `json:"path,omitempty"`
	Expires  time.Time `json:"expires,omitempty"`
	Secure   bool      `json:"secure,omitempty"`
	HTTPOnly bool      `json:"http_only,omitempty"`
}

// SiteProfile 是站点账号的用户数据快照（NexusPHP 系站点通用字段）。
// 体积字段单位 bytes；解析不到的字段保持零值。
type SiteProfile struct {
	Username    string  `json:"username"`
	UserID      string  `json:"user_id,omitempty"`
	UserLevel   string  `json:"user_level,omitempty"`
	JoinAt      string  `json:"join_at,omitempty"`
	Upload      int64   `json:"upload"`
	Download    int64   `json:"download"`
	Ratio       float64 `json:"ratio"`
	Bonus       float64 `json:"bonus"`
	Seeding     int     `json:"seeding"`
	SeedingSize int64   `json:"seeding_size"`
	Leeching    int     `json:"leeching"`
}

// TorrentSearchRequest 是种子搜索请求。Keyword 为空表示浏览站点最新种子。
type TorrentSearchRequest struct {
	Keyword   string // 搜索关键词
	MediaType string // movie / series / 空（不限）；决定使用站点的哪个搜索路径和分类
	IMDBID    string // 可选；站点支持 imdb 搜索时优先使用（如 tt1234567）
	Page      int    // 页码，0 起
}

// TorrentResult 是归一化的种子搜索结果（字段对齐 data-model.md 的 search_candidates）。
// 体积字段单位 bytes；解析不到的字段保持零值。
type TorrentResult struct {
	Title       string `json:"title"`
	Subtitle    string `json:"subtitle,omitempty"`
	DetailURL   string `json:"detail_url"`
	DownloadURL string `json:"download_url,omitempty"`
	Magnet      string `json:"magnet,omitempty"`
	SizeBytes   int64  `json:"size_bytes"`
	Seeders     int    `json:"seeders"`
	Leechers    int    `json:"leechers"`
	Grabs       int    `json:"grabs"`                  // 完成数
	PublishedAt string `json:"published_at,omitempty"` // YYYY-MM-DD HH:MM:SS
	Category    string `json:"category,omitempty"`
	IMDBID      string `json:"imdb_id,omitempty"`
	// 促销因子：DownloadFactor 1=正常计下载量 0=免费；UploadFactor 1=正常 2=上传翻倍。
	DownloadFactor float64  `json:"download_factor"`
	UploadFactor   float64  `json:"upload_factor"`
	Promotion      string   `json:"promotion,omitempty"` // 由促销因子派生：免费 / 2X / 2X免费 / 50% 等
	HitAndRun      bool     `json:"hit_and_run,omitempty"`
	Labels         []string `json:"labels,omitempty"`
}

type SubtitleAttachment struct {
	Name        string `json:"name"`
	Language    string `json:"language,omitempty"`
	DownloadURL string `json:"download_url"`
	SizeBytes   int64  `json:"size_bytes,omitempty"`
}

type MediaInfoTag struct {
	Facet string `json:"facet"`
	Value string `json:"value"`
}

type TorrentMediaInfo struct {
	Raw            string         `json:"raw,omitempty"`
	Tags           []MediaInfoTag `json:"tags,omitempty"`
	ObservedFacets []string       `json:"observed_facets,omitempty"`
}

// SiteProvider 屏蔽站点差异，供站点账号健康检查、用户数据同步和种子搜索使用。
type SiteProvider interface {
	Kind() string
	// TestConnection 用账号凭据（Cookie/UA/代理）访问站点验证可达性与登录态。
	TestConnection(ctx context.Context) error
	// Profile 抓取并解析站点用户数据；Cookie 失效返回错误。
	Profile(ctx context.Context) (SiteProfile, error)
	// Search 按关键词搜索种子并归一化；站点无搜索规则时返回错误。
	Search(ctx context.Context, req TorrentSearchRequest) ([]TorrentResult, error)
}

// ---- 通知渠道（docs/notification-system.md §3）----

// NotificationMessage 是发往外部渠道的一条通知。文案由宿主模板渲染完成，
// 插件只负责按渠道协议投递，不参与内容组装。
type NotificationMessage struct {
	Title       string            `json:"title"`
	Body        string            `json:"body"`
	Severity    string            `json:"severity,omitempty"` // info / success / warning / critical / action
	Category    string            `json:"category,omitempty"`
	ActionLabel string            `json:"action_label,omitempty"`
	ActionURL   string            `json:"action_url,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// NotifierProvider 屏蔽钉钉、Telegram、邮件、Webhook 等外部通知渠道差异。
// 站内通知永远由宿主直接写库，不经过该接口。
type NotifierProvider interface {
	Kind() string
	TestConnection(ctx context.Context) error
	Send(ctx context.Context, msg NotificationMessage) error
}

// ---- 字幕来源 ----

// SubtitleSearchRequest 描述为单个视频文件搜索字幕所需的上下文。
// 识别信息可能不完整；插件按可用字段搜索，缺关键字段时返回空结果，不算错误。
type SubtitleSearchRequest struct {
	MediaType     string   `json:"media_type,omitempty"` // movie / series
	Title         string   `json:"title,omitempty"`
	OriginalTitle string   `json:"original_title,omitempty"`
	Year          int      `json:"year,omitempty"`
	SeasonNumber  int      `json:"season_number,omitempty"`
	EpisodeNumber int      `json:"episode_number,omitempty"`
	IMDBID        string   `json:"imdb_id,omitempty"`
	TMDBID        int64    `json:"tmdb_id,omitempty"`
	FileName      string   `json:"file_name,omitempty"` // 目标视频文件名，用于按命名匹配
	Languages     []string `json:"languages,omitempty"` // 期望语言优先级，如 ["zh", "en"]
	// Context 是来源相关的透传信息，如 PT 站字幕插件需要的
	// site_account_id 和 detail_url；由宿主按下载任务来源填充。
	Context map[string]string `json:"context,omitempty"`
}

// SubtitleResult 是归一化的候选字幕。
type SubtitleResult struct {
	Provider    string            `json:"provider"` // 插件 id
	Name        string            `json:"name"`
	Language    string            `json:"language,omitempty"`
	Format      string            `json:"format,omitempty"` // srt / ass / sup ...
	SizeBytes   int64             `json:"size_bytes,omitempty"`
	Score       float64           `json:"score,omitempty"` // 来源侧匹配度 0-1；宿主还会做文件级匹配
	DownloadURL string            `json:"download_url,omitempty"`
	Ref         map[string]string `json:"ref,omitempty"` // 插件私有的下载定位信息（如 file_id）
}

// SubtitleSourceProvider 屏蔽 PT 站附件、OpenSubtitles 等字幕来源差异。
// 宿主负责候选与视频文件的匹配、解压和落盘；插件只负责搜索与取回原始字幕内容。
type SubtitleSourceProvider interface {
	Kind() string
	TestConnection(ctx context.Context) error
	Search(ctx context.Context, req SubtitleSearchRequest) ([]SubtitleResult, error)
	Download(ctx context.Context, result SubtitleResult) ([]byte, error)
}

// ---- 浏览器渲染（站点反爬挑战兜底）----

// RenderRequest 是一次页面渲染请求。宿主在直接 HTTP 命中 Cloudflare /
// DDoS-GUARD 等反爬挑战时调用，用真实浏览器环境取回页面。
type RenderRequest struct {
	URL            string            `json:"url"`
	UserAgent      string            `json:"user_agent,omitempty"`
	Headers        map[string]string `json:"headers,omitempty"`
	Cookies        []HTTPCookie      `json:"cookies,omitempty"`
	WaitUntil      string            `json:"wait_until,omitempty"`    // load / domcontentloaded / networkidle
	WaitSelector   string            `json:"wait_selector,omitempty"` // 可选：等待选择器出现后再取快照
	TimeoutSeconds int               `json:"timeout_seconds,omitempty"`
}

// RenderResult 是渲染完成后的页面快照。Cookies 返回渲染后的完整 cookie，
// 供宿主回写站点账号（如 Cloudflare 通过后的 cf_clearance）。
type RenderResult struct {
	HTML     string       `json:"html"`
	FinalURL string       `json:"final_url,omitempty"`
	Status   int          `json:"status,omitempty"`
	Cookies  []HTTPCookie `json:"cookies,omitempty"`
}

// RendererProvider 屏蔽 Playwright、Lightpanda 等浏览器仿真后端差异。
type RendererProvider interface {
	Kind() string
	TestConnection(ctx context.Context) error
	Render(ctx context.Context, req RenderRequest) (RenderResult, error)
}
