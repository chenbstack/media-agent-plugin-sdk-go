// Package torrentinfo 解析 .torrent 文件（bencode）并提取整理与下载流程需要的
// 元信息：v1 info hash、种子名、文件清单与总大小。宿主用它在提交下载器前完成
// 查重与选集校验，下载器插件用它在缺少 InfoHash 时自行解析 TorrentData。
package torrentinfo

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// File 是种子内的一个文件。Path 是相对种子根目录（Info.Name）的路径，
// 多级目录用 "/" 连接；单文件种子的 Path 等于 Info.Name。
type File struct {
	Path      string
	SizeBytes int64
}

// Info 是种子文件的解析结果。
type Info struct {
	// InfoHash 是 v1 info hash（sha1，十六进制小写）。
	InfoHash string
	// Name 是种子根目录名（单文件种子为文件名）。
	Name string
	// Files 按种子内顺序排列；单文件种子也会有一项。
	Files []File
	// TotalSizeBytes 是所有文件大小之和。
	TotalSizeBytes int64
	// Private 表示 info 字典声明了 private=1（PT 种子）。
	Private bool
}

var (
	errNotTorrent    = errors.New("不是有效的种子文件")
	errUnexpectedEnd = errors.New("种子文件不完整")
)

// Parse 解析 .torrent 文件内容。内容不是 bencode 字典、缺少 info 字典或是
// 仅含 v2 结构（无 v1 文件清单）时返回错误。
func Parse(data []byte) (Info, error) {
	d := &decoder{data: data}
	if len(data) == 0 || data[0] != 'd' {
		return Info{}, errNotTorrent
	}
	d.pos++
	var infoSpan []byte
	var infoValue map[string]any
	for {
		if d.pos >= len(d.data) {
			return Info{}, errUnexpectedEnd
		}
		if d.data[d.pos] == 'e' {
			d.pos++
			break
		}
		key, err := d.readString()
		if err != nil {
			return Info{}, err
		}
		if key == "info" {
			start := d.pos
			value, err := d.parseValue()
			if err != nil {
				return Info{}, err
			}
			dict, ok := value.(map[string]any)
			if !ok {
				return Info{}, errNotTorrent
			}
			infoSpan = d.data[start:d.pos]
			infoValue = dict
			continue
		}
		if _, err := d.parseValue(); err != nil {
			return Info{}, err
		}
	}
	if infoSpan == nil {
		return Info{}, errors.New("种子缺少 info 字典")
	}
	sum := sha1.Sum(infoSpan)
	out := Info{InfoHash: hex.EncodeToString(sum[:])}
	out.Name = dictString(infoValue, "name.utf-8", "name")
	if private, ok := infoValue["private"].(int64); ok && private == 1 {
		out.Private = true
	}
	if rawFiles, ok := infoValue["files"].([]any); ok {
		for _, rawFile := range rawFiles {
			entry, ok := rawFile.(map[string]any)
			if !ok {
				return Info{}, errNotTorrent
			}
			length, _ := entry["length"].(int64)
			parts := dictStringList(entry, "path.utf-8", "path")
			if len(parts) == 0 {
				continue
			}
			file := File{Path: strings.Join(parts, "/"), SizeBytes: length}
			out.Files = append(out.Files, file)
			out.TotalSizeBytes += length
		}
	} else if length, ok := infoValue["length"].(int64); ok {
		out.Files = []File{{Path: out.Name, SizeBytes: length}}
		out.TotalSizeBytes = length
	} else {
		return Info{}, errors.New("种子缺少 v1 文件清单（可能是仅 v2 格式的种子）")
	}
	return out, nil
}

func dictString(dict map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := dict[key].(string); ok && value != "" {
			return value
		}
	}
	return ""
}

func dictStringList(dict map[string]any, keys ...string) []string {
	for _, key := range keys {
		raw, ok := dict[key].([]any)
		if !ok {
			continue
		}
		out := make([]string, 0, len(raw))
		for _, item := range raw {
			part, ok := item.(string)
			if !ok || part == "" {
				continue
			}
			out = append(out, part)
		}
		if len(out) > 0 {
			return out
		}
	}
	return nil
}

type decoder struct {
	data []byte
	pos  int
}

func (d *decoder) parseValue() (any, error) {
	if d.pos >= len(d.data) {
		return nil, errUnexpectedEnd
	}
	switch c := d.data[d.pos]; {
	case c == 'i':
		return d.readInt()
	case c == 'l':
		d.pos++
		var out []any
		for {
			if d.pos >= len(d.data) {
				return nil, errUnexpectedEnd
			}
			if d.data[d.pos] == 'e' {
				d.pos++
				return out, nil
			}
			item, err := d.parseValue()
			if err != nil {
				return nil, err
			}
			out = append(out, item)
		}
	case c == 'd':
		d.pos++
		out := map[string]any{}
		for {
			if d.pos >= len(d.data) {
				return nil, errUnexpectedEnd
			}
			if d.data[d.pos] == 'e' {
				d.pos++
				return out, nil
			}
			key, err := d.readString()
			if err != nil {
				return nil, err
			}
			value, err := d.parseValue()
			if err != nil {
				return nil, err
			}
			out[key] = value
		}
	case c >= '0' && c <= '9':
		return d.readString()
	default:
		return nil, fmt.Errorf("无法识别的 bencode 类型 %q", c)
	}
}

func (d *decoder) readInt() (int64, error) {
	if d.pos >= len(d.data) || d.data[d.pos] != 'i' {
		return 0, errNotTorrent
	}
	d.pos++
	start := d.pos
	for d.pos < len(d.data) && d.data[d.pos] != 'e' {
		d.pos++
	}
	if d.pos >= len(d.data) {
		return 0, errUnexpectedEnd
	}
	raw := string(d.data[start:d.pos])
	d.pos++
	var negative bool
	if strings.HasPrefix(raw, "-") {
		negative = true
		raw = raw[1:]
	}
	if raw == "" {
		return 0, errNotTorrent
	}
	var value int64
	for _, r := range raw {
		if r < '0' || r > '9' {
			return 0, errNotTorrent
		}
		value = value*10 + int64(r-'0')
	}
	if negative {
		value = -value
	}
	return value, nil
}

func (d *decoder) readString() (string, error) {
	start := d.pos
	for d.pos < len(d.data) && d.data[d.pos] != ':' {
		c := d.data[d.pos]
		if c < '0' || c > '9' {
			return "", errNotTorrent
		}
		d.pos++
	}
	if d.pos >= len(d.data) || d.pos == start {
		return "", errUnexpectedEnd
	}
	var length int
	for _, c := range d.data[start:d.pos] {
		length = length*10 + int(c-'0')
		if length > len(d.data) {
			return "", errNotTorrent
		}
	}
	d.pos++
	if d.pos+length > len(d.data) {
		return "", errUnexpectedEnd
	}
	out := string(d.data[d.pos : d.pos+length])
	d.pos += length
	return out, nil
}
