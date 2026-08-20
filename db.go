package pluginsdk

import (
	"context"
	"errors"
	"fmt"
)

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

	// Batch 在一个事务里按序执行多条写语句，返回与 statements 等长、一一对应的
	// 结果。任一条失败则整批回滚，不留半份数据，返回的 error 会指明是第几条。
	//
	// 它存在的理由是通信成本：每次回调宿主都要过一趟 RPC，在循环里逐条发送时这笔
	// 开销会乘以行数。语句本身的编译与校验和单条路径完全一致。
	Batch(ctx context.Context, statements []Statement) ([]DBResult, error)
}

// DBWriter 是 PluginDB 的三个写方法。单独拎出来只为 ApplyStatements 能在没有 Batch
// 的实现上工作。
type DBWriter interface {
	Insert(ctx context.Context, query Insert) (DBResult, error)
	Update(ctx context.Context, query Update) (DBResult, error)
	Delete(ctx context.Context, query Delete) (DBResult, error)
}

// ApplyStatements 按序把一批语句分派给逐条写方法，返回与之一一对应的结果。
//
// 它**不提供原子性**：真正的 Batch 由宿主在一个事务里执行。这里是给内存实现和测试
// 替身用的，省得每家各写一遍分派。
func ApplyStatements(ctx context.Context, writer DBWriter, statements []Statement) ([]DBResult, error) {
	results := make([]DBResult, 0, len(statements))
	for i, statement := range statements {
		var (
			result DBResult
			err    error
		)
		switch {
		case statement.Insert != nil:
			result, err = writer.Insert(ctx, *statement.Insert)
		case statement.Update != nil:
			result, err = writer.Update(ctx, *statement.Update)
		case statement.Delete != nil:
			result, err = writer.Delete(ctx, *statement.Delete)
		default:
			err = errors.New("语句未指定操作")
		}
		if err != nil {
			return nil, fmt.Errorf("第 %d 条语句: %w", i+1, err)
		}
		results = append(results, result)
	}
	return results, nil
}
