package thing

import (
	"fmt"
	"testing"

	"github.com/burugo/thing"
	"github.com/burugo/thing/common"
	"github.com/burugo/thing/drivers/db/sqlite"
	"github.com/stretchr/testify/assert"
)

// --- Reproduce ServiceType gob registration issue ---

// ServiceType simulates the user's named string alias type
type ServiceType string

const (
	ServiceTypeStdio          ServiceType = "stdio"
	ServiceTypeSSE            ServiceType = "sse"
	ServiceTypeStreamableHTTP ServiceType = "streamable_http"
)

// MCPServiceTest simulates the user's model with ServiceType field
type MCPServiceTest struct {
	thing.BaseModel
	Name        string      `db:"name"`
	Description string      `db:"description"`
	Type        ServiceType `db:"type"` // This should trigger gob registration issue
	Enabled     bool        `db:"enabled"`
}

func (m *MCPServiceTest) TableName() string {
	return "mcp_services_test"
}

func TestGobRegistrationIssueWithServiceType(t *testing.T) {
	// Setup test environment
	dbAdapter, err := sqlite.NewSQLiteAdapter(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name()))
	assert.NoError(t, err)

	// Use nil for cacheClient to use DefaultLocalCache (this triggers the gob encoding issue)
	err = thing.Configure(dbAdapter, nil)
	assert.NoError(t, err)

	mcpThing, err := thing.Use[*MCPServiceTest]()
	assert.NoError(t, err)

	err = thing.AutoMigrate(&MCPServiceTest{})
	assert.NoError(t, err)

	// Create a model with ServiceType field
	service := &MCPServiceTest{
		Name:        "Test MCP Service",
		Description: "Test service for gob registration",
		Type:        ServiceTypeStdio, // This ServiceType value will be stored via interface{} in cache
		Enabled:     true,
	}

	// Save the model - this should trigger caching and potentially the gob error
	err = mcpThing.Save(service)
	assert.NoError(t, err, "Save should not fail due to gob registration issues")
	assert.NotZero(t, service.ID)

	// Retrieve by ID - this uses cache and should trigger gob decoding
	retrieved, err := mcpThing.ByID(service.ID)
	assert.NoError(t, err, "ByID should not fail due to gob registration issues")
	assert.Equal(t, service.Type, retrieved.Type, "ServiceType should be correctly stored and retrieved")
	assert.Equal(t, ServiceTypeStdio, retrieved.Type)

	// Query operations that use cache
	allServices, err := mcpThing.All()
	assert.NoError(t, err, "All() should not fail due to gob registration issues")
	assert.Len(t, allServices, 1)
	assert.Equal(t, service.Type, allServices[0].Type)

	// Test different ServiceType values
	service2 := &MCPServiceTest{
		Name:        "SSE Service",
		Description: "Server-sent events service",
		Type:        ServiceTypeSSE,
		Enabled:     false,
	}

	err = mcpThing.Save(service2)
	assert.NoError(t, err, "Save with different ServiceType should not fail")

	retrieved2, err := mcpThing.ByID(service2.ID)
	assert.NoError(t, err, "ByID with different ServiceType should not fail")
	assert.Equal(t, ServiceTypeSSE, retrieved2.Type)
}

// --- Original test ---

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
