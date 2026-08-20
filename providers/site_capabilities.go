package providers

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// 站点 Provider 的可选能力契约。
//
// SiteProvider 只声明所有站点都必须具备的四个方法。种子下载、MediaInfo 读取、字幕
// 附件和规则诊断是可选能力：官方站点插件全部具备，第三方纯规则插件可能一个都没有。
//
// 这些能力过去靠宿主在自己包内声明同名接口、对 Provider 做类型断言来探测。站点插件
// 迁出宿主进程后，跨进程适配器只实现 SDK 声明的方法，那些断言会在运行时无声失败并
// 走进「当作不支持」的分支——用户看到的是下载失败、字幕消失、校验跳过，且不留日志。
//
// 所以能力有无改由 SiteCapabilities 显式声明：宿主先问能力，再调方法。声明具备却调
// 用失败时返回 ErrCapabilityUnsupported，是显式错误而不是静默降级。

// ErrCapabilityUnsupported 表示 Provider 不具备被调用的可选能力。
// 宿主应据此明确降级并记录日志，不要当作「没有结果」。
var ErrCapabilityUnsupported = errors.New("站点 Provider 不支持该能力")

// UnsupportedCapabilityError 构造一个带能力名的 ErrCapabilityUnsupported。
func UnsupportedCapabilityError(capability string) error {
	return fmt.Errorf("%w: %s", ErrCapabilityUnsupported, capability)
}

// 站点可选能力的 capability 标识，与插件 manifest 的 capabilities 声明一致。
const (
	CapabilitySiteTorrentFetch  = "site.torrent.fetch"
	CapabilitySiteMediaInfoRead = "site.mediainfo.read"
	CapabilitySiteSubtitlesRead = "site.subtitles.read"
	CapabilitySiteDiagnose      = "site.diagnose"
	// CapabilitySiteSupportResolve 是插件级能力：判定站点地址是否被支持。
	CapabilitySiteSupportResolve = "site.support.resolve"
)

// SiteCapabilities 声明一个站点 Provider 具备哪些可选能力。
// 零值表示「只有 SiteProvider 的四个基础方法」。
type SiteCapabilities struct {
	// TorrentFetch：能用账号凭据在宿主侧下载 .torrent 文件。
	// 不具备时宿主只能把下载地址直接交给下载器，需要 Cookie 的站点会失败。
	TorrentFetch bool `json:"torrent_fetch,omitempty"`
	// MediaInfoRead：能从详情页读取 MediaInfo，供下载前的质量规则校验使用。
	MediaInfoRead bool `json:"mediainfo_read,omitempty"`
	// SubtitlesRead：能列出并下载详情页的字幕附件。
	SubtitlesRead bool `json:"subtitles_read,omitempty"`
	// Diagnose：能对站点规则执行只读诊断。
	Diagnose bool `json:"diagnose,omitempty"`
	// IMDBSearch：搜索支持按 IMDb ID 精确匹配。
	// 这是纯查询结果而非独立方法，并入能力集避免为一个布尔值单开一次 RPC。
	IMDBSearch bool `json:"imdb_search,omitempty"`
}

// SiteCapabilityReporter 由能提供可选能力的站点 Provider 实现。
// 未实现该接口的 Provider 一律视为只有基础能力。
type SiteCapabilityReporter interface {
	SiteCapabilities() SiteCapabilities
}

// CapabilitiesOfSite 读取站点 Provider 的可选能力。Provider 自己声明了能力集就以
// 声明为准；没有声明的（第三方同进程 Provider）退回类型断言推导，保持旧行为。
//
// 跨进程适配器一定实现 SiteCapabilityReporter，所以它永远走声明这条路——这正是
// 目的：适配器无条件实现全部可选接口，断言推导对它得出的结论必然是错的。
// 宿主应统一走这个函数，不要各自做类型断言。
func CapabilitiesOfSite(provider SiteProvider) SiteCapabilities {
	if reporter, ok := provider.(SiteCapabilityReporter); ok {
		return reporter.SiteCapabilities()
	}
	var caps SiteCapabilities
	_, caps.TorrentFetch = provider.(TorrentFetcher)
	_, caps.MediaInfoRead = provider.(TorrentMediaInfoProvider)
	_, caps.SubtitlesRead = provider.(SiteSubtitleProvider)
	_, caps.Diagnose = provider.(SiteDiagnoser)
	if imdb, ok := provider.(interface{ SupportsIMDBSearch() bool }); ok {
		caps.IMDBSearch = imdb.SupportsIMDBSearch()
	}
	return caps
}

// TorrentFetcher 用账号凭据在宿主侧下载种子文件，返回 .torrent 原始字节。
// 对应 CapabilitySiteTorrentFetch。
type TorrentFetcher interface {
	FetchTorrent(ctx context.Context, url string) ([]byte, error)
}

// TorrentMediaInfoProvider 从种子详情页读取 MediaInfo。
// 对应 CapabilitySiteMediaInfoRead。
type TorrentMediaInfoProvider interface {
	TorrentMediaInfo(ctx context.Context, detailURL string) (TorrentMediaInfo, error)
}

