package pluginsdk

import "context"

type Schedule struct {
	TaskType        string `json:"task_type"`
	OwnerType       string `json:"owner_type,omitempty"`
	OwnerID         string `json:"owner_id,omitempty"`
	IntervalSeconds int    `json:"interval_seconds"`
	Enabled         bool   `json:"enabled"`
	NextRunAt       string `json:"next_run_at,omitempty"`
	LastRunAt       string `json:"last_run_at,omitempty"`
	LastStatus      string `json:"last_status,omitempty"`
}

type ScheduleWrite struct {
	TaskType        string `json:"task_type"`
	IntervalSeconds int    `json:"interval_seconds"`
	Enabled         bool   `json:"enabled"`
}

type Schedules interface {
	ListSchedules(context.Context) ([]Schedule, error)
	GetSchedule(context.Context, string) (Schedule, error)
	SetSchedule(context.Context, ScheduleWrite) (HostWriteResult, error)
}
