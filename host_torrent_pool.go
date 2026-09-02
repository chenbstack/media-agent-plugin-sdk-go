package pluginsdk

import "context"

// PoolReleaseState 是宿主对「这个种子还轮不轮得到插件用」的裁决。
//
// 判定要 join 订阅评估状态、候选快照和决策三张表，插件自己算不但重复实现，还会把
// 订阅的内部结构变成事实上的插件 API——订阅那边改一次表，所有插件跟着坏。所以宿主
// 算完只给结论。
type PoolReleaseState string

const (
	// PoolHeld：宿主还没看完，插件不能碰。
	PoolHeld PoolReleaseState = "held"
	// PoolReleased：宿主看过且不要，可以拿去用。
	PoolReleased PoolReleaseState = "released"
	// PoolTaken：宿主已经用它建了下载。
	PoolTaken PoolReleaseState = "taken"
)

// PoolReleaseReason 说明放行是怎么来的，便于插件在界面上解释，也便于排查
// 「为什么这个种子迟迟不放行」。
type PoolReleaseReason string

const (
	// PoolReleasedByAllEvaluated：所有活跃订阅都评估过它且都没要。
	PoolReleasedByAllEvaluated PoolReleaseReason = "all_evaluated"
	// PoolReleasedByTimeout：入池太久仍无人认领。没有活跃订阅时种子永远等不到
	// 评估，只有这条兜底能让它放行。
	PoolReleasedByTimeout PoolReleaseReason = "timeout"
)

// PoolRelease 是一条种子的放行裁决。EvaluatedSubscriptions 与 ActiveSubscriptions
// 一并给出，插件据此显示「已被 3/5 个订阅看过」这类进度，而不必猜还差多少。
type PoolRelease struct {
	State                  PoolReleaseState  `json:"state"`
	Reason                 PoolReleaseReason `json:"reason,omitempty"`
	EvaluatedSubscriptions int               `json:"evaluated_subscriptions"`
	ActiveSubscriptions    int               `json:"active_subscriptions"`
}

// PoolTorrent 是站点种子池里的一条记录。宿主的最新种子同步负责填充这个池子，
// 池子按最近一次看到的时间保留有限时长（当前 72 小时）后清理。
//
// 因此插件不能把这里的 ID 当作长期引用：做种要持续数天到数周，而池子里的记录早就
// 没了。需要长期跟踪的信息（下了哪些、什么时候下的）由插件自己落私有表。
type PoolTorrent struct {
	ID            string `json:"id"`
	SiteAccountID string `json:"site_account_id"`
	SiteName      string `json:"site_name,omitempty"`

	Title     string `json:"title"`
	Subtitle  string `json:"subtitle,omitempty"`
	DetailURL string `json:"detail_url,omitempty"`

	// DownloadURL 与 MagnetURI 至少有一个非空——两个都空的记录没法下载，宿主不会返回。
	DownloadURL string `json:"download_url,omitempty"`
	MagnetURI   string `json:"magnet_uri,omitempty"`

	SizeBytes int64 `json:"size_bytes"`
	Seeders   int   `json:"seeders"`
	Leechers  int   `json:"leechers"`
	Grabs     int   `json:"grabs"`

	Freeleech bool   `json:"freeleech"`
	Promotion string `json:"promotion,omitempty"`

	// PublishTimeUnix 为 0 表示站点没给发布时间。按种龄筛选时这类记录要单独决定
	// 取舍，不能当成「刚发布」。
	PublishTimeUnix int64  `json:"publish_time_unix,omitempty"`
	FirstSeenAt     string `json:"first_seen_at"`

	Release PoolRelease `json:"release"`
}

// PoolTorrentQuery 限定一次种子池查询。插件不能自己写筛选条件——那等于把池子的表
// 结构暴露出去。
type PoolTorrentQuery struct {
	// SiteAccountIDs 为空表示不限站点。宿主始终只返回已启用站点的种子。
	SiteAccountIDs []string `json:"site_account_ids,omitempty"`
	// OnlyReleased 为 true 时只返回宿主已经放行的种子，这是刷流这类「捡漏」场景
	// 应该用的取法。
	OnlyReleased bool `json:"only_released,omitempty"`
	// MinAgeSeconds 按入池时间再筛一道，0 表示不限。
	MinAgeSeconds int `json:"min_age_seconds,omitempty"`
	// Limit 为 0 时由宿主取默认值，宿主同时有自己的上限。
	Limit int `json:"limit,omitempty"`
}

// SiteTorrentPool 让插件只读地取用宿主已经抓下来的站点种子池，附带宿主对每条种子
// 的放行裁决。需要 host 权限 "site.torrents.pool.read"。
//
// 池子由宿主的最新种子同步填充：没开同步的站点在这里没有数据。
type SiteTorrentPool interface {
	ListPoolTorrents(ctx context.Context, query PoolTorrentQuery) ([]PoolTorrent, error)
}
