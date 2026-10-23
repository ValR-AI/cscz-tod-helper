// Package web serves the helper's interface on 127.0.0.1 and its JSON API.
package web

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"todhelper/internal/autosplit"
	"todhelper/internal/bundle"
	"todhelper/internal/savestate"
	"todhelper/internal/settings"
)

//go:embed ui
var uiFiles embed.FS

const DefaultPort = 47634

type App struct {
	Version string
	DataDir string

	mu       sync.Mutex
	settings settings.Settings
	store    bundle.Store
	ls       *autosplit.LiveSplit    // LiveSplit's TCP server
	lso      *autosplit.LiveSplitOne // LiveSplit One's "Connect to Server"
	watcher  *autosplit.Watcher
	log      *activityLog
}

// LiveSplitOnePath is where LiveSplit One connects.
const LiveSplitOnePath = "/livesplit-one"

func (a *App) timerLocked() autosplit.Timer {
	if a.settings.TimerKind == settings.TimerLiveSplitOne {
		return a.lso
	}
	return a.ls
}

func (a *App) settingsPath() string { return filepath.Join(a.DataDir, "config.json") }

// NewApp loads the settings and makes sure there is an active bundle. port
// is the interface's port, used in the LiveSplit One address.
func NewApp(version, dataDir string, port int) (*App, error) {
	a := &App{Version: version, DataDir: dataDir, store: bundle.Store{Dir: filepath.Join(dataDir, "bundles")},
		log: &activityLog{dir: filepath.Join(dataDir, "logs")}}
	s, err := settings.Load(a.settingsPath())
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", a.settingsPath(), err)
	}
	a.settings = s
	a.ls = autosplit.NewLiveSplit(s.LiveSplitAddr)
	a.lso = autosplit.NewLiveSplitOne(fmt.Sprintf("ws://127.0.0.1:%d%s", port, LiveSplitOnePath))
	a.watcher = autosplit.NewWatcher(a.timerLocked(), filepath.Join(dataDir, "logs"))
	if err := a.ensureBundleLocked(); err != nil {
		return nil, err
	}
	a.log.add("START", "CS:CZ ToD Helper %s, Save folder: %s, bundle: %s", version, orNone(s.SaveDir), a.settings.ActiveBundle)
	if s.WatchEnabled && savestate.IsProfileFile(s.WatchFile) && savestate.CheckSaveDir(s.SaveDir) == nil {
		a.startWatchLocked()
	}
	return a, nil
}

func (a *App) Close() {
	a.log.add("STOP", "CS:CZ ToD Helper closed")
	a.watcher.Stop()
	a.ls.Close()
}

func orNone(s string) string {
	if s == "" {
		return "(not set)"
	}
	return s
}

func (a *App) saveSettingsLocked() error { return settings.Save(a.settingsPath(), a.settings) }

func (a *App) ensureBundleLocked() error {
	names, err := a.store.List()
	if err != nil {
		return err
	}
	if len(names) == 0 {
		if _, err := a.store.Create("My bundle"); err != nil {
			return err
		}
		names = []string{"My bundle"}
	}
	for _, n := range names {
		if n == a.settings.ActiveBundle {
			return nil
		}
	}
	a.settings.ActiveBundle = names[0]
	return a.saveSettingsLocked()
}

// ---------------------------------------------------------------------------
// HTTP plumbing
// ---------------------------------------------------------------------------

type apiError struct {
	status int
	msg    string
	extra  map[string]any
}

func (e *apiError) Error() string { return e.msg }

func fail(status int, f string, a ...any) error {
	return &apiError{status: status, msg: fmt.Sprintf(f, a...)}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func (a *App) api(fn func(r *http.Request) (any, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		v, err := fn(r)
		if err != nil {
			status := http.StatusInternalServerError
			body := map[string]any{"error": err.Error()}
			var ae *apiError
			if errors.As(err, &ae) {
				status = ae.status
				for k, v := range ae.extra {
					body[k] = v
				}
			}
			writeJSON(w, status, body)
			return
		}
		writeJSON(w, http.StatusOK, v)
	}
}

func decode(r *http.Request, v any) error {
	if err := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20)).Decode(v); err != nil {
		return fail(http.StatusBadRequest, "bad request: %v", err)
	}
	return nil
}

