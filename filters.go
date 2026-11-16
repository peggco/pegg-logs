package main

import (
	"fmt"
	"strings"
	"time"
)

type Filters struct {
	Levels []string `json:"levels,omitempty"`
	Query  string   `json:"query,omitempty"`
	From   string   `json:"from,omitempty"`
	To     string   `json:"to,omitempty"`
	Days   int      `json:"days,omitempty"`
	Limit  int      `json:"limit,omitempty"`
	Page   int      `json:"page,omitempty"`
	Sort   string   `json:"sort,omitempty"`
}

type ResolvedFilters struct {
	Levels []string
	Query  string
	From   *time.Time
	To     *time.Time
	Limit  int
	Page   int
	Offset int
	Sort   string
}

var allowedLevels = []string{"DEBUG", "INFO", "WARN", "ERROR"}

func resolveFilters(settings Settings, f Filters, now time.Time) (ResolvedFilters, error) {
	rf := ResolvedFilters{
		Query: strings.TrimSpace(f.Query),
		Sort:  "desc",
		Page:  f.Page,
		Limit: f.Limit,
	}
	if rf.Page <= 0 {
		rf.Page = 1
	}
	if rf.Limit <= 0 {
		rf.Limit = settings.DefaultLimit
	}
	if rf.Limit > 1000 {
		rf.Limit = 1000
	}
	rf.Offset = (rf.Page - 1) * rf.Limit

	switch strings.ToLower(strings.TrimSpace(f.Sort)) {
	case "asc", "oldest":
		rf.Sort = "asc"
	case "desc", "newest", "":
		rf.Sort = "desc"
	default:
		return ResolvedFilters{}, fmt.Errorf("invalid sort %q: use asc or desc", f.Sort)
	}

	for _, level := range f.Levels {
		for _, part := range strings.Split(level, ",") {
			part = strings.ToUpper(strings.TrimSpace(part))
			if part == "" {
				continue
			}
			if !isAllowedLevel(part) {
				return ResolvedFilters{}, fmt.Errorf("invalid level %q: use DEBUG, INFO, WARN, or ERROR", part)
			}
			rf.Levels = appendUnique(rf.Levels, part)
		}
	}

	from, err := parseFilterTime(f.From, false)
	if err != nil {
		return ResolvedFilters{}, fmt.Errorf("invalid from: %w", err)
	}
	to, err := parseFilterTime(f.To, true)
	if err != nil {
		return ResolvedFilters{}, fmt.Errorf("invalid to: %w", err)
	}
	if from == nil && to == nil {
		days := f.Days
		if days <= 0 {
			days = settings.DefaultRangeDays
		}
		if days > 0 {
			start := now.AddDate(0, 0, -days)
			from = &start
		}
	}
	if from != nil && to != nil && to.Before(*from) {
		return ResolvedFilters{}, fmt.Errorf("to must be after from")
	}
	rf.From = from
	rf.To = to
	return rf, nil
}

func parseFilterTime(value string, endOfDay bool) (*time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	layouts := []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05", "2006-01-02 15:04:05", "2006-01-02"}
	for _, layout := range layouts {
		parsed, err := time.ParseInLocation(layout, value, time.Local)
		if err == nil {
			if endOfDay && (layout == "2006-01-02") {
				parsed = parsed.Add(24*time.Hour - time.Nanosecond)
			}
			return &parsed, nil
		}
	}
	return nil, fmt.Errorf("use RFC3339 or YYYY-MM-DD")
}

func isAllowedLevel(level string) bool {
	for _, allowed := range allowedLevels {
		if level == allowed {
			return true
		}
	}
	return false
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}
