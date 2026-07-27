package pluginsdk

import "testing"

func TestParseLibraryRefreshPendingReadsHostPayload(t *testing.T) {
	parsed := ParseLibraryRefreshPending(map[string]any{
		"media": map[string]any{"type": "series", "title": "The Bear"},
		"subtitle_context": map[string]any{
			"site_account_id": "acct1",
			"detail_url":      "https://pt.example/details.php?id=1",
			"original_title":  "The Bear",
			"year":            2022,
			"imdb_id":         "tt14452776",
			"tmdb_id":         136315,
		},
		"files_missing_subtitles": []map[string]any{
			{
				"file_ref": "import1", "path": "/media/S01E01.mkv", "media_kind": "video",
				"season": 1, "episode": 1,
				"audio_tracks":    []map[string]any{{"codec": "eac3", "language": "eng", "channels": 6}},
				"subtitle_tracks": []map[string]any{{"language": "eng", "forced": true}},
			},
		},
	})

	if parsed.MediaType != "series" || parsed.Title != "The Bear" {
		t.Fatalf("媒体信息不符: %+v", parsed)
	}
	if parsed.Context.SiteAccountID != "acct1" || parsed.Context.TMDBID != 136315 || parsed.Context.Year != 2022 {
		t.Fatalf("检索线索不符: %+v", parsed.Context)
	}
	if len(parsed.Files) != 1 {
		t.Fatalf("应解析出一个待办文件: %+v", parsed.Files)
	}
	file := parsed.Files[0]
	if file.FileRef != "import1" || file.Season != 1 || file.Episode != 1 {
		t.Fatalf("文件字段不符: %+v", file)
	}
	if file.Tracks == nil || len(file.Tracks.Audios) != 1 || file.Tracks.Audios[0].Channels != 6 {
		t.Fatalf("音轨不符: %+v", file.Tracks)
	}
	if len(file.Tracks.Subtitles) != 1 || !file.Tracks.Subtitles[0].Forced {
		t.Fatalf("字幕轨不符: %+v", file.Tracks)
	}
}

// 进程内投递保持宿主构造时的类型，过一趟 RPC 的 JSON 就全变成 float64 和 []any。
// 只认一种的话，插件会在另一种投递方式下静默拿到 0 和空列表。
func TestParseLibraryRefreshPendingAcceptsJSONShapes(t *testing.T) {
	parsed := ParseLibraryRefreshPending(map[string]any{
		"subtitle_context": map[string]any{"year": float64(2022), "tmdb_id": float64(136315)},
		"files_missing_subtitles": []any{
			map[string]any{
				"file_ref": "import1", "season": float64(2), "episode": float64(7),
				"subtitle_tracks": []any{map[string]any{"language": "jpn"}},
			},
		},
	})

	if parsed.Context.Year != 2022 || parsed.Context.TMDBID != 136315 {
		t.Fatalf("float64 形状的数字应认得: %+v", parsed.Context)
	}
	if len(parsed.Files) != 1 || parsed.Files[0].Season != 2 || parsed.Files[0].Episode != 7 {
		t.Fatalf("float64 形状的季集应认得: %+v", parsed.Files)
	}
	if parsed.Files[0].Tracks == nil || len(parsed.Files[0].Tracks.Subtitles) != 1 {
		t.Fatalf("[]any 形状的轨道列表应认得: %+v", parsed.Files[0].Tracks)
	}
}

// 没有句柄就落不了盘，这种条目对插件毫无用处，留着只会让它拿空 ref 去调宿主。
func TestParseLibraryRefreshPendingDropsEntriesWithoutFileRef(t *testing.T) {
	parsed := ParseLibraryRefreshPending(map[string]any{
		"files_missing_subtitles": []map[string]any{
			{"path": "/media/S01E01.mkv"},
			{"file_ref": "  ", "path": "/media/S01E02.mkv"},
			{"file_ref": "import3"},
		},
	})
	if len(parsed.Files) != 1 || parsed.Files[0].FileRef != "import3" {
		t.Fatalf("只该留下带句柄的条目: %+v", parsed.Files)
	}
}

// 没探到轨道和"这片一条轨道都没有"该分得开：前者插件无从判断，后者是确凿事实。
func TestParseLibraryRefreshPendingLeavesTracksNilWhenAbsent(t *testing.T) {
	parsed := ParseLibraryRefreshPending(map[string]any{
		"files_missing_subtitles": []map[string]any{{"file_ref": "import1"}},
	})
	if len(parsed.Files) != 1 || parsed.Files[0].Tracks != nil {
		t.Fatalf("没有轨道字段时 Tracks 应为 nil: %+v", parsed.Files)
	}

	empty := ParseLibraryRefreshPending(map[string]any{
		"files_missing_subtitles": []map[string]any{
			{"file_ref": "import1", "subtitle_tracks": []map[string]any{}},
		},
	})
	if empty.Files[0].Tracks != nil {
		t.Fatalf("空轨道列表目前也归为未探到: %+v", empty.Files[0].Tracks)
	}
}
