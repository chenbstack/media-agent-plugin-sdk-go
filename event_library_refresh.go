package pluginsdk

import (
	"strings"

	"github.com/chenbstack/media-agent-plugin-sdk-go/providers"
)

// EventLibraryRefreshPending 是 library.refresh.pending 同步钩子的事件类型名。
//
// 这个钩子位在「整理已完成、媒体库还没刷」的时间窗上。插件想让自己的产出跟着这轮
// 刷新一起进库（补字幕、生成 nfo、抓剧照），就订阅它——晚一步这轮刷新带不上，只能
// 干等下一次全量同步。
const EventLibraryRefreshPending = "library.refresh.pending"

// LibraryRefreshPending 是该事件 payload 里字幕插件用得上的那部分。
//
// payload 本身还带着 transfer、files 等整理信息，这里只挑出「哪些文件还缺字幕、
// 各自什么情况、要哪些语言、上哪儿找」。字段是宿主与插件之间的契约，别照着 map 的
// 键名自己解析：键名变了编译期发现不了，运行期表现是插件默默什么都不干。
type LibraryRefreshPending struct {
	// Files 是还缺字幕的视频。宿主每投一个插件前都会重算，前一个插件搞定的
	// 文件不会再出现在这里——所以它为空就是"没你的活了"，直接返回即可。
	Files []SubtitleTarget
	// Languages 是用户设置的字幕语言，按优先级从高到低（如 ["zh-CN","zh-TW"]）。
	//
	// 插件搜什么语言、在多个候选里挑哪个，都以它为准，别自己写死一门语言——宿主
	// 那边「这个文件还缺不缺字幕」用的是同一份设置，两边不一致就会没完没了：插件
	// 下了一份宿主不认的语言，宿主下轮整理照样判定还缺，于是再投递一次。
	//
	// 为空说明宿主版本比这个字段老，退回插件自己的默认值即可。
	Languages []string
	// Context 是找字幕要用的来源信息，插件够不着宿主数据库，只能由宿主发下来。
	Context SubtitleLookup
	// MediaType 是 movie 或 series；Title 是媒体标题。
	MediaType string
	Title     string
}

// SubtitleTarget 是一个待配字幕的视频文件。
type SubtitleTarget struct {
	// FileRef 是宿主发下来的不透明句柄，落字幕时原样递回 MediaSidecars.WriteSubtitle。
	// 它不是路径，也拼不出路径——写到哪个目录、叫什么名字全由宿主算。
	FileRef string
	// Path 只作展示和片名匹配用。别拿它去拼写入位置，宿主不认插件给的路径。
	Path string
	// MediaKind 目前恒为 video。
	MediaKind string
	// Season / Episode 为 0 表示电影或宿主没识别出季集。
	Season  int
	Episode int
	// Tracks 是宿主用 ffprobe 探到的轨道现状。为 nil 表示没探到（不是"没有轨道"）,
	// 这两种情况该做的判断不一样。
	Tracks *providers.MediaTracks
	// MovieHash 是这个文件的 OSDb hash，SizeBytes 是它的字节数，两者一起交给
	// OpenSubtitles 这类按内容匹配的来源。
	//
	// 精确匹配和按名字猜是两回事：同一部片同一个命名之下，不同压制的时间轴对不上，
	// 按名字挑出来的字幕经常是错轴的，而错轴字幕比没有更糟——它会让宿主判定这片
	// 字幕齐了，从此不再补。
	//
	// 宿主算不出来时为空：存储不支持区间读，或者文件小于 128KB 凑不满采样。
	MovieHash string
	SizeBytes int64
}

// SubtitleLookup 是宿主发下来的字幕检索线索。
type SubtitleLookup struct {
	// SiteAccountID / DetailURL 指向这个种子的来源站点和详情页，供站点附件类来源使用。
	// 资源不是从站点下来的时候它们为空。
	SiteAccountID string
	DetailURL     string
	OriginalTitle string
	Year          int
	IMDBID        string
	TMDBID        int64
}

