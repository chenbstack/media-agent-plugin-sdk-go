package pluginsdk

import "context"

// DBResult is the write result returned by PluginDB writes.
type DBResult struct {
	RowsAffected int64 `json:"rows_affected"`
	LastInsertID int64 `json:"last_insert_id"`
}

// PluginDB exposes a plugin-scoped database surface backed by the host
// SQLite database.
//
// 接口只收结构化语句，不收 SQL 文本。宿主按插件声明的 DBSchema 把语句编译成 SQL：
// 表名和列名从声明里查、运算符从固定表里取、值一律走占位符，所以插件既不需要、也
// 无法提供任何标识符。物理表名同样不再暴露——它由宿主拼成
// plugin_data_<plugin>_<logical>，插件全程只用逻辑表名。
type PluginDB interface {
	Select(ctx context.Context, query Select) ([]map[string]any, error)
	Insert(ctx context.Context, query Insert) (DBResult, error)
	Update(ctx context.Context, query Update) (DBResult, error)
	Delete(ctx context.Context, query Delete) (DBResult, error)
}
