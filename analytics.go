package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Stats struct {
	Total  int64            `json:"total"`
	Levels map[string]int64 `json:"levels"`
	Days   []DayCount       `json:"days"`
	DBPath string           `json:"db_path,omitempty"`
}

type Meta struct {
	DBPath        string `json:"db_path"`
	DBSize        int64  `json:"db_size"`
	Total         int64  `json:"total"`
	LastTimestamp string `json:"last_timestamp,omitempty"`
	DefaultDays   int    `json:"default_range_days"`
	DefaultLimit  int    `json:"default_limit"`
}

type ExportResult struct {
	Format string `json:"format"`
	Path   string `json:"path"`
	Rows   int    `json:"rows"`
}

func buildStats(ctx context.Context, client *Client, rf ResolvedFilters) (Stats, error) {
	levels, total, err := client.levelCounts(ctx, rf)
	if err != nil {
		return Stats{}, err
	}
	days, err := client.dailyCounts(ctx, rf)
	if err != nil {
		return Stats{}, err
	}
	return Stats{Total: total, Levels: levels, Days: days, DBPath: client.path}, nil
}

func buildMeta(ctx context.Context, client *Client, settings Settings) (Meta, error) {
	meta := Meta{
		DBPath:       client.path,
		DefaultDays:  settings.DefaultRangeDays,
		DefaultLimit: settings.DefaultLimit,
	}
	if info, err := os.Stat(client.path); err == nil {
		meta.DBSize = info.Size()
	}
	levels, total, err := client.levelCounts(ctx, ResolvedFilters{})
	if err != nil {
		return Meta{}, err
	}
	meta.Total = total
	_ = levels
	if latest, err := client.latest(ctx); err == nil && latest != nil {
		meta.LastTimestamp = formatTime(latest.Timestamp)
	}
	return meta, nil
}

func exportLogs(ctx context.Context, client *Client, rf ResolvedFilters, format, path string) (ExportResult, error) {
	format = strings.ToLower(strings.TrimSpace(format))
	if format == "" {
		format = "json"
	}
	if format != "csv" && format != "json" {
		return ExportResult{}, fmt.Errorf("invalid format %q: use csv or json", format)
	}
	exportFilter := rf
	exportFilter.Limit = 100000
	exportFilter.Offset = 0
	rows, total, err := client.queryLogs(ctx, exportFilter)
	if err != nil {
		return ExportResult{}, err
	}
	if total > len(rows) {
		rows = rows[:len(rows)]
	}
	if strings.TrimSpace(path) == "" {
		ext := "." + format
		path = filepath.Join(client.settings.exportDir(), "logs-"+time.Now().Format("20060102-150405")+ext)
	} else if !filepath.IsAbs(path) {
		path = filepath.Join(client.settings.Workdir, path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return ExportResult{}, fmt.Errorf("create export directory: %w", err)
	}

	switch format {
	case "csv":
		if err := writeLogsCSV(path, rows); err != nil {
			return ExportResult{}, err
		}
	case "json":
		if err := writeLogsJSON(path, rows); err != nil {
			return ExportResult{}, err
		}
	}
	return ExportResult{Format: format, Path: path, Rows: len(rows)}, nil
}

func writeLogsCSV(path string, rows []LogRow) error {
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create export: %w", err)
	}
	defer file.Close()
	writer := csv.NewWriter(file)
	if err := writer.Write([]string{"timestamp", "level", "message"}); err != nil {
		return err
	}
	for _, row := range rows {
		if err := writer.Write([]string{formatTime(row.Timestamp), row.Level, row.Message}); err != nil {
			return err
		}
	}
	writer.Flush()
	return writer.Error()
}

func writeLogsJSON(path string, rows []LogRow) error {
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create export: %w", err)
	}
	defer file.Close()
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	return encoder.Encode(rows)
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.Local().Format("2006-01-02 15:04:05")
}

func formatCount(value int64) string {
	switch {
	case value >= 1_000_000:
		return strconv.FormatFloat(float64(value)/1_000_000, 'f', 1, 64) + "M"
	case value >= 1_000:
		return strconv.FormatFloat(float64(value)/1_000, 'f', 1, 64) + "k"
	default:
		return strconv.FormatInt(value, 10)
	}
}

func formatBytes(value int64) string {
	const unit = 1024
	if value < unit {
		return fmt.Sprintf("%d B", value)
	}
	div, exp := int64(unit), 0
	for n := value / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(value)/float64(div), "KMGTPE"[exp])
}
