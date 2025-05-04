package thing_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/burugo/thing"
	"github.com/burugo/thing/drivers/db/sqlite"
	"github.com/burugo/thing/internal/cache"

	"github.com/stretchr/testify/require"
)

// mock Role struct for test
type Role struct {
	ID   int64
	Name string
}

// mock helper: 获取角色ID列表
func getRoleIDs(roles []*Role) []int64 {
	ids := make([]int64, 0, len(roles))
	for _, r := range roles {
		ids = append(ids, r.ID)
	}
	return ids
}

// preloadRoles: 支持缓存，key=user_roles:{userID}
func preloadRoles(ctx context.Context, db thing.DBAdapter, cache thing.CacheClient, userID int64) ([]*Role, bool, error) {
	cacheKey := fmt.Sprintf("user_roles:%d", userID)
	// 1. 先查缓存
	if mock, ok := cache.(*mockCacheClient); ok {
		if val, err := mock.Get(ctx, cacheKey); err == nil {
			var ids []int64
			if err := json.Unmarshal([]byte(val), &ids); err == nil {
				// 再查 DB 批量加载 Role
				roles := make([]*Role, 0, len(ids))
				for _, id := range ids {
					row := db.DB().QueryRowContext(ctx, "SELECT id, name FROM roles WHERE id = ?", id)
					var r Role
					if err := row.Scan(&r.ID, &r.Name); err == nil {
						roles = append(roles, &r)
					}
				}
				return roles, true, nil // hit
			}
		}
	}
	// 2. 缓存 miss，查 DB
	rows, err := db.DB().QueryContext(ctx, `SELECT r.id, r.name FROM roles r
	JOIN user_roles ur ON r.id = ur.role_id WHERE ur.user_id = ?`, userID)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	var roles []*Role
	var ids []int64
	for rows.Next() {
		var r Role
		if err := rows.Scan(&r.ID, &r.Name); err != nil {
			return nil, false, err
		}
		roles = append(roles, &r)
		ids = append(ids, r.ID)
	}
	// 写入缓存
	if mock, ok := cache.(*mockCacheClient); ok {
		if b, err := json.Marshal(ids); err == nil {
			_ = mock.Set(ctx, cacheKey, string(b), 0)
		}
	}
	return roles, false, nil
}

func TestManyToMany_Cache_Basic(t *testing.T) {
	ctx := context.Background()
	// 初始化 DB 和 ORM
	db, err := sqlite.NewSQLiteAdapter(":memory:")
	require.NoError(t, err)
	defer db.Close()
	// 初始化 mock cache
	mockCache := &mockCacheClient{}
	// thing.Configure(db, mockCache) // 移除全局 cache 注入

	// 创建 user/role 表和中间表
	_, err = db.Exec(ctx, `CREATE TABLE users (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT);
	CREATE TABLE roles (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT);
	CREATE TABLE user_roles (user_id INTEGER, role_id INTEGER, PRIMARY KEY(user_id, role_id));`)
	require.NoError(t, err)

	// 创建 user 和 role
	res, err := db.Exec(ctx, "INSERT INTO users (name) VALUES (?)", "Alice")
	require.NoError(t, err)
	userID, _ := res.LastInsertId()
	res, err = db.Exec(ctx, "INSERT INTO roles (name) VALUES (?)", "Admin")
	require.NoError(t, err)
	role1ID, _ := res.LastInsertId()
	res, err = db.Exec(ctx, "INSERT INTO roles (name) VALUES (?)", "Editor")
	require.NoError(t, err)
	role2ID, _ := res.LastInsertId()
	_, err = db.Exec(ctx, "INSERT INTO user_roles (user_id, role_id) VALUES (?, ?), (?, ?)", userID, role1ID, userID, role2ID)
	require.NoError(t, err)

	// 记录初始计数
	get0 := mockCache.Counters["Get"]
	set0 := mockCache.Counters["Set"]

	// 用 DB+缓存实现测试
	roles, hit, err := preloadRoles(ctx, db, mockCache, userID)
	require.NoError(t, err)
	require.ElementsMatch(t, []int64{role1ID, role2ID}, getRoleIDs(roles))
	require.False(t, hit, "first load should be cache miss")
	require.Equal(t, get0+1, mockCache.Counters["Get"], "first load should call Get once")
	require.Equal(t, set0+1, mockCache.Counters["Set"], "first load should call Set once")

	roles2, hit2, err := preloadRoles(ctx, db, mockCache, userID)
	require.NoError(t, err)
	require.ElementsMatch(t, []int64{role1ID, role2ID}, getRoleIDs(roles2))
	require.True(t, hit2, "second load should be cache hit")
	require.Equal(t, get0+2, mockCache.Counters["Get"], "second load should call Get again")
	require.Equal(t, set0+1, mockCache.Counters["Set"], "second load should not call Set again")
}

