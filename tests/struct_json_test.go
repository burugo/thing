package thing_test

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/burugo/thing"
	"github.com/burugo/thing/drivers/db/sqlite"
)

type Address struct {
	Province string `json:"province"`
	City     string `json:"city"`
}

func (a Address) Value() (driver.Value, error) {
	return json.Marshal(a)
}

func (a *Address) Scan(value interface{}) error {
	bytes, ok := value.([]byte)
	if !ok {
		return fmt.Errorf("Address.Scan: expected []byte, got %T", value)
	}
	return json.Unmarshal(bytes, a)
}

type AddressUser struct {
	thing.BaseModel
	Name    string    `db:"name"`
	Address Address   `db:"address"`
	BornAt  time.Time `db:"born_at"`
}

func TestAddressUserWithJSON(t *testing.T) {
	dbAdapter, err := sqlite.NewSQLiteAdapter(":memory:")
	if err != nil {
		t.Fatalf("failed to create sqlite adapter: %v", err)
	}
	thing.Configure(dbAdapter, nil)
	err = thing.AutoMigrate(&AddressUser{})
	if err != nil {
		t.Fatalf("AutoMigrate failed: %v", err)
	}

	userThing, err := thing.New[*AddressUser](dbAdapter, nil)
	if err != nil {
		t.Fatalf("thing.New failed: %v", err)
	}

	addr := Address{Province: "浙江", City: "杭州"}
	born := time.Date(2000, 1, 2, 3, 4, 5, 0, time.UTC)
	u := &AddressUser{
		Name:    "张三",
		Address: addr,
		BornAt:  born,
	}
	err = userThing.Save(u)
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	if u.ID == 0 {
		t.Fatalf("Save did not set ID")
	}

	u2, err := userThing.ByID(u.ID)
	if err != nil {
		t.Fatalf("ByID failed: %v", err)
	}
	if u2.Name != u.Name {
		t.Errorf("Name mismatch: got %s, want %s", u2.Name, u.Name)
	}
	if u2.Address != addr {
		t.Errorf("Address mismatch: got %+v, want %+v", u2.Address, addr)
	}
	if !u2.BornAt.Equal(born) {
		t.Errorf("BornAt mismatch: got %v, want %v", u2.BornAt, born)
	}

	u3, err := userThing.ByID(u.ID)
	if err != nil {
		t.Fatalf("ByID (cache) failed: %v", err)
	}
	if u3.Address != addr {
		t.Errorf("Address (cache) mismatch: got %+v, want %+v", u3.Address, addr)
	}
	if !u3.BornAt.Equal(born) {
		t.Errorf("BornAt (cache) mismatch: got %v, want %v", u3.BornAt, born)
	}

	// 断言缓存命中次数大于 0，验证确实命中缓存
	stats := userThing.CacheStats(context.Background())
	cacheHit := stats.Counters["GetModelHit"]
	if cacheHit == 0 {
		t.Errorf("Expected cache hit for GetModelHit, got 0. Stats: %+v", stats.Counters)
	}
	// 可选：输出缓存统计信息
	t.Logf("Cache stats: %+v", stats.Counters)
}
