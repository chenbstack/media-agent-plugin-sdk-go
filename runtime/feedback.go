// Package runtime contains host-provided, cross-cutting plugin capabilities.
package runtime

import "context"

type LogLevel string

const (
	LogDebug LogLevel = "debug"
	LogInfo  LogLevel = "info"
	LogWarn  LogLevel = "warn"
	LogError LogLevel = "error"
)

type ToastLevel string

const (
	ToastInfo    ToastLevel = "info"
	ToastSuccess ToastLevel = "success"
	ToastWarning ToastLevel = "warning"
	ToastError   ToastLevel = "error"
)

type Feedback interface {
	Log(ctx context.Context, level LogLevel, message string, attrs ...any)
	Debug(ctx context.Context, message string, attrs ...any)
	Info(ctx context.Context, message string, attrs ...any)
	Warn(ctx context.Context, message string, attrs ...any)
	Error(ctx context.Context, message string, attrs ...any)
	Toast(ctx context.Context, input ToastInput) error
	Notify(ctx context.Context, input NotificationInput) error
}

type ToastInput struct {
	Level      ToastLevel `json:"level"`
	Title      string     `json:"title,omitempty"`
	Message    string     `json:"message"`
	DurationMS int        `json:"duration_ms,omitempty"`
}

type NotificationInput struct {
	Level    ToastLevel     `json:"level,omitempty"`
	Title    string         `json:"title"`
	Message  string         `json:"message"`
	Category string         `json:"category,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}
