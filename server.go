package main

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

//go:embed web/index.html web/app.js web/style.css
var webFS embed.FS

type Dashboard struct {
	settings Settings
	token    string

	mu       sync.Mutex
	server   *http.Server
	listener net.Listener
	url      string
	started  time.Time
}

type serverInfo struct {
	URL       string `json:"url"`
	Token     string `json:"token"`
	PID       int    `json:"pid"`
	StartedAt string `json:"started_at,omitempty"`
	DBPath    string `json:"db_path,omitempty"`
}

func newDashboard(settings Settings) (*Dashboard, error) {
	settings.normalize()
	probe, err := newClient(settings)
	if err != nil {
		return nil, err
	}
	probe.Close()
	token := strings.TrimSpace(settings.Token)
	if token == "" {
		token = randomToken()
	}
	return &Dashboard{settings: settings, token: token, started: time.Now()}, nil
}

func (d *Dashboard) Start() error {
	d.mu.Lock()
	if d.server != nil {
		d.mu.Unlock()
		return nil
	}
	d.mu.Unlock()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("listen on 127.0.0.1:0: %w", err)
	}
	server := &http.Server{Handler: d.handler()}
	d.mu.Lock()
	d.server = server
	d.listener = listener
	d.url = "http://" + listener.Addr().String()
	d.started = time.Now()
	d.mu.Unlock()
	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "logs: dashboard stopped: %v\n", err)
		}
	}()
	return nil
}

func (d *Dashboard) Stop(ctx context.Context) error {
	d.mu.Lock()
	server := d.server
	d.server = nil
	d.url = ""
	d.mu.Unlock()
	if server == nil {
		return nil
	}
	return server.Shutdown(ctx)
}

func (d *Dashboard) URL() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.url
}

func (d *Dashboard) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", d.serveStatic)
	mux.HandleFunc("/api/meta", d.authorized(d.handleMeta))
	mux.HandleFunc("/api/stats", d.authorized(d.handleStats))
	mux.HandleFunc("/api/logs", d.authorized(d.handleLogs))
	mux.HandleFunc("/api/export.csv", d.authorized(d.handleExportCSV))
	mux.HandleFunc("/api/export.json", d.authorized(d.handleExportJSON))
	return mux
}

func (d *Dashboard) authorized(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !d.tokenMatches(r) {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized: missing or invalid dashboard token"})
			return
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		next(w, r)
	}
}

func (d *Dashboard) tokenMatches(r *http.Request) bool {
	token := r.Header.Get("X-Logs-Token")
	if token == "" {
		auth := r.Header.Get("Authorization")
		if strings.HasPrefix(strings.ToLower(auth), "bearer ") {
			token = strings.TrimSpace(auth[7:])
		}
	}
	if token == "" {
		token = r.URL.Query().Get("token")
	}
	return subtle.ConstantTimeCompare([]byte(token), []byte(d.token)) == 1
}

func (d *Dashboard) serveStatic(w http.ResponseWriter, r *http.Request) {
	var name, contentType string
	switch r.URL.Path {
	case "/", "/index.html":
		name, contentType = "web/index.html", "text/html; charset=utf-8"
	case "/app.js":
		name, contentType = "web/app.js", "text/javascript; charset=utf-8"
	case "/style.css":
		name, contentType = "web/style.css", "text/css; charset=utf-8"
	default:
		http.NotFound(w, r)
		return
	}
	data, err := webFS.ReadFile(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(data)
}

func (d *Dashboard) openClient(r *http.Request) (*Client, ResolvedFilters, error) {
	filters := filtersFromQuery(r.URL.Query())
	rf, err := resolveFilters(d.settings, filters, time.Now())
	if err != nil {
		return nil, ResolvedFilters{}, err
	}
	client, err := newClient(d.settings)
	if err != nil {
		return nil, ResolvedFilters{}, err
	}
	return client, rf, nil
}

func (d *Dashboard) handleMeta(w http.ResponseWriter, r *http.Request) {
	client, err := newClient(d.settings)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	defer client.Close()
	meta, err := buildMeta(r.Context(), client, d.settings)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, meta)
}

