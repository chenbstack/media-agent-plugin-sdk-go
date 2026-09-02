package pluginsdk

import (
	"context"
	"time"

	"github.com/chenbstack/media-agent-plugin-sdk-go/providers"
)

// DownloaderInfo 是一个已配置的下载器连接。凭据永远不在里面。
type DownloaderInfo struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Enabled bool   `json:"enabled"`
	Default bool   `json:"default"`
}

// DownloadTaskInfo 是一条下载任务的快照：既有下载器那边的活数据（进度、分享率、
// 上传速度），也有宿主这边的归属信息（来源、是否参与整理）。
//
// 活数据来自宿主上一轮下载同步，不是调用这一刻现打下载器取的。做种策略类插件据此
// 判断删不删种没有问题——轮询间隔以分钟计，而分享率和做种时长以小时到天计。但要
// 拿它做「刚刚是不是变了」这种即时判断就会失准。
type DownloadTaskInfo struct {
	ID           string `json:"id"`
	DownloaderID string `json:"downloader_id"`
	ExternalHash string `json:"external_hash,omitempty"`

	Name     string                  `json:"name"`
	State    providers.DownloadState `json:"state"`
	Progress float64                 `json:"progress"`
	Ratio    float64                 `json:"ratio"`

	// 累计上传量宿主没有：下载器接口不返回它，宿主也就无从缓存。要估算用
	// Ratio × SizeBytes，要精确值只能由插件自己按轮次累计。
	SizeBytes     int64 `json:"size_bytes"`
	UploadSpeed   int64 `json:"upload_speed"`
	DownloadSpeed int64 `json:"download_speed"`

	SavePath string   `json:"save_path,omitempty"`
	Category string   `json:"category,omitempty"`
	Tags     []string `json:"tags,omitempty"`

	AddedAt     time.Time `json:"added_at,omitempty"`
	CompletedAt time.Time `json:"completed_at,omitempty"`

	// Origin 是 "host" 或 "plugin:<id>"。插件看得到别的插件和宿主建的任务——这是
	// 这项能力的设计意图，不是疏漏。
	Origin string `json:"origin"`
	// Visible 与 AutoOrganize 是建任务时定下的策略，见 AddTorrentInput。
	Visible        bool   `json:"visible"`
	AutoOrganize   bool   `json:"auto_organize"`
	SubscriptionID string `json:"subscription_id,omitempty"`
}

// DownloadFileInfo 是任务里的一个文件。
type DownloadFileInfo struct {
	Index          int    `json:"index"`
	Path           string `json:"path"`
	SizeBytes      int64  `json:"size_bytes"`
	CompletedBytes int64  `json:"completed_bytes"`
	Selected       bool   `json:"selected"`
	Priority       int    `json:"priority"`
}

// DownloadFileSelection 指定某个文件下不下。
type DownloadFileSelection struct {
	Index    int  `json:"index"`
	Selected bool `json:"selected"`
}

// DownloadTaskQuery 限定一次任务查询。字段全空表示「所有下载器上的所有任务」。
type DownloadTaskQuery struct {
	DownloaderIDs []string `json:"downloader_ids,omitempty"`
	// Origins 按来源过滤，如 "plugin:site-brush"。为空表示不限来源。
	Origins []string `json:"origins,omitempty"`
	// States 按下载状态过滤，为空表示不限。
	States []providers.DownloadState `json:"states,omitempty"`
	Limit  int                       `json:"limit,omitempty"`
}

// AddTorrentInput 是插件请宿主加一个种子。
//
// TorrentURL、Magnet、TorrentData 三选一，TorrentData 优先。
type AddTorrentInput struct {
	// DownloaderID 为空时用宿主的默认下载器。
	DownloaderID string `json:"downloader_id,omitempty"`

	// SiteAccountID 指明 TorrentURL 属于哪个站点账号。PT 站点的下载链接要带
	// Cookie 才取得到，直接把 URL 丢给下载器只会拿回一个登录页。给了这个字段，
	// 宿主就用该站点的凭据把种子取下来、校验过再以文件形式提交给下载器；凭据
	// 全程不经过插件。
	//
	// 种子池里的每条记录都带 SiteAccountID，照原样传回来即可。公开站点的直链
	// 可以留空。
	SiteAccountID string `json:"site_account_id,omitempty"`

	TorrentURL  string `json:"torrent_url,omitempty"`
	Magnet      string `json:"magnet,omitempty"`
	TorrentData []byte `json:"torrent_data,omitempty"`
	TorrentName string `json:"torrent_name,omitempty"`

	Name     string   `json:"name"`
	SavePath string   `json:"save_path,omitempty"`
	Category string   `json:"category,omitempty"`
	Tags     []string `json:"tags,omitempty"`
	Paused   bool     `json:"paused,omitempty"`

	// Visible 决定这个任务是否出现在宿主的下载任务页。
	//
	// AutoOrganize 决定它完成后是否进入验收与整理入库。
	//
	// 两者的零值都是「否」，这是有意选的保守默认：插件加的种多半不是媒体库内容
	// （是的话该走订阅），漏设一个字段导致不入库，比漏设导致乱入库轻得多。
	Visible      bool `json:"visible,omitempty"`
	AutoOrganize bool `json:"auto_organize,omitempty"`
}

// DownloadTasks 只读地看下载任务，包括别的插件和宿主自己建的。需要 host 权限
// "downloads.tasks.read"。
type DownloadTasks interface {
	ListDownloaders(ctx context.Context) ([]DownloaderInfo, error)
	ListTasks(ctx context.Context, query DownloadTaskQuery) ([]DownloadTaskInfo, error)
	GetTask(ctx context.Context, taskID string) (DownloadTaskInfo, error)
	ListTaskFiles(ctx context.Context, taskID string) ([]DownloadFileInfo, error)
}

// DownloadControl 操作下载器。需要 host 权限 "downloads.control"。
//
// 这项权限的范围是所有任务，不只是本插件建的：RemoveTask 能删掉用户手动添加的种和
// 订阅下载的种，deleteData 为真时连同文件一起删。宿主会为每次删除留审计记录，但
// 拦不住一个想删的插件——授权前请确认插件值得这份信任。
type DownloadControl interface {
	AddTorrent(ctx context.Context, input AddTorrentInput) (DownloadTaskInfo, error)
	PauseTask(ctx context.Context, taskID string) error
	ResumeTask(ctx context.Context, taskID string) error
	RemoveTask(ctx context.Context, taskID string, deleteData bool) error
	SetFileSelection(ctx context.Context, taskID string, files []DownloadFileSelection) error
}
