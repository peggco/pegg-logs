<div align="center">

<img src="https://cdn.pegg.dev/logo/infinity.gif" alt="Pegg" width="420" />

# Pegg Logs

### Know what Pegg did.

A local, read-only log explorer for the Pegg SQLite log database. Search by level, text, and
time range, see counts, export CSV or JSON, and open a filtered dashboard. The plugin also
adds a Logs settings page to the TUI and desktop.

[Plugins docs](https://docs.pegg.dev/pegg/configurations/plugins/) · [Logs docs](https://docs.pegg.dev/official-plugins/logs/) · [Releases](https://github.com/peggco/pegg-logs/releases) · [Website](https://pegg.dev) · [Discussions](https://github.com/peggco/pegg/discussions)

[![Latest release](https://img.shields.io/github/v/release/peggco/pegg-logs?style=flat-square&sort=semver)](https://github.com/peggco/pegg-logs/releases)
[![Go 1.26.5+](https://img.shields.io/badge/go-1.26.5%2B-00ADD8?style=flat-square&logo=go&logoColor=white)](https://github.com/peggco/pegg-logs)
[![Plugin protocol v2](https://img.shields.io/badge/plugin%20protocol-v2-black?style=flat-square)](https://docs.pegg.dev/pegg/configurations/plugins/)
[![License: Apache-2.0](https://img.shields.io/badge/license-Apache--2.0-black?style=flat-square)](https://github.com/peggco/pegg-logs/blob/HEAD/LICENSE)

**Install**

```bash
pegg plugins install peggco/pegg-logs
```

Then load the `logs` skill with `/logs` before the first log tool call.
</div>

## Features

- Read-only access to `~/.pegg/logs.db`; the plugin never writes to the log database.
- Level filters for `DEBUG`, `INFO`, `WARN`, and `ERROR`.
- Case-insensitive message search with automatic debounce in the dashboard.
- Date presets and custom `from`/`to` ranges.
- Counts by level and day, pagination, and CSV/JSON export.
- Local dashboard with a random port, random token, and loopback-only binding.
- Logs settings page in the TUI and desktop with recent logs, counts, dashboard controls,
  configuration fields, and CSV export.
- Dark/light dashboard matching the Pegg Usage and Kanban design language.

## Data source

- Database: `~/.pegg/logs.db` (override with `db_path` or `PEGG_LOGS_DB`).
- Table: `logs(id, timestamp, level, message)`.
- Timestamps are stored by the Pegg logger in local time with nanosecond precision.
- The database is opened with SQLite `mode=ro`, `query_only(1)`, one connection, and a
  busy timeout. The plugin never writes to it.
- Levels: `DEBUG`, `INFO`, `WARN`, `ERROR`.

## Filters

All tools, the dashboard, and `search`/`export` share the same filter fields:

| Field | Meaning |
|---|---|
| `levels` | Levels to include. Accepts an array or comma-separated values. |
| `query` | Case-insensitive substring matched against the message. |
| `from`, `to` | RFC3339 or `YYYY-MM-DD`. Date-only `to` includes the whole day. |
| `days` | Lookback when `from`/`to` are absent. Defaults to `default_range_days` (7). |
| `limit` | Page size. Default `default_limit` (100), capped at 1000. |
| `page` | Page number starting at 1. |
| `sort` | `desc` (newest first) or `asc`. |

## Configuration

`~/.pegg/pegg.json`:

```json
{
  "plugins": {
    "options": {
      "logs": {
        "enabled": true,
        "db_path": "~/.pegg/logs.db",
        "default_range_days": 7,
        "default_limit": 100,
        "request_timeout_seconds": 30
      }
    }
  }
}
```

Environment variables:

| Variable | Meaning |
|---|---|
| `PEGG_LOGS_DB` | Database path. |
| `PEGG_LOGS_RANGE_DAYS` | Default range in days. |
| `PEGG_LOGS_LIMIT` | Default page size. |
| `PEGG_LOGS_TIMEOUT_SECONDS` | Query timeout. |
| `PEGG_LOGS_DISABLED` | `true` disables the plugin. |
| `PEGG_LOGS_DASHBOARD_TOKEN` | Override the generated dashboard token. |

Precedence: defaults → workdir → host `plugins.options.logs` (or
`plugins.options.pegg-logs`) → plugin options → environment → normalization.

## Tools

| Tool | Permission | Description |
|---|---|---|
| `logs_search` | allow | Search logs with level, text, date, page, and sort filters. |
| `logs_stats` | allow | Count logs by level and day for a filtered range. |
| `logs_export` | semi-judge | Write filtered logs to CSV or JSON. |

`logs_export` defaults to `.pegg/logs/logs-<timestamp>.<format>` under the workspace.
Relative paths resolve against the workspace; absolute paths are honored.

## Dashboard

```bash
pegg logview open
pegg logview start
pegg logview stop
```

The dashboard binds `127.0.0.1` on a random port with a random token. Runtime state is
stored at `~/.pegg/logs/server.json` with mode `0600`.

The web UI includes:

- Level toggles and a search box with a clear button, `/` shortcut, `Enter` to search,
  and 300 ms debounce while typing.
- Date presets and custom `from`/`to` inputs.
- Summary cards for total, errors, warnings, info, and debug.
- Pagination and CSV/JSON export links that carry the dashboard token.

## Commands

The built-in `pegg logs` command already shows application logs, so this plugin uses the
`logview` command namespace:

```text
pegg logview status                 Database, row count, latest log, dashboard state
pegg logview open                   Start the dashboard and open it
pegg logview start                  Start the dashboard without a browser
pegg logview stop                   Stop the dashboard
pegg logview search --level ERROR   Search logs
pegg logview export --format csv    Export filtered logs
pegg logview config                 Resolved settings
```

Direct CLI:

```bash
./bin/pegg-logs status
./bin/pegg-logs search --level WARN --days 7 --limit 20
./bin/pegg-logs export --format json --path /tmp/logs.json
./bin/pegg-logs start
./bin/pegg-logs stop
```

`pegg-logs serve --foreground` runs the dashboard in the foreground for debugging.

## TUI and desktop

The plugin contributes a **Logs** settings page with:

- Total, error, warn, info, and debug counts.
- The eight most recent log entries.
- Dashboard status and database path.
- Buttons to start, open, stop, reload, and export CSV.
- Configuration fields for the database path, default range, page size, and timeout.

Desktop settings render the same page through the plugin TUI protocol.

## Build and install

```bash
cd pegg-logs
go mod tidy
go vet ./...
go test ./...
go build -trimpath -ldflags "-s -w" -o bin/pegg-logs .
install -m 0755 bin/pegg-logs "$HOME/.pegg/plugins/pegg-logs"
```

Verify:

```bash
pegg plugins list --runtime
pegg logview status
```

`go.mod` requires `github.com/peggco/pegg v0.2.4` and `modernc.org/sqlite`.

## Privacy

- The database is opened read-only and never modified.
- The dashboard binds loopback and requires a random token for every API request.
- Tokens are never printed, logged, or returned in tool output or errors.
- Exports contain only the filtered log rows the caller requested.

## Limitations

- The plugin reads the SQLite logger driver. If Pegg is configured with a different logger
  driver, `~/.pegg/logs.db` may not exist.
- Bitbucket-style remote log storage is not included; only the local database is queried.
- The dashboard is a local view. It is not a replacement for a centralized log system.

## License

Apache-2.0. See [LICENSE](LICENSE).