func (d *Dashboard) handleStats(w http.ResponseWriter, r *http.Request) {
	client, rf, err := d.openClient(r)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	defer client.Close()
	stats, err := buildStats(r.Context(), client, rf)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

func (d *Dashboard) handleLogs(w http.ResponseWriter, r *http.Request) {
	client, rf, err := d.openClient(r)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	defer client.Close()
	rows, total, err := client.queryLogs(r.Context(), rf)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"logs":  rows,
		"total": total,
		"page":  rf.Page,
		"limit": rf.Limit,
		"sort":  rf.Sort,
	})
}

func (d *Dashboard) handleExportCSV(w http.ResponseWriter, r *http.Request) {
	client, rf, err := d.openClient(r)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	defer client.Close()
	rf.Limit = 100000
	rf.Offset = 0
	rows, _, err := client.queryLogs(r.Context(), rf)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="logs-`+time.Now().Format("20060102-150405")+`.csv"`)
	writer := csv.NewWriter(w)
	_ = writer.Write([]string{"timestamp", "level", "message"})
	for _, row := range rows {
		_ = writer.Write([]string{formatTime(row.Timestamp), row.Level, row.Message})
	}
	writer.Flush()
}

func (d *Dashboard) handleExportJSON(w http.ResponseWriter, r *http.Request) {
	client, rf, err := d.openClient(r)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	defer client.Close()
	rf.Limit = 100000
	rf.Offset = 0
	rows, _, err := client.queryLogs(r.Context(), rf)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="logs-`+time.Now().Format("20060102-150405")+`.json"`)
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	_ = encoder.Encode(rows)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeAPIError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	message := err.Error()
	if strings.Contains(message, "invalid ") || strings.Contains(message, "must be") {
		status = http.StatusBadRequest
	}
	writeJSON(w, status, map[string]any{"error": message})
}

func filtersFromQuery(values url.Values) Filters {
	limit, _ := strconv.Atoi(values.Get("limit"))
	page, _ := strconv.Atoi(values.Get("page"))
	days, _ := strconv.Atoi(values.Get("days"))
	return Filters{
		Levels: values["level"],
		Query:  values.Get("query"),
		From:   values.Get("from"),
		To:     values.Get("to"),
		Days:   days,
		Limit:  limit,
		Page:   page,
		Sort:   values.Get("sort"),
	}
}

func runServe(settings Settings, flags map[string]string) error {
	if value := strings.TrimSpace(flags["db"]); value != "" {
		settings.DBPath = value
	}
	if value := strings.TrimSpace(flags["timeout"]); value != "" {
		if n, err := strconv.Atoi(value); err == nil && n > 0 {
			settings.RequestTimeoutSeconds = n
		}
	}
	if value := strings.TrimSpace(flags["range-days"]); value != "" {
		if n, err := strconv.Atoi(value); err == nil && n > 0 {
			settings.DefaultRangeDays = n
		}
	}
	if value := strings.TrimSpace(flags["limit"]); value != "" {
		if n, err := strconv.Atoi(value); err == nil && n > 0 {
			settings.DefaultLimit = n
		}
	}
	return serveForeground(settings, flags["open"] == "true")
}