// guard only lets the helper's own page call the API: the Host must be the
// loopback address (no DNS rebinding) and writes need a custom header, which
// other websites cannot send without a CORS preflight that we never accept.
func guard(port int, next http.Handler) http.Handler {
	allowed := map[string]bool{
		"127.0.0.1:" + strconv.Itoa(port): true,
		"localhost:" + strconv.Itoa(port): true,
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !allowed[r.Host] {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if r.Header.Get("X-ToD-Helper") != "1" || !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}

func (a *App) Handler(port int) http.Handler {
	mux := http.NewServeMux()
	ui, _ := fs.Sub(uiFiles, "ui")
	mux.Handle("GET /", http.FileServer(http.FS(ui)))
	mux.HandleFunc("GET /api/ping", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]string{"app": "cscz-tod-helper", "version": a.Version})
	})
	mux.HandleFunc("GET /api/state", a.api(func(r *http.Request) (any, error) { return a.state() }))
	mux.HandleFunc("POST /api/evaluate", a.api(a.evaluate))
	mux.HandleFunc("GET /api/import", a.api(a.importCandidates))
	mux.HandleFunc("POST /api/import", a.api(a.importProfiles))
	mux.HandleFunc("POST /api/pack", a.api(a.makePack))
	mux.HandleFunc("GET /api/terrorists", a.api(func(r *http.Request) (any, error) { return savestate.Terrorists(), nil }))
	mux.HandleFunc("POST /api/savestate/save", a.api(a.saveSavestate))
	mux.HandleFunc("POST /api/savestate/delete", a.api(a.deleteSavestate))
	mux.HandleFunc("POST /api/bundle/reset", a.api(a.resetBundle))
	mux.HandleFunc("POST /api/bundles", a.api(a.bundles))
	mux.HandleFunc("POST /api/settings", a.api(a.updateSettings))
	mux.HandleFunc("GET /api/profiles", a.api(a.profiles))
	mux.HandleFunc("POST /api/autosplit", a.api(a.setAutosplit))
	mux.HandleFunc("POST /api/browse", a.api(a.browse))
	mux.Handle("GET "+LiveSplitOnePath, a.lso)
	mux.HandleFunc("GET /api/autosplit", a.api(func(r *http.Request) (any, error) {
		since, _ := strconv.ParseInt(r.URL.Query().Get("since"), 10, 64)
		return a.watcher.Status(since), nil
	}))
	return guard(port, mux)
}

// ---------------------------------------------------------------------------
// State
// ---------------------------------------------------------------------------

type savestateView struct {
	Savestate savestate.Savestate `json:"savestate"`
	Status    savestate.Status    `json:"status"`
	Beaten    int                 `json:"beaten"`
	Points    int                 `json:"points"`
}

type stateView struct {
	Version      string            `json:"version"`
	DataDir      string            `json:"dataDir"`
	Settings     settings.Settings `json:"settings"`
	SaveDirError string            `json:"saveDirError"`
	Bundles      []string          `json:"bundles"`
	Active       string            `json:"active"`
	Savestates   []savestateView   `json:"savestates"`
	OS           string            `json:"os"`
	LiveSplitOne string            `json:"liveSplitOneURL"`
}

func (a *App) state() (*stateView, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.stateLocked()
}

func (a *App) stateLocked() (*stateView, error) {
	if err := a.ensureBundleLocked(); err != nil {
		return nil, err
	}
	v := &stateView{Version: a.Version, DataDir: a.DataDir, Settings: a.settings, Active: a.settings.ActiveBundle,
		OS: runtime.GOOS, LiveSplitOne: a.lso.URL}
	if err := savestate.CheckSaveDir(a.settings.SaveDir); err != nil {
		v.SaveDirError = err.Error()
	}
	v.Bundles, _ = a.store.List()
	b, err := a.store.Load(a.settings.ActiveBundle)
	if err != nil {
		return nil, err
	}
	v.Savestates = []savestateView{}
	for _, s := range b.Savestates {
		ev := savestate.Evaluate(s)
		v.Savestates = append(v.Savestates, savestateView{
			Savestate: s, Status: savestate.FileStatus(a.settings.SaveDir, s),
			Beaten: ev.BeatenCount, Points: ev.Points,
		})
	}
	return v, nil
}

