package torrentinfo

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"testing"
)

func bstr(value string) string {
	return fmt.Sprintf("%d:%s", len(value), value)
}

func TestParseSingleFileTorrent(t *testing.T) {
	info := "d" + bstr("length") + "i1048576e" + bstr("name") + bstr("Sample.S01E01.mkv") + bstr("piece length") + "i16384e" + bstr("pieces") + bstr("aaaaaaaaaaaaaaaaaaaa") + bstr("private") + "i1e" + "e"
	raw := "d" + bstr("announce") + bstr("https://tracker.example/announce") + bstr("info") + info + "e"

	parsed, err := Parse([]byte(raw))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	sum := sha1.Sum([]byte(info))
	if parsed.InfoHash != hex.EncodeToString(sum[:]) {
		t.Fatalf("InfoHash = %s, want %s", parsed.InfoHash, hex.EncodeToString(sum[:]))
	}
	if parsed.Name != "Sample.S01E01.mkv" {
		t.Fatalf("Name = %q", parsed.Name)
	}
	if !parsed.Private {
		t.Fatal("Private 应为 true")
	}
	if len(parsed.Files) != 1 || parsed.Files[0].Path != "Sample.S01E01.mkv" || parsed.Files[0].SizeBytes != 1048576 {
		t.Fatalf("Files = %+v", parsed.Files)
	}
	if parsed.TotalSizeBytes != 1048576 {
		t.Fatalf("TotalSizeBytes = %d", parsed.TotalSizeBytes)
	}
}

func TestParseMultiFileTorrent(t *testing.T) {
	file1 := "d" + bstr("length") + "i100e" + bstr("path") + "l" + bstr("Season 01") + bstr("Sample.S01E01.mkv") + "e" + "e"
	file2 := "d" + bstr("length") + "i200e" + bstr("path") + "l" + bstr("Season 01") + bstr("Sample.S01E02.mkv") + "e" + "e"
	info := "d" + bstr("files") + "l" + file1 + file2 + "e" + bstr("name") + bstr("Sample Show") + bstr("piece length") + "i16384e" + bstr("pieces") + bstr("aaaaaaaaaaaaaaaaaaaa") + "e"
	raw := "d" + bstr("info") + info + "e"

	parsed, err := Parse([]byte(raw))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if parsed.Name != "Sample Show" {
		t.Fatalf("Name = %q", parsed.Name)
	}
	if len(parsed.Files) != 2 {
		t.Fatalf("Files = %+v", parsed.Files)
	}
	if parsed.Files[0].Path != "Season 01/Sample.S01E01.mkv" || parsed.Files[1].Path != "Season 01/Sample.S01E02.mkv" {
		t.Fatalf("文件路径应相对种子根目录: %+v", parsed.Files)
	}
	if parsed.TotalSizeBytes != 300 {
		t.Fatalf("TotalSizeBytes = %d", parsed.TotalSizeBytes)
	}
	if parsed.Private {
		t.Fatal("未声明 private 时应为 false")
	}
}

func TestParsePrefersUTF8Keys(t *testing.T) {
	file := "d" + bstr("length") + "i100e" + bstr("path") + "l" + bstr("bad") + "e" + bstr("path.utf-8") + "l" + bstr("好剧") + bstr("第一集.mkv") + "e" + "e"
	info := "d" + bstr("files") + "l" + file + "e" + bstr("name") + bstr("bad-name") + bstr("name.utf-8") + bstr("好剧") + "e"
	raw := "d" + bstr("info") + info + "e"

	parsed, err := Parse([]byte(raw))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if parsed.Name != "好剧" {
		t.Fatalf("Name = %q, 应优先 name.utf-8", parsed.Name)
	}
	if parsed.Files[0].Path != "好剧/第一集.mkv" {
		t.Fatalf("Path = %q, 应优先 path.utf-8", parsed.Files[0].Path)
	}
}

func TestParseRejectsInvalidContent(t *testing.T) {
	for name, raw := range map[string]string{
		"空内容":      "",
		"HTML 页面":  "<html><body>请先登录</body></html>",
		"磁力链接":     "magnet:?xt=urn:btih:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"缺少 info":  "d" + bstr("announce") + bstr("https://tracker.example") + "e",
		"截断的种子":    "d" + bstr("info") + "d" + bstr("length") + "i10",
		"仅 v2 的种子": "d" + bstr("info") + "d" + bstr("file tree") + "d" + "e" + bstr("meta version") + "i2e" + bstr("name") + bstr("x") + "e" + "e",
	} {
		if _, err := Parse([]byte(raw)); err == nil {
			t.Fatalf("%s: 应返回错误", name)
		}
	}
}

func TestParseHandlesLargeStringLengthSafely(t *testing.T) {
	if _, err := Parse([]byte("d999999999999999999:x" + "e")); err == nil {
		t.Fatal("超长字符串长度应报错而不是越界")
	}
}

func TestParseRejectsNonDigitLength(t *testing.T) {
	if _, err := Parse([]byte("dxe")); err == nil {
		t.Fatal("非法 key 应报错")
	}
}
