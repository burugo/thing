package thing

import (
	"fmt"
	"testing"

	"github.com/burugo/thing"
	"github.com/burugo/thing/common"
	"github.com/burugo/thing/drivers/db/sqlite"
	"github.com/stretchr/testify/assert"
)

type TestBugModel struct {
	thing.BaseModel
	Name  string `db:"name"`
	Value int    `db:"value"`
}

func (m *TestBugModel) TableName() string {
	return "test_bug_models"
}

func setupTestBugThing(t *testing.T) *thing.Thing[*TestBugModel] {
	dbAdapter, err := sqlite.NewSQLiteAdapter(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name()))
	if err != nil {
		t.Fatalf("Failed to create SQLite adapter: %v", err)
	}
	// Use nil for cacheClient to use DefaultLocalCache
	err = thing.Configure(dbAdapter, nil)
	if err != nil {
		t.Fatalf("Failed to configure Thing: %v", err)
	}
	tbThing, err := thing.Use[*TestBugModel]()
	if err != nil {
		t.Fatalf("Failed to get Thing instance: %v", err)
	}
	err = thing.AutoMigrate(&TestBugModel{})
	if err != nil {
		t.Fatalf("Failed to auto-migrate: %v", err)
	}
	return tbThing
}

func TestSaveAndAllWithDefaultLocalCache(t *testing.T) {
	tbThing := setupTestBugThing(t)

	// 1. 初始状态 All 应为空
	allItems, err := tbThing.All()
	assert.NoError(t, err)
	assert.Empty(t, allItems, "Initially, All() should return empty slice")

	// 2. Save 一个新实体
	item := &TestBugModel{Name: "A", Value: 1}
	err = tbThing.Save(item)
	assert.NoError(t, err)
	assert.NotZero(t, item.ID)

	// 3. 立即 All
	allAfterSave, err := tbThing.All()
	assert.NoError(t, err)
	assert.Len(t, allAfterSave, 1, "After Save, All() should return 1 item (bug: if 0, cache未更新)")
	if len(allAfterSave) == 1 {
		assert.Equal(t, item.ID, allAfterSave[0].ID)
		assert.Equal(t, item.Name, allAfterSave[0].Name)
		assert.Equal(t, item.Value, allAfterSave[0].Value)
	} else {
		t.Errorf("Expected 1 item after save, got %d. Possible cache bug.", len(allAfterSave))
	}

	// 4. Save 第二个实体
	item2 := &TestBugModel{Name: "B", Value: 2}
	err = tbThing.Save(item2)
	assert.NoError(t, err)
	assert.NotZero(t, item2.ID)

	allAfterSecond, err := tbThing.All()
	assert.NoError(t, err)
	assert.Len(t, allAfterSecond, 2, "After saving two, All() should return 2 items")

	// 5. Delete 第一个实体
	err = tbThing.Delete(item)
	assert.NoError(t, err)
	_, err = tbThing.ByID(item.ID)
	assert.ErrorIs(t, err, common.ErrNotFound)

	allAfterDelete, err := tbThing.All()
	assert.NoError(t, err)
	assert.Len(t, allAfterDelete, 1, "After delete, All() should return 1 item")
	if len(allAfterDelete) == 1 {
		assert.Equal(t, item2.ID, allAfterDelete[0].ID)
	}
}