// ---------------------------------------------------------------------------
// Savestates
// ---------------------------------------------------------------------------

func (a *App) evaluate(r *http.Request) (any, error) {
	var s savestate.Savestate
	if err := decode(r, &s); err != nil {
		return nil, err
	}
	return savestate.Evaluate(s), nil
}

type saveRequest struct {
	Savestate savestate.Savestate `json:"savestate"`
	Original  string              `json:"original"`  // name before editing; empty for a new one
	Overwrite bool                `json:"overwrite"` // replace a profile the bundle does not know
}

func (a *App) saveSavestate(r *http.Request) (any, error) {
	var req saveRequest
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	ev := savestate.Evaluate(req.Savestate)
	if len(ev.Problems) > 0 {
		return nil, fail(http.StatusBadRequest, "%s", strings.Join(ev.Problems, "; "))
	}
	s := ev.Savestate

	a.mu.Lock()
	defer a.mu.Unlock()
	saveDir := a.settings.SaveDir
	if err := savestate.CheckSaveDir(saveDir); err != nil {
		return nil, fail(http.StatusBadRequest, "%v", err)
	}
	b, err := a.store.Load(a.settings.ActiveBundle)
	if err != nil {
		return nil, err
	}
	// A new name that the game turns into the same file is not a rename on disk.
	renamed := req.Original != "" && !savestate.SameFile(req.Original, s.Name)
	path := savestate.Path(saveDir, s.Name)
	// Only a profile none of the bundles knows (a real career, for example)
	// needs confirmation before it is overwritten.
	foreign := b.Find(s.Name) < 0 && len(a.store.OtherBundlesUsing(s.Name, b.Name)) == 0
	if _, err := os.Stat(path); err == nil && (req.Original == "" || renamed) && foreign && !req.Overwrite {
		return nil, &apiError{status: http.StatusConflict,
			msg:   fmt.Sprintf("A profile called %s already exists in your Save folder. Overwrite it? A copy is kept in the helper's backups folder.", s.Name),
			extra: map[string]any{"needsOverwrite": true}}
	}
	if err := b.Put(s, req.Original); err != nil {
		return nil, fail(http.StatusConflict, "%v", err)
	}
	var warnings []string
	if _, err := os.Stat(path); err == nil && req.Overwrite && foreign {
		if dest, err := a.backup(path); err == nil {
			a.log.add("BACKUP", "%s copied to %s before overwriting it", savestate.FileName(s.Name), dest)
			warnings = append(warnings, "The previous profile was copied to "+dest)
		}
	}
	why := "saved from the builder, bundle " + b.Name
	if renamed {
		why = fmt.Sprintf("saved from the builder, renamed from %s, bundle %s", req.Original, b.Name)
	}
	if err := a.install(saveDir, s, why); err != nil {
		return nil, err
	}
	if renamed {
		a.remove(saveDir, req.Original, "old name of "+s.Name)
	}
	if err := a.store.Save(b); err != nil {
		return nil, err
	}
	if others := a.store.OtherBundlesUsing(s.Name, b.Name); len(others) > 0 {
		warnings = append(warnings, fmt.Sprintf("The bundle(s) %s also have a savestate called %s: they write the same file.", strings.Join(others, ", "), s.Name))
	}
	st, err := a.stateLocked()
	return map[string]any{"state": st, "warnings": warnings, "savestate": s}, err
}

// backup copies a profile into the helper's own folder (never into Save).
func (a *App) backup(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(a.DataDir, "backups")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	dest := filepath.Join(dir, filepath.Base(path)+"."+time.Now().Format("20060102-150405")+".bak")
	return dest, os.WriteFile(dest, data, 0o644)
}

