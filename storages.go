package pluginsdk

import "context"

type Storage struct {
	ID            string         `json:"id"`
	Kind          string         `json:"kind"`
	Name          string         `json:"name"`
	RootDirectory string         `json:"root_directory"`
	Status        string         `json:"status,omitempty"`
	Config        map[string]any `json:"config,omitempty"`
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

type DirectoryMapping struct {
	ID              string            `json:"id"`
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

// Storages owns both storage configurations and the directory mappings that
// connect source and target storages.
type Storages interface {
	ListStorages(context.Context) ([]Storage, error)
	GetStorage(context.Context, string) (Storage, error)
	UpsertStorage(context.Context, StorageWrite) (HostWriteResult, error)
	ListDirectoryMappings(context.Context) ([]DirectoryMapping, error)
	GetDirectoryMapping(context.Context, string) (DirectoryMapping, error)
	UpsertDirectoryMapping(context.Context, DirectoryMappingWrite) (HostWriteResult, error)
}
