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
