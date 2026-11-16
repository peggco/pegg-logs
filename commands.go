package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	peggplugin "github.com/peggco/pegg/pkg/plugin"
)

func commandSpecs() []peggplugin.CommandSpec {
	return []peggplugin.CommandSpec{{
		Use:   "logview",
		Short: "Search and inspect Pegg system logs",
		Children: []peggplugin.CommandSpec{
			{Use: "status", Short: "Show logs database and dashboard status"},
			{Use: "open", Short: "Start the logs dashboard and open it"},
			{Use: "start", Short: "Start the logs dashboard without opening a browser"},
			{Use: "stop", Short: "Stop the background dashboard"},
			{Use: "search", Short: "Search logs", Flags: filterFlags()},
			{Use: "export", Short: "Export filtered logs", Flags: append(filterFlags(),
				peggplugin.FlagSpec{Name: "format", Usage: "csv or json"},
				peggplugin.FlagSpec{Name: "path", Usage: "Destination file"},
			)},
			{Use: "config", Short: "Print the resolved logs configuration"},
		},
	}}
}

func filterFlags() []peggplugin.FlagSpec {
	return []peggplugin.FlagSpec{
		{Name: "level", Usage: "DEBUG, INFO, WARN, ERROR (repeatable or comma-separated)"},
		{Name: "query", Usage: "Text to find in the message"},
		{Name: "from", Usage: "Start date (RFC3339 or YYYY-MM-DD)"},
		{Name: "to", Usage: "End date (RFC3339 or YYYY-MM-DD)"},
		{Name: "days", Usage: "Lookback days"},
		{Name: "limit", Usage: "Maximum rows"},
		{Name: "page", Usage: "Page number"},
		{Name: "sort", Usage: "desc or asc"},
	}
}

func runCommand(ctx context.Context, settings Settings, req peggplugin.CommandRequest) (string, error) {
	if len(req.Path) == 0 {
		return "", fmt.Errorf("missing subcommand")
	}
	sub := req.Path[0]
	if (sub == pluginName || sub == "logview") && len(req.Path) > 1 {
		sub = req.Path[1]
	}
	switch sub {
	case "status":
		return commandStatus(ctx, settings)
	case "open":
		url, err := commandStart(settings, true)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Logs dashboard: %s\n", url), nil
	case "start":
		url, err := commandStart(settings, false)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Logs dashboard: %s\n", url), nil
	case "stop":
		return commandStop(settings)
	case "search":
		return commandSearch(ctx, settings, req.Flags)
	case "export":
		return commandExport(ctx, settings, req.Flags)
	case "config":
		return redactSettings(configSummary(settings), settings), nil
	default:
		return "", fmt.Errorf("unknown command %q; use status, open, start, stop, search, export or config", sub)
	}
}

func commandStatus(ctx context.Context, settings Settings) (string, error) {
	settings.normalize()
	var b strings.Builder
	fmt.Fprintf(&b, "Database: %s\n", settings.dbPath())
	client, err := newClient(settings)
	if err != nil {
		fmt.Fprintf(&b, "Status: unavailable (%v)\n", err)
	} else {
		meta, metaErr := buildMeta(ctx, client, settings)
		if metaErr != nil {
			fmt.Fprintf(&b, "Status: error (%v)\n", metaErr)
		} else {
			fmt.Fprintf(&b, "Size: %s\n", formatBytes(meta.DBSize))
			fmt.Fprintf(&b, "Rows: %d\n", meta.Total)
			if meta.LastTimestamp != "" {
				fmt.Fprintf(&b, "Latest: %s\n", meta.LastTimestamp)
			}
		}
		client.Close()
	}
	if url := runningServerURL(settings); url != "" {
		fmt.Fprintf(&b, "Dashboard: %s\n", url)
	} else {
		b.WriteString("Dashboard: stopped\n")
	}
	return b.String(), nil
}

func commandSearch(ctx context.Context, settings Settings, flags map[string]string) (string, error) {
	client, err := newClient(settings)
	if err != nil {
		return "", err
	}
	defer client.Close()
	filters := filtersFromFlags(flags)
	rf, err := resolveFilters(settings, filters, time.Now())
	if err != nil {
		return "", err
	}
	rows, total, err := client.queryLogs(ctx, rf)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d rows (showing %d, page %d)\n", total, len(rows), rf.Page)
	for _, row := range rows {
		fmt.Fprintf(&b, "%s %-5s %s\n", formatTime(row.Timestamp), row.Level, row.Message)
	}
	return b.String(), nil
}

func commandExport(ctx context.Context, settings Settings, flags map[string]string) (string, error) {
	client, err := newClient(settings)
	if err != nil {
		return "", err
	}
	defer client.Close()
	rf, err := resolveFilters(settings, filtersFromFlags(flags), time.Now())
	if err != nil {
		return "", err
	}
	result, err := exportLogs(ctx, client, rf, flags["format"], flags["path"])
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Exported %d rows to %s (%s)\n", result.Rows, result.Path, result.Format), nil
}

func filtersFromFlags(flags map[string]string) Filters {
	limit, _ := strconv.Atoi(strings.TrimSpace(flags["limit"]))
	page, _ := strconv.Atoi(strings.TrimSpace(flags["page"]))
	days, _ := strconv.Atoi(strings.TrimSpace(flags["days"]))
	return Filters{
		Levels: splitLevels(flags["level"]),
		Query:  strings.TrimSpace(flags["query"]),
		From:   strings.TrimSpace(flags["from"]),
		To:     strings.TrimSpace(flags["to"]),
		Days:   days,
		Limit:  limit,
		Page:   page,
		Sort:   strings.TrimSpace(flags["sort"]),
	}
}

func splitLevels(value string) []string {
	var out []string
	for _, part := range strings.Split(value, ",") {
		part = strings.ToUpper(strings.TrimSpace(part))
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func configSummary(settings Settings) string {
	data := settings.sanitized()
	keys := []string{"enabled", "db_path", "default_range_days", "default_limit", "request_timeout_seconds", "dashboard_token"}
	var b strings.Builder
	for _, key := range keys {
		fmt.Fprintf(&b, "%-24s %v\n", key+":", data[key])
	}
	return b.String()
}

func parseFlags(args []string) ([]string, map[string]string) {
	flags := map[string]string{}
	var positional []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "--") {
			positional = append(positional, arg)
			continue
		}
		name := strings.TrimPrefix(arg, "--")
		if name == "" {
			continue
		}
		if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
			flags[name] = args[i+1]
			i++
			continue
		}
		flags[name] = "true"
	}
	return positional, flags
}

func runCLI(args []string) int {
	settings := defaultSettings()
	settings.applyEnv()
	settings.normalize()
	if len(args) == 0 {
		fmt.Println("usage: pegg-logs <status|open|start|stop|search|export|config> [flags]")
		return 2
	}
	sub := args[0]
	if sub == "serve" {
		positional, flags := parseFlags(args[1:])
		_ = positional
		if err := runServe(settings, flags); err != nil {
			fmt.Fprintln(os.Stderr, redactSettings(err.Error(), settings))
			return 1
		}
		return 0
	}
	_, flags := parseFlags(args[1:])
	ctx := context.Background()
	output, err := runCommand(ctx, settings, peggplugin.CommandRequest{Path: []string{sub}, Flags: flags})
	if err != nil {
		fmt.Fprintln(os.Stderr, redactSettings(err.Error(), settings))
		return 1
	}
	fmt.Print(output)
	return 0
}
