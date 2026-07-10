package runtime

import (
	"context"
	"time"
)

type RunState string

const (
	RunPending   RunState = "pending"
	RunRunning   RunState = "running"
	RunCompleted RunState = "completed"
	RunPartial   RunState = "partial"
	RunFailed    RunState = "failed"
	RunCanceled  RunState = "canceled"
)

type TaskState string

const (
	TaskPending   TaskState = "pending"
	TaskRunning   TaskState = "running"
	TaskCompleted TaskState = "completed"
	TaskPartial   TaskState = "partial"
	TaskFailed    TaskState = "failed"
	TaskCanceled  TaskState = "canceled"
)

type ProgressStart struct {
	Title   string         `json:"title"`
	Message string         `json:"message,omitempty"`
	Tasks   []ProgressTask `json:"tasks,omitempty"`
}

type ProgressTask struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Position int    `json:"position,omitempty"`
}

type ProgressUpdate struct {
	State   TaskState `json:"state"`
	Current int       `json:"current,omitempty"`
	Total   int       `json:"total,omitempty"`
	Message string    `json:"message,omitempty"`
}

type ProgressTaskState struct {
	ID      string    `json:"id"`
	Name    string    `json:"name"`
	State   TaskState `json:"state"`
	Current int       `json:"current,omitempty"`
	Total   int       `json:"total,omitempty"`
	Message string    `json:"message,omitempty"`
}

type ProgressSnapshot struct {
	RunID      string              `json:"run_id"`
	State      RunState            `json:"state"`
	Title      string              `json:"title,omitempty"`
	Message    string              `json:"message,omitempty"`
	Tasks      []ProgressTaskState `json:"tasks,omitempty"`
	StartedAt  time.Time           `json:"started_at,omitempty"`
	UpdatedAt  time.Time           `json:"updated_at,omitempty"`
	FinishedAt *time.Time          `json:"finished_at,omitempty"`
}

type Progress interface {
	Start(ctx context.Context, input ProgressStart) (ProgressRun, error)
	Current(ctx context.Context) (ProgressSnapshot, error)
}

type ProgressRun interface {
	ID() string
	Update(ctx context.Context, update ProgressUpdate) error
	Task(ctx context.Context, taskID string) (TaskProgress, error)
	Finish(ctx context.Context, state RunState, message string) error
}

type TaskProgress interface {
	Update(ctx context.Context, update ProgressUpdate) error
	Complete(ctx context.Context, message string) error
	Fail(ctx context.Context, message string) error
}