func (a *App) deleteSavestate(r *http.Request) (any, error) {
	var req struct {
		Name string `json:"name"`
	}
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	b, err := a.store.Load(a.settings.ActiveBundle)
	if err != nil {
		return nil, err
	}
	i := b.Find(req.Name)
	if i < 0 {
		return nil, fail(http.StatusNotFound, "no savestate called %s in this bundle", req.Name)
	}
	name := b.Savestates[i].Name
	b.Remove(name)
	if err := a.store.Save(b); err != nil {
		return nil, err
	}
	if savestate.CheckSaveDir(a.settings.SaveDir) == nil {
		if err := a.remove(a.settings.SaveDir, name, "savestate deleted from bundle "+b.Name); err != nil {
			return nil, err
		}
	}
	return a.stateLocked()
}

func (a *App) resetBundle(r *http.Request) (any, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := savestate.CheckSaveDir(a.settings.SaveDir); err != nil {
		return nil, fail(http.StatusBadRequest, "%v", err)
	}
	b, err := a.store.Load(a.settings.ActiveBundle)
	if err != nil {
		return nil, err
	}
	restored := 0
	var errs []string
	a.log.add("RESET", "bundle %s: %d savestate(s)", b.Name, len(b.Savestates))
	for _, s := range b.Savestates {
		if err := a.install(a.settings.SaveDir, s, "Reset bundle "+b.Name); err != nil {
			errs = append(errs, s.Name+": "+err.Error())
			continue
		}
		restored++
	}
	st, err := a.stateLocked()
	return map[string]any{"state": st, "restored": restored, "errors": errs}, err
}

// ---------------------------------------------------------------------------
// Bundles and settings
// ---------------------------------------------------------------------------

func (a *App) bundles(r *http.Request) (any, error) {
	var req struct {
		Action  string `json:"action"` // create, rename, delete, select
		Name    string `json:"name"`
		NewName string `json:"newName"`
	}
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	switch req.Action {
	case "create":
		if _, err := a.store.Create(strings.TrimSpace(req.Name)); err != nil {
			return nil, fail(http.StatusBadRequest, "%v", err)
		}
		a.settings.ActiveBundle = strings.TrimSpace(req.Name)
	case "rename":
		newName := strings.TrimSpace(req.NewName)
		if err := a.store.Rename(req.Name, newName); err != nil {
			return nil, fail(http.StatusBadRequest, "%v", err)
		}
		if a.settings.ActiveBundle == req.Name {
			a.settings.ActiveBundle = newName
		}
	case "delete":
		b, err := a.store.Load(req.Name)
		if err != nil {
			return nil, fail(http.StatusNotFound, "%v", err)
		}
		if savestate.CheckSaveDir(a.settings.SaveDir) == nil {
			for _, s := range b.Savestates {
				a.remove(a.settings.SaveDir, s.Name, "bundle "+b.Name+" deleted")
			}
		}
		if err := a.store.Delete(req.Name); err != nil {
			return nil, err
		}
	case "select":
		if !a.store.Exists(req.Name) {
			return nil, fail(http.StatusNotFound, "bundle %q not found", req.Name)
		}
		a.settings.ActiveBundle = req.Name
	default:
		return nil, fail(http.StatusBadRequest, "unknown action %q", req.Action)
	}
	if err := a.saveSettingsLocked(); err != nil {
		return nil, err
	}
	return a.stateLocked()
}

