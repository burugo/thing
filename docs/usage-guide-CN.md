# Thing ORM 使用指南

本文基于 `Thing ORM Usage Guide` 项目规则整理，作为日常开发时的主入口。README 只保留快速开始；模型定义、查询、缓存利用率和复杂场景建议都放在这里。

## 核心定位

Thing 适合高频 CRUD、按 ID 读取、分页列表、轻量关系预加载，以及需要自动缓存失效的业务系统。

它不试图覆盖所有 SQL 能力。复杂报表、临时分析、数据库特有语法、重 JOIN 查询可以走 raw SQL，但这类读取不应该假设能复用 Thing 的模型缓存和列表缓存。

## 安装

```bash
go get github.com/burugo/thing
```

## 配置

### 创建数据库适配器

```go
import "github.com/burugo/thing/drivers/db/sqlite"

db, err := sqlite.NewSQLiteAdapter("app.db")
if err != nil {
	log.Fatal(err)
}
```

MySQL 和 PostgreSQL 使用对应 driver：

```go
// "github.com/burugo/thing/drivers/db/mysql"
// "github.com/burugo/thing/drivers/db/postgres"
```

### 配置缓存

`thing.New` 的第二个参数传 `nil` 时使用内存缓存。生产或多实例部署通常使用 Redis。

```go
import (
	"github.com/redis/go-redis/v9"
	redisCache "github.com/burugo/thing/drivers/cache/redis"
)

rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
cacheClient := redisCache.NewClient(rdb)
```

### 创建模型入口

推荐显式创建每个模型的 `Thing` 实例：

```go
users, err := thing.New[*User](db, cacheClient)
if err != nil {
	log.Fatal(err)
}
```

也可以在小项目中使用全局配置：

```go
if err := thing.Configure(db, cacheClient); err != nil {
	log.Fatal(err)
}

users, err := thing.Use[*User]()
```

## 模型定义

模型通常嵌入 `thing.BaseModel`，并用 `db` tag 声明列名、索引和唯一约束。

```go
type User struct {
	thing.BaseModel
	Name  string `db:"name,index"`
	Email string `db:"email,unique"`
	Age   int    `db:"age,default:18"`
}

func (u *User) TableName() string { return "users" }
```

规则：

- `thing.BaseModel` 提供 `ID`、`CreatedAt`、`UpdatedAt`、`Deleted`。
- `db:"name"` 显式映射列名；省略时会把 Go 字段名转成 snake_case。
- `db:"-"` 表示该字段不是本表列，关系字段必须加。
- 索引声明放在 `db` tag：`index`、`unique`、`index:idx_name`、`unique:uq_name`。
- `thing` tag 只用于关系声明，不用于索引。

复合索引示例：

```go
type Account struct {
	thing.BaseModel
	TenantID int64  `db:"tenant_id,index:idx_tenant_email"`
	Email    string `db:"email,index:idx_tenant_email"`
	KeyA     string `db:"key_a,unique:uq_pair"`
	KeyB     string `db:"key_b,unique:uq_pair"`
}
```

## Migration

`AutoMigrate` 使用全局 DB 配置，因此先调用 `thing.Configure`：

```go
if err := thing.Configure(db, cacheClient); err != nil {
	log.Fatal(err)
}

if err := thing.AutoMigrate(&User{}); err != nil {
	log.Fatal(err)
}
```

多个模型可以一起迁移：

```go
if err := thing.AutoMigrate(&User{}, &Post{}); err != nil {
	log.Fatal(err)
}
```

## 基础 CRUD

```go
user := &User{Name: "Alice", Email: "alice@example.com"}

// Create or update
if err := users.Save(user); err != nil {
	log.Fatal(err)
}

// Cached single read
found, err := users.ByID(user.ID)

// Cached batch read
userMap, err := users.ByIDs([]int64{user.ID})

// Soft delete: set deleted=true and update updated_at
err = users.SoftDelete(user)

// Hard delete
err = users.Delete(user)
```

优先使用 `ByID` / `ByIDs` 读取实体。它们是模型缓存的主路径，比散落的 `SELECT *` 更容易复用缓存。

## 查询

Thing 的查询返回 `*CachedResult[T]`，延迟到 `Fetch`、`First`、`All`、`Count` 时执行。

```go
result := users.Query(thing.QueryParams{
	Where: "age > ? AND status = ?",
	Args:  []interface{}{18, "active"},
	Order: "id DESC",
})

page, err := result.Fetch(0, 20)
count, err := result.Count()
first, err := result.First()
all, err := result.All()
```

链式写法：

```go
page, err := users.
	Where("age > ?", 18).
	Order("id DESC").
	Fetch(0, 20)
```

默认查询会排除软删除记录。需要包含软删除记录时：

```go
page, err := users.
	Where("email = ?", email).
	WithDeleted().
	Fetch(0, 10)
```

## 关系和预加载

关系字段使用 `thing` tag，并且必须 `db:"-"`。

```go
type User struct {
	thing.BaseModel
	Name  string  `db:"name"`
	Books []*Book `thing:"hasMany;fk:user_id;model:Book" db:"-"`
}

type Book struct {
	thing.BaseModel
	UserID int64 `db:"user_id,index"`
	Title  string `db:"title"`
	User   *User  `thing:"belongsTo;fk:user_id;model:User" db:"-"`
}
```

