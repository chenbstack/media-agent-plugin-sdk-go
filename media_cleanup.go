package pluginsdk

import "context"

// 本文件是媒体清理类插件向宿主索取的破坏性能力：删媒体库文件、列出并删除订阅。
// 删除下载任务沿用 DownloadControl.RemoveTask，不在这里重复定义。

type MediaDeleteInput struct {
	// FileRefs 是 MediaLibrary.List 返回的 FileRef，宿主按句柄定位文件，插件指定不了路径。
	FileRefs []string `json:"file_refs"`
}

type MediaDeleteItemResult struct {
	FileRef string `json:"file_ref"`
	// Status 取值 deleted / skipped / failed。skipped 表示句柄已失效或文件早已不在库里。
	Status        string `json:"status"`
	Path          string `json:"path,omitempty"`
	SizeBytes     int64  `json:"size_bytes,omitempty"`
	FailureReason string `json:"failure_reason,omitempty"`
}

type MediaDeleteResult struct {
	Items      []MediaDeleteItemResult `json:"items"`
	Deleted    int                     `json:"deleted"`
	Skipped    int                     `json:"skipped"`
	Failed     int                     `json:"failed"`
	FreedBytes int64                   `json:"freed_bytes"`
}

// MediaDelete 删除宿主媒体库里的视频文件及其相邻附属文件，并通知媒体服务器刷新。
// 删除不可恢复；宿主负责存储 Provider、路径收敛和审计。需要 host 权限
// "media.library.delete"。
type MediaDelete interface {
	DeleteMediaFiles(ctx context.Context, input MediaDeleteInput) (MediaDeleteResult, error)
}

// SubscriptionInfo 是一条订阅的只读快照。
type SubscriptionInfo struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	MediaType string `json:"media_type"`
	Year      int    `json:"year,omitempty"`
	Season    int    `json:"season,omitempty"`
	// Status 是宿主订阅状态，如 active / paused / completed。
	Status string `json:"status"`
	// SourceName 是订阅来源，如「热门订阅 · 年度高分」；用户手建的订阅为空。
	SourceName string `json:"source_name,omitempty"`
	CreatedAt  string `json:"created_at"`
	// CompletedAt 为空表示订阅尚未完成。
	CompletedAt string `json:"completed_at,omitempty"`
}

type SubscriptionQuery struct {
	Statuses []string `json:"statuses,omitempty"`
	// SourcePrefix 按来源名前缀过滤，如 "热门订阅"。
	SourcePrefix string `json:"source_prefix,omitempty"`
	// CreatedBefore 是 RFC3339 时间，只返回此前创建的订阅。
	CreatedBefore string `json:"created_before,omitempty"`
	Limit         int    `json:"limit,omitempty"`
	Offset        int    `json:"offset,omitempty"`
}

type SubscriptionDeleteInput struct {
	ID string `json:"id"`
	// DeleteFiles 为真时连同订阅在媒体库里的文件一起删除，否则只删订阅记录。
	DeleteFiles bool `json:"delete_files"`
}

type SubscriptionDeleteResult struct {
	ID           string `json:"id"`
	Deleted      bool   `json:"deleted"`
	DeletedFiles int    `json:"deleted_files,omitempty"`
}

// SubscriptionCleanup 列出并删除订阅。ListSubscriptions 需要 host 权限
// "subscriptions.read"，DeleteSubscription 需要 "subscriptions.delete"。
type SubscriptionCleanup interface {
	ListSubscriptions(ctx context.Context, query SubscriptionQuery) ([]SubscriptionInfo, error)
	DeleteSubscription(ctx context.Context, input SubscriptionDeleteInput) (SubscriptionDeleteResult, error)
}
