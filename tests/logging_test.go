package thing_test

import (
	"bytes"
	"context"
	"log"
	"strings"
	"testing"

	"github.com/burugo/thing"
	"github.com/stretchr/testify/require"
)

func TestDefaultLoggingSuppressesSQLLogs(t *testing.T) {
	var buf bytes.Buffer
	var stdLog bytes.Buffer
	originalStdLogWriter := log.Writer()
	log.SetOutput(&stdLog)
	thing.SetLogOutput(&buf)
	thing.SetLogLevel(thing.LogDefault)
	t.Cleanup(func() {
		log.SetOutput(originalStdLogWriter)
		thing.SetLogger(nil)
		thing.SetLogLevel(thing.LogDefault)
	})

	dbAdapter, _, cleanup := setupTestDB(t)
	defer cleanup()

	_, err := dbAdapter.Exec(context.Background(), "INSERT INTO users (name, email) VALUES (?, ?)", "Silent", "silent@example.com")
	require.NoError(t, err)
	require.NotContains(t, buf.String(), "DB Exec:")
	require.NotContains(t, stdLog.String(), "DB Exec:")
	require.False(t, strings.Contains(buf.String(), "INSERT INTO users"), buf.String())
	require.False(t, strings.Contains(stdLog.String(), "INSERT INTO users"), stdLog.String())
}

func TestDebugLoggingEmitsSQLLogs(t *testing.T) {
	var buf bytes.Buffer
	thing.SetLogOutput(&buf)
	thing.SetLogLevel(thing.LogDebug)
	t.Cleanup(func() {
		thing.SetLogger(nil)
		thing.SetLogLevel(thing.LogDefault)
	})

	dbAdapter, _, cleanup := setupTestDB(t)
	defer cleanup()

	_, err := dbAdapter.Exec(context.Background(), "INSERT INTO users (name, email) VALUES (?, ?)", "Debug", "debug@example.com")
	require.NoError(t, err)
	require.Contains(t, buf.String(), "DB Exec:")
	require.Contains(t, buf.String(), "INSERT INTO users")
}

func TestSilentLoggingSuppressesWarnings(t *testing.T) {
	var buf bytes.Buffer
	thing.SetLogOutput(&buf)
	thing.SetLogLevel(thing.LogSilent)
	t.Cleanup(func() {
		thing.SetLogger(nil)
		thing.SetLogLevel(thing.LogDefault)
	})

	thingUser, _, _, cleanup := setupCacheTest[*User](t)
	defer cleanup()

	_, err := thingUser.Query(thing.QueryParams{
		Where: "LOWER(name) = ?",
		Args:  []interface{}{"silent degrade"},
	}).Fetch(0, 10)
	require.NoError(t, err)

	user := &User{Name: "Silent Degrade", Email: "silent-degrade@example.com"}
	require.NoError(t, thingUser.Save(user))
	require.Empty(t, buf.String())
}

func TestConfigureWithConfigAppliesLoggerAndLogLevel(t *testing.T) {
	var buf bytes.Buffer
	t.Cleanup(func() {
		thing.SetLogger(nil)
		thing.SetLogLevel(thing.LogDefault)
	})

	dbAdapter, cacheClient, cleanup := setupTestDB(t)
	defer cleanup()

	require.NoError(t, thing.ConfigureWithConfig(thing.Config{
		DB:       dbAdapter,
		Cache:    cacheClient,
		Logger:   log.New(&buf, "", 0),
		LogLevel: thing.LogDebug,
	}))

	_, err := dbAdapter.Exec(context.Background(), "INSERT INTO users (name, email) VALUES (?, ?)", "Config", "config@example.com")
	require.NoError(t, err)
	require.Contains(t, buf.String(), "DB Exec:")
}

func TestCheckQueryMatchParseFailureHiddenAtWarn(t *testing.T) {
	var buf bytes.Buffer
	thing.SetLogOutput(&buf)
	thing.SetLogLevel(thing.LogWarn)
	t.Cleanup(func() {
		thing.SetLogger(nil)
		thing.SetLogLevel(thing.LogDefault)
	})

	thingUser, _, _, cleanup := setupCacheTest[*User](t)
	defer cleanup()

	_, err := thingUser.Query(thing.QueryParams{
		Where: "LOWER(name) = ?",
		Args:  []interface{}{"cache degrade"},
	}).Fetch(0, 10)
	require.NoError(t, err)

	user := &User{Name: "Cache Degrade", Email: "cache-degrade@example.com"}
	require.NoError(t, thingUser.Save(user))

	require.NotContains(t, buf.String(), "CheckQueryMatch")
}

func TestCheckQueryMatchParseFailureLogsAtDebugNotError(t *testing.T) {
	var buf bytes.Buffer
	thing.SetLogOutput(&buf)
	thing.SetLogLevel(thing.LogDebug)
	t.Cleanup(func() {
		thing.SetLogger(nil)
		thing.SetLogLevel(thing.LogDefault)
	})

	thingUser, _, _, cleanup := setupCacheTest[*User](t)
	defer cleanup()

	_, err := thingUser.Query(thing.QueryParams{
		Where: "LOWER(name) = ?",
		Args:  []interface{}{"cache debug degrade"},
	}).Fetch(0, 10)
	require.NoError(t, err)

	user := &User{Name: "Cache Debug Degrade", Email: "cache-debug-degrade@example.com"}
	require.NoError(t, thingUser.Save(user))

	logs := buf.String()
	require.Contains(t, logs, "CheckQueryMatch")
	require.NotContains(t, logs, "ERROR CheckQueryMatch")
}

func TestSQLErrorLoggingOmitsSQLAndArgs(t *testing.T) {
	var buf bytes.Buffer
	thing.SetLogOutput(&buf)
	thing.SetLogLevel(thing.LogError)
	t.Cleanup(func() {
		thing.SetLogger(nil)
		thing.SetLogLevel(thing.LogDefault)
	})

	dbAdapter, _, cleanup := setupTestDB(t)
	defer cleanup()

	_, err := dbAdapter.Exec(context.Background(), "INSERT INTO missing_table (name) VALUES (?)", "secret-arg")
	require.Error(t, err)

	logs := buf.String()
	require.Contains(t, logs, "DB Exec Error")
	require.NotContains(t, logs, "INSERT INTO missing_table")
	require.NotContains(t, logs, "secret-arg")
}

func TestSQLQueryErrorLoggingOmitsSQLAndArgs(t *testing.T) {
	var buf bytes.Buffer
	thing.SetLogOutput(&buf)
	thing.SetLogLevel(thing.LogError)
	t.Cleanup(func() {
		thing.SetLogger(nil)
		thing.SetLogLevel(thing.LogDefault)
	})

	dbAdapter, _, cleanup := setupTestDB(t)
	defer cleanup()

	var rows []User
	err := dbAdapter.Select(context.Background(), &rows, "SELECT * FROM missing_table WHERE name = ?", "secret-arg")
	require.Error(t, err)

	logs := buf.String()
	require.Contains(t, logs, "DB Select Query Error")
	require.NotContains(t, logs, "SELECT * FROM missing_table")
	require.NotContains(t, logs, "secret-arg")
}
