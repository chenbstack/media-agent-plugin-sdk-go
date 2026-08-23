package pluginsdk

import "context"

// SubtitleWrite 是插件请求宿主落一个字幕 sidecar 的输入。
//
// 这里没有路径，是故意的：插件给的是 FileRef——宿主在事件里发下来的不透明句柄——
// 宿主自己查出它属于哪个存储、算出 sidecar 该叫什么名字放在哪。插件既指不了目录，
// 也没法用 ../ 走出去，能碰到的永远只有宿主点名交给它的那个媒体文件旁边。
type SubtitleWrite struct {
	// FileRef 来自事件 payload 的 files[].file_ref，不要自己拼。
	FileRef string `json:"file_ref"`
	// Content 是字幕文件的原始字节，宿主按 Ext 落盘，不做转码。
	Content []byte `json:"content"`
	// Language 是 BCP 47 或 ISO 639 语言码，如 zh / zh-Hans / en，决定 sidecar 的语言后缀。
	Language string `json:"language,omitempty"`
	// Ext 是不带点的字幕扩展名，如 srt / ass / ssa；留空时宿主按内容判定，判不出按 srt 落。
	Ext string `json:"ext,omitempty"`
	// Forced 标记这是强制字幕轨（只翻译外语对白的那种），影响 sidecar 命名。
	Forced bool `json:"forced,omitempty"`
	// Source 是给人看的来源说明，会进订阅追踪，如 "OpenSubtitles" 或站点名。
	Source string `json:"source,omitempty"`
}

// SubtitleWriteResult 是一次字幕落盘的结果。
type SubtitleWriteResult struct {
	// Path 是宿主实际写入（或已存在）的 sidecar 路径，只用于展示和排查。
	Path string `json:"path"`
	// Change：created 落盘成功 / unchanged 同名文件已存在，宿主保留原文件没有覆盖。
	//
	// unchanged 不是失败，但插件该把它当成"我这次没起作用"——别再拿它当成功证据去
	// 跳过后续来源。
	Change string `json:"change"`
}

const (
	SubtitleWriteCreated   = "created"
	SubtitleWriteUnchanged = "unchanged"
)

// MediaSidecars 让插件把随媒体文件存放的附属文件交给宿主落盘。
//
// 这是一个受限能力，不是文件系统：插件只能针对宿主发给它的 FileRef 写，写什么类型
// 由方法决定（目前只有字幕），路径、命名、存储后端全由宿主掌握。需要 host 权限
// "media.sidecar.write"。
type MediaSidecars interface {
	WriteSubtitle(ctx context.Context, input SubtitleWrite) (SubtitleWriteResult, error)
}

// SubtitleSidecar 是躺在某个媒体文件旁边的一份外挂字幕。
//
// 它由宿主列目录得来，不是按命名规则推算的——用户手工拷进去的字幕不遵守任何规则，
// 靠推算会漏看，而漏看的代价是又生成一份重复的。
type SubtitleSidecar struct {
	// Name 是文件名，不含目录。回读时原样递给 ReadSubtitle，别自己拼路径。
	Name string `json:"name"`
	// Language 是宿主从文件名里解析出的语言段（zh-CN / en / ...）。解析不出为空 ——
	// 那通常是用户手工放的、命名不带语言的那一份，不代表它没有语言。
	Language string `json:"language,omitempty"`
	// Ext 是不带点的扩展名，如 srt / ass。
	Ext string `json:"ext,omitempty"`
	// Forced 表示文件名里带 .forced 段：只翻译外语对白的那种字幕。拿它当翻译源会
	// 得到一份只有零星几句的成品。
	Forced bool `json:"forced,omitempty"`
	// SizeBytes 是文件字节数，供插件在读之前决定要不要读。
	SizeBytes int64 `json:"size_bytes,omitempty"`
}

// MediaSidecarReader 让插件读回媒体文件旁边已有的字幕。
//
// 它和 MediaSidecars 刻意分成两个接口、两条权限：写是「往用户的媒体目录里放东西」，
// 读是「把用户的媒体内容取出来」，后者才是内容可能被送出这台机器的那一步。一个只
// 需要落盘的字幕来源插件不该顺带获得读取用户已有字幕的能力。
//
// 需要 host 权限 "media.sidecar.read"。作用域和写侧一样收在 FileRef 上：插件只能
// 读宿主点名交给它的那个媒体文件旁边的字幕，Name 必须来自 ListSubtitles 的返回。
type MediaSidecarReader interface {
	ListSubtitles(ctx context.Context, fileRef string) ([]SubtitleSidecar, error)
	// ReadSubtitle 读回一份字幕的原始字节。name 必须是 ListSubtitles 列出来的文件名，
	// 宿主不接受任意路径。
	ReadSubtitle(ctx context.Context, fileRef, name string) ([]byte, error)
}
