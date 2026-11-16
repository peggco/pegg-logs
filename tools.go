package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

func executeTool(ctx context.Context, client *Client, name, raw string) (string, error) {
	switch name {
	case "logs_search":
		return toolSearch(ctx, client, raw)
	case "logs_stats":
		return toolStats(ctx, client, raw)
	case "logs_export":
		return toolExport(ctx, client, raw)
	default:
		return "", fmt.Errorf("unknown logs tool %q", name)
	}
}

func toolSearch(ctx context.Context, client *Client, raw string) (string, error) {
	var args Filters
	if err := decodeToolArgs(raw, &args); err != nil {
		return "", err
	}
	rf, err := resolveFilters(client.settings, args, time.Now())
	if err != nil {
		return "", err
	}
	rows, total, err := client.queryLogs(ctx, rf)
	if err != nil {
		return "", err
	}
	return jsonOutput(map[string]any{
		"total":  total,
		"page":   rf.Page,
		"limit":  rf.Limit,
		"sort":   rf.Sort,
		"levels": rf.Levels,
		"query":  rf.Query,
		"logs":   rows,
	})
}

func toolStats(ctx context.Context, client *Client, raw string) (string, error) {
	var args Filters
	if err := decodeToolArgs(raw, &args); err != nil {
		return "", err
	}
	rf, err := resolveFilters(client.settings, args, time.Now())
	if err != nil {
		return "", err
	}
	stats, err := buildStats(ctx, client, rf)
	if err != nil {
		return "", err
	}
	return jsonOutput(stats)
}

func toolExport(ctx context.Context, client *Client, raw string) (string, error) {
	var args struct {
		Filters
		Format string `json:"format"`
		Path   string `json:"path"`
	}
	if err := decodeToolArgs(raw, &args); err != nil {
		return "", err
	}
	rf, err := resolveFilters(client.settings, args.Filters, time.Now())
	if err != nil {
		return "", err
	}
	result, err := exportLogs(ctx, client, rf, args.Format, args.Path)
	if err != nil {
		return "", err
	}
	return jsonOutput(result)
}

func decodeToolArgs(raw string, dst any) error {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "null" {
		return nil
	}
	if err := json.Unmarshal([]byte(raw), dst); err != nil {
		return fmt.Errorf("decode arguments: %w", err)
	}
	return nil
}

func jsonOutput(value any) (string, error) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}
