package pluginsdk

import (
	"context"
	"fmt"
	"strings"
	"time"
)

const CapabilityScheduledTask = "scheduled_task.run"

type ScheduledTaskExecutorKind string

const (
	ScheduledTaskExecutorPluginHandler ScheduledTaskExecutorKind = "plugin_handler"
	ScheduledTaskExecutorHostWorkflow  ScheduledTaskExecutorKind = "host_workflow"
	ScheduledTaskWorkflowSiteCredentialsSync                       = "site_credentials.sync"
)

type ScheduledTaskOverlapPolicy string

const (
	ScheduledTaskOverlapSkip  ScheduledTaskOverlapPolicy = "skip"
	ScheduledTaskOverlapQueue ScheduledTaskOverlapPolicy = "queue"
)

// ScheduledTaskDefinition declares one host-managed periodic task. The host
// owns persistence, timing, retries and lifecycle reconciliation; plugins must
// not start their own timers.
type ScheduledTaskDefinition struct {
	ID                     string                     `yaml:"id" json:"id"`
	Name                   string                     `yaml:"name" json:"name"`
	Description            string                     `yaml:"description,omitempty" json:"description,omitempty"`
	DefaultEnabled         bool                       `yaml:"default_enabled,omitempty" json:"default_enabled,omitempty"`
	DefaultIntervalSeconds int                        `yaml:"default_interval_seconds" json:"default_interval_seconds"`
	MinIntervalSeconds     int                        `yaml:"min_interval_seconds,omitempty" json:"min_interval_seconds,omitempty"`
	MaxIntervalSeconds     int                        `yaml:"max_interval_seconds,omitempty" json:"max_interval_seconds,omitempty"`
	TimeoutSeconds         int                        `yaml:"timeout_seconds,omitempty" json:"timeout_seconds,omitempty"`
	MaxAttempts            int                        `yaml:"max_attempts,omitempty" json:"max_attempts,omitempty"`
	OverlapPolicy          ScheduledTaskOverlapPolicy `yaml:"overlap_policy,omitempty" json:"overlap_policy,omitempty"`
	Executor               ScheduledTaskExecutor      `yaml:"executor" json:"executor"`
	Permissions            *Permissions               `yaml:"permissions,omitempty" json:"permissions,omitempty"`
	RequiredEntitlements   []string                   `yaml:"required_entitlements,omitempty" json:"required_entitlements,omitempty"`
}

type ScheduledTaskExecutor struct {
	Kind     ScheduledTaskExecutorKind `yaml:"kind" json:"kind"`
	Workflow string                    `yaml:"workflow,omitempty" json:"workflow,omitempty"`
}

func (d ScheduledTaskDefinition) Validate(pluginID string, parent Permissions, declaredEntitlements map[string]struct{}) error {
	if !manifestIdentifier.MatchString(d.ID) || strings.TrimSpace(d.Name) == "" {
		return fmt.Errorf("插件 %s: scheduled task 必须包含合法 id 和非空 name", pluginID)
	}
	if d.DefaultIntervalSeconds <= 0 {
		return fmt.Errorf("插件 %s scheduled task %s: default_interval_seconds 必须为正数", pluginID, d.ID)
	}
	if d.MinIntervalSeconds < 0 || d.MaxIntervalSeconds < 0 {
		return fmt.Errorf("插件 %s scheduled task %s: interval 边界不能为负数", pluginID, d.ID)
	}
	if d.MinIntervalSeconds > 0 && d.DefaultIntervalSeconds < d.MinIntervalSeconds {
		return fmt.Errorf("插件 %s scheduled task %s: 默认周期小于最小周期", pluginID, d.ID)
	}
	if d.MaxIntervalSeconds > 0 && d.DefaultIntervalSeconds > d.MaxIntervalSeconds {
		return fmt.Errorf("插件 %s scheduled task %s: 默认周期大于最大周期", pluginID, d.ID)
	}
	if d.MinIntervalSeconds > 0 && d.MaxIntervalSeconds > 0 && d.MinIntervalSeconds > d.MaxIntervalSeconds {
		return fmt.Errorf("插件 %s scheduled task %s: 最小周期大于最大周期", pluginID, d.ID)
	}
	if d.TimeoutSeconds < 0 || d.MaxAttempts < 0 {
		return fmt.Errorf("插件 %s scheduled task %s: timeout_seconds/max_attempts 不能为负数", pluginID, d.ID)
	}
	switch d.OverlapPolicy {
	case "", ScheduledTaskOverlapSkip, ScheduledTaskOverlapQueue:
	default:
		return fmt.Errorf("插件 %s scheduled task %s: overlap_policy 只支持 skip 或 queue", pluginID, d.ID)
	}
	switch d.Executor.Kind {
	case ScheduledTaskExecutorPluginHandler:
		if strings.TrimSpace(d.Executor.Workflow) != "" {
			return fmt.Errorf("插件 %s scheduled task %s: plugin_handler 不能声明 workflow", pluginID, d.ID)
		}
	case ScheduledTaskExecutorHostWorkflow:
		if !manifestIdentifier.MatchString(d.Executor.Workflow) {
			return fmt.Errorf("插件 %s scheduled task %s: host_workflow 必须声明合法 workflow", pluginID, d.ID)
		}
	default:
		return fmt.Errorf("插件 %s scheduled task %s: executor.kind 只支持 plugin_handler 或 host_workflow", pluginID, d.ID)
	}
	if d.Permissions != nil {
		if err := validatePermissionSubset(parent, *d.Permissions); err != nil {
			return fmt.Errorf("插件 %s scheduled task %s 权限声明无效: %w", pluginID, d.ID, err)
		}
	}
	if _, err := validateEntitlements(pluginID, "scheduled task "+d.ID, d.RequiredEntitlements, declaredEntitlements); err != nil {
		return err
	}
	return nil
}

type ScheduledTaskTrigger string

const (
	ScheduledTaskTriggerSchedule ScheduledTaskTrigger = "schedule"
	ScheduledTaskTriggerManual   ScheduledTaskTrigger = "manual"
)

type ScheduledTaskRequest struct {
	TaskID      string               `json:"task_id"`
	Trigger     ScheduledTaskTrigger `json:"trigger"`
	JobID       string               `json:"job_id,omitempty"`
	ScheduledAt time.Time            `json:"scheduled_at,omitempty"`
	Attempt     int                  `json:"attempt,omitempty"`
	Input       map[string]any       `json:"input,omitempty"`
}

type ScheduledTaskResult struct {
	Message string         `json:"message,omitempty"`
	Data    map[string]any `json:"data,omitempty"`
}

type ScheduledTaskHandler interface {
	RunScheduledTask(context.Context, ScheduledTaskRequest) (ScheduledTaskResult, error)
}
