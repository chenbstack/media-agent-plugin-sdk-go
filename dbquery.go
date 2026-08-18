package pluginsdk

// 插件私有库的结构化查询。
//
// 插件不再递交 SQL 文本，而是递交这里的结构体。宿主按已声明的 DBSchema 把它编译成
// SQL：表名和列名从声明里查出来，运算符从固定表里取，插件递交的每一个值都走 `?`
// 占位符。结果是宿主发给 SQLite 的 SQL 文本完全不含插件提供的字符串，注入在结构上
// 不可能发生，宿主也不需要再去解析 SQL 判断它碰了哪些表。
//
// 结构体全部可 JSON 序列化，跨进程插件按原样走 RPC，不需要另一套线格式。

// Expr 是表达式节点。它用「多个可空字段里只有一个非空」表示变体，而不是 interface，
// 这样 encoding/json 双向直通，不必写 MarshalJSON。零值表示「没有这个表达式」，
// 用在可选的 WHERE / HAVING 上。
type Expr struct {
	Col      *ColumnRef  `json:"col,omitempty"`
	Param    *ParamValue `json:"param,omitempty"`
	Excluded string      `json:"excluded,omitempty"`
	Call     *FuncCall   `json:"call,omitempty"`
	Binary   *BinaryExpr `json:"binary,omitempty"`
	Unary    *UnaryExpr  `json:"unary,omitempty"`
	List     *ListExpr   `json:"list,omitempty"`
	Case     *CaseExpr   `json:"case,omitempty"`
	// Star 是 COUNT(*) 里的那个星号，只能作为聚合函数的唯一实参。
	Star bool `json:"star,omitempty"`
}

// IsZero 报告这是不是一个「缺省」表达式。可选位置（WHERE、HAVING、部分索引条件）
// 用零值表示不带该子句。
func (e Expr) IsZero() bool {
	return e.Col == nil && e.Param == nil && e.Excluded == "" && e.Call == nil &&
		e.Binary == nil && e.Unary == nil && e.List == nil && e.Case == nil && !e.Star
}

// ColumnRef 引用一列。Table 在单表查询里可以省略；带 JOIN 时必须写别名或逻辑表名，
// 否则宿主无法判定这一列属于哪张表，也就无法校验它是否已声明。
type ColumnRef struct {
	Table string `json:"table,omitempty"`
	Name  string `json:"name"`
}

// ParamValue 包着一个绑定值。用指针字段承载是为了让 Param(nil) 与「没有参数」可区分。
type ParamValue struct {
	Value any `json:"value"`
}

// FuncCall 是一次函数调用。Name 必须在宿主的白名单里，宿主按白名单里的原文渲染，
// 不使用插件递交的字符串。
type FuncCall struct {
	Name string `json:"name"`
	Args []Expr `json:"args,omitempty"`
	// Distinct 对应 COUNT(DISTINCT x) 这类聚合写法。
	Distinct bool `json:"distinct,omitempty"`
}

// BinaryOperator 是二元运算符的封闭集合。宿主用它查一张固定的 SQL 文本表，
// 所以运算符也不是插件递交的字符串。
type BinaryOperator string

const (
	OpEq      BinaryOperator = "eq"
	OpNe      BinaryOperator = "ne"
	OpLt      BinaryOperator = "lt"
	OpLte     BinaryOperator = "lte"
	OpGt      BinaryOperator = "gt"
	OpGte     BinaryOperator = "gte"
	OpLike    BinaryOperator = "like"
	OpNotLike BinaryOperator = "not_like"
	OpIn      BinaryOperator = "in"
	OpNotIn   BinaryOperator = "not_in"
	OpAnd     BinaryOperator = "and"
	OpOr      BinaryOperator = "or"
	OpAdd     BinaryOperator = "add"
	OpSub     BinaryOperator = "sub"
	OpMul     BinaryOperator = "mul"
	OpDiv     BinaryOperator = "div"
	OpConcat  BinaryOperator = "concat"
)

// UnaryOperator 是一元运算符的封闭集合。
type UnaryOperator string

