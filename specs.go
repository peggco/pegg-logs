package main

import (
	"encoding/json"

	peggplugin "github.com/peggco/pegg/pkg/plugin"
)

const toolBundle = "logs"

func toolSpecs() []peggplugin.ToolSpec {
	return []peggplugin.ToolSpec{
		spec("logs_search",
			"Search Pegg system logs by level, text, and time range.",
			mergeProps(filterProps(), props{
				"limit": integer("Maximum rows to return (default 100, max 1000)"),
				"page":  integer("Page number starting at 1"),
				"sort":  enum("Sort order", "desc", "asc"),
			})),
		spec("logs_stats",
			"Count logs by level and day for a filtered range.",
			filterProps()),
		writeSpec("logs_export",
			"Export filtered logs to CSV or JSON.",
			mergeProps(filterProps(), props{
				"format": enum("Export format", "csv", "json"),
				"path":   str("Destination file (default .pegg/logs/logs-<timestamp>.<format>)"),
			})),
	}
}

type props map[string]any

func spec(name, description string, properties props, required ...string) peggplugin.ToolSpec {
	return buildSpec(name, description, properties, true, required...)
}

func writeSpec(name, description string, properties props, required ...string) peggplugin.ToolSpec {
	return buildSpec(name, description, properties, false, required...)
}

func buildSpec(name, description string, properties props, readOnly bool, required ...string) peggplugin.ToolSpec {
	if properties == nil {
		properties = props{}
	}
	payload := map[string]any{"type": "object", "properties": properties}
	if len(required) > 0 {
		payload["required"] = required
	}
	encoded, _ := json.Marshal(payload)
	return peggplugin.ToolSpec{
		Name:        name,
		Description: description,
		Parameters:  string(encoded),
		Bundle:      toolBundle,
		Group:       "Logs",
		ReadOnly:    readOnly,
	}
}

func filterProps() props {
	return props{
		"levels": array("Levels to include", enum("Log level", "DEBUG", "INFO", "WARN", "ERROR")),
		"query":  str("Case-insensitive text to find in the message"),
		"from":   str("Start of the range (RFC3339 or YYYY-MM-DD)"),
		"to":     str("End of the range (RFC3339 or YYYY-MM-DD)"),
		"days":   integer("Lookback days when from/to are omitted (default from config)"),
	}
}

func permissionRules() map[string]string {
	return map[string]string{
		"logs_search": "allow",
		"logs_stats":  "allow",
		"logs_export": "semi-judge",
	}
}

func mergeProps(base props, extra props) props {
	merged := props{}
	for key, value := range base {
		merged[key] = value
	}
	for key, value := range extra {
		merged[key] = value
	}
	return merged
}

func str(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}

func enum(description string, values ...string) map[string]any {
	return map[string]any{"type": "string", "description": description, "enum": values}
}

func integer(description string) map[string]any {
	return map[string]any{"type": "integer", "description": description}
}

func array(description string, items map[string]any) map[string]any {
	return map[string]any{"type": "array", "description": description, "items": items}
}
