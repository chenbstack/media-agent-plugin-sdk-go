package pluginsdk

import "context"

// PlaybackResolveInput 是插件请求宿主为一个存储文件解析实时播放地址的输入。
// StorageID 与 Path 必须来自宿主事件或用户选择，插件不应自行读取存储密钥。
type PlaybackResolveInput struct {
	StorageID string         `json:"storage_id"`
	Path      string         `json:"path"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

type PlaybackResolveResult struct {
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
}

// MediaPlayback 让插件通过宿主已配置的 StorageProvider 解析临时播放直链。
// 需要 host 权限 "media.playback.resolve"。
type MediaPlayback interface {
	ResolvePlaybackURL(ctx context.Context, input PlaybackResolveInput) (PlaybackResolveResult, error)
}
