package pluginsdk

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

type codedErr struct{}

func (codedErr) Error() string { return "status=503" }
func (codedErr) Code() string  { return "download.downloader_unavailable" }

func TestErrAttrsExpandsChainAndCode(t *testing.T) {
	err := fmt.Errorf("同步传输状态: %w", fmt.Errorf("调用 session-stats: %w", codedErr{}))
	attrs := ErrAttrs(err)
	got := map[string]any{}
	for i := 0; i < len(attrs); i += 2 {
		got[attrs[i].(string)] = attrs[i+1]
	}
	chain, _ := got["error_chain"].([]string)
	if len(chain) != 3 || chain[0] != "同步传输状态" || chain[2] != "status=503" {
		t.Fatalf("error_chain = %v", chain)
	}
	if got["error_code"] != "download.downloader_unavailable" {
		t.Fatalf("error_code = %v", got["error_code"])
	}
	if ErrAttrs(nil) != nil {
		t.Fatal("nil error should yield nil attrs")
	}
	if attrs := ErrAttrs(errors.New("plain")); len(attrs) != 2 {
		t.Fatalf("plain error attrs = %v", attrs)
	}
}

func TestCompactStackKeepsOnlyPluginFrames(t *testing.T) {
	stack := CompactStack()
	if strings.Contains(stack, "runtime.") || strings.Contains(stack, "testing.") {
		t.Fatalf("stack contains non-plugin frames: %q", stack)
	}
}