// ParseLibraryRefreshPending 从事件 payload 里取出字幕插件要用的部分。
//
// 数字字段认 int 和 float64 两种形状：进程内投递保持宿主构造时的类型，过一趟 RPC 的
// JSON 就全变成 float64 了，只认一种的插件会在另一种投递方式下静默拿到 0。
func ParseLibraryRefreshPending(payload map[string]any) LibraryRefreshPending {
	out := LibraryRefreshPending{}
	media := payloadMap(payload, "media")
	out.MediaType = payloadString(media, "type")
	out.Title = payloadString(media, "title")

	out.Languages = payloadStringList(payload, "subtitle_languages")

	lookup := payloadMap(payload, "subtitle_context")
	out.Context = SubtitleLookup{
		SiteAccountID: payloadString(lookup, "site_account_id"),
		DetailURL:     payloadString(lookup, "detail_url"),
		OriginalTitle: payloadString(lookup, "original_title"),
		Year:          payloadInt(lookup, "year"),
		IMDBID:        payloadString(lookup, "imdb_id"),
		TMDBID:        int64(payloadInt(lookup, "tmdb_id")),
	}

	for _, item := range payloadList(payload, "files_missing_subtitles") {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		ref := payloadString(entry, "file_ref")
		if ref == "" {
			// 没有句柄就落不了盘，这条目对插件毫无用处。
			continue
		}
		out.Files = append(out.Files, SubtitleTarget{
			FileRef:   ref,
			Path:      payloadString(entry, "path"),
			MediaKind: payloadString(entry, "media_kind"),
			Season:    payloadInt(entry, "season"),
			Episode:   payloadInt(entry, "episode"),
			Tracks:    payloadTracks(entry),
			MovieHash: payloadString(entry, "movie_hash"),
			SizeBytes: int64(payloadInt(entry, "size_bytes")),
		})
	}
	return out
}

func payloadTracks(entry map[string]any) *providers.MediaTracks {
	audios := payloadList(entry, "audio_tracks")
	subtitles := payloadList(entry, "subtitle_tracks")
	if len(audios) == 0 && len(subtitles) == 0 {
		return nil
	}
	tracks := &providers.MediaTracks{}
	for _, item := range audios {
		track, ok := item.(map[string]any)
		if !ok {
			continue
		}
		tracks.Audios = append(tracks.Audios, providers.MediaAudioTrack{
			Codec:    payloadString(track, "codec"),
			Language: payloadString(track, "language"),
			Title:    payloadString(track, "title"),
			Channels: payloadInt(track, "channels"),
		})
	}
	for _, item := range subtitles {
		track, ok := item.(map[string]any)
		if !ok {
			continue
		}
		forced, _ := track["forced"].(bool)
		tracks.Subtitles = append(tracks.Subtitles, providers.MediaSubtitleTrack{
			Codec:    payloadString(track, "codec"),
			Language: payloadString(track, "language"),
			Title:    payloadString(track, "title"),
			Forced:   forced,
		})
	}
	return tracks
}

func payloadList(payload map[string]any, key string) []any {
	if payload == nil {
		return nil
	}
	if raw, ok := payload[key].([]any); ok {
		return raw
	}
	// 进程内投递不过 JSON，切片保持着宿主构造时的具体类型。
	if typed, ok := payload[key].([]map[string]any); ok {
		out := make([]any, 0, len(typed))
		for _, entry := range typed {
			out = append(out, entry)
		}
		return out
	}
	return nil
}

// payloadStringList 认 []string 和 []any 两种形状：进程内投递保持宿主构造时的
// []string，过一趟 RPC 的 JSON 就变成 []any 了。
func payloadStringList(payload map[string]any, key string) []string {
	if payload == nil {
		return nil
	}
	switch value := payload[key].(type) {
	case []string:
		return append([]string(nil), value...)
	case []any:
		out := make([]string, 0, len(value))
		for _, item := range value {
			if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
				out = append(out, strings.TrimSpace(text))
			}
		}
		return out
	}
	return nil
}

func payloadMap(payload map[string]any, key string) map[string]any {
	if payload == nil {
		return nil
	}
	out, _ := payload[key].(map[string]any)
	return out
}

func payloadString(payload map[string]any, key string) string {
	if payload == nil {
		return ""
	}
	value, _ := payload[key].(string)
	return strings.TrimSpace(value)
}

func payloadInt(payload map[string]any, key string) int {
	if payload == nil {
		return 0
	}
	switch value := payload[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	}
	return 0
}
