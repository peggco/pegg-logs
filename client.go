package main

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Client struct {
	db       *sql.DB
	path     string
	timeout  time.Duration
	settings Settings
}

type LogRow struct {
	ID        int64     `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	Level     string    `json:"level"`
	Message   string    `json:"message"`
}

type DayCount struct {
	Day   string `json:"day"`
	Count int64  `json:"count"`
}

func newClient(s Settings) (*Client, error) {
	s.normalize()
	path := s.dbPath()
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("logs database not found at %s; set db_path or PEGG_LOGS_DB", path)
		}
		return nil, fmt.Errorf("stat logs database %s: %w", path, err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("logs database %s is a directory", path)
	}

	db, err := sql.Open("sqlite", readOnlyDSN(path))
	if err != nil {
		return nil, fmt.Errorf("open logs database %s: %w", path, err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	ctx, cancel := context.WithTimeout(context.Background(), s.timeout())
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("open logs database %s: %w", path, err)
	}
	return &Client{db: db, path: path, timeout: s.timeout(), settings: s}, nil
}

func readOnlyDSN(path string) string {
	escaped := (&url.URL{Path: filepath.ToSlash(path)}).EscapedPath()
	return "file:" + escaped + "?mode=ro&_pragma=query_only(1)&_pragma=busy_timeout(5000)"
}

func (c *Client) Close() error {
	if c == nil || c.db == nil {
		return nil
	}
	return c.db.Close()
}

func truncateOutput(value string, limit int) string {
	if limit <= 0 {
		limit = 60 << 10
	}
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "\n... (truncated)"
}

func (c *Client) queryLogs(ctx context.Context, rf ResolvedFilters) ([]LogRow, int, error) {
	where, args := buildWhere(rf)
	var total int
	if err := c.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM logs WHERE "+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count logs: %w", err)
	}

	order := "DESC"
	if rf.Sort == "asc" {
		order = "ASC"
	}
	query := "SELECT id, timestamp, level, message FROM logs WHERE " + where +
		" ORDER BY timestamp " + order + ", id " + order + " LIMIT ? OFFSET ?"
	selectArgs := append(append([]any(nil), args...), rf.Limit, rf.Offset)

	rows, err := c.db.QueryContext(ctx, query, selectArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("query logs: %w", err)
	}
	defer rows.Close()

	result := make([]LogRow, 0, rf.Limit)
	for rows.Next() {
		var row LogRow
		var timestamp string
		if err := rows.Scan(&row.ID, &timestamp, &row.Level, &row.Message); err != nil {
			return nil, 0, fmt.Errorf("scan log: %w", err)
		}
		row.Timestamp = parseLogTime(timestamp)
		row.Level = strings.ToUpper(strings.TrimSpace(row.Level))
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("read logs: %w", err)
	}
	return result, total, nil
}

func (c *Client) levelCounts(ctx context.Context, rf ResolvedFilters) (map[string]int64, int64, error) {
	where, args := buildWhere(rf)
	rows, err := c.db.QueryContext(ctx, "SELECT level, COUNT(*) FROM logs WHERE "+where+" GROUP BY level", args...)
	if err != nil {
		return nil, 0, fmt.Errorf("count levels: %w", err)
	}
	defer rows.Close()

	counts := map[string]int64{}
	var total int64
	for rows.Next() {
		var level string
		var count int64
		if err := rows.Scan(&level, &count); err != nil {
			return nil, 0, fmt.Errorf("scan level count: %w", err)
		}
		level = strings.ToUpper(strings.TrimSpace(level))
		counts[level] = count
		total += count
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("read level counts: %w", err)
	}
	return counts, total, nil
}

func (c *Client) dailyCounts(ctx context.Context, rf ResolvedFilters) ([]DayCount, error) {
	where, args := buildWhere(rf)
	rows, err := c.db.QueryContext(ctx,
		"SELECT substr(timestamp, 1, 10) AS day, COUNT(*) FROM logs WHERE "+where+
			" GROUP BY day ORDER BY day ASC", args...)
	if err != nil {
		return nil, fmt.Errorf("count days: %w", err)
	}
	defer rows.Close()

	var result []DayCount
	for rows.Next() {
		var row DayCount
		if err := rows.Scan(&row.Day, &row.Count); err != nil {
			return nil, fmt.Errorf("scan day count: %w", err)
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read day counts: %w", err)
	}
	return result, nil
}

func (c *Client) latest(ctx context.Context) (*LogRow, error) {
	rows, total, err := c.queryLogs(ctx, ResolvedFilters{Limit: 1, Sort: "desc"})
	if err != nil || total == 0 || len(rows) == 0 {
		return nil, err
	}
	return &rows[0], nil
}

func buildWhere(rf ResolvedFilters) (string, []any) {
	var clauses []string
	var args []any
	if len(rf.Levels) > 0 {
		placeholders := make([]string, len(rf.Levels))
		for i, level := range rf.Levels {
			placeholders[i] = "?"
			args = append(args, level)
		}
		clauses = append(clauses, "level IN ("+strings.Join(placeholders, ",")+")")
	}
	if rf.Query != "" {
		clauses = append(clauses, "message LIKE ? ESCAPE '\\'")
		args = append(args, "%"+escapeLike(rf.Query)+"%")
	}
	if rf.From != nil {
		clauses = append(clauses, "timestamp >= ?")
		args = append(args, formatSQLTime(*rf.From))
	}
	if rf.To != nil {
		clauses = append(clauses, "timestamp <= ?")
		args = append(args, formatSQLTime(*rf.To))
	}
	if len(clauses) == 0 {
		return "1=1", args
	}
	return strings.Join(clauses, " AND "), args
}

func escapeLike(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, "%", `\%`)
	value = strings.ReplaceAll(value, "_", `\_`)
	return value
}

func formatSQLTime(value time.Time) string {
	return value.Format("2006-01-02 15:04:05.999999999")
}

var logTimeLayouts = []string{
	"2006-01-02 15:04:05.999999999 -0700 MST",
	"2006-01-02 15:04:05.999999999 -0700",
	"2006-01-02 15:04:05 -0700 MST",
	"2006-01-02 15:04:05",
	time.RFC3339Nano,
}

func parseLogTime(value string) time.Time {
	value = strings.TrimSpace(value)
	if idx := strings.Index(value, " m="); idx >= 0 {
		value = value[:idx]
	}
	for _, layout := range logTimeLayouts {
		if parsed, err := time.ParseInLocation(layout, value, time.Local); err == nil {
			return parsed
		}
	}
	return time.Time{}
}