func (a *App) updateSettings(r *http.Request) (any, error) {
	var req struct {
		SaveDir       *string `json:"saveDir"`
		LiveSplitAddr *string `json:"liveSplitAddr"`
		TimerKind     *string `json:"timerKind"`
	}
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if req.SaveDir != nil {
		dir := strings.TrimSpace(*req.SaveDir)
		if dir != "" {
			dir = filepath.Clean(dir)
		}
		if err := savestate.CheckSaveDir(dir); err != nil {
			return nil, fail(http.StatusBadRequest, "%v", err)
		}
		if dir != a.settings.SaveDir {
			a.log.add("SETTINGS", "Save folder: %s", orNone(dir))
		}
		a.settings.SaveDir = dir
		if a.settings.WatchEnabled && savestate.IsProfileFile(a.settings.WatchFile) {
			a.startWatchLocked()
		}
	}
	if req.LiveSplitAddr != nil {
		addr := strings.TrimSpace(*req.LiveSplitAddr)
		if _, _, err := net.SplitHostPort(addr); err != nil {
			return nil, fail(http.StatusBadRequest, "the LiveSplit address must look like 127.0.0.1:16834")
		}
		a.settings.LiveSplitAddr = addr
		a.ls.SetAddr(addr)
	}
	if req.TimerKind != nil {
		switch *req.TimerKind {
		case settings.TimerLiveSplit, settings.TimerLiveSplitOne:
			a.settings.TimerKind = *req.TimerKind
			a.watcher.SetTimer(a.timerLocked())
		default:
			return nil, fail(http.StatusBadRequest, "unknown timer %q", *req.TimerKind)
		}
	}
	if err := a.saveSettingsLocked(); err != nil {
		return nil, err
	}
	return a.stateLocked()
}

// browse shows the system's folder picker for the Save folder.
func (a *App) browse(r *http.Request) (any, error) {
	a.mu.Lock()
	start := a.settings.SaveDir
	a.mu.Unlock()
	if start == "" {
		start, _ = os.UserHomeDir()
	}
	path, err := BrowseFolder("Choose the game's Save folder (czero/Save)", start)
	if err != nil { // no picker on this system: not an error of the request
		return map[string]any{"path": "", "cancelled": true, "message": err.Error()}, nil
	}
	return map[string]any{"path": path, "cancelled": path == ""}, nil
}

func (a *App) profiles(r *http.Request) (any, error) {
	a.mu.Lock()
	dir := a.settings.SaveDir
	a.mu.Unlock()
	list, err := savestate.ListProfiles(dir)
	if err != nil {
		return nil, fail(http.StatusBadRequest, "%v", err)
	}
	if list == nil {
		list = []savestate.ProfileFile{}
	}
	return list, nil
}

// startWatchLocked watches the profile file of the settings. The watcher
// shows the name written in the profile, as the game does.
func (a *App) startWatchLocked() {
	pf := savestate.ReadProfileFile(a.settings.SaveDir, a.settings.WatchFile)
	a.watcher.Start(pf.Name, filepath.Join(a.settings.SaveDir, a.settings.WatchFile))
}

func (a *App) setAutosplit(r *http.Request) (any, error) {
	var req struct {
		Enabled bool   `json:"enabled"`
		File    string `json:"file"` // CZProfile-*.czp in the Save folder
	}
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if req.Enabled {
		if err := savestate.CheckSaveDir(a.settings.SaveDir); err != nil {
			return nil, fail(http.StatusBadRequest, "%v", err)
		}
		if !savestate.IsProfileFile(req.File) {
			return nil, fail(http.StatusBadRequest, "pick the profile to watch")
		}
		a.settings.WatchFile = req.File
		a.startWatchLocked()
		a.log.add("WATCH", "autosplitter watches %s", req.File)
	} else {
		a.watcher.Stop()
		a.log.add("WATCH", "autosplitter stopped")
	}
	a.settings.WatchEnabled = req.Enabled
	if err := a.saveSettingsLocked(); err != nil {
		return nil, err
	}
	return a.watcher.Status(0), nil
}

// ---------------------------------------------------------------------------
// Running
// ---------------------------------------------------------------------------

// Listen opens the server port: the default one when free, or tells the
// caller that another copy of the helper already uses it.
func Listen(port int) (net.Listener, int, bool, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
	if err == nil {
		return ln, ln.Addr().(*net.TCPAddr).Port, false, nil
	}
	if port != 0 && otherInstance(port) {
		return nil, port, true, nil
	}
	ln, err = net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, 0, false, err
	}
	return ln, ln.Addr().(*net.TCPAddr).Port, false, nil
}

func otherInstance(port int) bool {
	c := http.Client{Timeout: time.Second}
	resp, err := c.Get("http://127.0.0.1:" + strconv.Itoa(port) + "/api/ping")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	var v map[string]string
	return json.NewDecoder(resp.Body).Decode(&v) == nil && v["app"] == "cscz-tod-helper"
}
