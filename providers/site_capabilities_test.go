package providers

import (
	"context"
	"strings"
	"testing"
)

type bareSite struct{}

func (bareSite) Kind() string                                 { return "bare" }
func (bareSite) TestConnection(context.Context) error         { return nil }
func (bareSite) Profile(context.Context) (SiteProfile, error) { return SiteProfile{}, nil }
func (bareSite) Search(context.Context, TorrentSearchRequest) ([]TorrentResult, error) {
	return nil, nil
}

type declaredSite struct {
	bareSite
	caps SiteCapabilities
}

func (s declaredSite) SiteCapabilities() SiteCapabilities { return s.caps }

// 只实现 SiteProvider 四个基础方法的 Provider 一律视为没有可选能力。
func TestCapabilitiesOfSiteDefaultsToNone(t *testing.T) {
	if caps := CapabilitiesOfSite(bareSite{}); caps != (SiteCapabilities{}) {
		t.Fatalf("capabilities = %+v, want 全空", caps)
	}
}

func TestCapabilitiesOfSiteUsesDeclaration(t *testing.T) {
	want := SiteCapabilities{TorrentFetch: true, IMDBSearch: true}
	if caps := CapabilitiesOfSite(declaredSite{caps: want}); caps != want {
		t.Fatalf("capabilities = %+v, want %+v", caps, want)
	}
}

func TestUnsupportedCapabilityErrorCarriesName(t *testing.T) {
	err := UnsupportedCapabilityError(CapabilitySiteSubtitlesRead)
	if !strings.Contains(err.Error(), CapabilitySiteSubtitlesRead) {
		t.Fatalf("错误信息未带能力名: %v", err)
	}
}

func TestDiagnosticInputNormalizeAndValidate(t *testing.T) {
	input := DiagnosticInput{Keyword: "  沙丘  ", IMDBID: " tt1160419 ", MediaType: " movie "}
	if err := input.NormalizeAndValidate(); err != nil {
		t.Fatalf("NormalizeAndValidate: %v", err)
	}
	if input.Keyword != "沙丘" || input.IMDBID != "tt1160419" || input.MediaType != "movie" {
		t.Fatalf("未清洗: %+v", input)
	}

	for name, bad := range map[string]DiagnosticInput{
		"关键词过长":   {Keyword: strings.Repeat("字", 201)},
		"IMDb 格式": {IMDBID: "1160419"},
		"媒体类型":    {MediaType: "anime"},
	} {
		bad := bad
		t.Run(name, func(t *testing.T) {
			if err := bad.NormalizeAndValidate(); err == nil {
				t.Fatalf("应校验失败: %+v", bad)
			}
		})
	}
}