const (
	OpNot       UnaryOperator = "not"
	OpIsNull    UnaryOperator = "is_null"
	OpIsNotNull UnaryOperator = "is_not_null"
	OpNegate    UnaryOperator = "negate"
)

// BinaryExpr 是二元运算。
type BinaryExpr struct {
	Op    BinaryOperator `json:"op"`
	Left  Expr           `json:"left"`
	Right Expr           `json:"right"`
}

// UnaryExpr 是一元运算。
type UnaryExpr struct {
	Op   UnaryOperator `json:"op"`
	Expr Expr          `json:"expr"`
}

// ListExpr 是 IN / NOT IN 右侧的值列表。
type ListExpr struct {
	Items []Expr `json:"items,omitempty"`
}

// CaseWhenClause 是 CASE 的一个分支。
type CaseWhenClause struct {
	When Expr `json:"when"`
	Then Expr `json:"then"`
}

// CaseExpr 是 CASE WHEN ... THEN ... ELSE ... END。
type CaseExpr struct {
	Whens []CaseWhenClause `json:"whens"`
	Else  Expr             `json:"else,omitempty"`
}

// TableRef 是 FROM / JOIN 的目标：逻辑表名，或一个子查询。
type TableRef struct {
	Table    string  `json:"table,omitempty"`
	Subquery *Select `json:"subquery,omitempty"`
	Alias    string  `json:"alias,omitempty"`
}

// JoinKind 是支持的连接类型。
type JoinKind string

const (
	JoinInner JoinKind = "inner"
	JoinLeft  JoinKind = "left"
)

// Join 是一次表连接。
type Join struct {
	Kind  JoinKind `json:"kind"`
	Table TableRef `json:"table"`
	On    Expr     `json:"on"`
}

// ResultColumn 是 SELECT 的一个输出列。As 为空时，列名由 SQLite 决定，
// 所以聚合和函数结果建议都起别名。
type ResultColumn struct {
	Expr Expr   `json:"expr"`
	As   string `json:"as,omitempty"`
}

// OrderTerm 是一个排序项。
type OrderTerm struct {
	Expr Expr `json:"expr"`
	Desc bool `json:"desc,omitempty"`
}

// Select 是一次查询。
//
// Columns 留空表示「这张表已声明的全部列」——宿主按 DBSchema 展开成明确的列清单，
// 而不是发 `SELECT *`，所以库里如果有声明之外的列也不会被带出来。
type Select struct {
	From     TableRef       `json:"from"`
	Columns  []ResultColumn `json:"columns,omitempty"`
	Joins    []Join         `json:"joins,omitempty"`
	Where    Expr           `json:"where,omitempty"`
	GroupBy  []Expr         `json:"group_by,omitempty"`
	Having   Expr           `json:"having,omitempty"`
	OrderBy  []OrderTerm    `json:"order_by,omitempty"`
	Limit    *int64         `json:"limit,omitempty"`
	Offset   *int64         `json:"offset,omitempty"`
	Distinct bool           `json:"distinct,omitempty"`
}

// Assignment 是一次赋值，用在 UPDATE SET 和 upsert 的 DO UPDATE SET 里。
// Value 是表达式，所以 `access_count = access_count + 1` 这种自增写得出来。
type Assignment struct {
	Column string `json:"column"`
	Value  Expr   `json:"value"`
}

// ConflictAction 是冲突时的动作，DoNothing 与 DoUpdate 二选一。
type ConflictAction struct {
	DoNothing bool         `json:"do_nothing,omitempty"`
	DoUpdate  []Assignment `json:"do_update,omitempty"`
	Where     Expr         `json:"where,omitempty"`
}

// OnConflict 是 INSERT 的冲突处理。Columns 是冲突判定列（对应
// ON CONFLICT(a, b)），留空表示任意唯一约束冲突。
type OnConflict struct {
	Columns []string       `json:"columns,omitempty"`
	Action  ConflictAction `json:"action"`
}

