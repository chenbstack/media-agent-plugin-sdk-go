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
	// Path 是宿主实际写入的 sidecar 路径，只用于展示和排查。
	Path string `json:"path"`
	// Change：created 新建 / updated 覆盖了同名文件 / unchanged 内容一致未动盘。
	Change string `json:"change"`
}

// MediaSidecars 让插件把随媒体文件存放的附属文件交给宿主落盘。
//
// 这是一个受限能力，不是文件系统：插件只能针对宿主发给它的 FileRef 写，写什么类型
// 由方法决定（目前只有字幕），路径、命名、存储后端全由宿主掌握。需要 host 权限
// "media.sidecar.write"。
type MediaSidecars interface {
	WriteSubtitle(ctx context.Context, input SubtitleWrite) (SubtitleWriteResult, error)
}
