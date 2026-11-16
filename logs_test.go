package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestQueryLogsAndStats(t *testing.T) {
	path := newTestDB(t)
	settings := testSettings(path)
	client, err := newClient(settings)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	rf, err := resolveFilters(settings, Filters{Levels: []string{"ERROR", "WARN"}}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	rows, total, err := client.queryLogs(context.Background(), rf)
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || len(rows) != 2 {
		t.Fatalf("total=%d rows=%d", total, len(rows))
	}
	if rows[0].Level != "ERROR" && rows[1].Level != "ERROR" {
		t.Fatalf("rows = %#v", rows)
	}

	stats, err := buildStats(context.Background(), client, rf)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Total != 2 || stats.Levels["WARN"] != 1 || stats.Levels["ERROR"] != 1 {
		t.Fatalf("stats = %#v", stats)
	}
}

func TestExportCSV(t *testing.T) {
	path := newTestDB(t)
	settings := testSettings(path)
	client, err := newClient(settings)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	rf, err := resolveFilters(settings, Filters{}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "logs.csv")
	result, err := exportLogs(context.Background(), client, rf, "csv", out)
	if err != nil {
		t.Fatal(err)
	}
	if result.Rows != 4 {
		t.Fatalf("rows = %d", result.Rows)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Fatal("empty export")
	}
}

func TestDashboardLogsAPI(t *testing.T) {
	path := newTestDB(t)
	settings := testSettings(path)
	settings.Token = "test-token"
	dashboard, err := newDashboard(settings)
	if err != nil {
		t.Fatal(err)
	}
	if err := dashboard.Start(); err != nil {
		t.Fatal(err)
	}
	defer dashboard.Stop(context.Background())

	url := dashboard.URL() + "/api/logs?level=ERROR&days=7&token=test-token"
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var payload struct {
		Total int      `json:"total"`
		Logs  []LogRow `json:"logs"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.Total != 1 || len(payload.Logs) != 1 || payload.Logs[0].Level != "ERROR" {
		t.Fatalf("payload = %#v", payload)
	}

	unauthorized := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/logs", nil)
	dashboard.handler().ServeHTTP(unauthorized, req)
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d", unauthorized.Code)
	}
}
