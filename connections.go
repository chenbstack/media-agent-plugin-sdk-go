package pluginsdk

import "context"

// Connection is a host-neutral, secret-masked connection configuration.
type Connection struct {
	ID        string         `json:"id"`
	Section   string         `json:"section"`
	Kind      string         `json:"kind"`
	Name      string         `json:"name"`
	Enabled   bool           `json:"enabled"`
	IsDefault bool           `json:"is_default,omitempty"`
	Config    map[string]any `json:"config,omitempty"`
}

// ConnectionWrite contains ordinary config separately from plaintext secrets.
// The host validates both against the target provider schema and moves secrets
// into encrypted storage before persisting the connection.
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

// Connections exposes permission-scoped connection reads and idempotent writes.
type Connections interface {
	ListConnections(context.Context, string) ([]Connection, error)
	GetConnection(context.Context, string, string) (Connection, error)
	UpsertConnection(context.Context, ConnectionWrite) (HostWriteResult, error)
}

// ConnectionCredentials exposes one explicitly named secret from an existing
// host connection. The host validates that the requested field is declared as
// secret by the connection provider and audits every reveal. Plugins only
// receive this capability when connections.credentials.read is granted.
type ConnectionCredentials interface {
	RevealConnectionCredential(ctx context.Context, section, id, field, reason string) (string, error)
}