// Insert 是一次写入，支持一次多行。
type Insert struct {
	Table      string      `json:"table"`
	Columns    []string    `json:"columns"`
	Rows       [][]Expr    `json:"rows"`
	OnConflict *OnConflict `json:"on_conflict,omitempty"`
}

// Update 是一次更新。
//
// Where 为零值时不会执行：全表更新必须显式把 All 置为 true。多加这一道是因为条件
// 通常由调用方按分支拼出来，拼空了的后果是整表被改，而这种 bug 在测试里往往看不出来。
type Update struct {
	Table string       `json:"table"`
	Set   []Assignment `json:"set"`
	Where Expr         `json:"where,omitempty"`
	All   bool         `json:"all,omitempty"`
}

// Delete 是一次删除。All 的含义与 Update.All 相同。
type Delete struct {
	Table string `json:"table"`
	Where Expr   `json:"where,omitempty"`
	All   bool   `json:"all,omitempty"`
}

// ---- 构造器 ----
//
// 结构体可以直接写，但嵌套起来很啰嗦。下面这组构造器让常见写法保持一行，
// 同时不牺牲上面那些结构体的直接可用性。

// From 构造一个指向逻辑表的 TableRef。
func From(table string) TableRef { return TableRef{Table: table} }

// FromAs 构造一个带别名的 TableRef，多表查询里用别名区分同名列。
func FromAs(table, alias string) TableRef { return TableRef{Table: table, Alias: alias} }

// FromSubquery 把一个子查询作为 FROM / JOIN 的目标，别名必填。
func FromSubquery(query Select, alias string) TableRef {
	return TableRef{Subquery: &query, Alias: alias}
}

// Col 引用当前表的一列。
func Col(name string) Expr { return Expr{Col: &ColumnRef{Name: name}} }

// TableCol 引用指定表（或别名）的一列。
func TableCol(table, name string) Expr { return Expr{Col: &ColumnRef{Table: table, Name: name}} }

// Param 绑定一个值。它总是编译成 `?` 占位符，永远不会出现在 SQL 文本里。
func Param(value any) Expr { return Expr{Param: &ParamValue{Value: value}} }

// Params 把一组值批量转成绑定表达式，方便拼 IN 列表。
func Params[T any](values []T) []Expr {
	out := make([]Expr, 0, len(values))
	for _, value := range values {
		out = append(out, Param(value))
	}
	return out
}

// Excluded 引用 upsert 冲突行里的新值，对应 SQLite 的 excluded.<column>。
func Excluded(column string) Expr { return Expr{Excluded: column} }

// Star 是 COUNT(*) 里的星号。
func Star() Expr { return Expr{Star: true} }

// Fn 调用一个白名单函数。
func Fn(name string, args ...Expr) Expr {
	return Expr{Call: &FuncCall{Name: name, Args: args}}
}

func binary(op BinaryOperator, left, right Expr) Expr {
	return Expr{Binary: &BinaryExpr{Op: op, Left: left, Right: right}}
}

func Eq(left, right Expr) Expr      { return binary(OpEq, left, right) }
func Ne(left, right Expr) Expr      { return binary(OpNe, left, right) }
func Lt(left, right Expr) Expr      { return binary(OpLt, left, right) }
func Lte(left, right Expr) Expr     { return binary(OpLte, left, right) }
func Gt(left, right Expr) Expr      { return binary(OpGt, left, right) }
func Gte(left, right Expr) Expr     { return binary(OpGte, left, right) }
func Like(left, right Expr) Expr    { return binary(OpLike, left, right) }
func NotLike(left, right Expr) Expr { return binary(OpNotLike, left, right) }
func Add(left, right Expr) Expr     { return binary(OpAdd, left, right) }
func Sub(left, right Expr) Expr     { return binary(OpSub, left, right) }
func Mul(left, right Expr) Expr     { return binary(OpMul, left, right) }
func Div(left, right Expr) Expr     { return binary(OpDiv, left, right) }
func Concat(left, right Expr) Expr  { return binary(OpConcat, left, right) }

// In 生成 `left IN (...)`。items 为空时生成一个恒假条件，与 SQL 的空 IN 语义一致，
// 调用方不必自己处理空列表。
func In(left Expr, items ...Expr) Expr {
	return binary(OpIn, left, Expr{List: &ListExpr{Items: items}})
}

