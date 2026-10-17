package providers

import "context"

// 站点页面样本采集（宿主侧设计见 media-agent-lab docs/site-sample-collection.md）。
//
// 没有专用规则的站点，插件按站点类型采集制作规则所需的页面或接口数据，原样交给宿主；
// 脱敏、复检与上传全部由宿主完成。插件只负责「抓到什么、抓不到什么、为什么」，
// 不做任何脱敏判断，也不接触 Cloud。

// CapabilitySiteSampleCollect 是 Provider 级能力：采集无专用规则站点的样本。
const CapabilitySiteSampleCollect = "site.sample.collect"

// 样本里的站点类型。
const (
	SampleSiteHTML    = "html"
	SampleSiteAPI     = "api"
	SampleSiteSPA     = "spa"
	SampleSiteUnknown = "unknown"
)

// 样本条目的能力名：规则要覆盖的数据。
const (
	SampleCapProfile   = "profile"
	SampleCapTorrents  = "torrents"
	SampleCapDetail    = "detail"
	SampleCapSeeding   = "seeding"
	SampleCapBonus     = "bonus"
	SampleCapSubtitles = "subtitles"
	SampleCapPage      = "page"
)

// 样本条目状态。
const (
	SampleStatusOK      = "ok"
	SampleStatusMissing = "missing"
)

// SiteSampleCollector 由站点插件实现：判定站点类型并采集制作规则所需的数据。
// 对应 CapabilitySiteSampleCollect。只做只读访问，不访问下载、签到、退出等有副作用的地址。
type SiteSampleCollector interface {
	CollectSiteSample(ctx context.Context) (SiteSample, error)
}

// SiteSample 是插件采集到的、尚未脱敏的数据。
type SiteSample struct {
	SiteType      string            `json:"site_type"`
	Framework     string            `json:"framework,omitempty"`
	Evidence      string            `json:"evidence,omitempty"`
	PluginVersion string            `json:"plugin_version,omitempty"`
	Items         []SampleItem      `json:"items"`
	Known         SampleKnownValues `json:"known"`
	Baseline      []BaselineField   `json:"baseline,omitempty"`
}

// SampleItem 是一项能力对应的原始数据。Required 的项拿不到时整份样本不上传。
type SampleItem struct {
	Capability string `json:"capability"`
	Required   bool   `json:"required"`
	Status     string `json:"status"`
	// Reason 是 missing 时面向用户的中文原因，例如「种子列表页需要登录」。
	Reason string `json:"reason,omitempty"`
	// Source 标明数据怎么来的：http_page / api_call / rendered_page / captured_call 等。
	Source string `json:"source,omitempty"`
	// Classifier 记录由分类器定位时的依据与置信度。
	Classifier string          `json:"classifier,omitempty"`
	Page       *SamplePage     `json:"page,omitempty"`
	Exchange   *SampleExchange `json:"exchange,omitempty"`
}

// SamplePage 是一张 HTML 页面（含渲染后的 DOM）。
type SamplePage struct {
	URL      string `json:"url"`
	FinalURL string `json:"final_url,omitempty"`
	Status   int    `json:"status"`
	HTML     []byte `json:"html"`
}

// SampleExchange 是一次接口请求与响应。
type SampleExchange struct {
	Method          string            `json:"method"`
	URL             string            `json:"url"`
	RequestHeaders  map[string]string `json:"request_headers,omitempty"`
	RequestBody     []byte            `json:"request_body,omitempty"`
	Status          int               `json:"status"`
	ResponseHeaders map[string]string `json:"response_headers,omitempty"`
	ResponseBody    []byte            `json:"response_body,omitempty"`
}

// SampleKnownValues 是插件已经识别出的身份值，供宿主定向替换与复检。
type SampleKnownValues struct {
	Username string `json:"username,omitempty"`
	UserID   string `json:"user_id,omitempty"`
}

// BaselineField 记录框架基准规则在这批数据上某个字段是否命中。
type BaselineField struct {
	Block string `json:"block"` // search / profile / detail / seeding / subtitles
	Field string `json:"field"`
	Hit   bool   `json:"hit"`
}
