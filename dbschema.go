package pluginsdk

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// 插件私有库的表结构声明。
//
// 插件不再自己建表、加列、写迁移脚本，而是声明「我需要长这样的表」，由宿主把实际
// 结构对齐过去。这样做有两个理由：一是插件作者不必再手写
// `SELECT col FROM t LIMIT 0` 探测列是否存在这类土办法；二是宿主在插件代码执行之前
// 就知道它有哪些表、哪些列，查询编译器可以据此校验每一个标识符，插件永远没有机会
// 自己提供表名或列名。

// maxDBIdentifierLength 限制逻辑标识符长度。物理表名还要加
// `plugin_data_<plugin>_` 前缀，留足余量避免拼出超长的 SQLite 标识符。
const maxDBIdentifierLength = 48

// dbIdentifierPattern 是逻辑表名、列名、索引名的唯一合法形态。收得这么紧是故意的：
// 标识符最终要拼进 SQL 文本（占位符只能替值不能替标识符），限制在
// 小写字母开头的 [a-z0-9_] 之后，引号注入、关键字冲突、大小写折叠差异一并消失。
var dbIdentifierPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// ColumnType 是插件私有表支持的列类型，对应 SQLite 的存储类。
type ColumnType string

const (
	ColumnText    ColumnType = "text"
	ColumnInteger ColumnType = "integer"
	ColumnReal    ColumnType = "real"
	ColumnBlob    ColumnType = "blob"
)

// Column 声明一列。
//
// Default 只接受标量（string / 整数 / 浮点 / bool / nil）。它是唯一会被渲染进 DDL
// 文本的插件提供值——CREATE TABLE 的 DEFAULT 子句不接受占位符——所以宿主按声明的
// ColumnType 渲染并转义，且这个值来自随插件包一起签名的 schema，不是运行时输入。
type Column struct {
	Name    string     `json:"name"`
	Type    ColumnType `json:"type"`
	NotNull bool       `json:"not_null,omitempty"`
	// PrimaryKey 是单列主键的简写；复合主键用 Table.PrimaryKey。
	PrimaryKey bool `json:"primary_key,omitempty"`
	Default    any  `json:"default,omitempty"`
}

// IndexColumn 是索引里的一列，Desc 对应 SQLite 的 DESC 排序。
type IndexColumn struct {
	Name string `json:"name"`
	Desc bool   `json:"desc,omitempty"`
}

// Index 声明一条索引。Name 是逻辑名，宿主加插件前缀后落库。
type Index struct {
	Name    string        `json:"name"`
	Columns []IndexColumn `json:"columns"`
	Unique  bool          `json:"unique,omitempty"`
	// Where 声明部分索引的条件，nil 表示普通索引。条件里只能引用本表的列。
	Where *Expr `json:"where,omitempty"`
}

// Table 声明一张插件私有表。Name 是逻辑名，物理表名由宿主拼成
// plugin_data_<plugin>_<name>，插件全程看不到也用不上物理名。
type Table struct {
	Name    string   `json:"name"`
	Columns []Column `json:"columns"`
	// PrimaryKey 声明复合主键；与 Column.PrimaryKey 二选一，不可并用。
	PrimaryKey []string `json:"primary_key,omitempty"`
	Indexes    []Index  `json:"indexes,omitempty"`
}

// MigrationStepKind 是声明式对齐表达不了、必须按序执行一次的迁移动作。
type MigrationStepKind string

const (
	// StepDropColumn 删除一列。对齐逻辑永远不会自动删列：插件少声明一列可能只是
	// 漏写，自动删除会静默丢数据，所以删列必须由插件显式列为一步。
	StepDropColumn MigrationStepKind = "drop_column"
	// StepRenameColumn 重命名一列并保留数据。
	StepRenameColumn MigrationStepKind = "rename_column"
	// StepDropTable 删除整张表。
	StepDropTable MigrationStepKind = "drop_table"
)

