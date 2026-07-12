package pluginsdk

import (
	"context"
	"encoding/json"
)

// ConnectionWrite is a host-neutral connection configuration. Config contains
// ordinary values; Secrets contains plaintext values that the host must move
// into its encrypted secret store before persisting the connection.
type ConnectionWrite struct {
	TargetID       string            `json:"target_id,omitempty"`
	IdempotencyKey string            `json:"idempotency_key"`
	Section        string            `json:"section"` // sites / downloaders / media_servers / notifiers / metadata_sources
	Kind           string            `json:"kind"`
	Name           string            `json:"name"`
	Enabled        bool              `json:"enabled"`
	IsDefault      bool              `json:"is_default,omitempty"`
	Config         map[string]any    `json:"config,omitempty"`
	Secrets        map[string]string `json:"secrets,omitempty"`
}

type StorageWrite struct {
	TargetID       string            `json:"target_id,omitempty"`
	IdempotencyKey string            `json:"idempotency_key"`
	Kind           string            `json:"kind"`
	Name           string            `json:"name"`
	RootDirectory  string            `json:"root_directory"`
	Config         map[string]any    `json:"config,omitempty"`
	Secrets        map[string]string `json:"secrets,omitempty"`
}

type DirectoryMappingWrite struct {
	TargetID        string            `json:"target_id,omitempty"`
	IdempotencyKey  string            `json:"idempotency_key"`
	Name            string            `json:"name"`
	Priority        int               `json:"priority,omitempty"`
	MediaType       string            `json:"media_type"`
	MediaCategory   string            `json:"media_category"`
	SourceStorageID string            `json:"source_storage_id"`
	SourceDirectory string            `json:"source_directory"`
	TargetStorageID string            `json:"target_storage_id"`
	TargetDirectory string            `json:"target_directory"`
	TransferType    string            `json:"transfer_type"`
	OverwriteMode   string            `json:"overwrite_mode"`
	SmartRename     bool              `json:"smart_rename"`
	Scraping        bool              `json:"scraping"`
	Notify          bool              `json:"notify"`
	RenameTemplates map[string]string `json:"rename_templates,omitempty"`
}

type SettingWrite struct {
	Key   string          `json:"key"`
	Value json.RawMessage `json:"value"`
}

type ScheduleWrite struct {
	TaskType        string `json:"task_type"`
	IntervalSeconds int    `json:"interval_seconds"`
	Enabled         bool   `json:"enabled"`
}

// Configuration exposes idempotent, schema-aware host configuration writes.
// It is intentionally narrower than database access and is guarded by the
// host.configuration.write permission.
type Configuration interface {
	UpsertConnection(context.Context, ConnectionWrite) (HostWriteResult, error)
	UpsertStorage(context.Context, StorageWrite) (HostWriteResult, error)
	UpsertDirectoryMapping(context.Context, DirectoryMappingWrite) (HostWriteResult, error)
	SetSetting(context.Context, SettingWrite) (HostWriteResult, error)
	SetSchedule(context.Context, ScheduleWrite) (HostWriteResult, error)
}