预加载：

```go
usersWithBooks, err := users.
	Where("id IN (?)", []int64{1, 2, 3}).
	Preload("Books").
	Fetch(0, 3)
```

多对多关系通过 join table：

```go
type User struct {
	thing.BaseModel
	Roles []*Role `thing:"manyToMany;model:Role;joinTable:user_roles;joinLocalKey:user_id;joinRelatedKey:role_id" db:"-"`
}
```

## 提高缓存利用率

这一节是使用 Thing 时最重要的性能习惯。

### 理解缓存路径

Thing 主要缓存两类数据：

- 模型缓存：`ByID` / `ByIDs` 按主键缓存单个模型。
- 查询缓存：`Fetch` 缓存列表 ID，`Count` 缓存计数。

列表查询命中后，Thing 会拿缓存里的 ID 再走模型读取路径，所以同一个模型可以在多个列表之间复用。

因此，高缓存命中率的关键不是写出一个巨大的 SQL，而是让业务读取回到这条路径：

```text
查询列表 ID -> ByIDs/模型缓存 -> 组装结果
```

### 优先查 ID 列表，再用 ByIDs

如果业务已经有 ID 列表，直接使用：

```go
ids := []int64{1, 2, 3}
userMap, err := users.ByIDs(ids)
```

如果 ID 来自另一个表，先查询那张表的模型，再回到主模型：

```go
labels, err := threadLabels.
	Where("user_id = ? AND label = ?", userID, label).
	Fetch(0, 100)
if err != nil {
	return err
}

threadIDs := make([]int64, 0, len(labels))
for _, item := range labels {
	threadIDs = append(threadIDs, item.ThreadID)
}

threadsPage, err := threads.
	Where("id IN (?)", threadIDs).
	Order("updated_at DESC").
	Fetch(0, len(threadIDs))
```

这样两段查询都仍然是 Thing 能理解的模型查询，能复用各自的列表缓存、计数缓存和模型缓存。

### 少用 SELECT *，多用 Thing 的读取入口

不推荐在业务路径里频繁这样写：

```go
var rows []*Thread
err := db.Select(ctx, &rows, `
	SELECT *
	FROM threads
	WHERE user_id = ?
	ORDER BY updated_at DESC
`, userID)
```

更推荐：

```go
rows, err := threads.
	Where("user_id = ?", userID).
	Order("updated_at DESC").
	Fetch(0, 50)
```

理由：raw `SELECT *` 直接绕开模型缓存和查询缓存。即使 SQL 很快，应用层也失去了跨请求、跨列表复用同一批模型的机会。

### 尽量不要把 JOIN 放进缓存路径

Thing 的缓存失效围绕“某个模型表的字段变化”工作。`JOIN`、`EXISTS`、子查询通常同时依赖多张表，失效边界更复杂，也更难让模型缓存复用。

不推荐：

```go
threads.Where(`
	EXISTS (
		SELECT 1
		FROM thread_labels
		WHERE thread_labels.thread_id = threads.id
		  AND thread_labels.label = ?
	)
`, label).Fetch(0, 50)
```

推荐拆成两步：

```go
labels, err := threadLabels.Where("label = ?", label).Fetch(0, 100)
// collect threadIDs...
threads, err := threadThing.Where("id IN (?)", threadIDs).Fetch(0, len(threadIDs))
```

如果确实是报表、聚合、后台一次性任务，使用 raw SQL 没问题；只是要把它视为非 Thing 缓存路径。

### `IN` 优先写 `IN (?)` + slice

推荐：

```go
threads.Where("id IN (?)", []int64{1, 2, 3}).Fetch(0, 3)
```

Thing 会在 SQL 构建阶段把 slice 展开成数据库需要的占位符。空 slice 会变成 `IN (NULL)`，安全地匹配不到记录。

即使当前 matcher 能处理部分手写展开形式，也不建议业务代码手写：

```go
threads.Where("id IN (?, ?, ?)", 1, 2, 3)
```

`IN (?)` + slice 更稳定，也能避免动态拼接 SQL 时的占位符和参数数量错误。

### 保持 WHERE 可解析

缓存失效会解析已缓存查询的 `WHERE` 条件。尽量使用模型字段上的简单条件：

- `=`, `!=`, `<>`
- `>`, `<`, `>=`, `<=`
- `LIKE`, `NOT LIKE`
- `IN`, `NOT IN`
- `AND`, `OR`, 括号
- `true`, `false`, `NULL` 这类字面量

尽量避免把函数、表达式、子查询放进缓存查询：

```go
// 不利于缓存失效定位
users.Where("LOWER(email) = ?", normalizedEmail)
```

更推荐把可查询值落成普通列：

```go
users.Where("email_normalized = ?", normalizedEmail)
```

如果确实使用了 Thing 无法解析的条件，Thing 会偏向安全：相关写入时删除对应 query cache，避免返回脏缓存。代价是缓存命中率下降。

### 排序字段要稳定

