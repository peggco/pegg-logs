package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	peggplugin "github.com/peggco/pegg/pkg/plugin"
)

const tuiPageID = "logs"

type tuiReport struct {
	stats  Stats
	recent []LogRow
	err    error
}

func loadTUIReport(settings Settings) tuiReport {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client, err := newClient(settings)
	if err != nil {
		return tuiReport{err: err}
	}
	defer client.Close()
	rf, err := resolveFilters(settings, Filters{Days: settings.DefaultRangeDays}, time.Now())
	if err != nil {
		return tuiReport{err: err}
	}
	stats, err := buildStats(ctx, client, rf)
	if err != nil {
		return tuiReport{err: err}
	}
	recent, _, err := client.queryLogs(ctx, ResolvedFilters{Limit: 8, Sort: "desc"})
	if err != nil {
		return tuiReport{err: err}
	}
	return tuiReport{stats: stats, recent: recent}
}

func tuiPage(settings Settings) peggplugin.TUIPage {
	report := loadTUIReport(settings)
	serverState := "stopped"
	serverBadge := "down"
	if url := runningServerURL(settings); url != "" {
		serverState = url
		serverBadge = "up"
	}
	metrics := []peggplugin.TUIMetric{
		{Label: "Total", Value: formatCount(report.stats.Total)},
		{Label: "Errors", Value: formatCount(report.stats.Levels["ERROR"])},
		{Label: "Warnings", Value: formatCount(report.stats.Levels["WARN"])},
		{Label: "Info", Value: formatCount(report.stats.Levels["INFO"])},
		{Label: "Debug", Value: formatCount(report.stats.Levels["DEBUG"])},
	}
	note := ""
	if report.err != nil {
		note = report.err.Error()
	}
	return peggplugin.TUIPage{
		ID:          tuiPageID,
		Title:       "Logs",
		Description: "Search Pegg system logs from the local SQLite log database.",
		Owner:       pluginName,
		Layout:      "list",
		Metrics:     metrics,
		Sections: []peggplugin.TUISection{
			{
				Title: "Recent logs",
				Kind:  "list",
				Items: logItems(report.recent),
			},
			{
				Title: "Dashboard",
				Kind:  "list",
				Items: []peggplugin.TUIItem{
					{ID: "server", Title: "Status", Detail: serverState, Badge: serverBadge},
					{ID: "db", Title: "Database", Detail: settings.dbPath()},
				},
				Note: note,
			},
		},
		Actions: []peggplugin.TUIAction{
			{ID: "start", Label: "Start", Description: "Start the local logs dashboard without opening a browser."},
			{ID: "open", Label: "Open", Description: "Start the dashboard if needed and open it in a browser."},
			{ID: "stop", Label: "Stop", Description: "Stop the background dashboard."},
			{ID: "reload", Label: "Reload", Description: "Reload logs and counts from the database."},
			{ID: "export_csv", Label: "Export CSV", Description: "Write the filtered logs to .pegg/logs."},
		},
		Settings: []peggplugin.TUIField{
			{ID: "db_path", Label: "Database path", Value: settings.DBPath, Placeholder: defaultDBPath},
			{ID: "default_range_days", Label: "Default range (days)", Value: strconv.Itoa(settings.DefaultRangeDays)},
			{ID: "default_limit", Label: "Default page size", Value: strconv.Itoa(settings.DefaultLimit)},
			{ID: "request_timeout_seconds", Label: "Timeout (seconds)", Value: strconv.Itoa(settings.RequestTimeoutSeconds)},
		},
		SaveAction: "settings.save",
		Footer:     "Read-only and local only. The dashboard binds 127.0.0.1 with a random token.",
	}
}

func logItems(rows []LogRow) []peggplugin.TUIItem {
	if len(rows) == 0 {
		return []peggplugin.TUIItem{{ID: "empty", Title: "No logs", Detail: "Nothing in the selected range.", Disabled: true}}
	}
	items := make([]peggplugin.TUIItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, peggplugin.TUIItem{
			ID:     strconv.FormatInt(row.ID, 10),
			Title:  formatTime(row.Timestamp),
			Detail: row.Message,
			Badge:  row.Level,
		})
	}
	return items
}

func (p *Plugin) executeTUIAction(req peggplugin.TUIActionRequest) (*peggplugin.TUIActionResult, error) {
	if req.PageID != "" && req.PageID != tuiPageID {
		return nil, fmt.Errorf("unknown logs TUI page %q", req.PageID)
	}
	settings := p.currentSettings()
	var output string
	var err error
	switch strings.TrimSpace(req.ActionID) {
	case "settings.save":
		output, err = p.saveSettings(req.Values)
	case "start":
		var url string
		url, err = commandStart(settings, false)
		if err == nil {
			output = fmt.Sprintf("Logs dashboard: %s", url)
		}
	case "open":
		var url string
		url, err = commandStart(settings, true)
		if err == nil {
			output = fmt.Sprintf("Opened %s", url)
		}
	case "stop":
		output, err = commandStop(settings)
	case "reload":
		output = "Reloaded"
	case "export_csv":
		output, err = exportFromTUI(settings)
	default:
		return nil, fmt.Errorf("unknown logs action %q", req.ActionID)
	}
	if err != nil {
		return nil, err
	}
	page := tuiPage(p.currentSettings())
	return &peggplugin.TUIActionResult{Output: output, Page: &page}, nil
}

func exportFromTUI(settings Settings) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	client, err := newClient(settings)
	if err != nil {
		return "", err
	}
	defer client.Close()
	rf, err := resolveFilters(settings, Filters{}, time.Now())
	if err != nil {
		return "", err
	}
	result, err := exportLogs(ctx, client, rf, "csv", "")
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Exported %d rows to %s", result.Rows, result.Path), nil
}

func (p *Plugin) saveSettings(values map[string]string) (string, error) {
	settings := p.currentSettings()
	if value, ok := values["db_path"]; ok && strings.TrimSpace(value) != "" {
		settings.DBPath = strings.TrimSpace(value)
	}
	if value, ok := values["default_range_days"]; ok {
		if n, err := parseInt(value); err == nil && n > 0 {
			settings.DefaultRangeDays = n
		}
	}
	if value, ok := values["default_limit"]; ok {
		if n, err := parseInt(value); err == nil && n > 0 {
			settings.DefaultLimit = n
		}
	}
	if value, ok := values["request_timeout_seconds"]; ok {
		if n, err := parseInt(value); err == nil && n > 0 {
			settings.RequestTimeoutSeconds = n
		}
	}
	settings.normalize()
	p.mu.Lock()
	p.settings = settings
	p.mu.Unlock()
	path, err := persistSettings(settings)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Settings saved to %s", path), nil
}
