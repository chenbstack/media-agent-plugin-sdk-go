package pluginsdk

import "context"

// PlaybackCacheWarmInput asks the host to cache the bytes most useful during
// media probing. Source and target identify the same file before and after an
// organize transfer; the plugin never reads provider credentials or media
// bytes itself.
type PlaybackCacheWarmInput struct {
	SourceStorageID string `json:"source_storage_id"`
	SourcePath      string `json:"source_path"`
	TargetStorageID string `json:"target_storage_id"`
	TargetPath      string `json:"target_path"`
	SizeBytes       int64  `json:"size_bytes,omitempty"`
	HeadBytes       int64  `json:"head_bytes,omitempty"`
	TailBytes       int64  `json:"tail_bytes,omitempty"`
}

// PlaybackCacheWarmResult reports what the host actually stored. A failed or
// partial warm-up must not make the organize operation fail; callers can use
// this for diagnostics and continue with normal upstream playback.
type PlaybackCacheWarmResult struct {
	TotalBytes int64 `json:"total_bytes"`
	HeadBytes  int64 `json:"head_bytes"`
	TailBytes  int64 `json:"tail_bytes"`
	Cached     bool  `json:"cached"`
}

// MediaPlaybackCache lets a plugin ask the host to prefetch bounded media
// ranges. The cache is owned by the host and is consumed by its playback
// gateway. Requires host permission "media.playback.cache.write".
type MediaPlaybackCache interface {
	Warm(ctx context.Context, input PlaybackCacheWarmInput) (PlaybackCacheWarmResult, error)
}