列表缓存不仅和 `WHERE` 有关，也和 `ORDER BY` 有关。常见写法：

```go
threads.
	Where("user_id = ?", userID).
	Order("updated_at DESC, id DESC").
	Fetch(0, 50)
```

当 `updated_at` 或 `id` 影响排序时，相关更新会触发列表缓存失效。排序表达式越复杂，越容易退化成保守失效。

### 用 KeepItem 表达复杂可见性

有些业务不适合全部写进 SQL，比如帖子列表中“用户隐藏了帖子”“权限策略很复杂”等。可以在模型上通过 `KeepItem()` 做最终过滤。

Thing 的列表加载会允许多取一批 ID，再通过 `KeepItem()` 过滤，保证业务可见性逻辑能集中在模型层。

适合放进 `KeepItem()` 的逻辑：

- 软删除或业务隐藏状态
- 不适合反复拼进 SQL 的可见性判断
- 需要和模型方法保持一致的最终过滤

如果某个条件简单、稳定、经常用于列表入口，仍然建议落成普通列并写入 `WHERE`，这样缓存候选集合更小。

### Count 和列表使用同一组条件

分页通常需要列表和总数：

```go
result := threads.Where("user_id = ?", userID).Order("updated_at DESC")

page, err := result.Fetch(0, 50)
total, err := result.Count()
```

复用同一个 `CachedResult` 或同一组 `QueryParams`，可以让列表缓存和 count 缓存保持一致。

### 预加载优先使用 Preload

如果只是加载关系模型，优先：

```go
users.
	Where("id IN (?)", ids).
	Preload("Books").
	Fetch(0, len(ids))
```

不要为了拿关系数据直接改成一条大型 JOIN。`Preload` 会让主模型和关系模型分别走 Thing 的读取路径，更利于缓存复用。

## JSON 输出

Thing 提供 `ToJSON` 控制输出字段。它是 `*thing.Thing[T]` 上的方法：

```go
users.ToJSON(user, thing.Include("id", "name"))
users.ToJSON(user, thing.Exclude("email"))
users.ToJSON(user, thing.WithFields("name,profile{avatar},-id"))
```

`WithFields` 支持嵌套字段、排除字段和输出顺序。导出的零参数单返回值方法可以作为虚拟字段显式输出：

```go
func (u *User) FullName() string {
	return u.FirstName + " " + u.LastName
}

users.ToJSON(user, thing.WithFields("first_name,full_name"))
```

## Hooks

可以通过事件监听做校验、审计、派发副作用。

```go
thing.RegisterListener(thing.EventTypeBeforeSave, func(ctx context.Context, eventType thing.EventType, model interface{}, data interface{}) error {
	user, ok := model.(*User)
	if !ok {
		return nil
	}
	if user.Email == "" {
		return errors.New("email is required")
	}
	return nil
})
```

常用事件：

- `EventTypeBeforeSave`
- `EventTypeAfterSave`
- `EventTypeBeforeCreate`
- `EventTypeAfterCreate`
- `EventTypeBeforeDelete`
- `EventTypeAfterDelete`
- `EventTypeBeforeSoftDelete`
- `EventTypeAfterSoftDelete`

`Before*` hook 返回错误会中止对应操作。

## Raw SQL

需要复杂 SQL 时可以从 `Thing` 实例拿到 adapter：

```go
db := users.DBAdapter()

_, err := db.Exec(ctx, "UPDATE users SET status = ? WHERE id = ?", "active", id)

var stats struct {
	Total int64 `db:"total"`
}
err = db.Get(ctx, &stats, "SELECT COUNT(*) AS total FROM users")
```

也可以拿到底层 `*sql.DB`：

```go
sqlDB := users.DB()
```

建议：

- 业务高频读优先走 Thing 的 `Query` / `ByID` / `ByIDs`。
- 报表、聚合、一次性后台任务可以走 raw SQL。
- raw SQL 写入后要确认是否需要主动清理相关缓存，避免绕开 ORM 写路径。

## 监控缓存

缓存客户端支持统计时，可以查看 hit/miss：

```go
stats, err := cacheClient.GetCacheStats(ctx)
```

如果某个接口缓存命中率低，优先检查：

- 是否绕过了 `ByID` / `ByIDs` / `Query().Fetch`
- 是否用了 raw `SELECT *`
- 是否把 JOIN / 子查询 / SQL 函数放进了缓存查询
- `WHERE` 是否能被 Thing 解析
- 是否频繁更新了列表排序字段
- 是否把本来可以落成普通列的业务状态藏在复杂表达式里

## 选择建议

| 场景 | 推荐 |
| --- | --- |
| 按主键读单条 | `ByID` |
| 已有一批 ID | `ByIDs` |
| 分页列表 | `Where(...).Order(...).Fetch(offset, limit)` |
| 列表总数 | 同一组条件上的 `Count()` |
| 关联列表 | 先查关联表得到 ID，再 `id IN (?)` 查主模型 |
| 加载关系对象 | `Preload` |
| 报表/聚合/一次性 JOIN | raw SQL |
| 高频业务读 | 避免 raw `SELECT *`，回到 Thing 缓存路径 |
