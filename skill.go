package main

import (
	peggplugin "github.com/peggco/pegg/pkg/plugin"
)

func skillSpec() peggplugin.SkillSpec {
	return peggplugin.SkillSpec{
		Name:         "logs",
		Description:  "Search and summarize Pegg system logs from the local SQLite log database.",
		WhenToUse:    "Use when the user asks what Pegg logged, wants recent errors or warnings, needs to find a message in the logs, or wants a log export or dashboard.",
		ArgumentHint: "[query or level]",
		AllowedTools: []string{
			"logs_search",
			"logs_stats",
			"logs_export",
		},
		ToolBundles: []string{toolBundle},
		Instructions: `Use the logs tools to inspect the local Pegg system log database.

Guidelines:
1. Start with logs_search for a specific question. Use levels, query, and days/from/to to narrow it.
2. Use logs_stats for counts by level and day before showing large result sets.
3. Use logs_export only when the user wants a CSV or JSON file. Report the returned path.
4. Shared filters: levels (DEBUG, INFO, WARN, ERROR), query, from, to, days, limit, page, sort.
5. Default to errors and warnings when the user asks "what went wrong"; otherwise start with the default range.
6. Keep output focused: summarize counts and show only the most relevant log lines.
7. The database is opened read-only. Never claim to have changed or deleted logs.`,
	}
}