// MigrationStep 是一次性的结构变更。宿主按 ID 记录已应用状态，同一个 ID 只执行一次。
//
// ID 一旦随版本发布就不可修改也不可复用：改了 ID，老库会把它当成没执行过的新步骤重跑；
// 复用 ID，新步骤会被当成已执行而静默跳过。
type MigrationStep struct {
	ID    string            `json:"id"`
	Kind  MigrationStepKind `json:"kind"`
	Table string            `json:"table"`
	// Column 是 drop_column / rename_column 的目标列。
	Column string `json:"column,omitempty"`
	// To 是 rename_column 的新列名。
	To string `json:"to,omitempty"`
}

// DBSchema 是插件对自己私有库的完整声明，挂在 Plugin.Schema 上。
//
// Tables 描述期望的最终结构，宿主每次启动都把实际结构对齐过去（建表、加列、建删
// 索引），已经一致时不执行任何 DDL。Steps 补充对齐表达不了的动作，按顺序各执行一次。
type DBSchema struct {
	Tables []Table         `json:"tables,omitempty"`
	Steps  []MigrationStep `json:"steps,omitempty"`
}

// IsZero 报告插件是否没有声明任何私有表。
func (s DBSchema) IsZero() bool { return len(s.Tables) == 0 && len(s.Steps) == 0 }

// ParseDBSchema 解析随插件包分发的 db.schema.json。宿主用它在不启动插件进程的前提下
// 拿到声明——声明既是建表依据，也是查询编译器的白名单，必须来自已签名的包内容而不是
// 插件进程自报。
func ParseDBSchema(data []byte) (DBSchema, error) {
	var schema DBSchema
	if err := json.Unmarshal(data, &schema); err != nil {
		return DBSchema{}, err
	}
	if err := schema.Validate(); err != nil {
		return DBSchema{}, err
	}
	return schema, nil
}

// Table 按逻辑名查声明。查询编译器靠它把列名校验到声明范围内。
func (s DBSchema) Table(name string) (Table, bool) {
	for _, table := range s.Tables {
		if table.Name == name {
			return table, true
		}
	}
	return Table{}, false
}

// Column 按列名查声明。
func (t Table) Column(name string) (Column, bool) {
	for _, column := range t.Columns {
		if column.Name == name {
			return column, true
		}
	}
	return Column{}, false
}

// HasColumn 报告表里是否声明了该列。
func (t Table) HasColumn(name string) bool {
	_, ok := t.Column(name)
	return ok
}

// PrimaryKeyColumns 归一化主键声明：复合主键用 Table.PrimaryKey，单列主键用
// Column.PrimaryKey，调用方不必分别处理两种写法。
func (t Table) PrimaryKeyColumns() []string {
	if len(t.PrimaryKey) > 0 {
		return append([]string(nil), t.PrimaryKey...)
	}
	var out []string
	for _, column := range t.Columns {
		if column.PrimaryKey {
			out = append(out, column.Name)
		}
	}
	return out
}

// Validate 校验声明本身是否自洽。宿主在对齐结构之前调用；插件包的打包器也会调用，
// 让错误在发版时就暴露，而不是等到用户装上之后建表失败。
func (s DBSchema) Validate() error {
	seen := map[string]bool{}
	for _, table := range s.Tables {
		if err := table.validate(); err != nil {
			return err
		}
		if seen[table.Name] {
			return fmt.Errorf("插件私有表 %q 重复声明", table.Name)
		}
		seen[table.Name] = true
	}
	stepIDs := map[string]bool{}
	for _, step := range s.Steps {
		if err := step.validate(); err != nil {
			return err
		}
		if stepIDs[step.ID] {
			return fmt.Errorf("迁移步骤 ID %q 重复", step.ID)
		}
		stepIDs[step.ID] = true
	}
	return nil
}

