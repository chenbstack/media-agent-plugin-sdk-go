package pluginsdk

import "context"

// LogLevel 是插件日志级别。
type LogLevel string

const (
	LogLevelDebug LogLevel = "debug"
	LogLevelInfo  LogLevel = "info"
	LogLevelWarn  LogLevel = "warn"
	LogLevelError LogLevel = "error"
)

// Logger 是宿主注入给插件的日志接口。
// attrs 使用 slog 风格的 key/value 参数；宿主会自动追加 plugin_id 和 instance_id。
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