func TestManyToMany_Cache_AddRemoveRelation(t *testing.T) {
	ctx := context.Background()
	// 初始化 DB 和 ORM
	db, err := sqlite.NewSQLiteAdapter(":memory:")
	require.NoError(t, err)
	defer db.Close()
	mockCache := &mockCacheClient{}

	_, err = db.Exec(ctx, `CREATE TABLE users (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT);
	CREATE TABLE roles (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT);
	CREATE TABLE user_roles (user_id INTEGER, role_id INTEGER, PRIMARY KEY(user_id, role_id));`)
	require.NoError(t, err)

	// 创建 user 和 role
	res, err := db.Exec(ctx, "INSERT INTO users (name) VALUES (?)", "Bob")
	require.NoError(t, err)
	userID, _ := res.LastInsertId()
	res, err = db.Exec(ctx, "INSERT INTO roles (name) VALUES (?)", "Viewer")
	require.NoError(t, err)
	role1ID, _ := res.LastInsertId()
	res, err = db.Exec(ctx, "INSERT INTO roles (name) VALUES (?)", "Editor")
	require.NoError(t, err)
	role2ID, _ := res.LastInsertId()

	// 先建立一条关联
	_, err = db.Exec(ctx, "INSERT INTO user_roles (user_id, role_id) VALUES (?, ?)", userID, role1ID)
	require.NoError(t, err)

	// 预热缓存
	roles, hit, err := preloadRoles(ctx, db, mockCache, userID)
	require.NoError(t, err)
	require.ElementsMatch(t, []int64{role1ID}, getRoleIDs(roles))
	require.False(t, hit)

	// 再次加载应命中缓存
	roles2, hit2, err := preloadRoles(ctx, db, mockCache, userID)
	require.NoError(t, err)
	require.ElementsMatch(t, []int64{role1ID}, getRoleIDs(roles2))
	require.True(t, hit2)

	// 新增一条关联，模拟失效缓存
	_, err = db.Exec(ctx, "INSERT INTO user_roles (user_id, role_id) VALUES (?, ?)", userID, role2ID)
	require.NoError(t, err)
	err = mockCache.Delete(ctx, fmt.Sprintf("user_roles:%d", userID)) // 失效缓存
	_ = err

	// 重新加载应 miss 并包含新 role
	roles3, hit3, err := preloadRoles(ctx, db, mockCache, userID)
	require.NoError(t, err)
	require.ElementsMatch(t, []int64{role1ID, role2ID}, getRoleIDs(roles3))
	require.False(t, hit3)

	// 删除一条关联，失效缓存
	_, err = db.Exec(ctx, "DELETE FROM user_roles WHERE user_id = ? AND role_id = ?", userID, role1ID)
	require.NoError(t, err)
	err = mockCache.Delete(ctx, fmt.Sprintf("user_roles:%d", userID))
	_ = err

	// 重新加载应 miss 并只剩下 role2
	roles4, hit4, err := preloadRoles(ctx, db, mockCache, userID)
	require.NoError(t, err)
	require.ElementsMatch(t, []int64{role2ID}, getRoleIDs(roles4))
	require.False(t, hit4)
}

// UserRole struct for ORM test
// 用于测试 ORM Save 是否自动失效 user_roles:{userID} 缓存

type UserRole struct {
	thing.BaseModel
	UserID int64
	RoleID int64
}

func TestUserRole_Save_TriggersCacheInvalidation(t *testing.T) {
	ctx := context.Background()
	// 初始化 DB 和 ORM
	db, err := sqlite.NewSQLiteAdapter(":memory:")
	require.NoError(t, err)
	defer db.Close()
	mockCache := &mockCacheClient{}

	_, err = db.Exec(ctx, `CREATE TABLE users (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT);
	CREATE TABLE roles (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT);
	CREATE TABLE user_roles (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		created_at DATETIME,
		updated_at DATETIME,
		deleted BOOLEAN DEFAULT FALSE,
		user_id INTEGER,
		role_id INTEGER
	);`)
	require.NoError(t, err)

	// 创建 user 和 role
	res, err := db.Exec(ctx, "INSERT INTO users (name) VALUES (?)", "Alice")
	require.NoError(t, err)
	userID, _ := res.LastInsertId()
	res, err = db.Exec(ctx, "INSERT INTO roles (name) VALUES (?)", "Admin")
	require.NoError(t, err)
	roleID, _ := res.LastInsertId()

	// 先手动写入 user_roles:{userID} 缓存，模拟已有缓存
	cacheKey := fmt.Sprintf("list:user_roles:%d", userID)
	ids := []int64{999} // 假设原来缓存里有个无关的 role
	b, _ := json.Marshal(ids)
	_ = mockCache.Set(ctx, cacheKey, string(b), 0)
	require.True(t, mockCache.Exists(cacheKey), "缓存应已存在")

	// 注册值级别索引（模拟 ORM preloadManyToMany 注册的 key）
	params := thing.QueryParams{
		Where: "user_id = ?",
		Args:  []interface{}{userID},
	}
	cache.GlobalCacheIndex.RegisterQuery("user_roles", cacheKey, toInternalQueryParams(params))

	// 用 ORM Save 新增一条 UserRole
	userRole := &UserRole{UserID: userID, RoleID: roleID}
	thingUserRole, err := thing.New[*UserRole](db, mockCache)
	require.NoError(t, err)
	err = thingUserRole.Save(userRole)
	require.NoError(t, err)

	// Save 后缓存应被失效（不存在）
	require.False(t, mockCache.Exists(cacheKey), "Save 后 list:user_roles:{userID} 缓存应被自动失效")
}

// 注意：mockCacheClient 定义在 mock_cache_test.go，需用 go test ./tests 跑全包测试，单文件 go test 不可用。