func (t Table) validate() error {
	if err := validateDBIdentifier("表名", t.Name); err != nil {
		return err
	}
	if len(t.Columns) == 0 {
		return fmt.Errorf("插件私有表 %q 至少要声明一列", t.Name)
	}
	seen := map[string]bool{}
	for _, column := range t.Columns {
		if err := validateDBIdentifier("列名", column.Name); err != nil {
			return fmt.Errorf("表 %q: %w", t.Name, err)
		}
		if seen[column.Name] {
			return fmt.Errorf("表 %q 的列 %q 重复声明", t.Name, column.Name)
		}
		seen[column.Name] = true
		switch column.Type {
		case ColumnText, ColumnInteger, ColumnReal, ColumnBlob:
		default:
			return fmt.Errorf("表 %q 的列 %q 类型 %q 不支持", t.Name, column.Name, column.Type)
		}
		if err := validateColumnDefault(column); err != nil {
			return fmt.Errorf("表 %q: %w", t.Name, err)
		}
	}
	if len(t.PrimaryKey) > 0 {
		for _, column := range t.Columns {
			if column.PrimaryKey {
				return fmt.Errorf("表 %q 同时声明了复合主键和列级主键，只能二选一", t.Name)
			}
		}
	}
	for _, name := range t.PrimaryKey {
		if !t.HasColumn(name) {
			return fmt.Errorf("表 %q 的主键列 %q 未声明", t.Name, name)
		}
	}
	indexNames := map[string]bool{}
	for _, index := range t.Indexes {
		if err := index.validate(t); err != nil {
			return err
		}
		if indexNames[index.Name] {
			return fmt.Errorf("表 %q 的索引 %q 重复声明", t.Name, index.Name)
		}
		indexNames[index.Name] = true
	}
	return nil
}

func (i Index) validate(table Table) error {
	if err := validateDBIdentifier("索引名", i.Name); err != nil {
		return fmt.Errorf("表 %q: %w", table.Name, err)
	}
	if len(i.Columns) == 0 {
		return fmt.Errorf("表 %q 的索引 %q 至少要包含一列", table.Name, i.Name)
	}
	for _, column := range i.Columns {
		if !table.HasColumn(column.Name) {
			return fmt.Errorf("表 %q 的索引 %q 引用了未声明的列 %q", table.Name, i.Name, column.Name)
		}
	}
	return nil
}

func (s MigrationStep) validate() error {
	if strings.TrimSpace(s.ID) == "" {
		return fmt.Errorf("迁移步骤必须有 ID")
	}
	if err := validateDBIdentifier("表名", s.Table); err != nil {
		return fmt.Errorf("迁移步骤 %q: %w", s.ID, err)
	}
	switch s.Kind {
	case StepDropTable:
		if s.Column != "" || s.To != "" {
			return fmt.Errorf("迁移步骤 %q 是 drop_table，不该带列名", s.ID)
		}
	case StepDropColumn:
		if err := validateDBIdentifier("列名", s.Column); err != nil {
			return fmt.Errorf("迁移步骤 %q: %w", s.ID, err)
		}
		if s.To != "" {
			return fmt.Errorf("迁移步骤 %q 是 drop_column，不该带新列名", s.ID)
		}
	case StepRenameColumn:
		if err := validateDBIdentifier("列名", s.Column); err != nil {
			return fmt.Errorf("迁移步骤 %q: %w", s.ID, err)
		}
		if err := validateDBIdentifier("新列名", s.To); err != nil {
			return fmt.Errorf("迁移步骤 %q: %w", s.ID, err)
		}
	default:
		return fmt.Errorf("迁移步骤 %q 的类型 %q 不支持", s.ID, s.Kind)
	}
	return nil
}

// validateColumnDefault 限制 DEFAULT 只能是标量。它是唯一进入 DDL 文本的声明值，
// 放行复杂类型等于放行任意 SQL 片段。
func validateColumnDefault(column Column) error {
	switch column.Default.(type) {
	case nil, string, bool,
		int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64,
		float32, float64:
		return nil
	default:
		return fmt.Errorf("列 %q 的默认值类型 %T 不支持，只能是字符串、数字、布尔", column.Name, column.Default)
	}
}

// ValidateDBIdentifier 供宿主复用同一套标识符规则。插件包是签过名的，但标识符规则
// 是整个边界的基石，宿主侧再校验一次的成本可以忽略。
func ValidateDBIdentifier(kind, value string) error { return validateDBIdentifier(kind, value) }

func validateDBIdentifier(kind, value string) error {
	if value == "" {
		return fmt.Errorf("%s不能为空", kind)
	}
	if len(value) > maxDBIdentifierLength {
		return fmt.Errorf("%s %q 超过 %d 个字符", kind, value, maxDBIdentifierLength)
	}
	if !dbIdentifierPattern.MatchString(value) {
		return fmt.Errorf("%s %q 只能是小写字母开头的字母、数字、下划线", kind, value)
	}
	return nil
}
