package pluginsdk

import "context"

// MirrorWrite 是插件请求宿主为某个媒体文件，在另一个存储里生成一个替身文件的输入。
//
// 典型用途是 .strm：整理完成后在一个供媒体服务器扫描的目录树里，按媒体文件原样的
// 相对路径放一个同名 .strm，内容指向真实播放地址。
//
// 这里同样没有路径，和 MediaSidecars 一个道理：插件给的是 FileRef——宿主在事件里
// 发下去的不透明句柄——宿主自己查出它在源存储里的相对路径，镜像到目标存储的同一
// 位置。插件能决定的只有写进哪个存储、换成什么扩展名、内容是什么；它指不了目录，
// 也没法用 ../ 走出去。
//
// 插件自己拼路径 os.WriteFile 的老写法有两处错：对网络存储（smb://）根本不成立，
// 在容器里又跑在与用户配置无关的沙箱 uid 上，写用户的目录只会拿到 permission denied。
type MirrorWrite struct {
	// FileRef 来自事件 payload 的 files[].file_ref，不要自己拼。
	FileRef string `json:"file_ref"`
	// TargetStorageID 是替身文件要落进的存储实例 id，来自插件配置里的存储选择器。
	// 它可以和源文件所在的存储不同——这正是 strm 的常见形态。
	TargetStorageID string `json:"target_storage_id"`
	// Ext 是替身文件的扩展名，不带点，如 strm。留空时沿用源文件的扩展名。
	Ext string `json:"ext,omitempty"`
	// Content 是替身文件的原始字节。这条通道是给小文本文件用的，宿主对体积有上限；
	// 媒体本身该走 StorageProvider，不要往这里塞。
	Content []byte `json:"content"`
	// Overwrite 为真时覆盖同名文件，否则保留原件并返回 unchanged。
	Overwrite bool `json:"overwrite"`
}

// MirrorWriteResult 是一次替身写入的结果。
type MirrorWriteResult struct {
	// Path 是宿主实际写入（或已存在）的目标存储内路径，只用于展示和排查。
	Path string `json:"path"`
	// Change：created 新建 / replaced 覆盖了同名文件 / unchanged 同名文件已存在
	// 且没开覆盖，宿主保留了原文件。
	//
	// unchanged 不是失败，但插件该把它当成「我这次没起作用」——别拿它当成功证据。
	Change string `json:"change"`
}

const (
	MirrorWriteCreated   = "created"
	MirrorWriteReplaced  = "replaced"
	MirrorWriteUnchanged = "unchanged"
)

// MediaMirrors 让插件为宿主点名的媒体文件，在另一个存储里生成一个路径镜像的替身文件。
//
// 这是一个受限能力，不是文件系统：锚点是宿主发下来的 FileRef，目标存储由用户的配置
// 决定，路径由宿主推导。需要 host 权限 "media.mirror.write"。
type MediaMirrors interface {
	WriteMirror(ctx context.Context, input MirrorWrite) (MirrorWriteResult, error)
}
