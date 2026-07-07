package pluginsdk

import "context"

// DBResult is the write result returned by PluginDB.Exec.
type DBResult struct {
	RowsAffected int64 `json:"rows_affected"`
	LastInsertID int64 `json:"last_insert_id"`
}

// PluginDB exposes a plugin-scoped database surface backed by the host
// SQLite database. Implementations must only allow access to tables owned by
// the current plugin.
type PluginDB interface {
	// TableName returns the physical SQLite table name for a plugin-local
	// logical table. All physical tables use the host-wide plugin_data_ prefix.
	TableName(logicalName string) (string, error)
	Exec(ctx context.Context, statement string, args ...any) (DBResult, error)
	Query(ctx context.Context, statement string, args ...any) ([]map[string]any, error)
}
