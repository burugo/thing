package thing_test

import (
	"context"
	"testing"
	"thing"
	"thing/internal/drivers/db/sqlite"

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

// preloadRoles: 仅查 DB，不带缓存
func preloadRoles(ctx context.Context, db thing.DBAdapter, userID int64) ([]*Role, error) {
	rows, err := db.DB().QueryContext(ctx, `SELECT r.id, r.name FROM roles r
	JOIN user_roles ur ON r.id = ur.role_id WHERE ur.user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var roles []*Role
	for rows.Next() {
		var r Role
		if err := rows.Scan(&r.ID, &r.Name); err != nil {
			return nil, err
		}
		roles = append(roles, &r)
	}
	return roles, nil
}

func TestManyToMany_Cache_Basic(t *testing.T) {
	ctx := context.Background()
	// 初始化 DB 和 ORM
	db, err := sqlite.NewSQLiteAdapter(":memory:")
	require.NoError(t, err)
	defer db.Close()
	thing.Configure(db, nil) // 不用 cache，后续可 mock

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

	// 用 DB 直查实现测试
	roles, err := preloadRoles(ctx, db, userID)
	require.NoError(t, err)
	require.ElementsMatch(t, []int64{role1ID, role2ID}, getRoleIDs(roles))

	// TODO: 实现 preloadRoles(userID) 并断言缓存 miss/hit
	// roles := preloadRoles(userID)
	// assert.ElementsMatch(t, []int64{role1ID, role2ID}, getRoleIDs(roles))
	// assertCacheHit(t, false)

	// roles2 := preloadRoles(userID)
	// assert.ElementsMatch(t, []int64{role1ID, role2ID}, getRoleIDs(roles2))
	// assertCacheHit(t, true)
}
