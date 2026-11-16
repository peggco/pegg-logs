package main

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func newTestDB(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "logs.db")
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE logs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp DATETIME,
		level TEXT,
		message TEXT
	)`); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	rows := []struct {
		at    time.Time
		level string
		msg   string
	}{
		{now.Add(-2 * time.Hour), "INFO", "server started"},
		{now.Add(-time.Hour), "WARN", "slow request"},
		{now.Add(-30 * time.Minute), "ERROR", "connection refused"},
		{now.Add(-10 * time.Minute), "DEBUG", "cache miss"},
	}
	for _, row := range rows {
		if _, err := db.Exec("INSERT INTO logs (timestamp, level, message) VALUES (?, ?, ?)",
			row.at.Format("2006-01-02 15:04:05.000000000 -0700 MST"), row.level, row.msg); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func testSettings(path string) Settings {
	settings := defaultSettings()
	settings.DBPath = path
	settings.Workdir = filepath.Dir(path)
	settings.DefaultRangeDays = 7
	settings.DefaultLimit = 100
	settings.normalize()
	return settings
}

func TestDefaultSettingsAndEnv(t *testing.T) {
	clearLogsEnv(t)
	settings := defaultSettings()
	settings.normalize()
	if !settings.Enabled || settings.DefaultLimit != 100 || settings.DefaultRangeDays != 7 {
		t.Fatalf("defaults = %+v", settings)
	}
	t.Setenv("PEGG_LOGS_DB", "/tmp/logs.db")
	t.Setenv("PEGG_LOGS_LIMIT", "250")
	t.Setenv("PEGG_LOGS_DISABLED", "true")
	settings.applyEnv()
	if settings.DBPath != "/tmp/logs.db" || settings.DefaultLimit != 250 || settings.Enabled {
		t.Fatalf("env = %+v", settings)
	}
}

func clearLogsEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"PEGG_LOGS_DB", "PEGG_LOGS_RANGE_DAYS", "PEGG_LOGS_LIMIT",
		"PEGG_LOGS_TIMEOUT_SECONDS", "PEGG_LOGS_DISABLED", "PEGG_LOGS_DASHBOARD_TOKEN",
	} {
		t.Setenv(key, "")
	}
}

func TestResolveFilters(t *testing.T) {
	settings := testSettings("/tmp/logs.db")
	now := time.Now()
	rf, err := resolveFilters(settings, Filters{Levels: []string{"error", "WARN"}, Query: "refused", Days: 1}, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(rf.Levels) != 2 || rf.Levels[0] != "ERROR" || rf.Levels[1] != "WARN" {
		t.Fatalf("levels = %#v", rf.Levels)
	}
	if rf.Query != "refused" || rf.From == nil || rf.Limit != 100 || rf.Sort != "desc" {
		t.Fatalf("filters = %#v", rf)
	}
	if _, err := resolveFilters(settings, Filters{Levels: []string{"TRACE"}}, now); err == nil {
		t.Fatal("expected invalid level error")
	}
}
