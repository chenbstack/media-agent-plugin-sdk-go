package pluginsdk

import (
	"context"
	"errors"
	"runtime"
	"strconv"
	"strings"
)

// LogLevel 是插件日志级别。
type LogLevel string

const (
	LogLevelDebug LogLevel = "debug"
	LogLevelInfo  LogLevel = "info"
	LogLevelWarn  LogLevel = "warn"
	LogLevelError LogLevel = "error"
)

// Logger 是宿主注入给插件的日志接口。
// attrs 使用 slog 风格的 key/value 参数；宿主会自动追加来源信息。Error 级别日志由 SDK
// 自动附上插件侧的精简调用栈（stack）；记录错误请用 ErrAttrs 展开错误链。
type Logger interface {
	Log(ctx context.Context, level LogLevel, message string, attrs ...any)
	Debug(ctx context.Context, message string, attrs ...any)
	Info(ctx context.Context, message string, attrs ...any)
	Warn(ctx context.Context, message string, attrs ...any)
	Error(ctx context.Context, message string, attrs ...any)
}

type noopLogger struct{}

// NoopLogger 返回忽略所有日志的 Logger。
func NoopLogger() Logger { return noopLogger{} }

func (noopLogger) Log(context.Context, LogLevel, string, ...any) {}
func (noopLogger) Debug(context.Context, string, ...any)         {}
func (noopLogger) Info(context.Context, string, ...any)          {}
func (noopLogger) Warn(context.Context, string, ...any)          {}
func (noopLogger) Error(context.Context, string, ...any)         {}

const maxErrorChain = 8

// ErrAttrs 把 err 展开成日志属性：error、error_chain（有多层 %w 包装时）和 error_code
// （err 实现了 Code() string 时）。用法：logger.Warn(ctx, "同步失败", pluginsdk.ErrAttrs(err)...)。
// err 为 nil 时返回 nil。
func ErrAttrs(err error) []any {
	if err == nil {
		return nil
	}
	attrs := []any{"error", err.Error()}
	var chain []string
	for current := err; current != nil && len(chain) < maxErrorChain; {
		next := errors.Unwrap(current)
		text := current.Error()
		if next != nil {
			if inner := next.Error(); strings.HasSuffix(text, inner) {
				text = strings.TrimSuffix(strings.TrimSuffix(text, inner), ": ")
			}
		}
		if text = strings.TrimSpace(text); text != "" {
			chain = append(chain, text)
		}
		current = next
	}
	if len(chain) > 1 {
		attrs = append(attrs, "error_chain", chain)
	}
	if stack := ErrStack(err); stack != "" {
		attrs = append(attrs, "stack", stack)
	}
	var coded interface{ Code() string }
	if errors.As(err, &coded) {
		if code := strings.TrimSpace(coded.Code()); code != "" {
			attrs = append(attrs, "error_code", code)
		}
	}
	return attrs
}

const maxStackFrames = 16

// CompactStack 采集调用点的精简调用栈，供 Error 级别日志使用：只留插件自己的代码帧，
// 去掉标准库、runtime、SDK 与 go-plugin/net/rpc 的帧。每帧一行：`函数名 (文件:行)`。
func CompactStack() string {
	pcs := make([]uintptr, 48)
	n := runtime.Callers(2, pcs)
	return compactFrames(pcs[:n])
}

func compactFrames(pcs []uintptr) string {
	frames := runtime.CallersFrames(pcs)
	var lines []string
	for {
		frame, more := frames.Next()
		if isPluginFrame(frame.Function) {
			lines = append(lines, frame.Function+" ("+shortFile(frame.File)+":"+strconv.Itoa(frame.Line)+")")
			if len(lines) >= maxStackFrames {
				break
			}
		}
		if !more {
			break
		}
	}
	return strings.Join(lines, "\n")
}

func isPluginFrame(function string) bool {
	if function == "" {
		return false
	}
	first, _, hasSlash := strings.Cut(function, "/")
	if !hasSlash {
		return strings.HasPrefix(function, "main.") // 无斜杠的包路径：只有 main 是插件自己的
	}
	if !strings.Contains(first, ".") { // 标准库（net/rpc、encoding/json）首段没有点
		return false
	}
	for _, skip := range []string{
		"github.com/chenbstack/media-agent-plugin-sdk-go",
		"github.com/hashicorp/",
		"google.golang.org/",
	} {
		if strings.HasPrefix(function, skip) {
			return false
		}
	}
	return true
}

func shortFile(file string) string {
	for _, marker := range []string{"/pack/", "/internal/"} {
		if i := strings.LastIndex(file, marker); i >= 0 {
			return file[i+1:]
		}
	}
	if i := strings.LastIndex(file, "/"); i >= 0 {
		return file[i+1:]
	}
	return file
}

type stackError struct {
	err error
	pcs []uintptr
}

func (e *stackError) Error() string { return e.err.Error() }
func (e *stackError) Unwrap() error { return e.err }

// WithStack 在错误产生处捕获调用栈：ErrAttrs 之后会优先用它作为 stack，指向错误从哪来，
// 而不是日志在哪打。err 为 nil 或错误链里已有栈时原样返回，所以可以在每一层放心调用，
// 最内层（最先调用）的那份栈会保留。Error() 与 Unwrap 行为不变。
func WithStack(err error) error {
	var existing *stackError
	if err == nil || errors.As(err, &existing) {
		return err
	}
	pcs := make([]uintptr, 32)
	n := runtime.Callers(2, pcs)
	return &stackError{err: err, pcs: pcs[:n]}
}

// ErrStack 返回错误链里 WithStack 捕获的精简调用栈，没有则返回空串。
func ErrStack(err error) string {
	var se *stackError
	if errors.As(err, &se) {
		return compactFrames(se.pcs)
	}
	return ""
}
