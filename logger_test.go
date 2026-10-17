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

func originOfError() error { return WithStack(errors.New("boom")) }

// SDK 自己的帧会被精简栈过滤，所以这里直接检查捕获的 pc，而不是 ErrStack 的文本。
func TestWithStackCapturesOriginOnceAndKeepsMessage(t *testing.T) {
	inner := originOfError()
	err := fmt.Errorf("outer: %w", inner)
	if got := WithStack(err); got != err {
		t.Fatalf("WithStack must not re-wrap an error that already has a stack")
	}
	var se *stackError
	if !errors.As(err, &se) || len(se.pcs) == 0 {
		t.Fatalf("expected captured pcs, got %v", se)
	}
	if err.Error() != "outer: boom" || !errors.Is(err, errors.Unwrap(inner)) {
		t.Fatalf("message/unwrap changed: %v", err)
	}
	if WithStack(nil) != nil || ErrStack(errors.New("x")) != "" {
		t.Fatalf("nil/plain errors must stay stackless")
	}
}
