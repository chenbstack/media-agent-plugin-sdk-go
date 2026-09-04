package pluginsdk

import "context"

// MediaLibraryItem 是宿主媒体库里可归档的一条主视频文件。
//
// FileRef 是宿主生成的不透明句柄，插件只能原样传回 MediaArchive；Path 仅用于
// 面板展示和规则命中提示，不能拿来拼接文件系统路径。
type MediaLibraryItem struct {
	FileRef       string `json:"file_ref"`
	MediaID       string `json:"media_id,omitempty"`
	MediaType     string `json:"media_type"`
	MediaCategory string `json:"media_category,omitempty"`
	Title         string `json:"title"`
	Year          int    `json:"year,omitempty"`
	Path          string `json:"path"`
	StorageID     string `json:"storage_id"`
	SizeBytes     int64  `json:"size_bytes"`
	ImportedAt    string `json:"imported_at"`
	// LastPlayedAt 为空表示宿主没有播放记录或从未播放。
	LastPlayedAt string `json:"last_played_at,omitempty"`
	Season       int    `json:"season,omitempty"`
	Episode      int    `json:"episode,omitempty"`
}

type MediaLibraryQuery struct {
	StorageID     string `json:"storage_id,omitempty"`
	MediaType     string `json:"media_type,omitempty"`
	MediaCategory string `json:"media_category,omitempty"`
	Before        string `json:"before,omitempty"`
	MinSizeBytes  int64  `json:"min_size_bytes,omitempty"`
	MaxSizeBytes  int64  `json:"max_size_bytes,omitempty"`
	Query         string `json:"query,omitempty"`
	Limit         int    `json:"limit,omitempty"`
	Offset        int    `json:"offset,omitempty"`
}

// MediaLibrary 只读查询已入库的视频文件。需要 host 权限 media.library.read。
type MediaLibrary interface {
	List(ctx context.Context, query MediaLibraryQuery) ([]MediaLibraryItem, error)
}

type MediaArchiveInput struct {
	FileRefs        []string `json:"file_refs"`
	TargetStorageID string   `json:"target_storage_id"`
	DeleteSource    bool     `json:"delete_source"`
	// PreserveSidecars 当前由宿主按视频相邻文件整体迁移，字段保留在契约里，便于
	// 后续增加可选 sidecar 范围而不改变插件调用形状。
	PreserveSidecars bool `json:"preserve_sidecars"`
}

type MediaArchiveItemResult struct {
	FileRef       string `json:"file_ref"`
	TaskID        string `json:"task_id,omitempty"`
	Status        string `json:"status"`
	SourcePath    string `json:"source_path,omitempty"`
	TargetPath    string `json:"target_path,omitempty"`
	SizeBytes     int64  `json:"size_bytes,omitempty"`
	FailureReason string `json:"failure_reason,omitempty"`
}

type MediaArchiveResult struct {
	Items     []MediaArchiveItemResult `json:"items"`
	TaskIDs   []string                 `json:"task_ids,omitempty"`
	Completed int                      `json:"completed"`
	Skipped   int                      `json:"skipped"`
	Failed    int                      `json:"failed"`
}

// MediaArchive 执行“复制并校验，成功后再删源”的归档操作。宿主负责路径收敛、
// 存储 Provider、冲突处理、校验和事件投递；插件不能直接操作媒体文件。
// 需要 host 权限 media.archive.write。
type MediaArchive interface {
	Archive(ctx context.Context, input MediaArchiveInput) (MediaArchiveResult, error)
}