// NotIn 生成 `left NOT IN (...)`。
func NotIn(left Expr, items ...Expr) Expr {
	return binary(OpNotIn, left, Expr{List: &ListExpr{Items: items}})
}

// And 把若干条件用 AND 串起来。零个条件返回零值表达式（等于不加条件），
// 一个条件原样返回，所以按分支拼条件的代码不必特判长度。
func And(exprs ...Expr) Expr { return fold(OpAnd, exprs) }

// Or 把若干条件用 OR 串起来，空值处理与 And 相同。
func Or(exprs ...Expr) Expr { return fold(OpOr, exprs) }

func fold(op BinaryOperator, exprs []Expr) Expr {
	var kept []Expr
	for _, expr := range exprs {
		if !expr.IsZero() {
			kept = append(kept, expr)
		}
	}
	if len(kept) == 0 {
		return Expr{}
	}
	out := kept[0]
	for _, expr := range kept[1:] {
		out = binary(op, out, expr)
	}
	return out
}

func Not(expr Expr) Expr       { return Expr{Unary: &UnaryExpr{Op: OpNot, Expr: expr}} }
func IsNull(expr Expr) Expr    { return Expr{Unary: &UnaryExpr{Op: OpIsNull, Expr: expr}} }
func IsNotNull(expr Expr) Expr { return Expr{Unary: &UnaryExpr{Op: OpIsNotNull, Expr: expr}} }
func Negate(expr Expr) Expr    { return Expr{Unary: &UnaryExpr{Op: OpNegate, Expr: expr}} }

// 常用聚合与标量函数的快捷写法，等价于对应的 Fn 调用。
func Count(expr Expr) Expr { return Fn("count", expr) }
func CountDistinct(expr Expr) Expr {
	return Expr{Call: &FuncCall{Name: "count", Args: []Expr{expr}, Distinct: true}}
}
func Sum(expr Expr) Expr                       { return Fn("sum", expr) }
func Avg(expr Expr) Expr                       { return Fn("avg", expr) }
func Min(exprs ...Expr) Expr                   { return Fn("min", exprs...) }
func Max(exprs ...Expr) Expr                   { return Fn("max", exprs...) }
func Coalesce(exprs ...Expr) Expr              { return Fn("coalesce", exprs...) }
func Substr(expr Expr, from, length Expr) Expr { return Fn("substr", expr, from, length) }

// CaseBuilder 累积 CASE 的分支，用 Else / End 收尾。
type CaseBuilder struct {
	whens []CaseWhenClause
}

// Case 开始一个 CASE 表达式。
func Case() *CaseBuilder { return &CaseBuilder{} }

// When 追加一个分支。
func (b *CaseBuilder) When(condition, result Expr) *CaseBuilder {
	b.whens = append(b.whens, CaseWhenClause{When: condition, Then: result})
	return b
}

// Else 带默认值收尾。
func (b *CaseBuilder) Else(result Expr) Expr {
	return Expr{Case: &CaseExpr{Whens: b.whens, Else: result}}
}

// End 不带默认值收尾，未命中任何分支时结果为 NULL。
func (b *CaseBuilder) End() Expr {
	return Expr{Case: &CaseExpr{Whens: b.whens}}
}

// Limit 把一个整数包成 Select.Limit 需要的指针，避免调用方自己声明临时变量。
func Limit(n int64) *int64 { return &n }

// Set 构造一次赋值。
func Set(column string, value Expr) Assignment {
	return Assignment{Column: column, Value: value}
}

// Result 构造一个带别名的输出列。
func Result(expr Expr, as string) ResultColumn {
	return ResultColumn{Expr: expr, As: as}
}

// Order 构造一个升序排序项。
func Order(expr Expr) OrderTerm { return OrderTerm{Expr: expr} }

// OrderDesc 构造一个降序排序项。
func OrderDesc(expr Expr) OrderTerm { return OrderTerm{Expr: expr, Desc: true} }
