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
	// ActiveSubscriptions 是站点范围覆盖这条种子所在站点的活跃订阅数，不是订阅总数。
	// 站点范围里没有这个站点的订阅根本不会去评估它，算进分母只会让计数永远追不上。
	ActiveSubscriptions int `json:"active_subscriptions"`
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

	// HitAndRun 表示站点在种子列表页给这条种子打了 H&R 标记。
	//
	// 它只反映站点打没打标：false 不等于「这个种没有考核要求」——站点可能全站
	// 都有 H&R 规则而不逐条标注，也可能页面结构变了没解析到。站点的实时考核状态
	// （还要做多久、当前分享率算不算达标）这里读不到。
	HitAndRun bool `json:"hit_and_run,omitempty"`

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
	// ExcludeHitAndRun 为 true 时不返回站点标了 H&R 的种子。宿主在查询里过滤掉，
	// 而不是让插件拿回来自己筛——查询有条数上限，H&R 种子会白占配额。
	ExcludeHitAndRun bool `json:"exclude_hit_and_run,omitempty"`
	// Limit 为 0 时由宿主取默认值，宿主同时有自己的上限。
	Limit int `json:"limit,omitempty"`
}

// SiteTorrentPool 让插件只读地取用宿主已经抓下来的站点种子池，附带宿主对每条种子
// 的放行裁决。需要 host 权限 "site.torrents.pool.read"。
//
// 池子由宿主的最新种子同步填充，同步关掉或抓取失败时这里就是空的。想让宿主去抓
// 一次要另一条权限，见 SiteTorrentPoolRefresh。
type SiteTorrentPool interface {
	ListPoolTorrents(ctx context.Context, query PoolTorrentQuery) ([]PoolTorrent, error)
}

// PoolRefreshInput 是一次抓取请求的范围。
type PoolRefreshInput struct {
	// SiteAccountIDs 为空表示所有已启用站点。宿主会先把不存在或已停用的站点剔掉。
	SiteAccountIDs []string `json:"site_account_ids,omitempty"`
}

// PoolRefreshNotAcceptedReason 说明宿主为什么没有受理这次抓取请求。
type PoolRefreshNotAcceptedReason string

const (
	// PoolRefreshAlreadyRunning：已经有一次同步在排队或执行，无需再排一次。
	PoolRefreshAlreadyRunning PoolRefreshNotAcceptedReason = "already_running"
	// PoolRefreshMinInterval：距上次抓取太近，还没到宿主允许的最小间隔。
	PoolRefreshMinInterval PoolRefreshNotAcceptedReason = "min_interval"
	// PoolRefreshNoSites：请求范围里没有可用的已启用站点。
	PoolRefreshNoSites PoolRefreshNotAcceptedReason = "no_sites"
)

// PoolRefreshResult 是宿主对抓取请求的答复。
type PoolRefreshResult struct {
	// Accepted 为 false 不是错误，是宿主按自己的闸门拒绝了这次请求。
	Accepted bool   `json:"accepted"`
	JobID    string `json:"job_id,omitempty"`
	// Reason 仅在 Accepted 为 false 时有值。
	Reason PoolRefreshNotAcceptedReason `json:"reason,omitempty"`
	// NextAllowedAt 是最小间隔挡下时下一次可以再问的时间（RFC3339），其余情况为空。
	NextAllowedAt string `json:"next_allowed_at,omitempty"`
}

// SiteTorrentPoolRefresh 让插件请求宿主对指定站点跑一次最新种子同步，用于宿主的
// 同步定时任务被关掉、或池子里就是没有这些站点数据的场合。
// 需要 host 权限 "site.torrents.pool.refresh"。
//
// 这是一个请求而不是命令：宿主有自己的最小间隔和并发闸门，不受理时返回
// Accepted=false 加原因，不当作错误。抓取由宿主用站点凭据执行，插件既拿不到凭据，
// 也决定不了抓几页；抓回来的种子照旧进池子、照旧走放行裁决，插件不会因此提前拿到
// 订阅还没看过的种子。
//
// 抓取是异步的：受理之后种子要等这一轮同步跑完才进池子，本轮查不到是正常的。
type SiteTorrentPoolRefresh interface {
	RefreshPool(ctx context.Context, in PoolRefreshInput) (PoolRefreshResult, error)
}