// SiteSubtitleProvider 列出并下载种子详情页的字幕附件。
// 对应 CapabilitySiteSubtitlesRead。
type SiteSubtitleProvider interface {
	SubtitleAttachments(ctx context.Context, detailURL string) ([]SubtitleAttachment, error)
	DownloadSubtitle(ctx context.Context, downloadURL string) ([]byte, error)
}

// SiteDiagnoser 对站点规则执行只读诊断：不写连接健康状态，也不落用户数据。
// 对应 CapabilitySiteDiagnose。
type SiteDiagnoser interface {
	Diagnose(ctx context.Context, input DiagnosticInput) DiagnosticReport
}

// 诊断状态取值。
const (
	DiagnosticOK      = "ok"
	DiagnosticWarning = "warning"
	DiagnosticError   = "error"
	DiagnosticSkipped = "skipped"
)

var reDiagnosticIMDBID = regexp.MustCompile(`tt\d+`)

// DiagnosticInput 是一次站点规则诊断的输入。
type DiagnosticInput struct {
	Keyword   string `json:"keyword"`
	IMDBID    string `json:"imdb_id"`
	MediaType string `json:"media_type"`
}

// NormalizeAndValidate 清洗并校验诊断输入，供宿主在收到请求时调用。
func (in *DiagnosticInput) NormalizeAndValidate() error {
	in.Keyword = strings.TrimSpace(in.Keyword)
	in.IMDBID = strings.TrimSpace(in.IMDBID)
	in.MediaType = strings.TrimSpace(in.MediaType)
	if len([]rune(in.Keyword)) > 200 {
		return fmt.Errorf("测试关键词不能超过 200 个字符")
	}
	if in.IMDBID != "" && reDiagnosticIMDBID.FindString(in.IMDBID) != in.IMDBID {
		return fmt.Errorf("IMDb ID 格式应为 tt 加数字")
	}
	if in.MediaType != "" && in.MediaType != "movie" && in.MediaType != "series" {
		return fmt.Errorf("媒体类型只支持 movie 或 series")
	}
	return nil
}

// DiagnosticCheck 是诊断报告里的一项检查结果。
type DiagnosticCheck struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Status      string `json:"status"`
	Message     string `json:"message"`
	DurationMS  int64  `json:"duration_ms,omitempty"`
	ResultCount int    `json:"result_count,omitempty"`
}

// DiagnosticReport 是一次站点规则诊断的完整结果。
type DiagnosticReport struct {
	Status      string            `json:"status"`
	RuleID      string            `json:"rule_id,omitempty"`
	RuleName    string            `json:"rule_name,omitempty"`
	RuleVersion string            `json:"rule_version,omitempty"`
	RuleType    string            `json:"rule_type,omitempty"`
	Fallback    bool              `json:"fallback"`
	Keyword     string            `json:"keyword,omitempty"`
	IMDBID      string            `json:"imdb_id,omitempty"`
	StartedAt   string            `json:"started_at"`
	DurationMS  int64             `json:"duration_ms"`
	Checks      []DiagnosticCheck `json:"checks"`
}

// ---- 站点地址支持性判定 ----
//
// 这条查询发生在「还没有站点账号」的时刻：用户在新建连接表单里粘贴一个站点地址，
// 宿主要据此判断该地址是否被支持、需要填哪些认证字段。此时构造不出 SiteProvider，
// 所以它是插件级查询（pluginsdk.Plugin.SiteSupportForURL），不挂在 Provider 上。

// AuthField 声明站点需要的一个认证字段及其表单属性。
// 字段值由宿主的站点连接配置保存并进入 secrets，规则文件里只有声明没有值。
type AuthField struct {
	Name        string `yaml:"name" json:"name"`
	Type        string `yaml:"type" json:"type"`
	Label       string `yaml:"label" json:"label"`
	Required    bool   `yaml:"required,omitempty" json:"required,omitempty"`
	Secret      bool   `yaml:"secret,omitempty" json:"secret,omitempty"`
	Placeholder string `yaml:"placeholder,omitempty" json:"placeholder,omitempty"`
	Help        string `yaml:"help,omitempty" json:"help,omitempty"`
	Multiline   bool   `yaml:"multiline,omitempty" json:"multiline,omitempty"`
	// RequestHeader 把该字段的值映射到 API 请求头（例如 api_key -> x-api-key）。
	// 只在插件内部使用，不下发给前端。
	RequestHeader string `yaml:"request_header,omitempty" json:"-"`
}

// SiteSupport 是站点地址的支持性判定结果。
// Fallback 为真表示没有匹配到专用规则，只能用通用兜底规则尝试。
type SiteSupport struct {
	Supported  bool        `json:"supported"`
	Fallback   bool        `json:"fallback,omitempty"`
	ID         string      `json:"id,omitempty"`
	Name       string      `json:"name,omitempty"`
	Version    string      `json:"version,omitempty"`
	Type       string      `json:"type,omitempty"`
	Domains    []string    `json:"domains,omitempty"`
	AuthFields []AuthField `json:"auth_fields"`
	Message    string      `json:"message,omitempty"`
}