func serveForeground(settings Settings, open bool) error {
	settings.normalize()
	dashboard, err := newDashboard(settings)
	if err != nil {
		return err
	}
	if err := dashboard.Start(); err != nil {
		return err
	}
	info := serverInfo{
		URL:       dashboard.URL(),
		Token:     dashboard.token,
		PID:       os.Getpid(),
		StartedAt: time.Now().UTC().Format(time.RFC3339),
		DBPath:    settings.dbPath(),
	}
	if err := writeServerInfo(settings.serverPath(), info); err != nil {
		fmt.Fprintf(os.Stderr, "logs: write server metadata: %v\n", err)
	}
	fmt.Printf("Logs dashboard: %s\n", dashboard.URL())
	if open {
		openBrowser(browserURL(dashboard.URL(), dashboard.token))
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	<-ctx.Done()
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()
	_ = dashboard.Stop(stopCtx)
	removeServerInfoIfOurs(settings.serverPath(), os.Getpid())
	return nil
}

func commandStart(settings Settings, open bool) (string, error) {
	settings.normalize()
	if info, err := readServerInfo(settings.serverPath()); err == nil && info.URL != "" {
		if healthOK(info.URL, info.Token) {
			if open {
				openBrowser(browserURL(info.URL, info.Token))
			}
			return info.URL, nil
		}
		_ = os.Remove(settings.serverPath())
	}
	if err := os.MkdirAll(settings.stateDir(), 0o755); err != nil {
		return "", fmt.Errorf("create logs state dir: %w", err)
	}
	binary := settings.PluginPath
	if binary == "" {
		exe, err := os.Executable()
		if err != nil {
			return "", fmt.Errorf("locate plugin binary: %w", err)
		}
		binary = exe
	}
	logFile, err := os.OpenFile(settings.serverLogPath(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return "", fmt.Errorf("open dashboard log: %w", err)
	}
	defer logFile.Close()

	token := randomToken()
	args := []string{
		"serve", "--foreground",
		"--db", settings.dbPath(),
		"--timeout", strconv.Itoa(settings.RequestTimeoutSeconds),
		"--range-days", strconv.Itoa(settings.DefaultRangeDays),
		"--limit", strconv.Itoa(settings.DefaultLimit),
	}
	cmd := exec.Command(binary, args...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	configureDetached(cmd)
	env := os.Environ()
	env = setEnv(env, "PEGG_PLUGIN", "")
	env = setEnv(env, "PEGG_LOGS_DASHBOARD_TOKEN", token)
	env = setEnv(env, "PEGG_LOGS_DB", settings.dbPath())
	env = setEnv(env, "PEGG_LOGS_RANGE_DAYS", strconv.Itoa(settings.DefaultRangeDays))
	env = setEnv(env, "PEGG_LOGS_LIMIT", strconv.Itoa(settings.DefaultLimit))
	env = setEnv(env, "PEGG_LOGS_TIMEOUT_SECONDS", strconv.Itoa(settings.RequestTimeoutSeconds))
	cmd.Env = env
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("start logs dashboard: %w", err)
	}
	_ = cmd.Process.Release()
	url, err := waitForServer(settings.serverPath(), 8*time.Second)
	if err != nil {
		return "", err
	}
	if open {
		openBrowser(browserURL(url, token))
	}
	return url, nil
}

func commandStop(settings Settings) (string, error) {
	info, err := readServerInfo(settings.serverPath())
	if err != nil {
		if os.IsNotExist(err) {
			return "Logs dashboard is not running\n", nil
		}
		return "", err
	}
	if info.PID > 0 {
		signalProcess(info.PID, false)
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) && processAlive(info.PID) {
			time.Sleep(50 * time.Millisecond)
		}
	}
	_ = os.Remove(settings.serverPath())
	return "Logs dashboard stopped\n", nil
}

func runningServerURL(settings Settings) string {
	info, err := readServerInfo(settings.serverPath())
	if err != nil || info.URL == "" {
		return ""
	}
	if !healthOK(info.URL, info.Token) {
		return ""
	}
	return info.URL
}

func waitForServer(path string, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if info, err := readServerInfo(path); err == nil && info.URL != "" {
			if healthOK(info.URL, info.Token) {
				return info.URL, nil
			}
		}
		time.Sleep(75 * time.Millisecond)
	}
	return "", fmt.Errorf("timed out waiting for %s", path)
}

func healthOK(baseURL, token string) bool {
	client := &http.Client{Timeout: 1500 * time.Millisecond}
	req, err := http.NewRequest(http.MethodGet, strings.TrimRight(baseURL, "/")+"/api/meta", nil)
	if err != nil {
		return false
	}
	req.Header.Set("X-Logs-Token", token)
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode == http.StatusOK
}

func readServerInfo(path string) (serverInfo, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return serverInfo{}, err
	}
	var info serverInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return serverInfo{}, fmt.Errorf("parse %s: %w", path, err)
	}
	return info, nil
}

func writeServerInfo(path string, info serverInfo) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

func removeServerInfoIfOurs(path string, pid int) {
	info, err := readServerInfo(path)
	if err != nil {
		return
	}
	if info.PID == pid {
		_ = os.Remove(path)
	}
}

func browserURL(baseURL, token string) string {
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return baseURL
	}
	query := parsed.Query()
	query.Set("token", token)
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func openBrowser(target string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", target)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	default:
		cmd = exec.Command("xdg-open", target)
	}
	_ = cmd.Start()
}

func randomToken() string {
	buffer := make([]byte, 24)
	if _, err := rand.Read(buffer); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buffer)
}

func setEnv(env []string, key, value string) []string {
	prefix := key + "="
	for i, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			env[i] = prefix + value
			return env
		}
	}
	return append(env, prefix+value)
}
