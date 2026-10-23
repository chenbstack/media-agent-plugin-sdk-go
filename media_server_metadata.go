package pluginsdk

import (
	"context"
	"errors"
)

// ErrMetadataRefreshUnsupported 表示宿主或媒体服务器插件包版本过旧，没有元数据列举与刷新能力。
var ErrMetadataRefreshUnsupported = errors.New("宿主或媒体服务器插件不支持元数据刷新，请升级主程序和官方插件包")

// 本文件是元数据刷新类插件向宿主索取的能力：列出媒体服务器条目的元数据完整度，并让宿主
// 请对应的媒体服务器 Provider 刷新指定条目的元数据。具体怎么刷新由媒体服务器 Provider
// 实现，插件只决定刷新谁、何时刷新。

type MetadataLibrary struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type MetadataServer struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Kind      string            `json:"kind"`
	Libraries []MetadataLibrary `json:"libraries,omitempty"`
}

type MetadataItemQuery struct {
	ServerID string `json:"server_id"`
	// LibraryID 为空表示该服务器的全部库。
	LibraryID string `json:"library_id,omitempty"`
	Offset    int    `json:"offset,omitempty"`
	Limit     int    `json:"limit,omitempty"`
	// ItemIDs restricts reads to these exact items for post-refresh verification.
	ItemIDs []string `json:"item_ids,omitempty"`
	// CheckPosterHealth requests a real media-server image read for items with a Primary image tag.
	CheckPosterHealth bool `json:"check_poster_health,omitempty"`
}

// MetadataItem 是一个电影或剧集的元数据完整度快照。时间为 RFC3339，缺失为空串。
type MetadataItem struct {
	ServerID  string `json:"server_id"`
	ItemID    string `json:"item_id"`
	LibraryID string `json:"library_id,omitempty"`
	ItemType  string `json:"item_type"` // movie / series
	Title     string `json:"title"`
	Year      int    `json:"year,omitempty"`
	// HasPoster / HasOverview / HasProviderIDs 为假表示缺少封面 / 简介 / 任何外部数据源 ID。
	HasPoster bool `json:"has_poster"`
	// PosterStatus is empty unless CheckPosterHealth was requested; values are valid, broken, or unknown.
	PosterStatus   string `json:"poster_status,omitempty"`
	HasOverview    bool   `json:"has_overview"`
	HasProviderIDs bool   `json:"has_provider_ids"`
	PremiereDate   string `json:"premiere_date,omitempty"`
	DateCreated    string `json:"date_created,omitempty"`
	LastRefreshed  string `json:"last_refreshed,omitempty"`
}

type MetadataItemPage struct {
	Items []MetadataItem `json:"items"`
	// PosterHealthChecked acknowledges CheckPosterHealth; older hosts leave it false.
	PosterHealthChecked bool `json:"poster_health_checked,omitempty"`
	// ItemIDsFiltered acknowledges exact-ID filtering; old hosts leave it false.
	ItemIDsFiltered bool `json:"item_ids_filtered,omitempty"`
	// Total 是该查询范围内的条目总数，用于翻页。
	Total int `json:"total"`
}

type MetadataRefreshInput struct {
	ServerID string   `json:"server_id"`
	ItemIDs  []string `json:"item_ids"`
	// ReplaceAll 为假只补齐缺失的元数据，为真则用数据源结果覆盖已有元数据和图片。
	ReplaceAll bool `json:"replace_all,omitempty"`
	// ReplaceImages replaces images only, preserving existing metadata in missing mode.
	ReplaceImages bool `json:"replace_images,omitempty"`
}

type MetadataRefreshItemResult struct {
	ItemID string `json:"item_id"`
	// Status 取值 refreshed / failed。refreshed 表示媒体服务器已接受刷新请求，
	// 不代表数据源一定查到了新元数据。
	Status        string `json:"status"`
	FailureReason string `json:"failure_reason,omitempty"`
}

type MetadataRefreshResult struct {
	Items     []MetadataRefreshItemResult `json:"items"`
	Refreshed int                         `json:"refreshed"`
	Failed    int                         `json:"failed"`
}

// MediaServerMetadata 列出媒体服务器条目的元数据完整度并请求刷新。需要 host 权限
// "media.server.metadata"。宿主较旧或媒体服务器插件包较旧时调用会返回
// ErrMetadataRefreshUnsupported。
type MediaServerMetadata interface {
	ListServers(ctx context.Context) ([]MetadataServer, error)
	ListItems(ctx context.Context, query MetadataItemQuery) (MetadataItemPage, error)
	RefreshItems(ctx context.Context, input MetadataRefreshInput) (MetadataRefreshResult, error)
}
