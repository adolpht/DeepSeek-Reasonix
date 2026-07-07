package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/atotto/clipboard"
	"github.com/wailsapp/wails/v2/pkg/runtime"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"rexion/internal/agent"
	"rexion/internal/billing"
	"rexion/internal/boot"
	"rexion/internal/config"
	"rexion/internal/control"
	"rexion/internal/datastore"
	"rexion/internal/event"
	"rexion/internal/fileref"
	fileenc "rexion/internal/fileutil/encoding"
	"rexion/internal/i18n"
	"rexion/internal/mcpdiag"
	"rexion/internal/memory"
	"rexion/internal/plugin"
	"rexion/internal/provider"
	"rexion/internal/recipe"
	"rexion/internal/registry"
	"rexion/internal/scheduler"
	"rexion/internal/skill"
	"rexion/internal/workflow"
)

// eventChannel is the Wails runtime event name the frontend subscribes to for the
// agent's typed event stream. One channel carries every event kind; the payload's
// `kind` field discriminates — the desktop analogue of the serve transport's SSE
// `data:` frames.
const eventChannel = "agent:event"

// singleInstanceID is used by Wails to route a second desktop launch back to the
// running instance. Keep it stable across releases so launcher/Dock/taskbar
// reopen behavior remains predictable on every platform.
const singleInstanceID = "com.Rexion.desktop"

// App is the Wails-bound application object: the desktop frontend's command
// surface. Its exported methods (Submit/Cancel/Approve/…) are generated into JS
// bindings. The app manages multiple WorkspaceTabs — each with its own controller
// scoped to a project workspace — and routes commands to the active tab. Events
// flow the other way: each tab's controller emits to a tabEventSink that
// forwards events tagged with tabId to the webview via runtime.EventsEmit.
type App struct {
	ctx context.Context

	// mu protects the tab map, tabOrder, activeTabID, and per-tab fields that are read
	// from bound methods. All bound methods that touch a controller use activeCtrl().
	mu          sync.RWMutex
	tabs        map[string]*WorkspaceTab
	tabOrder    []string
	activeTabID string
	readyHook   func()

	forceQuit atomic.Bool
	trayReady bool
	tray      *desktopTray

	mediaTokens      *mediaTokenStore
	dataStore        *datastore.Store
	sched            *scheduler.Scheduler
	recipeStore      *recipe.Store
	workflowStore    *workflow.Store
	clipboardHistory *ClipboardHistory
	terminals        *terminalManager

	// imProcessor is the App-level single IM background processor. It owns the
	// sole IM plugin connection + poll watcher, keeping IM message handling
	// isolated from user tab conversations. Nil until startIMProcessor runs.
	imMu      sync.Mutex
	imCtrl    *control.Controller
	imCancel  context.CancelFunc
	imStarted bool
}

// mediaTokenEntry holds metadata for a workspace media file served via temporary URL.
// When content is non-nil the middleware serves from memory instead of opening
// absPath — used for generated HTML previews (docx/xlsx → HTML) that don't
// exist as files on disk.
type mediaTokenEntry struct {
	absPath   string
	filename  string
	mime      string
	kind      string
	size      int64
	modTime   time.Time
	content   []byte // inline content; nil = serve from absPath
	createdAt time.Time
	expiresAt time.Time
}

// mediaTokenStore manages temporary tokens that grant access to workspace files
// through the AssetServer middleware. Tokens expire after a fixed TTL and are
// capped at a maximum count; creating a new token evicts the oldest entry when
// the store is full.
type mediaTokenStore struct {
	mu    sync.Mutex
	byTok map[string]*mediaTokenEntry
	order []string // oldest first
	maxN  int
	ttl   time.Duration
}

const mediaTokenMax = 256

func newMediaTokenStore() *mediaTokenStore {
	return &mediaTokenStore{
		byTok: map[string]*mediaTokenEntry{},
		maxN:  mediaTokenMax,
		ttl:   10 * time.Minute,
	}
}

func (s *mediaTokenStore) cleanupLocked() {
	now := time.Now()
	for len(s.order) > 0 {
		tok := s.order[0]
		e := s.byTok[tok]
		if e == nil {
			s.order = s.order[1:]
			continue
		}
		if !now.Before(e.expiresAt) {
			delete(s.byTok, tok)
			s.order = s.order[1:]
			continue
		}
		break
	}
	for len(s.order) > s.maxN {
		oldest := s.order[0]
		delete(s.byTok, oldest)
		s.order = s.order[1:]
	}
}

func (s *mediaTokenStore) create(absPath, filename, mime, kind string, size int64, modTime time.Time) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.cleanupLocked()

	tok := make([]byte, 16)
	if _, err := rand.Read(tok); err != nil {
		panic("crypto/rand.Read failed: " + err.Error())
	}
	token := hex.EncodeToString(tok)

	now := time.Now()
	s.byTok[token] = &mediaTokenEntry{
		absPath:   absPath,
		filename:  filename,
		mime:      mime,
		kind:      kind,
		size:      size,
		modTime:   modTime,
		createdAt: now,
		expiresAt: now.Add(s.ttl),
	}
	s.order = append(s.order, token)

	// Trim oldest if the new token pushed us over the limit.
	for len(s.order) > s.maxN {
		oldest := s.order[0]
		delete(s.byTok, oldest)
		s.order = s.order[1:]
	}

	return token
}

// createInline registers a blob of in-memory content (e.g. generated HTML)
// with the token store and returns a token. The middleware will serve the
// content directly without touching disk. Used by office-file previews
// (docx/xlsx → HTML) where no on-disk artifact exists.
func (s *mediaTokenStore) createInline(filename, mime, kind string, content []byte) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.cleanupLocked()

	tok := make([]byte, 16)
	if _, err := rand.Read(tok); err != nil {
		panic("crypto/rand.Read failed: " + err.Error())
	}
	token := hex.EncodeToString(tok)

	now := time.Now()
	s.byTok[token] = &mediaTokenEntry{
		absPath:   "", // no file — served from content
		filename:  filename,
		mime:      mime,
		kind:      kind,
		size:      int64(len(content)),
		modTime:   now,
		content:   content,
		createdAt: now,
		expiresAt: now.Add(s.ttl),
	}
	s.order = append(s.order, token)

	for len(s.order) > s.maxN {
		oldest := s.order[0]
		delete(s.byTok, oldest)
		s.order = s.order[1:]
	}

	return token
}

func (s *mediaTokenStore) get(token string) *mediaTokenEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.byTok[token]
	if e == nil {
		return nil
	}
	if time.Now().After(e.expiresAt) {
		delete(s.byTok, token)
		return nil
	}
	return e
}

func (a *App) ensureMediaTokenStore() *mediaTokenStore {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.mediaTokens == nil {
		a.mediaTokens = newMediaTokenStore()
	}
	return a.mediaTokens
}

// workspaceMediaMiddleware returns an HTTP middleware that intercepts
// /__Rexion_workspace_media/{token}/{filename} requests and serves the
// corresponding workspace file. All other paths pass through to the Wails
// default asset handler unchanged.
func (a *App) workspaceMediaMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			prefix := "/__Rexion_workspace_media/"
			if !strings.HasPrefix(r.URL.Path, prefix) {
				next.ServeHTTP(w, r)
				return
			}

			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}

			rest := strings.TrimPrefix(r.URL.Path, prefix)
			parts := strings.SplitN(rest, "/", 2)
			if len(parts) == 0 || parts[0] == "" {
				http.NotFound(w, r)
				return
			}
			token := parts[0]

			entry := a.ensureMediaTokenStore().get(token)
			if entry == nil {
				http.NotFound(w, r)
				return
			}

			w.Header().Set("Content-Type", entry.mime)
			w.Header().Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": entry.filename}))
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("Cache-Control", "private, max-age=600")

			// Inline content (e.g. generated HTML for docx/xlsx previews) is
			// served directly from memory — no disk file to open.
			if entry.content != nil {
				w.Header().Set("Content-Length", strconv.Itoa(len(entry.content)))
				w.WriteHeader(http.StatusOK)
				if r.Method != http.MethodHead {
					_, _ = w.Write(entry.content)
				}
				return
			}

			f, err := os.Open(entry.absPath)
			if err != nil {
				http.NotFound(w, r)
				return
			}
			defer f.Close()
			http.ServeContent(w, r, entry.filename, entry.modTime, f)
		})
	}
}

// NewApp constructs the bound object. Tabs are restored in startup from the
// last session's desktop-tabs.json.
func NewApp() *App {
	ds, err := datastore.Open()
	if err != nil {
		// Log but don't fail: the app can still run without persistent data.
		fmt.Fprintf(os.Stderr, "warning: data store init failed: %v\n", err)
	}
	rs, err := recipe.NewStore("")
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: recipe store init failed: %v\n", err)
	}
	ws, err := workflow.NewStore("")
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: workflow store init failed: %v\n", err)
	}
	var ch *ClipboardHistory
	if cfg, err := config.Load(); err == nil && cfg.ClipboardHistory.EnabledOrDefault() {
		ch, err = NewClipboardHistory(filepath.Join(desktopConfigDir(), "clipboard_history.db"))
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: clipboard history init failed: %v\n", err)
		} else {
			_ = ch.ClearOldClipboard(cfg.ClipboardHistory.RetentionDaysOrDefault())
			_ = ch.EnforceMaxEntries(cfg.ClipboardHistory.MaxEntriesOrDefault())
		}
	}
	app := &App{tabs: map[string]*WorkspaceTab{}, mediaTokens: newMediaTokenStore(), dataStore: ds, recipeStore: rs, workflowStore: ws, clipboardHistory: ch, terminals: newTerminalManager()}
	if ds != nil {
		app.sched = scheduler.NewScheduler(ds,
			func(name, skill, params string) string {
				return app.executeScheduledTask(name, skill, params)
			},
			func(name, skill string) {
				app.onScheduledTaskStart(name, skill)
			},
			func(name, skill, result string) {
				app.onScheduledTaskDone(name, skill, result)
			},
		)
	}
	return app
}

func (a *App) bootContext() context.Context {
	if a.ctx != nil {
		return a.ctx
	}
	return context.Background()
}

// Platform exposes the native OS to the frontend so chrome/layout affordances can
// stay platform-scoped instead of relying on browser user-agent guesses.
func (a *App) Platform() string {
	return goruntime.GOOS
}

// startup runs once the webview process is up, before the frontend can issue any
// bound call. It captures the Wails context (needed for EventsEmit), then kicks
// off the initialization in a background goroutine so the webview loads immediately.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	installSystemQuitHook()
	a.startTray()

	// Scaffold ~/.rexion/memory/ (the PKM files) on the Wails startup path so
	// the desktop app is independent of the CLI boot path. Best-effort: a failure
	// (e.g. read-only home) is logged but never blocks startup, since memory.Load
	// silently skips missing PKM files and the rest of memory still works.
	if _, _, err := memory.EnsureMemoryDir(); err != nil {
		fmt.Fprintln(os.Stderr, "warning: pkm memory dir not scaffolded:", err)
	}

	go a.restoreOrBuildTabs()

	// Start the cron scheduler in the background.
	if a.sched != nil {
		go a.sched.Start()
		// Register cron-triggered workflows with the scheduler so they fire
		// alongside Recipe cron tasks. Runs after sched.Start() so the cron
		// engine is ready; Register is safe to call post-Start.
		go a.registerWorkflowCronTriggers()
	}

	// Start clipboard monitoring in the background.
	if a.clipboardHistory != nil {
		go a.monitorClipboard()
	}

	// Activate voice input: probe for whisper and register the global hotkey
	// (Ctrl+Shift+V) so the user can start voice input from anywhere. The hotkey
	// is registered whenever voice_input is enabled — the frontend handles the
	// fallback toast when whisper is not installed.
	initVoiceInput()
	if voiceInputEnabled {
		if err := a.RegisterVoiceHotkey(); err != nil {
			fmt.Fprintln(os.Stderr, "warning: voice hotkey not registered:", err)
		}
	}

	// Activate clipboard global hotkey (Ctrl+Shift+R) to toggle FloatingWindow.
	if err := a.RegisterClipboardHotkey(); err != nil {
		fmt.Fprintln(os.Stderr, "warning: clipboard hotkey not registered:", err)
	}

	// Start the App-level IM background processor. It owns the sole IM plugin
	// connection + poll watcher, keeping IM message handling isolated from user
	// tab conversations. Runs in a goroutine so a slow IM plugin spawn never
	// blocks the UI. See startIMProcessor for details.
	go a.startIMProcessor()
}

func (a *App) monitorClipboard() {
	var lastContent string
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for range ticker.C {
		// Rich content (copied files or images) takes priority over plain
		// text. On non-Windows this is a no-op stub returning present=false,
		// so the original text-only behaviour is preserved there.
		if kind, content, present := readClipboardRichNonText(); present {
			if content != "" {
				// Reset text dedup: the clipboard switched to a non-text item,
				// so a later re-copy of the same text should be recorded again.
				lastContent = ""
				_ = a.clipboardHistory.RecordClipboard(kind, content)
				a.enforceClipboardMax()
			}
			continue
		}
		content, err := clipboard.ReadAll()
		if err != nil {
			continue
		}
		if content == "" || content == lastContent {
			continue
		}
		lastContent = content
		_ = a.clipboardHistory.RecordClipboard("text", content)
		a.enforceClipboardMax()
	}
}

// enforceClipboardMax trims the clipboard history to the configured maximum
// entry count (falling back to 1000 when the config cannot be loaded).
func (a *App) enforceClipboardMax() {
	if cfg, err := config.Load(); err == nil {
		_ = a.clipboardHistory.EnforceMaxEntries(cfg.ClipboardHistory.MaxEntriesOrDefault())
	} else {
		_ = a.clipboardHistory.EnforceMaxEntries(1000)
	}
}

func (a *App) beforeClose(ctx context.Context) bool {
	if a.forceQuit.Swap(false) || consumeSystemQuitRequested() {
		return false
	}
	cfg, _, err := a.loadDesktopUserConfigForEdit()
	if err != nil {
		cfg = config.LoadForEdit(config.UserConfigPath())
	}
	if cfg.DesktopCloseBehavior() == "background" {
		a.saveWindowStateSync()
		a.snapshotAllTabs()
		hideForBackground(ctx)
		return true
	}
	return false
}

func (a *App) showMainWindow() {
	if a.ctx != nil {
		showFromBackground(a.ctx)
	}
}

func (a *App) secondInstanceLaunch() {
	a.showMainWindow()
}

func (a *App) quitApp() {
	if a.ctx == nil {
		return
	}
	a.forceQuit.Store(true)
	runtime.Quit(a.ctx)
}

func hideForBackground(ctx context.Context) {
	if backgroundCloseUsesApplicationHide(goruntime.GOOS) {
		runtime.Hide(ctx)
		return
	}
	runtime.WindowHide(ctx)
}

func showFromBackground(ctx context.Context) {
	if backgroundCloseUsesApplicationHide(goruntime.GOOS) {
		runtime.Show(ctx)
	}
	runtime.WindowShow(ctx)
	runtime.WindowUnminimise(ctx)
}

func backgroundCloseUsesApplicationHide(goos string) bool {
	return goos == "darwin"
}

// restoreOrBuildTabs restores the tabs from the last session, or creates a
// default Global tab on first launch.
func (a *App) restoreOrBuildTabs() {
	ctx := a.ctx
	ensureWorkspace()

	// Load i18n from the first available config.
	if cfg, err := config.Load(); err == nil {
		i18n.DetectLanguage(cfg.Language)
	}

	f := loadTabsFile()
	if len(f.Tabs) > 0 {
		toBuild := make([]*WorkspaceTab, 0, len(f.Tabs))
		for _, entry := range f.Tabs {
			a.mu.Lock()
			id := a.restoredTabIDLocked(entry.ID)
			a.mu.Unlock()

			var tab *WorkspaceTab
			if entry.Scope == "project" {
				tab = a.createTabEntryWithID(entry.Scope, entry.WorkspaceRoot, entry.TopicID, id)
			} else {
				tab = a.createTabEntryWithID("global", globalTabWorkspaceRoot(), entry.TopicID, id)
			}
			tab.model = entry.Model
			tab.effort = cloneStringPtr(entry.Effort)
			tab.mode = persistedTabMode(entry.Mode)
			tab.workspaceType = normalizeWorkspaceType(entry.WorkspaceType)
			tab.SessionPath = strings.TrimSpace(entry.SessionPath)
			tab.sink = &tabEventSink{tabID: tab.ID, app: a, ctx: ctx}
			a.mu.Lock()
			a.tabs[tab.ID] = tab
			a.tabOrder = append(a.tabOrder, tab.ID)
			a.mu.Unlock()
			toBuild = append(toBuild, tab)
		}
		a.mu.Lock()
		if _, ok := a.tabs[f.ActiveTab]; ok {
			a.activeTabID = f.ActiveTab
		} else {
			ordered := a.orderedTabIDsLocked()
			if len(ordered) > 0 {
				a.activeTabID = ordered[0]
			}
		}
		a.mu.Unlock()
		for _, tab := range toBuild {
			a.startTabControllerBuild(tab)
		}
		return
	}

	// First launch: create a default Global tab.
	tab := a.createTabEntry("global", globalTabWorkspaceRoot(), "")
	tab.sink = &tabEventSink{tabID: tab.ID, app: a, ctx: ctx}
	tab.TopicTitle = "Global"
	a.mu.Lock()
	a.tabs[tab.ID] = tab
	a.tabOrder = append(a.tabOrder, tab.ID)
	a.activeTabID = tab.ID
	a.mu.Unlock()
	a.startTabControllerBuild(tab)
}

func (a *App) createTabEntry(scope, workspaceRoot, topicID string) *WorkspaceTab {
	return a.createTabEntryWithID(scope, workspaceRoot, topicID, newTabID())
}

func (a *App) createTabEntryWithID(scope, workspaceRoot, topicID, id string) *WorkspaceTab {
	return &WorkspaceTab{
		ID:            id,
		Scope:         scope,
		WorkspaceRoot: workspaceRoot,
		TopicID:       topicID,
		TopicTitle:    topicTitleForTab(scope, workspaceRoot, topicID),
		mode:          "normal",
		disabledMCP:   map[string]ServerView{},
	}
}

func (a *App) snapshotAllTabs() {
	a.mu.RLock()
	tabs := make([]*WorkspaceTab, 0, len(a.tabs))
	for _, t := range a.tabs {
		tabs = append(tabs, t)
	}
	a.mu.RUnlock()
	for _, t := range tabs {
		if t.Ctrl != nil {
			_ = t.Ctrl.Snapshot()
		}
	}
}

// shutdown snapshots all tabs, saves the final window geometry, and closes tabs.
func (a *App) shutdown(context.Context) {
	a.stopTray()
	// Kill all live terminal PTY sessions so no orphan shell processes survive.
	if a.terminals != nil {
		a.terminals.closeAll()
	}
	// Save window geometry synchronously from Go so it's persisted even if the
	// frontend's beforeunload promise hasn't resolved yet.
	a.saveWindowStateSync()

	// Stop the cron scheduler before closing the data store.
	if a.sched != nil {
		a.sched.Stop()
	}

	if a.dataStore != nil {
		a.dataStore.Close()
	}

	// Stop the App-level IM processor first so its poll watcher exits before
	// we close tab controllers (avoids racing on shared plugin host state).
	a.stopIMProcessor()

	a.mu.RLock()
	tabs := make([]*WorkspaceTab, 0, len(a.tabs))
	for _, t := range a.tabs {
		tabs = append(tabs, t)
	}
	a.mu.RUnlock()
	for _, t := range tabs {
		if t.Ctrl != nil {
			_ = t.Ctrl.Snapshot()
			t.Ctrl.Close()
		}
	}
}

// domReady is called (via OnDomReady) after the webview finishes loading its DOM
// but before the window is shown (StartHidden). It restores the saved window
// position and size, then calls WindowShow so the user never sees the default
// size/position flash.
func (a *App) domReady(_ context.Context) {
	state, ok := loadWindowState()
	if ok {
		// Validate saved position against current screens. Wails v2 doesn't
		// expose per-screen origin (x,y offsets) so we can only do a basic
		// sanity check: ensure the window origin falls within a generous
		// estimate of the screen area. If the user unplugged an external
		// display, negative or out-of-bounds coordinates are caught here.
		valid := state.X >= 0 && state.Y >= 0
		if valid {
			screens, err := runtime.ScreenGetAll(a.ctx)
			if err == nil && len(screens) > 0 {
				maxW, maxH := 0, 0
				for _, sc := range screens {
					if sc.Size.Width > maxW {
						maxW = sc.Size.Width
					}
					if sc.Size.Height > maxH {
						maxH = sc.Size.Height
					}
				}
				if state.X > maxW*2 || state.Y > maxH*2 {
					valid = false
				}
			}
		}
		if valid {
			runtime.WindowSetPosition(a.ctx, state.X, state.Y)
		} else {
			runtime.WindowCenter(a.ctx)
		}
	} else {
		runtime.WindowCenter(a.ctx)
	}

	if ok && state.Maximised {
		runtime.WindowMaximise(a.ctx)
	}

	runtime.WindowShow(a.ctx)
}

// --- bound command surface (frontend → controller) ---
// Each method guards on a nil controller so a pre-startup or failed-build call is
// a no-op, never a panic.

// Submit runs raw user input as a turn; slash commands and @-references are
// resolved by the controller. Output arrives asynchronously on eventChannel.
func (a *App) Submit(input string) {
	a.SubmitToTab("", input)
}

func (a *App) SubmitToTab(tabID, input string) {
	defer recoverPanic("SubmitToTab")
	trimmed := strings.TrimSpace(input)
	if trimmed == "/effort" || strings.HasPrefix(trimmed, "/effort ") {
		a.runEffortCommandForTab(tabID, trimmed)
		return
	}
	if ctrl := a.ctrlByTabID(tabID); ctrl != nil {
		ctrl.SubmitDisplay(input, input)
	}
}

// RunShell executes a shell command directly (bypassing the model) and streams
// output as events on eventChannel.
func (a *App) RunShell(command string) {
	a.RunShellForTab("", command)
}

func (a *App) RunShellForTab(tabID, command string) {
	defer recoverPanic("RunShellForTab")
	if ctrl := a.ctrlByTabID(tabID); ctrl != nil {
		ctrl.RunShell(command)
	}
}

// SubmitDisplay runs input as a turn while recording a shorter UI-only display
// string for the saved desktop transcript. The model still receives input.
func (a *App) SubmitDisplay(display, input string) {
	a.SubmitDisplayToTab("", display, input)
}

func (a *App) SubmitDisplayToTab(tabID, display, input string) {
	defer recoverPanic("SubmitDisplayToTab")
	ctrl := a.ctrlByTabID(tabID)
	if ctrl == nil {
		return
	}
	ctrl.SubmitDisplay(display, input)
}

// executeScheduledTask is the callback invoked by the scheduler when a scheduled
// task triggers. It submits the skill as a turn to the active workspace (or the
// first available workspace if none is active). The return value is a summary
// string recorded as the execution result.
//
// The params string is a JSON object that may carry two reserved keys:
//   - "_workspace": project root path; the task targets the tab whose
//     WorkspaceRoot matches this value (falls back to active/first tab).
//   - "_prompt": custom prompt text; when set it replaces the default
//     "/<skill> <params>" input entirely.
//
// Any remaining keys are passed through as the skill parameters.
func (a *App) executeScheduledTask(name, skill, params string) string {
	// Workflow cron trigger: name carries the "wf:" prefix. Route to
	// RunWorkflow instead of the recipe "/<skill> <params>" path. The
	// params string (if any) becomes the workflow input.
	if strings.HasPrefix(name, "wf:") {
		wfName := strings.TrimPrefix(name, "wf:")
		if err := a.RunWorkflow(wfName, params); err != nil {
			return fmt.Sprintf("workflow %q trigger failed: %v", wfName, err)
		}
		return "workflow triggered"
	}

	// Parse reserved keys from the parameters JSON.
	workspace := ""
	prompt := ""
	restParams := params
	if params != "" {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal([]byte(params), &obj); err == nil {
			if w, ok := obj["_workspace"]; ok {
				var ws string
				if json.Unmarshal(w, &ws) == nil {
					workspace = strings.TrimSpace(ws)
				}
			}
			if p, ok := obj["_prompt"]; ok {
				var ps string
				if json.Unmarshal(p, &ps) == nil {
					prompt = ps
				}
			}
			// Re-encode remaining keys (excluding reserved ones).
			rest := make(map[string]json.RawMessage, len(obj))
			for k, v := range obj {
				if k != "_workspace" && k != "_prompt" {
					rest[k] = v
				}
			}
			if len(rest) > 0 {
				if b, err := json.Marshal(rest); err == nil {
					restParams = string(b)
				}
			} else {
				restParams = ""
			}
		}
	}

	a.mu.RLock()
	ctrl := a.activeCtrlLocked()
	// If a specific workspace is requested, prefer a tab whose WorkspaceRoot matches.
	if workspace != "" {
		target := normalizeProjectRoot(workspace)
		for _, tab := range a.tabs {
			if tab.Scope == "project" && normalizeProjectRoot(tab.WorkspaceRoot) == target && tab.Ctrl != nil {
				ctrl = tab.Ctrl
				break
			}
		}
	}
	if ctrl == nil && len(a.tabs) > 0 {
		// Pick the first tab if no active one.
		for _, tab := range a.tabs {
			ctrl = tab.Ctrl
			break
		}
	}
	a.mu.RUnlock()

	if ctrl == nil {
		return "no workspace available"
	}

	// Build the input: custom prompt takes precedence; otherwise /<skill> <params>.
	input := prompt
	if input == "" {
		input = "/" + skill
		if restParams != "" {
			input = input + " " + restParams
		}
	}
	display := "[Scheduled: " + name + "]"
	ctrl.SubmitDisplay(display, input)
	return "submitted to workspace"
}

// onScheduledTaskStart is called when a scheduled task begins execution.
// It emits a frontend event so the UI can open a dedicated tab and sends a
// system notification to alert the user.
func (a *App) onScheduledTaskStart(name, skill string) {
	if a.ctx == nil {
		return
	}
	// Emit frontend event for tab creation.
	wruntime.EventsEmit(a.ctx, "scheduled_task_started", map[string]string{
		"name":  name,
		"skill": skill,
	})
	// Bring window to front if minimized.
	wruntime.WindowShow(a.ctx)
}

// onScheduledTaskDone is called when a scheduled task completes execution.
// It emits a frontend event and sends a system notification with the result.
func (a *App) onScheduledTaskDone(name, skill, result string) {
	if a.ctx == nil {
		return
	}
	title := "定时任务已完成"
	body := skill + " 执行完毕"
	if result != "" && result != "submitted to workspace" {
		body = body + ": " + result
	}
	wruntime.EventsEmit(a.ctx, "scheduled_task_completed", map[string]string{
		"name":   name,
		"skill":  skill,
		"result": result,
	})
	// System notification via tray or native notify (if available).
	if a.tray != nil {
		a.tray.Notify(title, body)
	}
}

// OpenTabForScheduledTask creates a new tab for a scheduled task execution.
// The tab title includes the task name and current date for easy identification.
func (a *App) OpenTabForScheduledTask(taskName string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Use first available workspace root or fall back to global scope.
	var workspaceRoot string
	for _, tab := range a.tabs {
		if tab.Scope == "project" && tab.WorkspaceRoot != "" {
			workspaceRoot = tab.WorkspaceRoot
			break
		}
	}

	scope := "project"
	if workspaceRoot == "" {
		scope = "global"
	}

	// Create topic with timestamp in title.
	dateStr := time.Now().Format("2006-01-02")
	topicTitle := fmt.Sprintf("定时任务：%s %s", taskName, dateStr)
	topic, err := a.CreateTopic(scope, workspaceRoot, topicTitle)
	if err != nil {
		return fmt.Errorf("create topic: %w", err)
	}

	// Create new tab entry.
	tab := a.createTabEntry(scope, workspaceRoot, topic.ID)
	a.tabs[tab.ID] = tab
	a.activeTabID = tab.ID

	// Emit tab list update event.
	wruntime.EventsEmit(a.ctx, "tabs:changed")
	return nil
}

// RunAnalyzeProject submits the /analyze-project slash command as a turn. The
// display string is the user-friendly "Repo Wiki" label; the input carries the
// slash command + project path so the model resolves it through the skill runner.
// If model is non-empty the active model is switched first (same as /model).
func (a *App) RunAnalyzeProject(projectPath string, model string) {
	if model != "" {
		_ = a.SetModel(model)
	}
	input := "/analyze-project"
	if p := strings.TrimSpace(projectPath); p != "" {
		input = input + " " + p
	}
	a.SubmitToTab("", input)
}

func (a *App) bindControllerDisplayRecorder(ctrl *control.Controller) {
	if ctrl == nil {
		return
	}
	ctrl.SetDisplayRecorder(func(content, display string) {
		dir := ctrl.SessionDir()
		if dir == "" {
			dir = config.SessionDir()
		}
		_ = recordSessionDisplay(dir, ctrl.SessionPath(), content, display)
	})
}

// Cancel aborts the in-flight turn.
func (a *App) Cancel() {
	a.CancelTab("")
}

func (a *App) CancelTab(tabID string) {
	defer recoverPanic("CancelTab")
	if ctrl := a.ctrlByTabID(tabID); ctrl != nil {
		ctrl.Cancel()
	}
}

// Approve answers a pending approval_request by ID: allow runs the call, session
// also remembers the grant for the rest of the session.
func (a *App) Approve(id string, allow, session, persist bool) {
	a.ApproveTab("", id, allow, session, persist)
}

func (a *App) ApproveTab(tabID, id string, allow, session, persist bool) {
	defer recoverPanic("ApproveTab")
	ctrl := a.ctrlByTabID(tabID)
	if ctrl != nil {
		ctrl.Approve(id, allow, session, persist)
	}
}

// SetPlanMode toggles read-only plan mode.
func (a *App) SetPlanMode(on bool) {
	if on {
		a.SetModeForTab("", "plan")
		return
	}
	a.SetModeForTab("", "normal")
}

// SetMode applies a composer gating mode ("plan" | "yolo" | anything else =
// normal) in one call, so a turn submitted right after the switch can't race a
// half-applied SetPlanMode/SetBypass pair.
func (a *App) SetMode(mode string) {
	a.SetModeForTab("", mode)
}

func (a *App) SetModeForTab(tabID, mode string) {
	normalized := normalizeTabMode(mode)
	a.mu.Lock()
	tab := a.tabByIDLocked(tabID)
	if tab == nil {
		a.mu.Unlock()
		return
	}
	tab.mode = normalized
	ctrl := tab.Ctrl
	tabIDForSave := tab.ID
	a.mu.Unlock()
	applyTabModeToController(ctrl, normalized)
	a.mu.Lock()
	if a.tabs[tabIDForSave] == tab {
		a.saveTabsLocked()
	}
	a.mu.Unlock()
}

func applyTabModeToController(ctrl *control.Controller, mode string) {
	if ctrl == nil {
		return
	}
	switch normalizeTabMode(mode) {
	case "plan":
		ctrl.SetMode(true, false)
	case "yolo":
		ctrl.SetMode(false, true)
	default:
		ctrl.SetMode(false, false)
	}
}

// QuestionAnswer is the frontend's reply to one question in an ask_request.
type QuestionAnswer struct {
	QuestionID string   `json:"questionId"`
	Selected   []string `json:"selected"`
}

// AnswerQuestion resolves a pending ask_request (the `ask` tool) by ID with the
// user's selections per question.
func (a *App) AnswerQuestion(id string, answers []QuestionAnswer) {
	a.AnswerQuestionForTab("", id, answers)
}

func (a *App) AnswerQuestionForTab(tabID, id string, answers []QuestionAnswer) {
	ctrl := a.ctrlByTabID(tabID)
	if ctrl == nil {
		return
	}
	out := make([]event.AskAnswer, len(answers))
	for i, an := range answers {
		out[i] = event.AskAnswer{QuestionID: an.QuestionID, Selected: an.Selected}
	}
	ctrl.AnswerQuestion(id, out)
}

// Compact runs one compaction pass on demand.
// Compact runs a plain compaction pass (the "compact now" button). Focus-guided
// compaction goes through Submit("/compact <focus>") instead.
func (a *App) Compact() error {
	a.mu.RLock()
	ctrl := a.activeCtrlLocked()
	a.mu.RUnlock()
	if ctrl == nil {
		return nil
	}
	return ctrl.Compact(a.ctx, "")
}

// NewSession snapshots the current conversation and rotates to a fresh one.
func (a *App) NewSession() error {
	a.mu.RLock()
	tab := a.activeTabLocked()
	ctrl := a.activeCtrlLocked()
	a.mu.RUnlock()
	if ctrl == nil {
		return nil
	}
	if err := ctrl.NewSession(); err != nil {
		return err
	}
	a.persistTabSessionPath(tab, ctrl.SessionPath())
	return nil
}

// CheckpointMeta summarises one rewind point (a user turn) for the desktop.
type CheckpointMeta struct {
	Turn            int      `json:"turn"`
	Prompt          string   `json:"prompt"`
	Files           []string `json:"files"` // paths changed during the turn
	Time            int64    `json:"time"`  // unix milliseconds
	CanCode         bool     `json:"canCode"`
	CanConversation bool     `json:"canConversation"`
}

// Checkpoints lists the session's rewind points, oldest first, for the rewind UI.
func (a *App) Checkpoints() []CheckpointMeta {
	return a.CheckpointsForTab("")
}

func (a *App) CheckpointsForTab(tabID string) []CheckpointMeta {
	a.mu.RLock()
	var ctrl *control.Controller
	if tab := a.tabByIDLocked(tabID); tab != nil {
		ctrl = tab.Ctrl
	}
	a.mu.RUnlock()
	if ctrl == nil {
		return []CheckpointMeta{}
	}
	metas := ctrl.Checkpoints()
	out := make([]CheckpointMeta, 0, len(metas))
	for _, m := range metas {
		out = append(out, CheckpointMeta{
			Turn:            m.Turn,
			Prompt:          m.Prompt,
			Files:           m.Paths,
			Time:            m.Time.UnixMilli(),
			CanCode:         len(m.Paths) > 0,
			CanConversation: ctrl.CheckpointHasBoundary(m.Turn),
		})
	}
	return out
}

// Rewind restores the session to the start of turn. scope is "code",
// "conversation", or "both" (anything else is treated as "both"). The frontend
// re-reads History after this resolves.
func (a *App) Rewind(turn int, scope string) error {
	a.mu.RLock()
	ctrl := a.activeCtrlLocked()
	a.mu.RUnlock()
	if ctrl == nil {
		return nil
	}
	s := control.RewindBoth
	switch scope {
	case "code":
		s = control.RewindCode
	case "conversation":
		s = control.RewindConversation
	}
	return ctrl.Rewind(turn, s)
}

// Fork branches the conversation at the start of turn into a new session tab
// (preserving the current tab), keeping code intact, and switches to the new tab.
func (a *App) Fork(turn int) (TabMeta, error) {
	a.mu.RLock()
	sourceTab := a.activeTabLocked()
	ctrl := a.activeCtrlLocked()
	if sourceTab == nil || ctrl == nil {
		a.mu.RUnlock()
		return TabMeta{}, nil
	}
	scope := sourceTab.Scope
	workspaceRoot := sourceTab.WorkspaceRoot
	sourceTitle := sourceTab.TopicTitle
	model := sourceTab.model
	effort := cloneStringPtr(sourceTab.effort)
	mode := currentTabMode(sourceTab)
	disabledMCP := cloneServerViewMap(sourceTab.disabledMCP)
	mcpOrder := append([]string(nil), sourceTab.mcpOrder...)
	a.mu.RUnlock()

	newPath, err := ctrl.ForkSession(turn, "")
	if err != nil {
		return TabMeta{}, err
	}
	topicID := newTopicID()
	topicTitle := forkTopicTitle(sourceTitle)
	titleRoot := workspaceRoot
	if scope == "global" {
		titleRoot = ""
	}
	if err := setTopicTitle(titleRoot, topicID, topicTitle); err != nil {
		return TabMeta{}, err
	}
	m, _ := agent.EnsureBranchMeta(newPath)
	m.Scope = scope
	m.WorkspaceRoot = workspaceRoot
	m.TopicID = topicID
	m.TopicTitle = topicTitle
	if err := agent.SaveBranchMeta(newPath, m); err != nil {
		return TabMeta{}, err
	}

	a.mu.Lock()
	tabID := a.newUniqueTabIDLocked()
	tab := &WorkspaceTab{
		ID:            tabID,
		Scope:         scope,
		WorkspaceRoot: workspaceRoot,
		TopicID:       topicID,
		TopicTitle:    topicTitle,
		SessionPath:   newPath,
		model:         model,
		effort:        effort,
		mode:          mode,
		disabledMCP:   disabledMCP,
		mcpOrder:      mcpOrder,
	}
	tab.sink = &tabEventSink{tabID: tabID, app: a}
	a.tabs[tabID] = tab
	a.tabOrder = append(a.tabOrder, tabID)
	a.activeTabID = tabID
	a.saveTabsLocked()
	meta := a.tabMeta(tab, true)
	a.mu.Unlock()

	a.emitProjectTreeChanged()
	a.startTabControllerBuild(tab)
	wruntime.EventsEmit(a.ctx, "tabs:changed")
	return meta, nil
}

// of turn into one summary (Claude Code's "summarize from/up to here"), keeping
// code intact. The frontend re-reads History after this resolves.
func (a *App) SummarizeFrom(turn int) error {
	a.mu.RLock()
	ctrl := a.activeCtrlLocked()
	a.mu.RUnlock()
	if ctrl == nil {
		return nil
	}
	return ctrl.SummarizeFrom(a.ctx, turn)
}

func (a *App) SummarizeUpTo(turn int) error {
	a.mu.RLock()
	ctrl := a.activeCtrlLocked()
	a.mu.RUnlock()
	if ctrl == nil {
		return nil
	}
	return ctrl.SummarizeUpTo(a.ctx, turn)
}

// SessionMeta summarises one saved session for the history panel.
type SessionMeta struct {
	Path           string `json:"path"`
	Preview        string `json:"preview"`         // first user message
	Title          string `json:"title,omitempty"` // user-chosen name, when set (overrides preview)
	Turns          int    `json:"turns"`
	CreatedAt      int64  `json:"createdAt"`      // unix milliseconds
	LastActivityAt int64  `json:"lastActivityAt"` // unix milliseconds
	ModTime        int64  `json:"modTime"`        // compatibility alias for lastActivityAt
	DeletedAt      int64  `json:"deletedAt,omitempty"`
	Current        bool   `json:"current"`
	Open           bool   `json:"open"`
	Scope          string `json:"scope,omitempty"`
	WorkspaceRoot  string `json:"workspaceRoot,omitempty"`
	TopicID        string `json:"topicId,omitempty"`
	TopicTitle     string `json:"topicTitle,omitempty"`
}

type WorkspaceMeta struct {
	Path    string `json:"path"`
	Name    string `json:"name"`
	Current bool   `json:"current"`
}

// ListSessions returns the saved sessions newest-first for the history panel,
// marking the one the current conversation is writing to and attaching any
// user-chosen titles.
func (a *App) ListSessions() []SessionMeta {
	dir := config.SessionDir()
	infos, err := agent.ListSessions(dir)
	if err != nil {
		return []SessionMeta{}
	}
	titles := loadSessionTitles(dir)
	open := a.openSessionPaths(dir)
	active := a.activeSessionPath(dir)
	out := make([]SessionMeta, 0, len(infos))
	for _, s := range infos {
		_, isOpen := open[s.Path]
		out = append(out, sessionMetaFromInfo(s, titles[filepath.Base(s.Path)], s.Path == active, isOpen, 0))
	}
	return out
}

// ListTrashedSessions returns sessions that were moved to the local trash,
// newest-deleted first. These can be previewed, restored, or permanently purged.
func (a *App) ListTrashedSessions() []SessionMeta {
	dir := config.SessionDir()
	paths, err := listTrashedSessionFiles(dir)
	if err != nil {
		return []SessionMeta{}
	}
	titles := loadSessionTitles(dir)
	out := make([]SessionMeta, 0, len(paths))
	for _, path := range paths {
		infos, err := agent.ListSessions(filepath.Dir(path))
		if err != nil || len(infos) == 0 {
			continue
		}
		deletedAt := trashedSessionDeletedAt(path)
		out = append(out, sessionMetaFromInfo(infos[0], titles[filepath.Base(path)], false, false, deletedAt))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].DeletedAt == out[j].DeletedAt {
			return out[i].LastActivityAt > out[j].LastActivityAt
		}
		return out[i].DeletedAt > out[j].DeletedAt
	})
	return out
}

func sessionMetaFromInfo(s agent.SessionInfo, title string, current, open bool, deletedAt int64) SessionMeta {
	return SessionMeta{
		Path:           s.Path,
		Preview:        s.Preview,
		Title:          title,
		Turns:          s.Turns,
		CreatedAt:      s.CreatedAt.UnixMilli(),
		LastActivityAt: s.LastActivityAt.UnixMilli(),
		ModTime:        s.LastActivityAt.UnixMilli(),
		DeletedAt:      deletedAt,
		Current:        current,
		Open:           open,
		Scope:          s.Scope,
		WorkspaceRoot:  s.WorkspaceRoot,
		TopicID:        s.TopicID,
		TopicTitle:     s.TopicTitle,
	}
}

// DeleteSession moves a saved session to the local trash. It refuses any open
// session because tab auto-save would recreate or append to the file later.
func (a *App) DeleteSession(path string) error {
	dir := config.SessionDir()
	sessionPath, key, err := validateSessionPath(dir, path)
	if err != nil {
		return err
	}
	if _, ok := a.openSessionPaths(dir)[sessionPath]; ok {
		return errActiveSession
	}
	if err := trashSessionArtifacts(dir, sessionPath, key); err != nil {
		return err
	}
	a.emitProjectTreeChanged()
	return nil
}

func (a *App) openSessionPaths(dir string) map[string]struct{} {
	a.mu.RLock()
	paths := make([]string, 0, len(a.tabs))
	for _, tab := range a.tabs {
		if tab != nil {
			paths = append(paths, tab.currentSessionPath())
		}
	}
	a.mu.RUnlock()

	out := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		currentPath, _, err := validateSessionPath(dir, path)
		if err == nil {
			out[currentPath] = struct{}{}
		}
	}
	return out
}

func (a *App) activeSessionPath(dir string) string {
	a.mu.RLock()
	var path string
	if tab := a.tabs[a.activeTabID]; tab != nil {
		path = tab.currentSessionPath()
	}
	a.mu.RUnlock()
	currentPath, _, err := validateSessionPath(dir, path)
	if err != nil {
		return ""
	}
	return currentPath
}

// RestoreSession moves a trashed session back into the saved-session list.
func (a *App) RestoreSession(path string) error {
	dir := config.SessionDir()
	_, key, _, err := validateTrashedSessionPath(dir, path)
	if err != nil {
		return err
	}
	if err := restoreTrashedSessionFile(dir, path); err != nil {
		return err
	}
	if err := restoreSessionTopicIndex(dir, filepath.Join(dir, key)); err != nil {
		return err
	}
	a.emitProjectTreeChanged()
	return nil
}

// PurgeTrashedSession permanently removes a trashed session and its title/display
// sidecars.
func (a *App) PurgeTrashedSession(path string) error {
	return purgeTrashedSessionFile(config.SessionDir(), path)
}

// RenameSession sets a custom display name for a session (empty clears it back to
// the preview). It only affects the history panel; the file on disk is unchanged.
func (a *App) RenameSession(path, title string) error {
	return setSessionTitle(config.SessionDir(), path, title)
}

// ResumeSession snapshots the current conversation, then loads the session at
// path and continues it on the active tab. The model and working folder are
// unchanged; only the transcript is swapped. Returns the resumed messages for
// the frontend to render.
func (a *App) ResumeSession(path string) ([]HistoryMessage, error) {
	return a.ResumeSessionForTab("", path)
}

// ResumeSessionForTab is the tab-scoped form of ResumeSession. History rows
// carry scope/workspace/topic metadata, so callers that opened or selected a
// matching tab should resume on that exact controller instead of whichever tab is
// active by the time the async call reaches the backend.
func (a *App) ResumeSessionForTab(tabID, path string) ([]HistoryMessage, error) {
	tab := a.tabByID(tabID)
	if tab == nil || tab.Ctrl == nil {
		return []HistoryMessage{}, fmt.Errorf("tab is not ready")
	}
	ctrl := tab.Ctrl
	loaded, err := agent.LoadSession(path)
	if err != nil {
		return nil, err
	}
	_ = ctrl.Snapshot() // persist the current session before switching away
	ctrl.Resume(loaded, path)
	a.rememberTabSessionPath(tab, path)
	return a.HistoryForTab(tabID), nil
}

// PreviewSession reads a saved session for display only. It does not snapshot or
// swap the active controller, so the history drawer can call it while a turn runs.
func (a *App) PreviewSession(path string) ([]HistoryMessage, error) {
	return previewSessionMessages(config.SessionDir(), path)
}

// PickWorkspace opens a folder chooser and, on a pick, opens a new project tab
// scoped to that folder. Returns the chosen path ("" if cancelled).
func (a *App) PickWorkspace() (string, error) {
	if a.ctx == nil {
		return "", nil
	}
	cur, _ := os.Getwd()
	a.mu.RLock()
	if tab := a.activeTabLocked(); tab != nil && tab.WorkspaceRoot != "" {
		cur = tab.WorkspaceRoot
	}
	a.mu.RUnlock()
	dir, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title:            "Choose working folder",
		DefaultDirectory: dialogDefaultDirectory(cur),
	})
	if err != nil || dir == "" {
		return "", err
	}
	return a.SwitchWorkspace(dir)
}

func dialogDefaultDirectory(preferred string) string {
	if dir := nearestExistingDirectory(preferred); dir != "" {
		return dir
	}
	if cwd, err := os.Getwd(); err == nil {
		if dir := nearestExistingDirectory(cwd); dir != "" {
			return dir
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		if dir := nearestExistingDirectory(home); dir != "" {
			return dir
		}
	}
	return ""
}

func nearestExistingDirectory(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	for {
		info, err := os.Stat(path)
		if err == nil {
			if info.IsDir() {
				return path
			}
			path = filepath.Dir(path)
			continue
		}
		parent := filepath.Dir(path)
		if parent == path {
			return ""
		}
		path = parent
	}
}

func (a *App) ListWorkspaces() []WorkspaceMeta {
	migrateLegacyWorkspacesIntoProjects()
	activeRoot := ""
	cur, _ := os.Getwd()
	a.mu.RLock()
	if tab := a.activeTabLocked(); tab != nil && tab.WorkspaceRoot != "" {
		activeRoot = normalizeProjectRoot(tab.WorkspaceRoot)
	}
	a.mu.RUnlock()
	if activeRoot == "" {
		activeRoot = normalizeProjectRoot(cur)
	}
	projects := loadProjectsFile().Projects
	out := make([]WorkspaceMeta, 0, len(projects))
	for _, project := range projects {
		out = append(out, WorkspaceMeta{
			Path:    project.Root,
			Name:    projectDisplayName(project),
			Current: activeRoot != "" && project.Root == activeRoot,
		})
	}
	return out
}

func (a *App) RemoveWorkspace(dir string) error {
	if dir == "" {
		return fmt.Errorf("workspace path is required")
	}
	dir = normalizeProjectRoot(dir)
	forgetWorkspace(dir)
	if err := removeProject(dir); err != nil {
		return err
	}
	a.emitProjectTreeChanged()
	return nil
}

func migrateLegacyWorkspacesIntoProjects() {
	legacy := loadWorkspaces()
	if len(legacy) == 0 {
		return
	}
	f := loadProjectsFile()
	seen := make(map[string]bool, len(f.Projects)+len(legacy))
	for _, p := range f.Projects {
		seen[p.Root] = true
	}
	changed := false
	for _, path := range legacy {
		root := normalizeProjectRoot(path)
		if root == "" || seen[root] {
			continue
		}
		f.Projects = append(f.Projects, desktopProject{Root: root})
		seen[root] = true
		changed = true
	}
	if changed {
		_ = saveProjectsFile(f)
	}
}

func workspaceName(path string) string {
	name := filepath.Base(path)
	if name == "." || name == string(filepath.Separator) || name == "" {
		return path
	}
	return name
}

func (a *App) SwitchWorkspace(dir string) (string, error) {
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = home
	}
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	info, err := os.Stat(dir)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory", dir)
	}
	saveWorkspace(dir)

	// Open a registered topic so the new workspace appears in the project tree
	// immediately instead of only existing as an in-memory tab.
	topic, err := a.CreateTopic("project", dir, "")
	if err != nil {
		return "", err
	}
	meta, err := a.OpenProjectTab(dir, topic.ID)
	if err != nil {
		return "", err
	}
	return meta.WorkspaceRoot, nil
}

// HistoryMessage is one prior turn, for the frontend to repopulate its transcript
// after a reload.
type HistoryMessage struct {
	Role       string            `json:"role"`
	Content    string            `json:"content"`
	Reasoning  string            `json:"reasoning,omitempty"`
	Level      string            `json:"level,omitempty"`
	ToolCalls  []HistoryToolCall `json:"toolCalls,omitempty"`
	ToolCallID string            `json:"toolCallId,omitempty"`
	ToolName   string            `json:"toolName,omitempty"`
	Pending    bool              `json:"pending,omitempty"`
	Trigger    string            `json:"trigger,omitempty"`
	Messages   int               `json:"messages,omitempty"`
	Summary    string            `json:"summary,omitempty"`
	Archive    string            `json:"archive,omitempty"`
}

type HistoryToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// History returns the session's message log.
func (a *App) History() []HistoryMessage {
	return a.HistoryForTab("")
}

func (a *App) HistoryForTab(tabID string) []HistoryMessage {
	ctrl := a.ctrlByTabID(tabID)
	if ctrl == nil {
		return []HistoryMessage{}
	}
	msgs := ctrl.History()
	return historyMessages(msgs, sessionDisplayResolver(config.SessionDir(), ctrl.SessionPath()))
}

func historyMessages(msgs []provider.Message, resolveUserContent func(string) string) []HistoryMessage {
	out := make([]HistoryMessage, 0, len(msgs))
	for _, m := range msgs {
		content := m.Content
		if m.Role == provider.RoleUser {
			content = resolveUserContent(m.Content)
		}
		reasoning := ""
		if m.Role == provider.RoleAssistant {
			reasoning = m.ReasoningContent
		}
		hm := HistoryMessage{Role: string(m.Role), Content: content, Reasoning: reasoning}
		if m.Role == provider.RoleAssistant && len(m.ToolCalls) > 0 {
			hm.ToolCalls = make([]HistoryToolCall, len(m.ToolCalls))
			for i, tc := range m.ToolCalls {
				hm.ToolCalls[i] = HistoryToolCall{ID: tc.ID, Name: tc.Name, Arguments: tc.Arguments}
			}
		}
		if m.Role == provider.RoleTool {
			hm.ToolCallID = m.ToolCallID
			hm.ToolName = m.Name
		}
		out = append(out, hm)
	}
	return out
}

func previewSessionMessages(sessionDir, path string) ([]HistoryMessage, error) {
	if out, ok, err := previewEventSessionMessages(path); ok || err != nil {
		return out, err
	}
	loaded, err := agent.LoadSession(path)
	if err != nil {
		return nil, err
	}
	return historyMessages(loaded.Snapshot(), sessionDisplayResolver(sessionDir, path)), nil
}

type previewEventRecord struct {
	Kind             string             `json:"kind"`
	Type             string             `json:"type"`
	Role             string             `json:"role"`
	Text             string             `json:"text"`
	Content          string             `json:"content"`
	Reasoning        string             `json:"reasoning"`
	ReasoningContent string             `json:"reasoningContent"`
	Level            string             `json:"level"`
	ToolCalls        []previewToolCall  `json:"toolCalls"`
	CallID           string             `json:"callId"`
	ToolCallID       string             `json:"toolCallId"`
	ToolName         string             `json:"toolName"`
	Name             string             `json:"name"`
	Output           string             `json:"output"`
	Compaction       *previewCompaction `json:"compaction"`
	Trigger          string             `json:"trigger"`
	Messages         int                `json:"messages"`
	Summary          string             `json:"summary"`
	Archive          string             `json:"archive"`
}

type previewToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
	Function  struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type previewCompaction struct {
	Trigger  string `json:"trigger"`
	Messages int    `json:"messages"`
	Summary  string `json:"summary"`
	Archive  string `json:"archive"`
}

func previewEventSessionMessages(path string) ([]HistoryMessage, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer f.Close()

	dec := json.NewDecoder(f)
	out := []HistoryMessage{}
	toolName := map[string]string{}
	sawEvent := false
	for {
		var rec previewEventRecord
		if err := dec.Decode(&rec); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			if sawEvent {
				return out, true, nil
			}
			return nil, false, nil
		}
		eventName := strings.TrimSpace(rec.Kind)
		if eventName == "" {
			eventName = strings.TrimSpace(rec.Type)
		}
		if eventName == "" {
			continue
		}
		sawEvent = true
		switch eventName {
		case "user.message":
			if rec.Text != "" {
				out = append(out, HistoryMessage{Role: "user", Content: rec.Text})
			}
		case "model.final":
			hm := HistoryMessage{Role: "assistant", Content: rec.Content, Reasoning: firstNonEmpty(rec.Reasoning, rec.ReasoningContent)}
			for _, tc := range rec.ToolCalls {
				id := tc.ID
				name := firstNonEmpty(tc.Name, tc.Function.Name)
				args := firstNonEmpty(tc.Arguments, tc.Function.Arguments)
				hm.ToolCalls = append(hm.ToolCalls, HistoryToolCall{ID: id, Name: name, Arguments: args})
				if id != "" {
					toolName[id] = name
				}
			}
			out = append(out, hm)
		case "tool.result":
			callID := firstNonEmpty(rec.CallID, rec.ToolCallID)
			out = append(out, HistoryMessage{
				Role:       "tool",
				ToolCallID: callID,
				ToolName:   firstNonEmpty(rec.ToolName, rec.Name, toolName[callID]),
				Content:    firstNonEmpty(rec.Output, rec.Content),
			})
		case "phase":
			out = append(out, HistoryMessage{Role: "phase", Content: firstNonEmpty(rec.Text, rec.Content)})
		case "notice":
			level := rec.Level
			if level != "warn" {
				level = "info"
			}
			out = append(out, HistoryMessage{Role: "notice", Level: level, Content: firstNonEmpty(rec.Text, rec.Content)})
		case "compaction_started":
			c := rec.compactionPayload()
			out = append(out, HistoryMessage{Role: "compaction", Pending: true, Trigger: c.Trigger})
		case "compaction_done":
			c := rec.compactionPayload()
			out = append(out, HistoryMessage{
				Role:     "compaction",
				Trigger:  c.Trigger,
				Messages: c.Messages,
				Summary:  c.Summary,
				Archive:  c.Archive,
			})
		}
	}
	return out, sawEvent, nil
}

func (r previewEventRecord) compactionPayload() previewCompaction {
	if r.Compaction != nil {
		return *r.Compaction
	}
	return previewCompaction{Trigger: r.Trigger, Messages: r.Messages, Summary: r.Summary, Archive: r.Archive}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// ContextInfo is the prompt-vs-window gauge payload. Both zero means no data yet.
type ContextInfo struct {
	Used         int     `json:"used"`
	Window       int     `json:"window"`
	CompactRatio float64 `json:"compactRatio,omitempty"`
}

// ContextUsage returns the latest context-window gauge numbers.
func (a *App) ContextUsage() ContextInfo {
	return a.ContextUsageForTab("")
}

func (a *App) ContextUsageForTab(tabID string) ContextInfo {
	ctrl := a.ctrlByTabID(tabID)
	if ctrl == nil {
		return ContextInfo{}
	}
	used, window := ctrl.ContextSnapshot()
	return ContextInfo{Used: used, Window: window, CompactRatio: ctrl.CompactRatio()}
}

// BalanceInfo is the wallet-balance readout for the status bar. Available is true
// only when a balance was fetched; Display is the formatted amount (e.g. "¥110.00")
// and is "" when the active provider declares no balance_url — the frontend then
// omits the readout. Err carries a fetch failure for an optional tooltip.
type BalanceInfo struct {
	Available bool   `json:"available"`
	Display   string `json:"display"`
	Err       string `json:"err,omitempty"`
}

// Balance queries the active provider's wallet balance (a network call). It
// returns an empty (unavailable) readout when no provider balance_url is set, the
// controller is down, or the fetch fails — so the status bar simply shows nothing
// rather than an error.
func (a *App) Balance() BalanceInfo {
	return a.BalanceForTab("")
}

func (a *App) BalanceForTab(tabID string) BalanceInfo {
	ctrl := a.ctrlByTabID(tabID)
	if ctrl == nil {
		return BalanceInfo{}
	}
	b, err := ctrl.Balance(a.ctx)
	if err != nil {
		return BalanceInfo{Err: err.Error()}
	}
	if b == nil {
		return BalanceInfo{} // provider declares no balance endpoint
	}
	return BalanceInfo{Available: true, Display: b.Display()}
}

// JobView is one running background job (bash/task started with
// run_in_background) for the status-bar indicator.
type JobView struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Label     string `json:"label"`
	Status    string `json:"status"`
	StartedAt int64  `json:"startedAt"`
}

// Jobs returns the still-running background jobs for the status bar. It refreshes
// on demand (mount, turn end, and on each notice the frontend receives).
func (a *App) Jobs() []JobView {
	out := []JobView{}
	ctrl := a.ctrlByTabID("")
	return a.jobsForCtrl(ctrl, out)
}

func (a *App) JobsForTab(tabID string) []JobView {
	out := []JobView{}
	ctrl := a.ctrlByTabID(tabID)
	return a.jobsForCtrl(ctrl, out)
}

func (a *App) jobsForCtrl(ctrl *control.Controller, out []JobView) []JobView {
	if ctrl == nil {
		return out
	}
	for _, v := range ctrl.Jobs() {
		out = append(out, JobView{ID: v.ID, Kind: v.Kind, Label: v.Label, Status: v.Status, StartedAt: v.StartedAt})
	}
	return out
}

// Meta describes the session for the frontend's header and status line.
type Meta struct {
	Label        string `json:"label"`
	Ready        bool   `json:"ready"`
	StartupErr   string `json:"startupErr,omitempty"`
	EventChannel string `json:"eventChannel"`
	Cwd          string `json:"cwd"`
	Bypass       bool   `json:"bypass"` // YOLO mode on (auto-approve every tool call)
}

// Meta reports the model label, readiness, any startup error, the working
// directory (for the status line), and the runtime event channel the frontend
// subscribes to.
func (a *App) Meta() Meta {
	return a.MetaForTab("")
}

func (a *App) MetaForTab(tabID string) Meta {
	tab := a.tabByID(tabID)
	if tab == nil {
		return Meta{EventChannel: eventChannel}
	}
	cwd := tab.WorkspaceRoot
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	return Meta{
		Label:        tab.Label,
		Ready:        tab.Ready,
		StartupErr:   tab.StartupErr,
		EventChannel: eventChannel,
		Cwd:          cwd,
		Bypass:       tab.Ctrl != nil && tab.Ctrl.Bypass(),
	}
}

// SetBypass toggles YOLO mode for the session: auto-approve every tool call
// (writers and bash run without asking). Deny rules still apply. Runtime-only —
// not written to config, so it resets on relaunch.
func (a *App) SetBypass(on bool) {
	if on {
		a.SetModeForTab("", "yolo")
		return
	}
	a.SetModeForTab("", "normal")
}

// CommandInfo describes one available slash command for the composer's "/" menu.
type CommandInfo struct {
	Name        string `json:"name"` // without the leading slash
	Description string `json:"description"`
	Hint        string `json:"hint,omitempty"` // argument hint, if any
	Kind        string `json:"kind"`           // "builtin" | "custom" | "mcp"
}

// Commands lists the slash commands available this session — built-in actions,
// custom commands (.rexion/commands), and MCP prompts — for the composer's "/"
// autocomplete menu.
func (a *App) Commands() []CommandInfo {
	out := []CommandInfo{
		{Name: "new", Description: i18n.M.CmdNew, Kind: "builtin"},
		{Name: "compact", Description: i18n.M.CmdCompact, Kind: "builtin"},
		{Name: "model", Description: i18n.M.CmdModel, Kind: "builtin"},
		{Name: "effort", Description: i18n.M.CmdEffort, Kind: "builtin"},
		{Name: "memory", Description: i18n.M.CmdMemory, Kind: "builtin"},
		{Name: "remember", Description: i18n.M.CmdRemember, Kind: "builtin"},
		{Name: "mcp", Description: i18n.M.CmdMcp, Kind: "builtin"},
		{Name: "hooks", Description: i18n.M.CmdHooks, Kind: "builtin"},
		{Name: "theme", Description: i18n.M.CmdTheme, Kind: "builtin"},
		{Name: "skill", Description: i18n.M.CmdSkill, Kind: "builtin"},
	}
	a.mu.RLock()
	ctrl := a.activeCtrlLocked()
	a.mu.RUnlock()
	if ctrl == nil {
		return out
	}
	// Skills are invocable as /<name> (the model runs inline ones; subagent ones
	// run isolated). Listing them here is what surfaces /init, /explore, … in the
	// composer's slash menu; selecting one submits "/<name>", which the controller
	// resolves via RunSkill.
	for _, s := range ctrl.Skills() {
		out = append(out, CommandInfo{Name: s.Name, Description: s.Description, Kind: "skill"})
	}
	for _, c := range ctrl.Commands() {
		out = append(out, CommandInfo{Name: c.Name, Description: c.Description, Hint: c.ArgHint, Kind: "custom"})
	}
	if h := ctrl.Host(); h != nil {
		for _, p := range h.Prompts() {
			out = append(out, CommandInfo{Name: p.Name, Description: p.Description, Kind: "mcp"})
		}
	}
	return out
}

// SlashArgItem is one sub-command / argument suggestion for the composer's slash
// menu (the part after the command word). Mirrors the CLI's arg completion via
// the shared control.SlashArgItems, so desktop and CLI offer the same hints.
type SlashArgItem struct {
	Label   string `json:"label"`
	Insert  string `json:"insert"`
	Hint    string `json:"hint"`
	Descend bool   `json:"descend"`
}

// SlashArgsResult carries the suggestions plus the byte offset in the input where
// the current token begins, so the composer replaces just that token.
type SlashArgsResult struct {
	Items []SlashArgItem `json:"items"`
	From  int            `json:"from"`
}

// SlashArgs completes the arguments of a management slash command (/mcp, /model,
// /skill, /hooks) for the composer — the same logic the chat TUI uses. Empty
// Items means the input has no structured arguments to complete.
func (a *App) SlashArgs(input string) SlashArgsResult {
	a.mu.RLock()
	ctrl := a.activeCtrlLocked()
	model := ""
	if tab := a.activeTabLocked(); tab != nil {
		model = tab.model
	}
	a.mu.RUnlock()
	if ctrl == nil {
		return SlashArgsResult{Items: []SlashArgItem{}}
	}
	data := control.ArgData{
		Skills:          ctrl.Skills(),
		DisabledSkills:  ctrl.DisabledSkills(),
		ConfiguredMCP:   ctrl.ConfiguredMCPNames(),
		DisconnectedMCP: ctrl.DisconnectedMCPNames(),
		CurrentModel:    model,
	}
	for _, m := range a.Models() {
		data.ModelRefs = append(data.ModelRefs, m.Ref)
	}
	if h := ctrl.Host(); h != nil {
		data.ServerNames = h.ServerNames()
	}
	items, from := control.SlashArgItems(input, data)
	// Non-nil so it serializes as a JSON array, never null — the frontend filters
	// over it directly.
	out := SlashArgsResult{Items: []SlashArgItem{}, From: from}
	for _, it := range items {
		out.Items = append(out.Items, SlashArgItem{Label: it.Label, Insert: it.Insert, Hint: it.Hint, Descend: it.Descend})
	}
	return out
}

// CapabilitiesView is the MCP & Skills drawer's data: connected/failed MCP
// servers and the discoverable skills, the GUI counterpart to `/mcp` + `/skill`.
type CapabilitiesView struct {
	Servers    []ServerView    `json:"servers"`
	Skills     []SkillView     `json:"skills"`
	SkillRoots []SkillRootView `json:"skillRoots"`
}

// ServerView is one MCP server for the drawer. Status is "connected" (with
// tool/prompt/resource counts), "deferred" (lazy/on-demand startup enabled),
// "failed" (with the connection error), "initializing" (background startup in
// progress), or "disabled".
type ServerView struct {
	Name           string     `json:"name"`
	Transport      string     `json:"transport"`
	Status         string     `json:"status"`
	BuiltIn        bool       `json:"builtIn,omitempty"`
	Configured     bool       `json:"configured,omitempty"`
	AutoStart      bool       `json:"autoStart"`
	Tier           string     `json:"tier,omitempty"`
	AutoStartTool  string     `json:"autoStartTool,omitempty"`
	Command        string     `json:"command,omitempty"`
	Args           []string   `json:"args,omitempty"`
	URL            string     `json:"url,omitempty"`
	EnvKeys        []string   `json:"envKeys,omitempty"`
	Tools          int        `json:"tools"`
	Prompts        int        `json:"prompts"`
	Resources      int        `json:"resources"`
	Error          string     `json:"error,omitempty"`
	ToolList       []ToolView `json:"toolList,omitempty"`
	AuthStatus     string     `json:"authStatus,omitempty"`
	AuthURL        string     `json:"authUrl,omitempty"`
	AuthConfigured bool       `json:"authConfigured,omitempty"`
}

type ToolView struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// SkillView is one discoverable skill for the drawer.
type SkillView struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Scope       string `json:"scope"`
	RunAs       string `json:"runAs"`
	Enabled     bool   `json:"enabled"`
}

type SkillRootSkillView struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Scope       string `json:"scope"`
	RunAs       string `json:"runAs"`
}

// SkillRootView is one skill discovery root for the drawer's Sources section.
type SkillRootView struct {
	Dir        string               `json:"dir"`
	Scope      string               `json:"scope"`
	Priority   int                  `json:"priority"`
	Status     string               `json:"status"`
	Configured bool                 `json:"configured"`
	Removable  bool                 `json:"removable"`
	Skills     int                  `json:"skills"`
	SkillItems []SkillRootSkillView `json:"skillItems,omitempty"`
	Warning    string               `json:"warning,omitempty"`
}

// Capabilities projects the session's MCP servers (connected + failed) and skills
// for the MCP & Skills drawer. Non-nil slices so the frontend can map over them.
func (a *App) Capabilities() CapabilitiesView {
	out := CapabilitiesView{Servers: []ServerView{}, Skills: []SkillView{}, SkillRoots: []SkillRootView{}}
	a.mu.RLock()
	tab := a.activeTabLocked()
	a.mu.RUnlock()
	if tab == nil {
		return out
	}
	ctrl := tab.Ctrl
	disabled := make(map[string]ServerView, len(tab.disabledMCP))
	for name, s := range tab.disabledMCP {
		disabled[name] = s
	}
	order := append([]string(nil), tab.mcpOrder...)
	if ctrl == nil {
		return out
	}
	seen := map[string]bool{}
	connected := map[string]bool{}
	retainedDisabled := map[string]ServerView{}
	var loadedCfg *config.Config
	configured := map[string]config.PluginEntry{}
	var configuredEntries []config.PluginEntry
	if cfg, err := config.LoadForRoot(tab.WorkspaceRoot); err == nil {
		loadedCfg = cfg
		configuredEntries = append(configuredEntries, cfg.Plugins...)
		for _, p := range configuredEntries {
			configured[p.Name] = p
		}
	}
	if h := ctrl.Host(); h != nil {
		for _, s := range h.Servers() {
			seen[s.Name] = true
			connected[s.Name] = true
			view := ServerView{
				Name: s.Name, Transport: s.Transport, Status: "connected",
				BuiltIn: s.Name == "codegraph",
				Tools:   s.Tools, Prompts: s.Prompts, Resources: s.Resources,
				ToolList: pluginToolsToView(s.ToolList),
			}
			if p, ok := configured[s.Name]; ok {
				view = withPluginConfig(view, p)
			} else if s.Name == "codegraph" && loadedCfg != nil {
				view = withCodegraphConfig(view, loadedCfg.Codegraph)
			}
			out.Servers = append(out.Servers, view)
		}
		for _, f := range h.Failures() {
			seen[f.Name] = true
			view := ServerView{
				Name: f.Name, Transport: f.Transport, Status: "failed", BuiltIn: f.Name == "codegraph", Error: f.Error,
			}
			if p, ok := configured[f.Name]; ok {
				view = withPluginConfig(view, p)
			} else if f.Name == "codegraph" && loadedCfg != nil {
				view = withCodegraphConfig(view, loadedCfg.Codegraph)
			}
			out.Servers = append(out.Servers, view)
		}
	}
	// Configured servers that are neither connected nor failed are either lazy
	// (deferred), background/eager (initializing), or toggled off this session.
	if len(configuredEntries) > 0 || loadedCfg != nil {
		for _, p := range configuredEntries {
			if seen[p.Name] {
				continue
			}
			if s, ok := disabled[p.Name]; ok {
				s.Status = "disabled"
				s = withPluginConfig(s, p)
				s.Error = ""
				out.Servers = append(out.Servers, s)
				retainedDisabled[p.Name] = s
				seen[p.Name] = true
				delete(disabled, p.Name)
				continue
			}
			status := "disabled"
			if p.ShouldAutoStart() {
				switch p.ResolvedTier() {
				case "background", "eager":
					status = "initializing"
				default:
					status = "deferred"
				}
			}
			out.Servers = append(out.Servers, withPluginConfig(ServerView{Name: p.Name, Status: status}, p))
			seen[p.Name] = true
		}
		if loadedCfg != nil && !seen["codegraph"] {
			status := "disabled"
			if loadedCfg.Codegraph.Enabled {
				switch loadedCfg.Codegraph.ResolvedTier() {
				case "background", "eager":
					status = "initializing"
				default:
					status = "deferred"
				}
			}
			if s, ok := disabled["codegraph"]; ok {
				s.Status = "disabled"
				s.Transport = "stdio"
				s.BuiltIn = true
				s = withCodegraphConfig(s, loadedCfg.Codegraph)
				s.Error = ""
				out.Servers = append(out.Servers, s)
				retainedDisabled["codegraph"] = s
				delete(disabled, "codegraph")
			} else {
				out.Servers = append(out.Servers, withCodegraphConfig(ServerView{Name: "codegraph", Status: status}, loadedCfg.Codegraph))
			}
			seen["codegraph"] = true
		}
	}
	out.Servers = orderServerViews(out.Servers, order)

	// Merge IM processor connection status. The IM plugin lives on the
	// App-level background controller (imCtrl), not on any tab controller,
	// so Capabilities() must check it separately. If the IM plugin is
	// connected there, override its status in the server list so the
	// frontend shows "已连接" instead of "disabled"/"initializing".
	a.imMu.Lock()
	imCtrl := a.imCtrl
	a.imMu.Unlock()
	if imCtrl != nil {
		if h := imCtrl.Host(); h != nil {
			for _, s := range h.Servers() {
				if s.Name != "im" {
					continue
				}
				// Find or add the IM entry in the output list.
				found := false
				for i, sv := range out.Servers {
					if sv.Name == "im" {
						out.Servers[i].Status = "connected"
						out.Servers[i].Error = ""
						if p, ok := configured["im"]; ok {
							out.Servers[i] = withPluginConfig(out.Servers[i], p)
						}
						found = true
						break
					}
				}
				if !found {
					view := ServerView{
						Name: "im", Status: "connected",
						Tools: s.Tools, Prompts: s.Prompts, Resources: s.Resources,
						ToolList: pluginToolsToView(s.ToolList),
					}
					if p, ok := configured["im"]; ok {
						view = withPluginConfig(view, p)
					}
					out.Servers = append(out.Servers, view)
				}
			}
			// Also check for IM failures on the processor.
			for _, f := range h.Failures() {
				if f.Name != "im" {
					continue
				}
				found := false
				for i, sv := range out.Servers {
					if sv.Name == "im" {
						out.Servers[i].Status = "failed"
						out.Servers[i].Error = f.Error
						found = true
						break
					}
				}
				if !found {
					view := ServerView{Name: "im", Status: "failed", Error: f.Error}
					if p, ok := configured["im"]; ok {
						view = withPluginConfig(view, p)
					}
					out.Servers = append(out.Servers, view)
				}
			}
		}
	}

	a.mu.Lock()
	for name := range connected {
		delete(retainedDisabled, name)
	}
	tab.disabledMCP = retainedDisabled
	tab.mcpOrder = mergeServerOrder(tab.mcpOrder, out.Servers)
	a.mu.Unlock()

	for _, s := range ctrl.AllSkills() {
		out.Skills = append(out.Skills, SkillView{
			Name: s.Name, Description: s.Description,
			Scope: string(s.Scope), RunAs: string(s.RunAs),
			Enabled: ctrl.SkillEnabled(s.Name),
		})
	}
	out.SkillRoots = skillRootsView()
	return out
}

func withPluginConfig(v ServerView, p config.PluginEntry) ServerView {
	tt := p.Type
	if tt == "" {
		tt = "stdio"
	}
	v.Transport = tt
	v.Configured = true
	v.AutoStart = p.ShouldAutoStart()
	v.Tier = p.ResolvedTier()
	v.AutoStartTool = p.AutoStartTool
	v.Command = p.Command
	v.Args = append([]string(nil), p.Args...)
	v.URL = p.URL
	v.AuthConfigured = mcpdiag.HasAuthConfig(p.Headers, p.Env, p.URL)
	if len(p.Env) > 0 {
		v.EnvKeys = make([]string, 0, len(p.Env))
		for k := range p.Env {
			v.EnvKeys = append(v.EnvKeys, k)
		}
		sort.Strings(v.EnvKeys)
	}
	auth := mcpdiag.DiagnoseAuth(v.Transport, v.Status, v.Error, v.URL, v.AuthConfigured)
	v.AuthStatus = auth.Status
	v.AuthURL = auth.URL
	return v
}

func withCodegraphConfig(v ServerView, c config.CodegraphConfig) ServerView {
	v.Name = "codegraph"
	v.Transport = "stdio"
	v.BuiltIn = true
	v.Configured = true
	v.AutoStart = c.ShouldAutoStart()
	v.Tier = c.ResolvedTier()
	v.AuthStatus = mcpdiag.AuthNone
	return v
}

func skillRootsView() []SkillRootView {
	cwd, _ := os.Getwd()
	cfg, _ := config.Load()
	userCfg := config.LoadForEdit(config.UserConfigPath())
	var custom []string
	var excluded []string
	maxDepth := 3
	if cfg != nil {
		custom = cfg.SkillCustomPaths()
		excluded = cfg.SkillExcludedPaths()
		maxDepth = cfg.SkillMaxDepth()
	}
	st := skill.New(skill.Options{ProjectRoot: cwd, CustomPaths: custom, ExcludedPaths: excluded, MaxDepth: maxDepth, DisableBuiltins: true, Stderr: io.Discard})
	counts := map[string]int{}
	skillItems := map[string][]SkillRootSkillView{}
	roots := st.Roots()
	for _, sk := range st.List() {
		root := skillDisplayRoot(sk, roots)
		counts[root]++
		skillItems[root] = append(skillItems[root], SkillRootSkillView{
			Name:        sk.Name,
			Description: sk.Description,
			Scope:       string(sk.Scope),
			RunAs:       string(sk.RunAs),
		})
	}
	for root := range skillItems {
		sort.Slice(skillItems[root], func(i, j int) bool {
			return skillItems[root][i].Name < skillItems[root][j].Name
		})
	}
	userConfigured := map[string]bool{}
	if userCfg != nil {
		for _, p := range userCfg.Skills.Paths {
			userConfigured[config.CanonicalSkillPath(p)] = true
		}
	}
	out := []SkillRootView{}
	for _, r := range roots {
		dir := config.CanonicalSkillPath(r.Dir)
		view := SkillRootView{
			Dir:        r.Dir,
			Scope:      string(r.Scope),
			Priority:   r.Priority + 1,
			Status:     string(r.Status),
			Configured: r.Scope == skill.ScopeCustom && userConfigured[dir],
			Removable:  true,
			Skills:     counts[dir],
			SkillItems: skillItems[dir],
		}
		out = append(out, view)
	}
	if userCfg != nil {
		for _, p := range userCfg.Skills.Paths {
			if rootActive(out, p) {
				continue
			}
			out = append(out, SkillRootView{
				Dir:        p,
				Scope:      string(skill.ScopeCustom),
				Status:     "inactive",
				Configured: true,
				Removable:  true,
				Warning:    "configured in user config but not active in this workspace; project [skills].paths may override it",
			})
		}
	}
	return out
}

func rootActive(roots []SkillRootView, path string) bool {
	want := config.CanonicalSkillPath(path)
	for _, r := range roots {
		if r.Scope == string(skill.ScopeCustom) && config.CanonicalSkillPath(r.Dir) == want {
			return true
		}
	}
	return false
}

// PickSkillFolder opens a directory picker for adding custom skill roots. It only
// returns a path; AddSkillPath performs normalization and writes config.
func (a *App) PickSkillFolder() (string, error) {
	if a.ctx == nil {
		return "", nil
	}
	cur, _ := os.Getwd()
	dir, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title:            "Choose skills folder",
		DefaultDirectory: dialogDefaultDirectory(cur),
	})
	if err != nil || dir == "" {
		return "", err
	}
	return normalizeSkillPath(dir), nil
}

// AddSkillPath adds a custom skill root to the user config and rebuilds the
// controller so the skills index and slash menu reflect it immediately.
func (a *App) AddSkillPath(path string) error {
	path = normalizeSkillPath(path)
	workspaceRoot := a.activeWorkspaceRoot()
	return a.applyConfigChange(func(c *config.Config) error {
		if isConventionSkillRoot(path, workspaceRoot) {
			return c.RestoreSkillPath(path)
		}
		return c.AddSkillPath(path)
	})
}

// RemoveSkillPath removes a skill source from the user config and rebuilds. For
// convention roots, it records a pseudo-delete in excluded_paths.
func (a *App) RemoveSkillPath(path string) error {
	path = normalizeSkillPath(path)
	return a.applyConfigChange(func(c *config.Config) error {
		removed, err := c.RemoveSkillPath(path)
		if err != nil || removed {
			return err
		}
		return c.ExcludeSkillPath(path)
	})
}

// RefreshSkills rebuilds the controller without changing config, reloading skill
// discovery, the system prompt index, and slash completions.
func (a *App) RefreshSkills() error {
	return a.rebuild()
}

// SetSkillEnabled persists a skill toggle and rebuilds the controller so the
// prompt index, slash menu, and skill tools reflect it immediately.
func (a *App) SetSkillEnabled(name string, enabled bool) error {
	return a.applyConfigChange(func(c *config.Config) error {
		return c.SetSkillEnabled(name, enabled)
	})
}

// --- Skill Registry ---

// RegistryEntryView is one skill available in the registry marketplace.
type RegistryEntryView struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Source      string   `json:"source"`
	RunAs       string   `json:"runAs"`
	Tags        []string `json:"tags"`
	Author      string   `json:"author"`
	Version     string   `json:"version"`
	Installed   bool     `json:"installed"`
}

// RegistrySourceView is one registry source (official or user-added).
type RegistrySourceView struct {
	Name        string `json:"name"`
	URL         string `json:"url"`
	Type        string `json:"type"`
	Description string `json:"description"`
	Trusted     bool   `json:"trusted"`
}

// BrowseSkills fetches available skills from all registry sources.
func (a *App) BrowseSkills() []RegistryEntryView {
	reg := a.newRegistry()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	entries, err := reg.ListEntries(ctx)
	if err != nil || len(entries) == 0 {
		return []RegistryEntryView{}
	}

	// Mark installed
	installed := a.installedSkillNames()
	out := make([]RegistryEntryView, 0, len(entries))
	for _, e := range entries {
		out = append(out, RegistryEntryView{
			Name:        e.Name,
			Description: e.Description,
			Source:      e.Source,
			RunAs:       e.RunAs,
			Tags:        e.Tags,
			Author:      e.Author,
			Version:     e.Version,
			Installed:   installed[e.Name],
		})
	}
	return out
}

// SearchRegistrySkills searches the registry for skills matching the query.
func (a *App) SearchRegistrySkills(query string) []RegistryEntryView {
	reg := a.newRegistry()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	entries, err := reg.SearchEntries(ctx, query)
	if err != nil || len(entries) == 0 {
		return []RegistryEntryView{}
	}

	installed := a.installedSkillNames()
	out := make([]RegistryEntryView, 0, len(entries))
	for _, e := range entries {
		out = append(out, RegistryEntryView{
			Name:        e.Name,
			Description: e.Description,
			Source:      e.Source,
			RunAs:       e.RunAs,
			Tags:        e.Tags,
			Author:      e.Author,
			Version:     e.Version,
			Installed:   installed[e.Name],
		})
	}
	return out
}

// InstallSkillFromRegistry installs a skill from the registry by name.
func (a *App) InstallSkillFromRegistry(name string, global bool) error {
	reg := a.newRegistry()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	entry, err := reg.GetEntry(ctx, name)
	if err != nil {
		return err
	}

	home, _ := os.UserHomeDir()
	installDir := filepath.Join(home, ".rexion", "skills")
	if !global {
		wsRoot := a.activeWorkspaceRoot()
		if wsRoot != "" {
			installDir = filepath.Join(wsRoot, ".rexion", "skills")
		}
	}

	if _, err := reg.InstallSkill(ctx, *entry, installDir); err != nil {
		return err
	}

	return a.rebuild()
}

// UninstallSkill removes an installed skill by name.
func (a *App) UninstallSkill(name string) error {
	a.mu.RLock()
	tab := a.activeTabLocked()
	a.mu.RUnlock()
	if tab == nil || tab.Ctrl == nil {
		return fmt.Errorf("no active session")
	}

	// Find the skill
	var sk *skill.Skill
	for _, s := range tab.Ctrl.AllSkills() {
		if s.Name == name {
			sk = &s
			break
		}
	}
	if sk == nil {
		return fmt.Errorf("skill %q not found", name)
	}
	if sk.Path == "(builtin)" {
		return fmt.Errorf("cannot uninstall built-in skill %q", name)
	}

	if err := os.RemoveAll(sk.Path); err != nil {
		return err
	}

	return a.rebuild()
}

// RegistrySources returns all configured registry sources (built-in + user-added).
func (a *App) RegistrySources() []RegistrySourceView {
	reg := a.newRegistry()
	sources := reg.Sources()
	out := make([]RegistrySourceView, 0, len(sources))
	for _, s := range sources {
		out = append(out, RegistrySourceView{
			Name:        s.Name,
			URL:         s.URL,
			Type:        s.Type,
			Description: s.Description,
			Trusted:     s.Trusted,
		})
	}
	return out
}

// AddRegistrySource adds a user-configured registry source.
func (a *App) AddRegistrySource(name, url, srcType, description string, trusted bool) error {
	return a.applyConfigChange(func(c *config.Config) error {
		return c.AddRegistrySource(config.RegistrySourceConfig{
			Name:        name,
			URL:         url,
			Type:        srcType,
			Description: description,
			Trusted:     trusted,
		})
	})
}

// RemoveRegistrySource removes a user-added registry source by name.
func (a *App) RemoveRegistrySource(name string) error {
	return a.applyConfigOnly(func(c *config.Config) error {
		_, err := c.RemoveRegistrySource(name)
		return err
	})
}

func (a *App) newRegistry() *registry.Registry {
	home, _ := os.UserHomeDir()
	var userSources []registry.Source
	if cfg, err := config.Load(); err == nil {
		for _, s := range cfg.RegistrySources() {
			userSources = append(userSources, registry.Source{
				Name:        s.Name,
				URL:         s.URL,
				Type:        s.Type,
				Description: s.Description,
				Trusted:     s.Trusted,
			})
		}
	}
	return registry.New(registry.Options{
		HomeDir:  home,
		Sources:  userSources,
		CacheTTL: time.Hour,
	})
}

func (a *App) installedSkillNames() map[string]bool {
	out := map[string]bool{}
	a.mu.RLock()
	tab := a.activeTabLocked()
	a.mu.RUnlock()
	if tab != nil && tab.Ctrl != nil {
		for _, s := range tab.Ctrl.AllSkills() {
			out[s.Name] = true
		}
	}
	return out
}

func normalizeSkillPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if path == "~" || strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		if home, err := os.UserHomeDir(); err == nil {
			if path == "~" {
				path = home
			} else {
				path = filepath.Join(home, path[2:])
			}
		}
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	info, err := os.Stat(path)
	if err != nil {
		return filepath.Clean(path)
	}
	if info.Mode().IsRegular() {
		if filepath.Base(path) == skill.SkillFile {
			return filepath.Clean(filepath.Dir(filepath.Dir(path)))
		}
		return filepath.Clean(filepath.Dir(path))
	}
	if info.IsDir() {
		if _, err := os.Stat(filepath.Join(path, skill.SkillFile)); err == nil {
			return filepath.Clean(filepath.Dir(path))
		}
	}
	return filepath.Clean(path)
}

func isConventionSkillRoot(path, workspaceRoot string) bool {
	want := config.CanonicalSkillPath(path)
	if want == "" {
		return false
	}
	bases := []string{workspaceRoot}
	if home, err := os.UserHomeDir(); err == nil {
		bases = append(bases, home)
	}
	for _, base := range bases {
		base = strings.TrimSpace(base)
		if base == "" {
			continue
		}
		for _, dir := range config.ConventionDirs {
			if want == config.CanonicalSkillPath(filepath.Join(base, dir, skill.SkillsDirname)) {
				return true
			}
		}
	}
	return false
}

func skillRootPath(path string) string {
	if filepath.Base(path) == skill.SkillFile {
		return filepath.Dir(path)
	}
	return path
}

func skillDisplayRoot(sk skill.Skill, roots []skill.Root) string {
	cleanPath := filepath.Clean(sk.Path)
	for _, r := range roots {
		if r.Scope != sk.Scope {
			continue
		}
		cleanRoot := filepath.Clean(r.Dir)
		prefix := cleanRoot + string(filepath.Separator)
		if cleanPath == cleanRoot || strings.HasPrefix(cleanPath, prefix) {
			return config.CanonicalSkillPath(r.Dir)
		}
	}
	return config.CanonicalSkillPath(filepath.Dir(skillRootPath(sk.Path)))
}

// MCPServerInput is the drawer's "add server" form. Transport is "stdio" (Command
// + Args + Env) or "http"/"sse" (URL). Mirrors config.PluginEntry's writable shape.
type MCPServerInput struct {
	Name          string            `json:"name"`
	Transport     string            `json:"transport"`
	Command       string            `json:"command"`
	Args          []string          `json:"args"`
	URL           string            `json:"url"`
	Env           map[string]string `json:"env"`
	AutoStartTool string            `json:"autoStartTool,omitempty"`
}

// AddMCPServer connects a server live and persists it to config (Customize → MCP →
// Add). Returns the number of tools it exposed.
func (a *App) AddMCPServer(in MCPServerInput) (int, error) {
	entry := config.PluginEntry{
		Name:          in.Name,
		Type:          normalizeMCPTransport(in.Transport),
		Command:       in.Command,
		Args:          in.Args,
		URL:           in.URL,
		Env:           in.Env,
		AutoStartTool: in.AutoStartTool,
	}
	entry, _ = config.NormalizePluginCommandLine(entry)

	// CRITICAL: the "im" plugin requires auto_start_tool = "auto_start" so
	// the Stream/Webhook connection is established immediately upon plugin
	// spawn. Without it, the plugin starts but never calls auto_start, so
	// DingTalk/Feishu Stream connections are never opened and messages are
	// never received. Force-set this if the caller omitted it.
	if in.Name == "im" && entry.AutoStartTool == "" {
		entry.AutoStartTool = "auto_start"
	}

	if err := a.saveDesktopMCPServer(entry); err != nil {
		return 0, err
	}

	// CRITICAL: the "im" plugin must ONLY be owned by the App-level IM
	// background processor (which uses a silent sink so its turns never
	// surface in any user tab transcript). If we connect it to the active
	// tab's controller here, the tab would (a) see IM steering in its system
	// prompt, (b) start its own poll watcher, and (c) inject IM messages into
	// the user's coding conversation — hijacking the window. So for IM we
	// persist the config and restart the dedicated processor instead.
	if in.Name == "im" {
		a.restartIMProcessor()
		return 1, nil
	}

	ctrl := a.activeCtrl()
	if ctrl == nil {
		return 0, fmt.Errorf("no active session")
	}
	return ctrl.ConnectMCPServer(entry)
}

// UpdateMCPServer edits a persisted external MCP server. The name is the stable
// identity; callers must remove + add if they want to rename a server.
func (a *App) UpdateMCPServer(name string, in MCPServerInput) error {
	if name == "codegraph" {
		return fmt.Errorf("codegraph is built in; configure it with [codegraph]")
	}
	if strings.TrimSpace(in.Name) != "" && strings.TrimSpace(in.Name) != name {
		return fmt.Errorf("renaming MCP servers is not supported; remove and add a new server")
	}
	updated, found, err := a.desktopMCPServerForEdit(name)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("no configured MCP server named %q", name)
	}
	updated.Type = normalizeMCPTransport(in.Transport)
	updated.Command = strings.TrimSpace(in.Command)
	updated.Args = append([]string(nil), in.Args...)
	updated.URL = strings.TrimSpace(in.URL)
	updated.Tier = ""
	if in.AutoStartTool != "" {
		updated.AutoStartTool = in.AutoStartTool
	}
	if in.Env != nil {
		updated.Env = in.Env
	}
	updated, _ = config.NormalizePluginCommandLine(updated)
	if updated.Type == "stdio" {
		updated.URL = ""
	} else {
		updated.Command = ""
		updated.Args = nil
	}
	if err := a.saveDesktopMCPServer(updated); err != nil {
		return err
	}

	// IM plugin: restart the dedicated background processor instead of
	// touching the active tab's controller. See AddMCPServer for rationale.
	// Also ensure auto_start_tool is set (it's required for Stream startup).
	if name == "im" {
		if updated.AutoStartTool == "" {
			updated.AutoStartTool = "auto_start"
			if err := a.saveDesktopMCPServer(updated); err != nil {
				return err
			}
		}
		a.restartIMProcessor()
		return nil
	}

	ctrl := a.activeCtrl()
	if ctrl == nil {
		return fmt.Errorf("no active session")
	}
	a.mu.RLock()
	tab := a.activeTabLocked()
	sessionDisabled := false
	if tab != nil {
		_, sessionDisabled = tab.disabledMCP[name]
	}
	a.mu.RUnlock()
	wasConnected := mcpConnected(ctrl, name)
	wasFailed := mcpFailed(ctrl, name)
	if wasConnected {
		ctrl.DisconnectMCPServer(name)
	}
	if !sessionDisabled && (wasConnected || wasFailed || updated.ResolvedTier() != "lazy") {
		if _, err := ctrl.ConnectMCPServer(updated); err != nil {
			recordMCPFailure(ctrl, updated, err)
			return nil
		}
	}
	return nil
}

// RemoveMCPServer disconnects a live server and drops it from config (the row's ✕).
func (a *App) RemoveMCPServer(name string) error {
	if name == "codegraph" {
		return fmt.Errorf("codegraph is built in; it cannot be removed")
	}
	removed, err := a.removeDesktopMCPServer(name)
	if err != nil {
		return err
	}

	// IM plugin: stop the dedicated background processor. No tab cleanup
	// needed because IM is never connected to tabs. See AddMCPServer.
	if name == "im" {
		a.stopIMProcessor()
		if removed {
			return nil
		}
		return fmt.Errorf("no MCP server named %q", name)
	}

	tab := a.activeTab()
	if tab == nil || tab.Ctrl == nil {
		return fmt.Errorf("no active session")
	}
	disconnected := tab.Ctrl.DisconnectMCPServer(name)
	if disconnected || removed {
		a.mu.Lock()
		delete(tab.disabledMCP, name)
		tab.mcpOrder = removeServerOrder(tab.mcpOrder, name)
		a.mu.Unlock()
		return nil
	}
	return fmt.Errorf("no MCP server named %q", name)
}

// ReconnectMCPServer disconnects the server if it is already connected (to force
// a fresh handshake and tool re-registration), then reconnects.  Failures are
// recorded on the Host so the UI can render them.
func (a *App) ReconnectMCPServer(name string) error {
	tab := a.activeTab()
	if tab == nil || tab.Ctrl == nil {
		return fmt.Errorf("no active session")
	}
	if mcpConnected(tab.Ctrl, name) {
		tab.Ctrl.DisconnectMCPServer(name)
	}
	_, err := a.connectConfiguredMCPServerForTab(tab, name)
	if err != nil {
		recordMCPFailure(tab.Ctrl, config.PluginEntry{Name: name}, err)
		return err
	}
	a.mu.Lock()
	delete(tab.disabledMCP, name)
	a.mu.Unlock()
	return nil
}

// ClearMCPServerAuthentication removes local auth-like config for one MCP and
// clears the current session's cached connection failure. It does not remove the
// server itself or try to sign the user out of the third-party browser session.
func (a *App) ClearMCPServerAuthentication(name string) error {
	if name == "codegraph" {
		return fmt.Errorf("codegraph is built in; it has no stored MCP authentication")
	}
	ctrl := a.activeCtrl()
	if ctrl == nil {
		return fmt.Errorf("no active session")
	}
	if _, _, _, err := config.ClearPluginAuthenticationInSource(name); err != nil {
		return err
	}
	ctrl.DisconnectMCPServer(name)
	if h := ctrl.Host(); h != nil {
		h.ClearFailure(name)
	}
	return nil
}

// SetMCPServerEnabled is the connector toggle: on reconnects a configured server
// for this session, off disconnects it (config untouched either way — like Claude
// Code's per-conversation enable/disable, it resets on the next session start).
func (a *App) SetMCPServerEnabled(name string, enabled bool) error {
	tab := a.activeTab()
	if tab == nil || tab.Ctrl == nil {
		return fmt.Errorf("no active session")
	}
	if name == "codegraph" {
		return a.setCodegraphEnabled(enabled)
	}
	if enabled {
		_, err := a.connectConfiguredMCPServerForTab(tab, name)
		if err == nil {
			a.mu.Lock()
			delete(tab.disabledMCP, name)
			a.mu.Unlock()
		}
		return err
	}
	if s, ok := findMCPServerView(tab.Ctrl, name); ok {
		s.Status = "disabled"
		s.Error = ""
		a.mu.Lock()
		if tab.disabledMCP == nil {
			tab.disabledMCP = map[string]ServerView{}
		}
		tab.disabledMCP[name] = s
		tab.mcpOrder = mergeServerOrder(tab.mcpOrder, []ServerView{s})
		a.mu.Unlock()
	}
	tab.Ctrl.DisconnectMCPServer(name)
	return nil
}

func (a *App) connectConfiguredMCPServerForTab(tab *WorkspaceTab, name string) (int, error) {
	if tab == nil || tab.Ctrl == nil {
		return 0, fmt.Errorf("no active session")
	}
	cfg, err := config.LoadForRoot(tab.WorkspaceRoot)
	if err != nil {
		return 0, err
	}
	for _, p := range cfg.Plugins {
		if p.Name == name {
			return tab.Ctrl.ConnectMCPServer(p)
		}
	}
	if name == "codegraph" {
		return tab.Ctrl.ConnectCodegraphMCPServer(cfg)
	}
	return 0, fmt.Errorf("no configured MCP server named %q", name)
}

// SetMCPServerTier is kept for old desktop bindings. New config writes drop the
// retired tier field, so this only affects the active session before the next
// config reload.
func (a *App) SetMCPServerTier(name, tier string) error {
	if name == "codegraph" {
		return a.setCodegraphTier(tier)
	}
	tier = normalizeMCPTier(tier)
	updated, found, err := a.desktopMCPServerForEdit(name)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("no configured MCP server named %q", name)
	}
	updated.Tier = tier
	if !updated.ShouldAutoStart() {
		on := true
		updated.AutoStart = &on
	}
	if err := a.saveDesktopMCPServer(updated); err != nil {
		return err
	}
	tab := a.activeTab()
	if tier != "lazy" && tab != nil && tab.Ctrl != nil && !mcpConnected(tab.Ctrl, name) {
		if _, err := tab.Ctrl.ConnectMCPServer(updated); err != nil {
			recordMCPFailure(tab.Ctrl, updated, err)
			return nil
		}
		a.mu.Lock()
		delete(tab.disabledMCP, name)
		a.mu.Unlock()
	}
	return nil
}

func (a *App) setCodegraphEnabled(enabled bool) error {
	cfg, path, err := a.loadDesktopUserConfigForEdit()
	if err != nil {
		return err
	}
	tab := a.activeTab()
	if tab == nil || tab.Ctrl == nil {
		return fmt.Errorf("no active session")
	}
	cfg.Codegraph.Enabled = enabled
	if err := cfg.SaveTo(path); err != nil {
		return err
	}
	if err := a.syncProjectCodegraphOverride(cfg.Codegraph); err != nil {
		return err
	}
	if enabled {
		a.mu.Lock()
		delete(tab.disabledMCP, "codegraph")
		a.mu.Unlock()
		if _, err := tab.Ctrl.ConnectCodegraphMCPServer(cfg); err != nil {
			recordCodegraphFailure(tab.Ctrl, cfg.Codegraph, err)
			return nil
		}
		return nil
	}
	if h := tab.Ctrl.Host(); h != nil {
		h.ClearFailure("codegraph")
	}
	tab.Ctrl.DisconnectMCPServer("codegraph")
	s := withCodegraphConfig(ServerView{Name: "codegraph", Status: "disabled"}, cfg.Codegraph)
	a.mu.Lock()
	if tab.disabledMCP == nil {
		tab.disabledMCP = map[string]ServerView{}
	}
	tab.disabledMCP["codegraph"] = s
	tab.mcpOrder = mergeServerOrder(tab.mcpOrder, []ServerView{s})
	a.mu.Unlock()
	return nil
}

func (a *App) setCodegraphTier(tier string) error {
	tier = normalizeMCPTier(tier)
	cfg, path, err := a.loadDesktopUserConfigForEdit()
	if err != nil {
		return err
	}
	cfg.Codegraph.Enabled = true
	cfg.Codegraph.Tier = tier
	if err := cfg.SaveTo(path); err != nil {
		return err
	}
	if err := a.syncProjectCodegraphOverride(cfg.Codegraph); err != nil {
		return err
	}
	tab := a.activeTab()
	if tab == nil || tab.Ctrl == nil {
		return nil
	}
	a.mu.Lock()
	delete(tab.disabledMCP, "codegraph")
	a.mu.Unlock()
	if tier != "lazy" && !mcpConnected(tab.Ctrl, "codegraph") {
		if _, err := tab.Ctrl.ConnectCodegraphMCPServer(cfg); err != nil {
			recordCodegraphFailure(tab.Ctrl, cfg.Codegraph, err)
			return nil
		}
	}
	return nil
}

func (a *App) desktopMCPServerForEdit(name string) (config.PluginEntry, bool, error) {
	cfg, _, err := a.loadDesktopUserConfigForEdit()
	if err != nil {
		return config.PluginEntry{}, false, err
	}
	if p, ok := findPluginEntry(cfg.Plugins, name); ok {
		return p, true, nil
	}
	if merged, err := config.LoadForRoot(a.activeWorkspaceRoot()); err == nil {
		if p, ok := findPluginEntry(merged.Plugins, name); ok {
			return p, true, nil
		}
	}
	return config.PluginEntry{}, false, nil
}

func (a *App) saveDesktopMCPServer(entry config.PluginEntry) error {
	cfg, path, err := a.loadDesktopUserConfigForEdit()
	if err != nil {
		return err
	}
	if err := cfg.UpsertPlugin(entry); err != nil {
		return err
	}
	if err := cfg.SaveTo(path); err != nil {
		return err
	}
	_, err = a.removeProjectMCPOverride(entry.Name)
	return err
}

func (a *App) removeDesktopMCPServer(name string) (bool, error) {
	removed := false
	cfg, path, err := a.loadDesktopUserConfigForEdit()
	if err != nil {
		return false, err
	}
	if cfg.RemovePlugin(name) {
		removed = true
		if err := cfg.SaveTo(path); err != nil {
			return false, err
		}
	}
	projectRemoved, err := a.removeProjectMCPOverride(name)
	if err != nil {
		return removed, err
	}
	return removed || projectRemoved, nil
}

func (a *App) removeProjectMCPOverride(name string) (bool, error) {
	path := projectConfigPathForRoot(a.activeWorkspaceRoot())
	userPath := config.UserConfigPath()
	if path == "" || sameConfigPath(path, userPath) {
		return false, nil
	}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	cfg := config.LoadForEdit(path)
	if !cfg.RemovePlugin(name) {
		return false, nil
	}
	if err := cfg.SaveTo(path); err != nil {
		return false, err
	}
	return true, nil
}

func (a *App) syncProjectCodegraphOverride(c config.CodegraphConfig) error {
	path := projectConfigPathForRoot(a.activeWorkspaceRoot())
	userPath := config.UserConfigPath()
	if path == "" || sameConfigPath(path, userPath) {
		return nil
	}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	cfg := config.LoadForEdit(path)
	cfg.Codegraph = c
	return cfg.SaveTo(path)
}

func findPluginEntry(entries []config.PluginEntry, name string) (config.PluginEntry, bool) {
	for _, p := range entries {
		if p.Name == name {
			return p, true
		}
	}
	return config.PluginEntry{}, false
}

func normalizeMCPTier(tier string) string {
	switch strings.ToLower(strings.TrimSpace(tier)) {
	case "eager":
		return "eager"
	case "background":
		return "background"
	case "":
		return "background"
	default:
		return "lazy"
	}
}

func normalizeMCPTransport(transport string) string {
	switch strings.ToLower(strings.TrimSpace(transport)) {
	case "http", "streamable-http":
		return "http"
	case "sse":
		return "sse"
	default:
		return "stdio"
	}
}

func mcpConnected(ctrl *control.Controller, name string) bool {
	if ctrl == nil || ctrl.Host() == nil {
		return false
	}
	for _, s := range ctrl.Host().Servers() {
		if s.Name == name {
			return true
		}
	}
	return false
}

func mcpFailed(ctrl *control.Controller, name string) bool {
	if ctrl == nil || ctrl.Host() == nil {
		return false
	}
	for _, f := range ctrl.Host().Failures() {
		if f.Name == name {
			return true
		}
	}
	return false
}

func recordMCPFailure(ctrl *control.Controller, e config.PluginEntry, err error) {
	if ctrl == nil || ctrl.Host() == nil || err == nil {
		return
	}
	exp := e.ExpandedPlugin()
	ctrl.Host().RecordFailure(plugin.Spec{
		Name:    exp.Name,
		Type:    exp.Type,
		Command: exp.Command,
		Args:    exp.Args,
		Env:     exp.Env,
		URL:     exp.URL,
		Headers: exp.Headers,
	}, err)
}

func recordCodegraphFailure(ctrl *control.Controller, c config.CodegraphConfig, err error) {
	if ctrl == nil || ctrl.Host() == nil || err == nil {
		return
	}
	cmd := strings.TrimSpace(c.Path)
	if cmd == "" {
		cmd = "codegraph"
	}
	ctrl.Host().RecordFailure(plugin.Spec{
		Name:    "codegraph",
		Type:    "stdio",
		Command: cmd,
		Args:    []string{"serve", "--mcp"},
	}, err)
}

func findMCPServerView(ctrl *control.Controller, name string) (ServerView, bool) {
	if ctrl == nil || ctrl.Host() == nil {
		return ServerView{}, false
	}
	for _, s := range ctrl.Host().Servers() {
		if s.Name == name {
			return ServerView{
				Name: s.Name, Transport: s.Transport, Status: "connected",
				Tools: s.Tools, Prompts: s.Prompts, Resources: s.Resources,
				ToolList: pluginToolsToView(s.ToolList),
			}, true
		}
	}
	for _, f := range ctrl.Host().Failures() {
		if f.Name == name {
			return ServerView{Name: f.Name, Transport: f.Transport, Status: "failed", Error: f.Error}, true
		}
	}
	return ServerView{}, false
}

func pluginToolsToView(tools []plugin.ToolInfo) []ToolView {
	if len(tools) == 0 {
		return nil
	}
	out := make([]ToolView, 0, len(tools))
	for _, t := range tools {
		out = append(out, ToolView{Name: t.Name, Description: t.Description})
	}
	return out
}

func orderServerViews(servers []ServerView, order []string) []ServerView {
	pos := make(map[string]int, len(order))
	for i, name := range order {
		pos[name] = i
	}
	sort.SliceStable(servers, func(i, j int) bool {
		pi, iok := pos[servers[i].Name]
		pj, jok := pos[servers[j].Name]
		switch {
		case iok && jok:
			return pi < pj
		case iok:
			return true
		case jok:
			return false
		default:
			return false
		}
	})
	return servers
}

func mergeServerOrder(order []string, servers []ServerView) []string {
	seen := make(map[string]bool, len(order)+len(servers))
	next := make([]string, 0, len(order)+len(servers))
	for _, name := range order {
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		next = append(next, name)
	}
	for _, s := range servers {
		if s.Name == "" || seen[s.Name] {
			continue
		}
		seen[s.Name] = true
		next = append(next, s.Name)
	}
	return next
}

func removeServerOrder(order []string, name string) []string {
	if name == "" || len(order) == 0 {
		return order
	}
	next := order[:0]
	for _, n := range order {
		if n != name {
			next = append(next, n)
		}
	}
	return next
}

// ModelInfo is one (provider, model) the bottom switcher can pick. Ref ("provider/
// model") is what SetModel takes; Provider/Model are for display.
type ModelInfo struct {
	Ref      string `json:"ref"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Current  bool   `json:"current"`
}

type EffortInfo struct {
	Supported bool     `json:"supported"`
	Current   string   `json:"current"`
	Default   string   `json:"default"`
	Levels    []string `json:"levels"`
}

// Models flattens the configured providers into their (provider, model) pairs —
// the switcher's options — marking the active one. A vendor with a `models` list
// yields one entry per model, all sharing the same endpoint/key. Unconfigured
// providers are skipped. Result is non-nil: the frontend reads .length, so a nil
// slice (JSON null) would crash the switcher on an empty list.
func (a *App) Models() []ModelInfo {
	return a.ModelsForTab("")
}

func (a *App) ModelsForTab(tabID string) []ModelInfo {
	a.mu.RLock()
	curModel := ""
	workspaceRoot := ""
	if tab := a.tabByIDLocked(tabID); tab != nil {
		curModel = tab.model
		workspaceRoot = tab.WorkspaceRoot
	}
	a.mu.RUnlock()
	cfg, err := config.LoadForRoot(workspaceRoot)
	if err != nil {
		return []ModelInfo{}
	}
	if entry, ok := cfg.ResolveModel(curModel); ok {
		curModel = entry.Name + "/" + entry.Model
	}
	access := providerAccessSet(cfg.Desktop.ProviderAccess)
	out := []ModelInfo{}
	for i := range cfg.Providers {
		p := &cfg.Providers[i]
		if !modelProviderAccessAllowed(access, p.Name) || !p.Configured() {
			continue
		}
		for _, m := range p.ModelList() {
			ref := p.Name + "/" + m
			out = append(out, ModelInfo{Ref: ref, Provider: p.Name, Model: m, Current: ref == curModel})
		}
	}
	return out
}

func modelProviderAccessAllowed(access map[string]bool, name string) bool {
	if len(access) == 0 {
		return true
	}
	return access[strings.TrimSpace(name)]
}

// SetModel switches the active model and carries the current conversation into the
// new model's session, so the chat continues seamlessly and subsequent turns use
// the new model. No-op if name is already active or the controller is down.
func (a *App) SetModel(name string) error {
	return a.SetModelForTab("", name)
}

func (a *App) SetModelForTab(tabID, name string) error {
	if a.ctx == nil || name == "" {
		return nil
	}
	tab := a.tabByID(tabID)
	if tab == nil {
		return nil
	}
	if name == tab.model {
		return nil
	}
	if tab.Ctrl != nil && tab.Ctrl.Running() {
		return fmt.Errorf("finish or cancel the current turn before changing model")
	}
	cfg, err := config.LoadForRoot(tab.WorkspaceRoot)
	if err != nil {
		return err
	}
	entry, ok := cfg.ResolveModel(name)
	if !ok {
		return fmt.Errorf("unknown model %q", name)
	}
	if !modelProviderAccessAllowed(providerAccessSet(cfg.Desktop.ProviderAccess), entry.Name) {
		return fmt.Errorf("model %q is not available because provider %q is not added", name, entry.Name)
	}
	name = entry.Name + "/" + entry.Model
	effortOverride := cloneStringPtr(tab.effort)
	if effortOverride != nil {
		normalized, err := config.NormalizeEffort(entry, config.EffortDisplay(&config.ProviderEntry{Effort: *effortOverride}))
		if err != nil {
			effortOverride = nil
		} else {
			effortOverride = &normalized
		}
	}

	var carried []provider.Message
	prevPath := ""
	if tab.Ctrl != nil {
		prevPath = tab.Ctrl.SessionPath()
		_ = tab.Ctrl.Snapshot()
		carried = tab.Ctrl.History()
		tab.Ctrl.Close()
	}

	newCtrl, err := boot.Build(a.bootContext(), boot.Options{
		Model:          name,
		RequireKey:     false,
		Sink:           tab.sink,
		WorkspaceRoot:  tab.WorkspaceRoot,
		EffortOverride: cloneStringPtr(effortOverride),
		ExcludePlugins: []string{"im"},
	})
	if err != nil {
		return err
	}
	a.bindControllerDisplayRecorder(newCtrl)
	a.mu.Lock()
	tab.Ctrl = newCtrl
	tab.model = name
	tab.effort = cloneStringPtr(effortOverride)
	tab.Label = newCtrl.Label()
	a.saveTabsLocked()
	a.mu.Unlock()
	newCtrl.EnableInteractiveApproval()
	applyTabModeToController(newCtrl, tab.mode)

	path := agent.ContinueSessionPath(prevPath, newCtrl.SessionDir(), newCtrl.Label())
	if len(carried) > 0 {
		newCtrl.Resume(&agent.Session{Messages: carried}, path)
	} else if path != "" {
		newCtrl.SetSessionPath(path)
	}
	a.persistTabSessionPath(tab, path)
	return nil
}

func (a *App) Effort() EffortInfo {
	return a.EffortForTab("")
}

func (a *App) EffortForTab(tabID string) EffortInfo {
	entry, err := a.currentProviderEntryForTab(tabID)
	if err != nil {
		return EffortInfo{Current: "auto", Levels: []string{}}
	}
	cap := config.EffortCapabilityForEntry(entry)
	if !cap.Supported {
		return EffortInfo{Supported: false, Current: "auto", Default: cap.Default, Levels: []string{}}
	}
	levels := cap.Levels
	if levels == nil {
		levels = []string{}
	}
	return EffortInfo{Supported: true, Current: config.EffortDisplay(entry), Default: cap.Default, Levels: levels}
}

func (a *App) SetEffort(level string) error {
	return a.SetEffortForTab("", level)
}

func (a *App) SetEffortForTab(tabID, level string) error {
	tab := a.tabByID(tabID)
	if tab == nil {
		if strings.TrimSpace(tabID) == "" {
			entry, err := a.currentProviderEntryForTab("")
			if err != nil {
				return err
			}
			effort, err := config.NormalizeEffort(entry, level)
			if err != nil {
				return err
			}
			return a.applyProviderEffortConfig(entry, effort)
		}
		return fmt.Errorf("tab %q not found", tabID)
	}
	ctrl := tab.Ctrl
	if ctrl != nil && ctrl.Running() {
		return fmt.Errorf("finish or cancel the current turn before changing effort")
	}
	entry, err := a.currentProviderEntryForTab(tabID)
	if err != nil {
		return err
	}
	effort, err := config.NormalizeEffort(entry, level)
	if err != nil {
		return err
	}
	var carried []provider.Message
	prevPath := ""
	if tab.Ctrl != nil {
		prevPath = tab.Ctrl.SessionPath()
		_ = tab.Ctrl.Snapshot()
		carried = tab.Ctrl.History()
		tab.Ctrl.Close()
	}
	newCtrl, err := boot.Build(a.bootContext(), boot.Options{
		Model:          tab.model,
		RequireKey:     false,
		Sink:           tab.sink,
		WorkspaceRoot:  tab.WorkspaceRoot,
		EffortOverride: &effort,
		ExcludePlugins: []string{"im"},
	})
	if err != nil {
		return err
	}
	a.bindControllerDisplayRecorder(newCtrl)
	a.mu.Lock()
	tab.Ctrl = newCtrl
	tab.effort = &effort
	tab.Label = newCtrl.Label()
	tab.StartupErr = ""
	tab.Ready = true
	a.saveTabsLocked()
	a.mu.Unlock()
	newCtrl.EnableInteractiveApproval()
	applyTabModeToController(newCtrl, tab.mode)
	path := agent.ContinueSessionPath(prevPath, newCtrl.SessionDir(), newCtrl.Label())
	if len(carried) > 0 {
		newCtrl.Resume(&agent.Session{Messages: carried}, path)
	} else if path != "" {
		newCtrl.SetSessionPath(path)
	}
	a.persistTabSessionPath(tab, path)
	return nil
}

func (a *App) applyProviderEffortConfig(entry *config.ProviderEntry, effort string) error {
	return a.applyConfigChange(func(cfg *config.Config) error {
		if _, ok := cfg.Provider(entry.Name); !ok {
			if err := cfg.UpsertProvider(*entry); err != nil {
				return err
			}
		}
		if entry.Kind == "anthropic" && effort != "" && entry.Thinking == "" {
			if err := cfg.SetProviderThinking(entry.Name, "adaptive"); err != nil {
				return err
			}
		}
		canonicalName := config.CanonicalDesktopOfficialProviderName(entry.Name)
		if canonicalName != entry.Name {
			if _, ok := cfg.Provider(canonicalName); ok {
				if err := cfg.SetProviderEffort(canonicalName, effort); err != nil {
					return err
				}
			}
		}
		return cfg.SetProviderEffort(entry.Name, effort)
	})
}

// DirEntry is one entry in the "@" file-reference menu.
type DirEntry struct {
	Name  string `json:"name"`
	IsDir bool   `json:"isDir"`
}

// FilePreview is a bounded, read-only file payload for the workspace side panel.
type FilePreview struct {
	Path      string `json:"path"`
	Body      string `json:"body"`
	Size      int64  `json:"size"`
	Truncated bool   `json:"truncated"`
	Binary    bool   `json:"binary"`
	Kind      string `json:"kind,omitempty"`
	Mime      string `json:"mime,omitempty"`
	URL       string `json:"url,omitempty"`
	Err       string `json:"err,omitempty"`
}

type WorkspaceChangeView struct {
	Path         string   `json:"path"`
	OldPath      string   `json:"oldPath,omitempty"`
	Sources      []string `json:"sources"`
	GitStatus    string   `json:"gitStatus,omitempty"`
	Turns        []int    `json:"turns,omitempty"`
	LatestPrompt string   `json:"latestPrompt,omitempty"`
	LatestTime   int64    `json:"latestTime,omitempty"`
}

type WorkspaceChangesView struct {
	Files        []WorkspaceChangeView `json:"files"`
	GitAvailable bool                  `json:"gitAvailable"`
	GitErr       string                `json:"gitErr,omitempty"`
}

// workspaceNoiseNames are local cache/vendor entries hidden from the file tree
// and "@" menu regardless of where they appear.
var workspaceNoiseNames = map[string]bool{
	".codex":       true,
	".codegraph":   true,
	".DS_Store":    true,
	".git":         true,
	".npm":         true,
	".pnpm-store":  true,
	"node_modules": true,
	"Thumbs.db":    true,
}

var workspaceNoiseDirs = map[string]bool{
	"bin":                      true,
	"desktop/build":            true,
	"desktop/frontend/dist":    true,
	"desktop/frontend/wailsjs": true,
	"dist":                     true,
	"npm/.stage":               true,
	"site/.astro":              true,
	"site/dist":                true,
	"stage":                    true,
	"tmp":                      true,
}

const filePreviewLimit = 256 * 1024
const fileRefSearchLimit = 20

var previewMediaMIMEs = map[string]string{
	".bmp":  "image/bmp",
	".gif":  "image/gif",
	".htm":  "text/html; charset=utf-8",
	".html": "text/html; charset=utf-8",
	".jpeg": "image/jpeg",
	".jpg":  "image/jpeg",
	".pdf":  "application/pdf",
	".png":  "image/png",
	".svg":  "image/svg+xml",
	".webp": "image/webp",
}

func trimUTF8PartialSuffix(data []byte) []byte {
	if utf8.Valid(data) {
		return data
	}
	for i := len(data) - 1; i >= 0 && len(data)-i <= utf8.UTFMax; i-- {
		if !utf8.RuneStart(data[i]) {
			continue
		}
		if !utf8.Valid(data[:i]) || utf8.FullRune(data[i:]) {
			return data
		}
		return data[:i]
	}
	return data
}

func previewMediaKind(path string) (kind string, mime string) {
	mime = previewMediaMIMEs[strings.ToLower(filepath.Ext(path))]
	if mime == "" {
		return "", ""
	}
	if strings.HasPrefix(mime, "image/") {
		return "image", mime
	}
	if mime == "application/pdf" {
		return "pdf", mime
	}
	if strings.HasPrefix(mime, "text/html") {
		return "html", mime
	}
	return "", ""
}

// previewOfficeKind reports whether the file is an office document (docx/xlsx/csv)
// that can be parsed to HTML for inline preview. Returns the kind label and true
// when the extension matches; ("", false) otherwise.
func previewOfficeKind(path string) (kind string, ok bool) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".docx":
		return "docx", true
	case ".xlsx", ".xlsm":
		return "xlsx", true
	case ".csv":
		return "csv", true
	}
	return "", false
}

func workspaceEntryRel(rel, name string) string {
	rel = strings.Trim(filepath.ToSlash(rel), "/")
	if rel == "" || rel == "." {
		return name
	}
	return rel + "/" + name
}

func skipWorkspaceEntry(rel, name string, isDir bool) bool {
	if workspaceNoiseNames[name] {
		return true
	}
	return isDir && workspaceNoiseDirs[workspaceEntryRel(rel, name)]
}

func (a *App) activeWorkspaceBase() (string, error) {
	root := a.activeWorkspaceRoot()
	if strings.TrimSpace(root) == "" || root == "." {
		return os.Getwd()
	}
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}
	return filepath.Clean(root), nil
}

func (a *App) workspacePath(rel string) (string, bool, error) {
	base, err := a.activeWorkspaceBase()
	if err != nil {
		return "", false, err
	}
	return workspacePathForBase(base, rel)
}

func workspacePath(rel string) (string, bool, error) {
	base, err := os.Getwd()
	if err != nil {
		return "", false, err
	}
	return workspacePathForBase(base, rel)
}

func workspacePathForBase(base, rel string) (string, bool, error) {
	base = filepath.Clean(base)
	if rel == "" {
		return "", false, os.ErrInvalid
	}
	path := rel
	if !filepath.IsAbs(path) {
		path = filepath.Join(base, rel)
	}
	path = filepath.Clean(path)
	r, err := filepath.Rel(base, path)
	if err != nil {
		return "", false, err
	}
	if r == ".." || strings.HasPrefix(r, ".."+string(os.PathSeparator)) {
		return "", false, os.ErrPermission
	}
	return path, true, nil
}

// ListDir lists one directory level (directories first, then files, each
// alphabetical) for the "@" file-reference menu. rel resolves against the active
// tab workspace. The menu navigates one level at a time, never recursively —
// bounded for huge trees.
func (a *App) ListDir(rel string) []DirEntry {
	base, err := a.activeWorkspaceBase()
	if err != nil {
		return []DirEntry{}
	}
	dir := base
	if rel != "" {
		path, ok, err := workspacePathForBase(base, rel)
		if err != nil || !ok {
			return []DirEntry{}
		}
		dir = path
	}
	es, err := os.ReadDir(dir)
	if err != nil {
		return []DirEntry{}
	}
	dirs, files := []DirEntry{}, []DirEntry{}
	for _, e := range es {
		name := e.Name()
		if skipWorkspaceEntry(rel, name, e.IsDir()) {
			continue
		}
		if e.IsDir() {
			dirs = append(dirs, DirEntry{Name: name, IsDir: true})
			continue
		}
		info, err := e.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		files = append(files, DirEntry{Name: name, IsDir: false})
	}
	sort.Slice(dirs, func(i, j int) bool { return strings.ToLower(dirs[i].Name) < strings.ToLower(dirs[j].Name) })
	sort.Slice(files, func(i, j int) bool { return strings.ToLower(files[i].Name) < strings.ToLower(files[j].Name) })
	return append(dirs, files...)
}

// SearchFileRefs finds workspace files by basename for bare "@token" completion.
func (a *App) SearchFileRefs(query string) []DirEntry {
	base, err := a.activeWorkspaceBase()
	if err != nil {
		return nil
	}
	paths := fileref.Search(base, query, fileRefSearchLimit)
	out := make([]DirEntry, 0, len(paths))
	for _, path := range paths {
		out = append(out, DirEntry{Name: path, IsDir: false})
	}
	return out
}

// ReadFile returns a small text preview for a file under the current workspace.
func (a *App) ReadFile(rel string) FilePreview {
	out := FilePreview{Path: rel}
	path, ok, err := a.workspacePath(rel)
	if err != nil || !ok {
		out.Err = "invalid path"
		return out
	}
	info, err := os.Stat(path)
	if err != nil {
		out.Err = err.Error()
		return out
	}
	if info.IsDir() {
		out.Err = "path is a directory"
		return out
	}
	if !info.Mode().IsRegular() {
		out.Err = "path is not a regular file"
		return out
	}
	out.Size = info.Size()
	if kind, mime := previewMediaKind(path); kind != "" {
		token := a.ensureMediaTokenStore().create(path, info.Name(), mime, kind, info.Size(), info.ModTime())
		out.Kind = kind
		out.Mime = mime
		out.URL = "/__Rexion_workspace_media/" + token + "/" + url.PathEscape(info.Name())
		return out
	}
	// Office documents (docx/xlsx/csv): parse to HTML and serve via inline
	// media token so the file tree can render them in an <iframe>.
	if kind, ok := previewOfficeKind(path); ok {
		htmlName := strings.TrimSuffix(info.Name(), filepath.Ext(path)) + ".html"
		var (
			htmlStr  string
			parseErr error
		)
		switch kind {
		case "docx":
			htmlStr, parseErr = parseDocxToHTML(path)
		case "xlsx":
			htmlStr, parseErr = parseXlsxToHTML(path)
		case "csv":
			htmlStr, parseErr = parseCSVToHTML(path)
		}
		if parseErr != nil {
			out.Err = parseErr.Error()
			return out
		}
		token := a.ensureMediaTokenStore().createInline(htmlName, "text/html; charset=utf-8", kind, []byte(htmlStr))
		out.Kind = kind
		out.Mime = "text/html; charset=utf-8"
		out.URL = "/__Rexion_workspace_media/" + token + "/" + url.PathEscape(htmlName)
		return out
	}
	f, err := os.Open(path)
	if err != nil {
		out.Err = err.Error()
		return out
	}
	defer f.Close()

	buf := make([]byte, filePreviewLimit+1)
	n, err := f.Read(buf)
	if err != nil && err != io.EOF {
		out.Err = err.Error()
		return out
	}
	data := buf[:n]
	if len(data) > filePreviewLimit {
		data = data[:filePreviewLimit]
		out.Truncated = true
	}

	// Check for BOM first (just the first 2-3 bytes — always complete
	// even at a truncation boundary). BOM-prefixed files skip the NUL
	// check since UTF-16 normally contains 0x00 for ASCII characters.
	bomKind := fileenc.DetectQuick(data)
	if bomKind != fileenc.UTF8 {
		enc, _ := fileenc.Detect(data)
		if enc == fileenc.LossyUTF8 {
			out.Binary = true
			return out
		}
		decoded := fileenc.Decode(data, enc)
		out.Body = string(decoded)
		return out
	}

	// No BOM — NUL in raw bytes is a binary signal.
	if bytes.Contains(data, []byte{0}) {
		out.Binary = true
		return out
	}

	// Trim any partial multi-byte rune at the truncation boundary BEFORE
	// encoding detection. Without this, a large UTF-8 file truncated
	// mid-character would fail utf8.Valid and be misdetected as GB18030
	// or LossyUTF8, producing mojibake or a false binary classification.
	if out.Truncated {
		data = trimUTF8PartialSuffix(data)
	}
	enc, _ := fileenc.Detect(data)
	if enc == fileenc.LossyUTF8 {
		out.Binary = true
		return out
	}
	out.Body = string(fileenc.Decode(data, enc))
	return out
}

// OpenWorkspacePath opens a file or folder from the workspace in the OS default app.
func (a *App) OpenWorkspacePath(rel string) error {
	path, ok, err := a.workspacePath(rel)
	if err != nil || !ok {
		return os.ErrInvalid
	}
	return openWorkspacePath(path)
}

// RevealWorkspacePath shows a workspace file in the native file manager.
func (a *App) RevealWorkspacePath(rel string) error {
	path, ok, err := a.workspacePath(rel)
	if err != nil || !ok {
		return os.ErrInvalid
	}
	return revealPath(path)
}

// RevealPath shows an arbitrary absolute path in the native file manager.
func (a *App) RevealPath(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return os.ErrInvalid
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	return revealPath(path)
}

func revealPath(path string) error {
	switch goruntime.GOOS {
	case "darwin":
		return exec.Command("open", "-R", path).Start()
	case "windows":
		return exec.Command("explorer", "/select,", path).Start()
	default:
		dir := path
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			dir = filepath.Dir(path)
		}
		return exec.Command("xdg-open", dir).Start()
	}
}

func (a *App) notice(text string) {
	a.noticeForTab("", text)
}

func (a *App) noticeForTab(tabID, text string) {
	tab := a.tabByID(tabID)
	if tab != nil && tab.sink != nil {
		tab.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: text})
	}
}

func (a *App) runEffortCommand(input string) {
	a.runEffortCommandForTab("", input)
}

func (a *App) runEffortCommandForTab(tabID, input string) {
	entry, err := a.currentProviderEntryForTab(tabID)
	if err != nil {
		a.noticeForTab(tabID, "effort: "+err.Error())
		return
	}
	cap := config.EffortCapabilityForEntry(entry)
	if !cap.Supported {
		a.noticeForTab(tabID, fmt.Sprintf("effort is not configurable for %s", entry.Name))
		return
	}
	args := strings.Fields(input)
	if len(args) < 2 {
		a.noticeForTab(tabID, fmt.Sprintf("effort for %s: %s (default: %s; options: %s)", entry.Name, config.EffortDisplay(entry), cap.Default, strings.Join(cap.Levels, "|")))
		return
	}
	if len(args) > 2 {
		a.noticeForTab(tabID, "usage: /effort "+strings.Join(cap.Levels, "|"))
		return
	}
	effort, err := config.NormalizeEffort(entry, args[1])
	if err != nil {
		a.noticeForTab(tabID, err.Error())
		return
	}
	if err := a.SetEffortForTab(tabID, args[1]); err != nil {
		a.noticeForTab(tabID, "effort: "+err.Error())
		return
	}
	display := effort
	if display == "" {
		display = "auto"
	}
	a.noticeForTab(tabID, fmt.Sprintf("effort for %s set to %s", entry.Name, display))
}

func (a *App) currentProviderEntry() (*config.ProviderEntry, error) {
	return a.currentProviderEntryForTab("")
}

func (a *App) currentProviderEntryForTab(tabID string) (*config.ProviderEntry, error) {
	a.mu.RLock()
	ref := ""
	workspaceRoot := ""
	effortOverride := (*string)(nil)
	if tab := a.tabByIDLocked(tabID); tab != nil {
		ref = tab.model
		workspaceRoot = tab.WorkspaceRoot
		effortOverride = cloneStringPtr(tab.effort)
	}
	a.mu.RUnlock()
	cfg, err := config.LoadForRoot(workspaceRoot)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(ref) == "" {
		ref = cfg.DefaultModel
	}
	resolved, _, ok := cfg.ResolveModelWithFallback(ref)
	if !ok {
		return nil, fmt.Errorf("unknown model %q", ref)
	}
	entry, ok := cfg.ResolveModel(resolved)
	if !ok {
		return nil, fmt.Errorf("unknown model %q", resolved)
	}
	if effortOverride != nil {
		entry.Effort = *effortOverride
	}
	return entry, nil
}

// SavePastedImage stores a browser clipboard image data URL under
// .rexion/attachments and returns the relative @-reference path.
func (a *App) SavePastedImage(dataURL string) (string, error) {
	return control.SaveImageDataURL(dataURL)
}

// SavePastedFile stores a dropped non-image file (the browser exposes its bytes
// as a data URL but not a real path) under .rexion/attachments and returns the
// relative @-reference path.
func (a *App) SavePastedFile(name, dataURL string) (string, error) {
	return control.SaveAttachmentDataURL(name, dataURL)
}

// AttachmentDataURL returns a safe data URL for a stored image attachment.
func (a *App) AttachmentDataURL(path string) (string, error) {
	return control.ImageDataURL(path)
}

// DroppedItem is one OS-dropped file resolved into a composer context entry: an
// in-tree file becomes a workspace @reference (read in place, no copy), while an
// image or out-of-tree file is copied into .rexion/attachments.
type DroppedItem struct {
	Kind       string `json:"kind"` // "workspace" | "attachment"
	Path       string `json:"path"`
	IsDir      bool   `json:"isDir,omitempty"`
	PreviewURL string `json:"previewUrl,omitempty"`
}

// AttachDropped turns an absolute path from the native file-drop bridge into a
// composer context entry. Images are stored as attachments so the chip shows a
// thumbnail; other in-workspace files are referenced relatively (no copy); files
// outside the workspace are copied into .rexion/attachments.
func (a *App) AttachDropped(path string) (DroppedItem, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return DroppedItem{}, err
	}
	if isImageExt(path) {
		if rel, err := control.SaveImageFile(path); err == nil {
			preview, _ := control.ImageDataURL(rel)
			return DroppedItem{Kind: "attachment", Path: rel, PreviewURL: preview}, nil
		}
	}
	if rel, ok := workspaceRelative(path); ok {
		return DroppedItem{Kind: "workspace", Path: rel, IsDir: info.IsDir()}, nil
	}
	if info.IsDir() {
		return DroppedItem{}, fmt.Errorf("can only attach files from outside the workspace")
	}
	rel, err := control.SaveAttachmentFile(path)
	if err != nil {
		return DroppedItem{}, err
	}
	return DroppedItem{Kind: "attachment", Path: rel}, nil
}

func isImageExt(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp":
		return true
	}
	return false
}

func workspaceRelative(path string) (string, bool) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", false
	}
	rel, err := filepath.Rel(cwd, path)
	if err != nil {
		return "", false
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

// --- memory panel (frontend ⇄ controller) ---

// MemoryDoc is one loaded doc-memory file for the panel: path, scope, and body.
type MemoryDoc struct {
	Path  string `json:"path"`
	Scope string `json:"scope"`
	Body  string `json:"body"`
}

// MemoryFact is one saved auto-memory, surfaced read-only in the panel.
type MemoryFact struct {
	Name        string `json:"name"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description"`
	Type        string `json:"type"`
	Body        string `json:"body"`
}

// MemoryScope is one writable quick-add target (scope id + the file it writes to).
type MemoryScope struct {
	Scope string `json:"scope"`
	Path  string `json:"path"`
}

// MemoryPKMFile is one personal-knowledge-base file (people.md / projects.md /
// preferences.md / writing_style.md under ~/.rexion/memory/), surfaced for the
// panel's PKM editor. Name is the bare filename; Path is absolute; Body is the
// trimmed file contents (empty when the file exists but is whitespace-only).
type MemoryPKMFile struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Body string `json:"body"`
}

// MemoryView is the whole memory panel payload: hierarchical docs, saved facts,
// the writable scopes for the quick-add selector, and the PKM files.
type MemoryView struct {
	Docs      []MemoryDoc     `json:"docs"`
	Facts     []MemoryFact    `json:"facts"`
	Scopes    []MemoryScope   `json:"scopes"`
	PKMFiles  []MemoryPKMFile `json:"pkmFiles"`
	StoreDir  string          `json:"storeDir"`
	Available bool            `json:"available"`
}

// writableScopes are the quick-add targets the panel offers, broad → specific.
var writableScopes = []memory.Scope{memory.ScopeUser, memory.ScopeProject, memory.ScopeLocal}

// Memory returns the loaded memory for the panel: the Rexion.md hierarchy, the
// saved auto-memories, the writable scopes, and the PKM files. Read-only;
// mutations go through Remember / SaveDoc / SavePKMFile.
func (a *App) Memory() MemoryView {
	// Always return non-nil slices: a nil Go slice marshals to JSON `null`, which
	// would crash the panel's `view.facts.length` / `.map`.
	view := MemoryView{
		Docs:     []MemoryDoc{},
		Facts:    []MemoryFact{},
		Scopes:   []MemoryScope{},
		PKMFiles: []MemoryPKMFile{},
	}
	a.mu.RLock()
	ctrl := a.activeCtrlLocked()
	a.mu.RUnlock()
	if ctrl == nil {
		view.PKMFiles = pkmFilesView()
		return view
	}
	set := ctrl.Memory()
	if set == nil {
		view.PKMFiles = pkmFilesView()
		return view
	}
	view.StoreDir = set.Store.Dir
	view.Available = true
	for _, d := range set.Docs {
		view.Docs = append(view.Docs, MemoryDoc{Path: d.Path, Scope: string(d.Scope), Body: d.Body})
	}
	for _, f := range set.Store.List() {
		view.Facts = append(view.Facts, MemoryFact{
			Name: f.Name, Title: f.Title, Description: f.Description, Type: string(f.Type), Body: f.Body,
		})
	}
	for _, sc := range writableScopes {
		if p := set.DocPath(sc); p != "" { // user scope yields "" when no config dir
			view.Scopes = append(view.Scopes, MemoryScope{Scope: string(sc), Path: p})
		}
	}
	// PKM files come from the set when PKM is enabled (already loaded by Load),
	// otherwise fall back to reading the directory so the panel can still show
	// (and let the user edit) files that exist on disk even before a rebuild.
	if len(set.PKM) > 0 {
		for _, d := range set.PKM {
			view.PKMFiles = append(view.PKMFiles, MemoryPKMFile{
				Name: filepath.Base(d.Path), Path: d.Path, Body: d.Body,
			})
		}
	} else {
		view.PKMFiles = pkmFilesView()
	}
	return view
}

// pkmFilesView reads the four PKM files from ~/.rexion/memory/ directly, for
// the panel. It always returns the full four-file list (in the fixed
// writing_style → preferences → people → projects order) so the editor can
// render every tab even when a file is missing — Body is "" for a missing or
// empty file. Missing the whole directory yields an empty slice.
func pkmFilesView() []MemoryPKMFile {
	dir, err := memory.MemoryDir()
	if err != nil {
		return []MemoryPKMFile{}
	}
	out := make([]MemoryPKMFile, 0, 4)
	for _, name := range memory.PKMFileOrder() {
		path := filepath.Join(dir, name)
		body, _ := os.ReadFile(path)
		out = append(out, MemoryPKMFile{Name: name, Path: path, Body: string(body)})
	}
	return out
}

// Remember quick-adds a one-line note to the doc-memory file for scope — the
// panel's explicit "remember" action, equivalent to typing "/remember <note>".
// An unknown scope falls back to project. Returns the file written.
func (a *App) Remember(scope, note string) (string, error) {
	a.mu.RLock()
	ctrl := a.activeCtrlLocked()
	a.mu.RUnlock()
	if ctrl == nil {
		return "", nil
	}
	return ctrl.QuickAdd(parseScope(scope), note)
}

// Forget deletes a saved auto-memory by name — the panel's delete action for a
// fact the model owns. A no-op when no controller is attached.
func (a *App) Forget(name string) error {
	a.mu.RLock()
	ctrl := a.activeCtrlLocked()
	a.mu.RUnlock()
	if ctrl == nil {
		return nil
	}
	return ctrl.ForgetMemory(name)
}

// SaveDoc overwrites a memory doc with the panel editor's contents. The controller
// validates path against the recognized memory files. Returns the file written.
func (a *App) SaveDoc(path, body string) (string, error) {
	a.mu.RLock()
	ctrl := a.activeCtrlLocked()
	a.mu.RUnlock()
	if ctrl == nil {
		return "", nil
	}
	return ctrl.SaveDoc(path, body)
}

// SavePKMFile overwrites one of the four personal-knowledge-base files
// (people.md / projects.md / preferences.md / writing_style.md) under
// ~/.rexion/memory/ with the panel editor's contents. name must be one of the
// recognized filenames — anything else is refused so the panel can't be used to
// write arbitrary paths. The file is written directly (the controller's SaveDoc
// path only knows the Rexion.md / AGENTS.md hierarchy), then the controller is
// rebuilt so the new PKM content folds into the cache-stable system prompt on
// the next turn. Returns the absolute path written.
func (a *App) SavePKMFile(name, content string) error {
	name = strings.TrimSpace(name)
	if !memory.ValidPKMFileName(name) {
		return fmt.Errorf("refusing to save %q: not a PKM file", name)
	}
	dir, err := memory.MemoryDir()
	if err != nil {
		return fmt.Errorf("resolve pkm dir: %w", err)
	}
	// EnsureMemoryDir is idempotent: it creates the dir + default files when
	// missing, so a first save never fails on a missing directory and never
	// clobbers an existing file.
	if _, _, err := memory.EnsureMemoryDir(); err != nil {
		return fmt.Errorf("ensure pkm dir: %w", err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", name, err)
	}
	// Rebuild the controller so the edited PKM body is re-loaded into the
	// system prompt prefix for the next turn. rebuild is best-effort: if it
	// fails the file is still on disk and will be picked up on the next boot.
	if err := a.rebuild(); err != nil {
		fmt.Fprintln(os.Stderr, "warning: controller rebuild after PKM save failed:", err)
	}
	return nil
}

// AppendPKMFile appends a learned preference snippet to one of the four PKM
// files under ~/.rexion/memory/. Used by the AutoLearn approval flow: when
// the user accepts a detected preference proposal, the frontend calls this
// method with the proposal's TargetFile and Content. The controller is rebuilt
// so the new PKM content folds into the cache-stable system prompt next turn.
func (a *App) AppendPKMFile(name, content string) error {
	if err := memory.AppendPKMFile(name, content); err != nil {
		return err
	}
	if err := a.rebuild(); err != nil {
		fmt.Fprintln(os.Stderr, "warning: controller rebuild after PKM append failed:", err)
	}
	return nil
}

// SuppressAutoLearnTypeForTab marks a preference type as rejected by the user
// for the given tab's conversation, so the auto-learner won't propose the same
// type again this session. tabID "" targets the active tab.
func (a *App) SuppressAutoLearnTypeForTab(tabID, typ string) {
	ctrl := a.ctrlByTabID(tabID)
	if ctrl != nil {
		ctrl.SuppressAutoLearnType(typ)
	}
}

// MailSummaryView is a compact mail summary for the DailyBriefPanel.
type MailSummaryView struct {
	From    string `json:"from"`
	Subject string `json:"subject"`
	Date    string `json:"date"`
}

// GetRecentMailSummaries fetches the 5 most recent emails from INBOX by
// dispatching a read_mail tool call through the active tab's controller. This
// bypasses the agent loop (no model call) so the DailyBriefPanel can load
// synchronously. Returns an empty list (not an error) if the mail MCP plugin is
// not connected or not configured — the panel shows a placeholder in that case.
func (a *App) GetRecentMailSummaries() []MailSummaryView {
	ctrl := a.ctrlByTabID("")
	if ctrl == nil {
		return []MailSummaryView{}
	}
	args, _ := json.Marshal(map[string]any{"folder": "INBOX", "limit": 5})
	out, err := ctrl.CallTool(a.ctx, "mcp__mail__read_mail", args)
	if err != nil {
		return []MailSummaryView{}
	}
	return parseMailSummaries(out)
}

// parseMailSummaries extracts From/Subject/Date from the plain-text output of
// the read_mail tool. Each email block typically starts with "From:" and ends
// before the next "From:" or the end of the output.
func parseMailSummaries(text string) []MailSummaryView {
	var result []MailSummaryView
	var current *MailSummaryView
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "From:") {
			if current != nil {
				result = append(result, *current)
			}
			current = &MailSummaryView{From: strings.TrimSpace(strings.TrimPrefix(line, "From:"))}
		} else if current != nil {
			if strings.HasPrefix(line, "Subject:") {
				current.Subject = strings.TrimSpace(strings.TrimPrefix(line, "Subject:"))
			} else if strings.HasPrefix(line, "Date:") {
				current.Date = strings.TrimSpace(strings.TrimPrefix(line, "Date:"))
			}
		}
	}
	if current != nil {
		result = append(result, *current)
	}
	if result == nil {
		result = []MailSummaryView{}
	}
	return result
}

// IMSessionView is the lightweight row used by the IM Sessions dock panel.
// It mirrors the imSession struct in the IM plugin, plus an optional
// agent_session path that links the IM command to the agent transcript that
// processed it (filled in by the desktop from the controller's session path).
type IMSessionView struct {
	ID             string `json:"id"`
	Platform       string `json:"platform"`
	ConversationID string `json:"conversationId,omitempty"`
	SenderID       string `json:"senderId,omitempty"`
	SenderName     string `json:"senderName,omitempty"`
	CommandID      string `json:"commandId"`
	Content        string `json:"content,omitempty"`
	Status         string `json:"status"`
	Result         string `json:"result,omitempty"`
	AgentSession   string `json:"agentSession,omitempty"`
	CreatedAt      int64  `json:"createdAt"`
	UpdatedAt      int64  `json:"updatedAt"`
	DoneAt         int64  `json:"doneAt,omitempty"`
}

// ListIMSessions fetches tracked IM sessions from the IM MCP plugin via the
// App-level IM processor. Returns an empty list (not an error) if the IM
// processor is not running — the panel shows a placeholder in that case.
//
// The optional status filter is forwarded to the plugin ("", "pending",
// "processing", "done", "failed"). Results are newest-first.
// Uses CallToolDirect for a direct Host→MCP path, avoiding the registry
// lookup overhead of CallTool.
func (a *App) ListIMSessions(status string) []IMSessionView {
	ctrl := a.imCtrlLocked()
	if ctrl == nil {
		return []IMSessionView{}
	}
	out, err := ctrl.CallToolDirect(a.ctx, "mcp__im__list_im_sessions", map[string]any{"status": status, "limit": 100})
	if err != nil {
		return []IMSessionView{}
	}
	return parseIMSessions(out)
}

// GetIMSession fetches a single IM session with its linked command. The
// agent_session path (if any) lets the frontend load the agent transcript
// that processed this IM command and render the execution trace.
// Uses CallToolDirect for a direct Host→MCP path.
func (a *App) GetIMSession(sessionID string) IMSessionDetailView {
	ctrl := a.imCtrlLocked()
	if ctrl == nil {
		return IMSessionDetailView{}
	}
	out, err := ctrl.CallToolDirect(a.ctx, "mcp__im__get_im_session", map[string]any{"session_id": sessionID})
	if err != nil {
		return IMSessionDetailView{}
	}
	return parseIMSessionDetail(out)
}

// DeleteIMSession deletes a single IM session by ID. Returns true on success.
// Uses CallToolDirect to bypass the read-only check since this is a user-initiated
// delete action from the UI panel.
func (a *App) DeleteIMSession(sessionID string) bool {
	ctrl := a.imCtrlLocked()
	if ctrl == nil {
		return false
	}
	_, err := ctrl.CallToolDirect(a.ctx, "mcp__im__delete_im_session", map[string]any{"session_id": sessionID})
	return err == nil
}

// ClearIMSessions removes all IM session records. Returns the count cleared,
// or -1 on error. Uses CallToolDirect to bypass the read-only check since this
// is a user-initiated clear action from the UI panel.
func (a *App) ClearIMSessions() int {
	ctrl := a.imCtrlLocked()
	if ctrl == nil {
		return -1
	}
	out, err := ctrl.CallToolDirect(a.ctx, "mcp__im__clear_im_sessions", map[string]any{})
	if err != nil {
		return -1
	}
	var result struct {
		Cleared int `json:"cleared"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		return -1
	}
	return result.Cleared
}

// imCtrlLocked returns the App-level IM controller under a short lock.
func (a *App) imCtrlLocked() *control.Controller {
	a.imMu.Lock()
	defer a.imMu.Unlock()
	return a.imCtrl
}

// startIMProcessor builds the App-level single IM background controller.
//
// It owns the sole IM plugin connection + poll watcher, keeping IM message
// handling isolated from user tab conversations. The controller is built with
// a silent sink (no frontend transcript push), bypass mode (auto-approve
// side-effect tools so IM processing is unattended), and a global-scope
// workspace root so it doesn't bind to any project.
//
// This is idempotent: if already started (or starting), it returns immediately.
// stopIMProcessor closes it.
func (a *App) startIMProcessor() {
	a.imMu.Lock()
	if a.imStarted {
		a.imMu.Unlock()
		slog.Info("im-processor: already started, skipping")
		return
	}
	a.imStarted = true
	a.imMu.Unlock()

	slog.Info("im-processor: starting App-level background controller...")

	// Use the global home directory as the workspace root so the IM controller
	// doesn't bind to any project (it's not project-scoped).
	home, _ := os.UserHomeDir()
	slog.Info("im-processor: workspace root", "root", home)

	// Auto-fix: if the IM plugin config exists but is missing auto_start_tool,
	// patch it so the Stream/Webhook connection is established on spawn.
	// This handles configs created before the auto_start_tool requirement
	// was enforced (e.g. manual edits to config.toml).
	if cfg, err := config.LoadForRoot(home); err == nil {
		for i, p := range cfg.Plugins {
			if p.Name == "im" && p.AutoStartTool == "" {
				cfg.Plugins[i].AutoStartTool = "auto_start"
				if uc := config.UserConfigPath(); uc != "" {
					if err := cfg.SaveTo(uc); err != nil {
						slog.Warn("im-processor: failed to patch auto_start_tool into config", "err", err)
					} else {
						slog.Info("im-processor: patched auto_start_tool into IM plugin config")
					}
				}
				break
			}
		}
	}

	// The IM controller lives for the whole app lifetime; use a child context
	// we can cancel on shutdown so the poll watcher and plugin subprocesses
	// exit cleanly.
	imCtx, cancel := context.WithCancel(a.ctx)

	// Silent sink: log IM processing turns without pushing them to the frontend
	// transcript. The IM Sessions panel queries history on demand via
	// list_im_sessions / get_im_session — no real-time push needed.
	silentSink := &silentEventSink{}

	// Use the global home directory as the workspace root so the IM controller
	// doesn't bind to any project (it's not project-scoped).
	slog.Info("im-processor: workspace root", "root", home)

	ctrl, err := boot.Build(imCtx, boot.Options{
		Model:         "", // use config default
		RequireKey:    false,
		Sink:          silentSink,
		WorkspaceRoot: home,
		// No ExcludePlugins here: this controller owns the IM plugin.
	})
	if err != nil {
		slog.Error("im-processor: failed to build controller", "err", err)
		fmt.Fprintln(os.Stderr, "warning: IM processor failed to start:", err)
		cancel()
		a.imMu.Lock()
		a.imStarted = false
		a.imMu.Unlock()
		return
	}

	// Bypass approval gates: IM processing is unattended and cannot pop modals.
	// Deny rules in the permission policy still apply for genuinely dangerous
	// operations, so this only skips the interactive approval prompt.
	ctrl.SetBypass(true)

	a.imMu.Lock()
	a.imCtrl = ctrl
	a.imCancel = cancel
	a.imMu.Unlock()

	// Log IM plugin connection status for diagnostics.
	// Also: if the IM plugin connected but auto_start was not called (e.g.
	// config.toml lacked auto_start_tool at the time boot.Build loaded it),
	// manually call auto_start now so DingTalk/Feishu Stream connections
	// are established. This is the belt-and-suspenders fix for the race
	// between config patch and boot.Build's config load.
	if h := ctrl.Host(); h != nil {
		imConnected := false
		for _, s := range h.Servers() {
			if s.Name == "im" {
				imConnected = true
				slog.Info("im-processor: IM plugin connected", "tools", len(s.ToolList))
				break
			}
		}
		for _, f := range h.Failures() {
			if f.Name == "im" {
				slog.Error("im-processor: IM plugin failed to connect", "error", f.Error)
			}
		}
		if !imConnected {
			slog.Warn("im-processor: IM plugin not found in connected servers (may be deferred/background)")
		}
	}

	// Ensure auto_start is called even if the config didn't have it at
	// boot.Build time. Start a background goroutine that waits for the IM
	// plugin to connect (deferred/background tier) and then calls auto_start.
	// The goroutine exits when the IM context is cancelled.
	go func() {
		// Wait up to 60 seconds for the IM plugin to become available.
		for i := 0; i < 30; i++ {
			select {
			case <-imCtx.Done():
				return
			default:
			}
			if h := ctrl.Host(); h != nil {
				for _, s := range h.Servers() {
					if s.Name == "im" {
						slog.Info("im-processor: calling auto_start for IM plugin")
						autoCtx, autoCancel := context.WithTimeout(imCtx, 30*time.Second)
						_, err := ctrl.Host().CallTool(autoCtx, "im", "auto_start", map[string]any{})
						autoCancel()
						if err != nil {
							slog.Warn("im-processor: auto_start call failed", "err", err)
						} else {
							slog.Info("im-processor: auto_start called successfully")
						}
						return
					}
				}
			}
			time.Sleep(2 * time.Second)
		}
		slog.Warn("im-processor: IM plugin never became available within 60s, auto_start not called")
	}()

	slog.Info("im-processor: started (App-level single background controller)")
}

// stopIMProcessor closes the App-level IM controller and releases its plugin
// subprocesses. Safe to call multiple times.
func (a *App) stopIMProcessor() {
	a.imMu.Lock()
	ctrl := a.imCtrl
	cancel := a.imCancel
	a.imCtrl = nil
	a.imCancel = nil
	a.imStarted = false
	a.imMu.Unlock()

	if cancel != nil {
		cancel()
	}
	if ctrl != nil {
		ctrl.Close()
		slog.Info("im-processor: stopped")
	}
}

// restartIMProcessor stops the current IM background processor (if any) and
// starts a fresh one that picks up the latest IM configuration from
// Rexion.toml. Called when the user saves / updates / removes IM plugin
// configuration so new credentials take effect without restarting the app.
func (a *App) restartIMProcessor() {
	slog.Info("im-processor: restarting (stop + start) to pick up new config...")
	a.stopIMProcessor()
	a.startIMProcessor()
}

// silentEventSink is an event.Sink that discards all events. Used by the IM
// background processor so its turns never surface in any user tab transcript.
type silentEventSink struct{}

func (s *silentEventSink) Emit(e event.Event) {
	// Log turn-level events at debug for diagnostics; drop everything else.
	if e.Kind == event.TurnDone && e.Err != nil {
		slog.Debug("im-processor: turn ended with error", "err", e.Err)
	}
}

// IMSessionDetailView is a single IM session plus its linked pending command.
type IMSessionDetailView struct {
	IMSessionView
	Command IMCommandView `json:"command,omitempty"`
}

// IMCommandView mirrors the plugin's pendingCommand for the detail panel.
type IMCommandView struct {
	ID         string            `json:"id"`
	Platform   string            `json:"platform"`
	Content    string            `json:"content"`
	WebhookURL string            `json:"webhookUrl,omitempty"`
	Extra      map[string]string `json:"extra,omitempty"`
	ReceivedAt int64             `json:"receivedAt"`
	Done       bool              `json:"done"`
	Result     string            `json:"result,omitempty"`
	DoneAt     int64             `json:"doneAt,omitempty"`
}

// parseIMSessions extracts a session list from the list_im_sessions tool
// output. The plugin returns either an indented JSON array or the plain
// string "no IM sessions" when the store is empty.
func parseIMSessions(text string) []IMSessionView {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" || trimmed == "no IM sessions" {
		return []IMSessionView{}
	}
	var raw []struct {
		ID             string     `json:"id"`
		Platform       string     `json:"platform"`
		ConversationID string     `json:"conversation_id,omitempty"`
		SenderID       string     `json:"sender_id,omitempty"`
		SenderName     string     `json:"sender_name,omitempty"`
		CommandID      string     `json:"command_id"`
		Content        string     `json:"content,omitempty"`
		Status         string     `json:"status"`
		Result         string     `json:"result,omitempty"`
		AgentSession   string     `json:"agent_session,omitempty"`
		CreatedAt      time.Time  `json:"created_at"`
		UpdatedAt      time.Time  `json:"updated_at"`
		DoneAt         *time.Time `json:"done_at,omitempty"`
	}
	if err := json.Unmarshal([]byte(trimmed), &raw); err != nil {
		return []IMSessionView{}
	}
	out := make([]IMSessionView, 0, len(raw))
	for _, r := range raw {
		v := IMSessionView{
			ID:             r.ID,
			Platform:       r.Platform,
			ConversationID: r.ConversationID,
			SenderID:       r.SenderID,
			SenderName:     r.SenderName,
			CommandID:      r.CommandID,
			Content:        r.Content,
			Status:         r.Status,
			Result:         r.Result,
			AgentSession:   r.AgentSession,
			CreatedAt:      r.CreatedAt.UnixMilli(),
			UpdatedAt:      r.UpdatedAt.UnixMilli(),
		}
		if r.DoneAt != nil {
			v.DoneAt = r.DoneAt.UnixMilli()
		}
		out = append(out, v)
	}
	return out
}

// parseIMSessionDetail extracts a single session + its linked command from
// the get_im_session tool output. Returns an empty struct on any parse error.
func parseIMSessionDetail(text string) IMSessionDetailView {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return IMSessionDetailView{}
	}
	var raw struct {
		ID             string     `json:"id"`
		Platform       string     `json:"platform"`
		ConversationID string     `json:"conversation_id,omitempty"`
		SenderID       string     `json:"sender_id,omitempty"`
		SenderName     string     `json:"sender_name,omitempty"`
		CommandID      string     `json:"command_id"`
		Content        string     `json:"content,omitempty"`
		Status         string     `json:"status"`
		Result         string     `json:"result,omitempty"`
		AgentSession   string     `json:"agent_session,omitempty"`
		CreatedAt      time.Time  `json:"created_at"`
		UpdatedAt      time.Time  `json:"updated_at"`
		DoneAt         *time.Time `json:"done_at,omitempty"`
		Command        *struct {
			ID         string            `json:"id"`
			Platform   string            `json:"platform"`
			Content    string            `json:"content"`
			WebhookURL string            `json:"webhook_url,omitempty"`
			Extra      map[string]string `json:"extra,omitempty"`
			ReceivedAt time.Time         `json:"received_at"`
			Done       bool              `json:"done"`
			Result     string            `json:"result,omitempty"`
			DoneAt     *time.Time        `json:"done_at,omitempty"`
		} `json:"command,omitempty"`
	}
	if err := json.Unmarshal([]byte(trimmed), &raw); err != nil {
		return IMSessionDetailView{}
	}
	v := IMSessionView{
		ID:             raw.ID,
		Platform:       raw.Platform,
		ConversationID: raw.ConversationID,
		SenderID:       raw.SenderID,
		SenderName:     raw.SenderName,
		CommandID:      raw.CommandID,
		Content:        raw.Content,
		Status:         raw.Status,
		Result:         raw.Result,
		AgentSession:   raw.AgentSession,
		CreatedAt:      raw.CreatedAt.UnixMilli(),
		UpdatedAt:      raw.UpdatedAt.UnixMilli(),
	}
	if raw.DoneAt != nil {
		v.DoneAt = raw.DoneAt.UnixMilli()
	}
	detail := IMSessionDetailView{IMSessionView: v}
	if raw.Command != nil {
		detail.Command = IMCommandView{
			ID:         raw.Command.ID,
			Platform:   raw.Command.Platform,
			Content:    raw.Command.Content,
			WebhookURL: raw.Command.WebhookURL,
			Extra:      raw.Command.Extra,
			ReceivedAt: raw.Command.ReceivedAt.UnixMilli(),
			Done:       raw.Command.Done,
			Result:     raw.Command.Result,
		}
		if raw.Command.DoneAt != nil {
			detail.Command.DoneAt = raw.Command.DoneAt.UnixMilli()
		}
	}
	return detail
}

// parseScope maps a frontend scope id to a memory.Scope, defaulting to project.
func parseScope(s string) memory.Scope {
	switch memory.Scope(s) {
	case memory.ScopeUser:
		return memory.ScopeUser
	case memory.ScopeLocal:
		return memory.ScopeLocal
	default:
		return memory.ScopeProject
	}
}

// RecipeView is the JSON-serializable recipe structure sent to the frontend.
// It mirrors internal/recipe.Recipe but with trigger config expanded for UI consumption.
type RecipeView struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Skill       string            `json:"skill"`
	Params      string            `json:"params"`
	Trigger     string            `json:"trigger"`
	CronExpr    string            `json:"cronExpr,omitempty"`
	EventType   string            `json:"eventType,omitempty"`
	MatchRules  map[string]string `json:"matchRules,omitempty"`
	CreatedAt   int64             `json:"createdAt"`
	UpdatedAt   int64             `json:"updatedAt"`
}

// SaveRecipe persists a new or updated recipe. The name must be unique and filename-safe.
func (a *App) SaveRecipe(r RecipeView) error {
	if a.recipeStore == nil {
		return fmt.Errorf("recipe store not initialized")
	}
	rec := recipe.Recipe{
		Name:        r.Name,
		Description: r.Description,
		Skill:       r.Skill,
		Params:      r.Params,
		Trigger:     recipe.TriggerType(r.Trigger),
		TriggerConfig: recipe.TriggerConfig{
			CronExpr:   r.CronExpr,
			EventType:  r.EventType,
			MatchRules: r.MatchRules,
		},
		CreatedAt: r.CreatedAt,
		UpdatedAt: r.UpdatedAt,
	}
	return a.recipeStore.Save(rec)
}

// LoadRecipe reads a recipe by name. Returns error if not found.
func (a *App) LoadRecipe(name string) (RecipeView, error) {
	if a.recipeStore == nil {
		return RecipeView{}, fmt.Errorf("recipe store not initialized")
	}
	rec, err := a.recipeStore.Load(name)
	if err != nil {
		return RecipeView{}, err
	}
	return RecipeView{
		Name:        rec.Name,
		Description: rec.Description,
		Skill:       rec.Skill,
		Params:      rec.Params,
		Trigger:     string(rec.Trigger),
		CronExpr:    rec.TriggerConfig.CronExpr,
		EventType:   rec.TriggerConfig.EventType,
		MatchRules:  rec.TriggerConfig.MatchRules,
		CreatedAt:   rec.CreatedAt,
		UpdatedAt:   rec.UpdatedAt,
	}, nil
}

// ListRecipes returns all recipes, sorted by creation time descending.
func (a *App) ListRecipes() ([]RecipeView, error) {
	if a.recipeStore == nil {
		return nil, fmt.Errorf("recipe store not initialized")
	}
	recipes, err := a.recipeStore.List()
	if err != nil {
		return nil, err
	}
	views := make([]RecipeView, 0, len(recipes))
	for _, r := range recipes {
		views = append(views, RecipeView{
			Name:        r.Name,
			Description: r.Description,
			Skill:       r.Skill,
			Params:      r.Params,
			Trigger:     string(r.Trigger),
			CronExpr:    r.TriggerConfig.CronExpr,
			EventType:   r.TriggerConfig.EventType,
			MatchRules:  r.TriggerConfig.MatchRules,
			CreatedAt:   r.CreatedAt,
			UpdatedAt:   r.UpdatedAt,
		})
	}
	return views, nil
}

// DeleteRecipe removes a recipe by name. Returns error if not found.
func (a *App) DeleteRecipe(name string) error {
	if a.recipeStore == nil {
		return fmt.Errorf("recipe store not initialized")
	}
	return a.recipeStore.Delete(name)
}

// TriggerEventRecipes checks for event-triggered recipes matching the given event
// type and context, then executes each matching recipe by creating a new tab and
// submitting the skill command. Returns the names of triggered recipes.
func (a *App) TriggerEventRecipes(eventType string, context map[string]string) ([]string, error) {
	if a.recipeStore == nil {
		return nil, nil
	}
	matched, err := a.recipeStore.FindMatchingEventRecipes(eventType, context)
	if err != nil || len(matched) == 0 {
		return nil, err
	}
	var triggered []string
	for _, r := range matched {
		// Create a tab for the recipe execution.
		tabTitle := fmt.Sprintf("Recipe: %s", r.Name)
		topic, topicErr := a.CreateTopic("global", "", tabTitle)
		if topicErr != nil {
			continue
		}
		// Submit the skill command with parameters.
		input := "/" + r.Skill
		if r.Params != "" && r.Params != "{}" {
			input += " " + r.Params
		}
		a.SubmitDisplayToTab(topic.ID, "Recipe: "+r.Name, input)
		triggered = append(triggered, r.Name)
	}
	return triggered, nil
}

// TriggerEventWorkflows is the workflow analogue of TriggerEventRecipes: it
// finds event-triggered workflows whose EventType and MatchRules satisfy the
// supplied context and runs each via RunWorkflow. The context map is JSON-
// encoded and passed as the workflow input so ${input} substitution in prompt
// nodes can access event fields. Returns the names of triggered workflows.
//
// Mirrors recipe.Store.FindMatchingEventRecipes semantics (substring match on
// each rule value) so a migrated Recipe→Workflow preserves its trigger
// behaviour. Safe to call when no workflow store is initialised (returns nil).
func (a *App) TriggerEventWorkflows(eventType string, context map[string]string) ([]string, error) {
	if a.workflowStore == nil {
		return nil, nil
	}
	matched, err := a.workflowStore.FindMatchingEventWorkflows(eventType, context)
	if err != nil || len(matched) == 0 {
		return nil, err
	}
	var triggered []string
	for _, w := range matched {
		// Encode the context as the workflow input so prompt nodes can use
		// ${input} to read event fields. Keep it compact — match rules are
		// already satisfied, the workflow just needs the payload.
		input := ""
		if len(context) > 0 {
			if b, err := json.Marshal(context); err == nil {
				input = string(b)
			}
		}
		if err := a.RunWorkflow(w.Name, input); err != nil {
			fmt.Fprintf(os.Stderr, "trigger event workflow %q: %v\n", w.Name, err)
			continue
		}
		triggered = append(triggered, w.Name)
	}
	return triggered, nil
}

// registerWorkflowCronTriggers loads all cron-triggered workflows from the
// store and registers each with the existing scheduler. The scheduler calls
// executeScheduledTask on each tick; that function recognises the "wf:" name
// prefix and routes to RunWorkflow instead of submitting a skill command.
//
// Called once during startup, after sched.Start(). Re-running it (e.g. after
// a workflow is saved with a trigger change) re-registers — Register replaces
// any existing entry with the same name.
func (a *App) registerWorkflowCronTriggers() {
	if a.workflowStore == nil || a.sched == nil {
		return
	}
	wfs, err := a.workflowStore.ListByTrigger(workflow.TriggerCron)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: load cron workflows: %v\n", err)
		return
	}
	for _, w := range wfs {
		cronExpr := strings.TrimSpace(w.TriggerConfig.CronExpr)
		if cronExpr == "" {
			continue
		}
		// Prefix with "wf:" so executeScheduledTask can route to RunWorkflow
		// instead of the recipe "/<skill> <params>" path.
		name := "wf:" + w.Name
		if err := a.sched.Register(name, cronExpr, "", ""); err != nil {
			fmt.Fprintf(os.Stderr, "warning: register cron workflow %q (%s): %v\n", w.Name, cronExpr, err)
		}
	}
}

// ReloadWorkflowTriggers re-registers all cron-triggered workflows with the
// scheduler. Call this after a workflow is saved or deleted so the cron
// schedule stays in sync. Exposed via the Wails binding so the frontend can
// invoke it from the WorkflowEditor after a save. Register replaces existing
// entries with the same name, so re-saving a workflow updates its schedule
// in place; deleted workflows are pruned on the next full restart.
func (a *App) ReloadWorkflowTriggers() error {
	if a.workflowStore == nil || a.sched == nil {
		return nil
	}
	a.registerWorkflowCronTriggers()
	return nil
}

// MigrateRecipeToWorkflow converts an existing Recipe into a single-node skill
// Workflow and persists it. The resulting Workflow carries the Recipe's trigger
// configuration (cron / event) so the migration is lossless: a one-skill Recipe
// becomes a one-skill-node Workflow that runs identically.
//
// The original Recipe is NOT deleted — the caller can remove it after verifying
// the migrated Workflow runs correctly. If a Workflow with the same name already
// exists it is overwritten.
func (a *App) MigrateRecipeToWorkflow(recipeName string) error {
	if a.recipeStore == nil {
		return fmt.Errorf("recipe store not initialized")
	}
	if a.workflowStore == nil {
		return fmt.Errorf("workflow store not initialized")
	}
	r, err := a.recipeStore.Load(recipeName)
	if err != nil {
		return fmt.Errorf("load recipe %q: %w", recipeName, err)
	}

	// Build a single skill node that mirrors the Recipe's /<skill> <params>
	// invocation. The node config uses the JSON form so it round-trips through
	// the WorkflowEditor skill picker cleanly. Use json.Marshal (not fmt.Sprintf
	// %q) so non-ASCII / control chars are escaped per the JSON spec.
	type skillCfg struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments,omitempty"`
	}
	cfg := skillCfg{Name: r.Skill}
	if strings.TrimSpace(r.Params) != "" && r.Params != "{}" {
		cfg.Arguments = r.Params
	}
	cfgData, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal skill config: %w", err)
	}
	skillConfig := string(cfgData)

	wf := workflow.Workflow{
		Name:        r.Name,
		Description: r.Description,
		Nodes: []workflow.WorkflowNode{{
			ID:     "node-1",
			Label:  r.Skill,
			Kind:   "skill",
			Config: skillConfig,
		}},
		Edges:   []workflow.WorkflowEdge{},
		Trigger: workflow.TriggerType(r.Trigger),
		TriggerConfig: workflow.TriggerConfig{
			CronExpr:   r.TriggerConfig.CronExpr,
			EventType:  r.TriggerConfig.EventType,
			MatchRules: r.TriggerConfig.MatchRules,
		},
		CreatedAt: r.CreatedAt,
		UpdatedAt: 0, // Save will stamp it
	}
	if wf.Trigger == "" {
		wf.Trigger = workflow.TriggerManual
	}
	if err := a.workflowStore.Save(wf); err != nil {
		return fmt.Errorf("save migrated workflow: %w", err)
	}
	return nil
}

// ListClipboardHistory returns recent clipboard entries.
func (a *App) ListClipboardHistory(limit, offset int) ([]ClipboardEntry, error) {
	if a.clipboardHistory == nil {
		return nil, fmt.Errorf("clipboard history not initialized")
	}
	return a.clipboardHistory.ListClipboard(limit, offset)
}

// SearchClipboardHistory searches clipboard entries matching the query.
func (a *App) SearchClipboardHistory(query string) ([]ClipboardEntry, error) {
	if a.clipboardHistory == nil {
		return nil, fmt.Errorf("clipboard history not initialized")
	}
	return a.clipboardHistory.SearchClipboard(query)
}

// onboardingKeyEnv is the default provider (deepseek) key from config.Default().
const onboardingKeyEnv = "DEEPSEEK_API_KEY"

// onboardingBalanceURL doubles as a zero-token connectivity + auth probe:
// billing.FetchWithClient surfaces 401/403 for a bad key.
const onboardingBalanceURL = "https://api.deepseek.com/user/balance"

// NativeConfirmRequest is the payload for ConfirmAction — a native OS confirmation
// dialog that replaces web-style confirm() for destructive or important actions.
type NativeConfirmRequest struct {
	Title        string `json:"title"`
	Message      string `json:"message"`
	Detail       string `json:"detail"`
	ConfirmLabel string `json:"confirmLabel"`
	CancelLabel  string `json:"cancelLabel"`
	Destructive  bool   `json:"destructive"`
}

// ConfirmAction shows a native confirmation dialog and returns true when the user
// clicks the confirm button. For destructive actions the dialog type is Warning so
// the platform can apply its danger styling (red tint on macOS, etc.).
func (a *App) ConfirmAction(req NativeConfirmRequest) (bool, error) {
	if a.ctx == nil {
		return false, nil
	}
	dialogType := runtime.QuestionDialog
	if req.Destructive {
		dialogType = runtime.WarningDialog
	}
	confirm := req.ConfirmLabel
	if confirm == "" {
		confirm = "OK"
	}
	cancel := req.CancelLabel
	if cancel == "" {
		cancel = "Cancel"
	}
	title := req.Title
	if title == "" {
		title = req.Message
	}
	body := req.Message
	if req.Detail != "" {
		if body != "" {
			body += "\n\n" + req.Detail
		} else {
			body = req.Detail
		}
	}
	defaultBtn := confirm
	if req.Destructive {
		// On destructive actions, make cancel the default so Enter / Space
		// does NOT accidentally confirm. ESC always maps to CancelButton.
		defaultBtn = cancel
	}
	result, err := runtime.MessageDialog(a.ctx, runtime.MessageDialogOptions{
		Type:          dialogType,
		Title:         title,
		Message:       body,
		Buttons:       []string{confirm, cancel},
		DefaultButton: defaultBtn,
		CancelButton:  cancel,
	})
	if err != nil {
		return false, err
	}
	return result == confirm, nil
}

func (a *App) NeedsOnboarding() bool {
	return len(a.Models()) == 0
}

// ConnectKey validates apiKey against the balance endpoint, persists it to the
// global credentials file, and rebuilds the controller so the new key takes effect.
func (a *App) ConnectKey(apiKey string) error {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return fmt.Errorf("key is required")
	}
	ctx, cancel := context.WithTimeout(a.ctx, 8*time.Second)
	defer cancel()
	if _, err := billing.FetchWithClient(ctx, nil, onboardingBalanceURL, apiKey); err != nil {
		return fmt.Errorf("validate: %w", err)
	}
	if err := upsertDotEnv(onboardingKeyEnv, apiKey); err != nil {
		return fmt.Errorf("save: %w", err)
	}
	if err := a.rebuild(); err != nil {
		// Key is persisted; surface the failure but let the next rebuild load it.
		a.mu.Lock()
		if tab := a.activeTabLocked(); tab != nil {
			tab.StartupErr = err.Error()
		}
		a.mu.Unlock()
	}
	return nil
}

// --- Source control (Git) bindings ---

// GitStatus returns the full git status view for the active workspace.
func (a *App) GitStatus() GitStatusView {
	base, err := a.activeWorkspaceBase()
	if err != nil {
		return GitStatusView{GitAvailable: false, GitErr: err.Error()}
	}
	return gitStatusView(base)
}

// GitBranches returns the list of branches for the active workspace.
func (a *App) GitBranches() []BranchView {
	base, err := a.activeWorkspaceBase()
	if err != nil {
		return nil
	}
	return gitBranches(base)
}

// GitLog returns the last n commits for the active workspace.
func (a *App) GitLog(n int) []CommitView {
	base, err := a.activeWorkspaceBase()
	if err != nil {
		return nil
	}
	return gitLog(base, n)
}

// GitDiff returns the diff for a file. If path is empty, returns the full diff.
// If staged is true, shows the index diff against HEAD.
func (a *App) GitDiff(path string, staged bool) GitDiffView {
	base, err := a.activeWorkspaceBase()
	if err != nil {
		return GitDiffView{Err: err.Error()}
	}
	return gitDiff(base, path, staged)
}

// GitRemotes returns the remote names and URLs for the active workspace.
func (a *App) GitRemotes() map[string]string {
	base, err := a.activeWorkspaceBase()
	if err != nil {
		return nil
	}
	return gitRemotes(base)
}

// GitAdd stages the given paths. If paths is empty, stages all changes.
func (a *App) GitAdd(paths []string) GitOperationResult {
	base, err := a.activeWorkspaceBase()
	if err != nil {
		return GitOperationResult{Success: false, Message: err.Error()}
	}
	return gitAdd(base, paths)
}

// GitReset unstages the given paths. If paths is empty, unstages everything.
func (a *App) GitReset(paths []string) GitOperationResult {
	base, err := a.activeWorkspaceBase()
	if err != nil {
		return GitOperationResult{Success: false, Message: err.Error()}
	}
	return gitReset(base, paths)
}

// GitCommit creates a commit with the given message.
func (a *App) GitCommit(message string) GitOperationResult {
	base, err := a.activeWorkspaceBase()
	if err != nil {
		return GitOperationResult{Success: false, Message: err.Error()}
	}
	return gitCommit(base, message)
}

// GitPush pushes to the remote. If upstream is empty, pushes with --set-upstream.
func (a *App) GitPush(upstream string) GitOperationResult {
	base, err := a.activeWorkspaceBase()
	if err != nil {
		return GitOperationResult{Success: false, Message: err.Error()}
	}
	return gitPush(base, upstream)
}

// GitPull pulls from the remote for the current branch.
func (a *App) GitPull() GitOperationResult {
	base, err := a.activeWorkspaceBase()
	if err != nil {
		return GitOperationResult{Success: false, Message: err.Error()}
	}
	return gitPull(base)
}

// GitFetch fetches from all remotes.
func (a *App) GitFetch() GitOperationResult {
	base, err := a.activeWorkspaceBase()
	if err != nil {
		return GitOperationResult{Success: false, Message: err.Error()}
	}
	return gitFetch(base)
}

// GitCheckout switches to or creates a branch.
func (a *App) GitCheckout(branch string, create bool) GitOperationResult {
	base, err := a.activeWorkspaceBase()
	if err != nil {
		return GitOperationResult{Success: false, Message: err.Error()}
	}
	return gitCheckout(base, branch, create)
}

// GitRestore discards working-tree changes for the given paths.
func (a *App) GitRestore(paths []string) GitOperationResult {
	base, err := a.activeWorkspaceBase()
	if err != nil {
		return GitOperationResult{Success: false, Message: err.Error()}
	}
	return gitRestore(base, paths)
}

// GitRestoreStaged unstages the given paths (restores index from HEAD).
func (a *App) GitRestoreStaged(paths []string) GitOperationResult {
	base, err := a.activeWorkspaceBase()
	if err != nil {
		return GitOperationResult{Success: false, Message: err.Error()}
	}
	return gitRestoreStaged(base, paths)
}

// GitStashPush creates a new stash with an optional message.
func (a *App) GitStashPush(message string) GitOperationResult {
	base, err := a.activeWorkspaceBase()
	if err != nil {
		return GitOperationResult{Success: false, Message: err.Error()}
	}
	return gitStashPush(base, message)
}

// GitStashPop applies and removes the stash at the given index.
func (a *App) GitStashPop(index int) GitOperationResult {
	base, err := a.activeWorkspaceBase()
	if err != nil {
		return GitOperationResult{Success: false, Message: err.Error()}
	}
	return gitStashPop(base, index)
}

// GitStashApply applies the stash at the given index without removing it.
func (a *App) GitStashApply(index int) GitOperationResult {
	base, err := a.activeWorkspaceBase()
	if err != nil {
		return GitOperationResult{Success: false, Message: err.Error()}
	}
	return gitStashApply(base, index)
}

// GitStashList returns the list of stash entries.
func (a *App) GitStashList() []StashEntryView {
	base, err := a.activeWorkspaceBase()
	if err != nil {
		return nil
	}
	return gitStashList(base)
}

// GitDeleteBranch deletes the given branch.
func (a *App) GitDeleteBranch(branch string, force bool) GitOperationResult {
	base, err := a.activeWorkspaceBase()
	if err != nil {
		return GitOperationResult{Success: false, Message: err.Error()}
	}
	return gitDeleteBranch(base, branch, force)
}

// GitRenameBranch renames the current branch.
func (a *App) GitRenameBranch(newName string) GitOperationResult {
	base, err := a.activeWorkspaceBase()
	if err != nil {
		return GitOperationResult{Success: false, Message: err.Error()}
	}
	return gitRenameBranch(base, newName)
}

// GitInit initialises a new git repository in the active workspace.
func (a *App) GitInit() GitOperationResult {
	base, err := a.activeWorkspaceBase()
	if err != nil {
		return GitOperationResult{Success: false, Message: err.Error()}
	}
	return gitInit(base)
}

// GitMerge merges the given branch into the current branch.
func (a *App) GitMerge(branch string, noFF bool) GitOperationResult {
	base, err := a.activeWorkspaceBase()
	if err != nil {
		return GitOperationResult{Success: false, Message: err.Error()}
	}
	return gitMerge(base, branch, noFF)
}

// GitRebase rebases the current branch onto the given branch.
func (a *App) GitRebase(branch string) GitOperationResult {
	base, err := a.activeWorkspaceBase()
	if err != nil {
		return GitOperationResult{Success: false, Message: err.Error()}
	}
	return gitRebase(base, branch)
}

// GitRebaseAbort aborts an in-progress rebase.
func (a *App) GitRebaseAbort() GitOperationResult {
	base, err := a.activeWorkspaceBase()
	if err != nil {
		return GitOperationResult{Success: false, Message: err.Error()}
	}
	return gitRebaseAbort(base)
}

// GitRebaseContinue continues an in-progress rebase.
func (a *App) GitRebaseContinue() GitOperationResult {
	base, err := a.activeWorkspaceBase()
	if err != nil {
		return GitOperationResult{Success: false, Message: err.Error()}
	}
	return gitRebaseContinue(base)
}

// GitCherryPick cherry-picks the given commit.
func (a *App) GitCherryPick(hash string) GitOperationResult {
	base, err := a.activeWorkspaceBase()
	if err != nil {
		return GitOperationResult{Success: false, Message: err.Error()}
	}
	return gitCherryPick(base, hash)
}

// GitMergeAbort aborts an in-progress merge.
func (a *App) GitMergeAbort() GitOperationResult {
	base, err := a.activeWorkspaceBase()
	if err != nil {
		return GitOperationResult{Success: false, Message: err.Error()}
	}
	return gitMergeAbort(base)
}

// GitConflictFiles returns files with merge conflicts.
func (a *App) GitConflictFiles() []string {
	base, err := a.activeWorkspaceBase()
	if err != nil {
		return nil
	}
	return gitConflictFiles(base)
}

// GitResolveConflict marks a file as resolved.
func (a *App) GitResolveConflict(path string) GitOperationResult {
	base, err := a.activeWorkspaceBase()
	if err != nil {
		return GitOperationResult{Success: false, Message: err.Error()}
	}
	return gitResolveConflict(base, path)
}

// GitCheckoutOurs resolves a conflict using "ours" version.
func (a *App) GitCheckoutOurs(path string) GitOperationResult {
	base, err := a.activeWorkspaceBase()
	if err != nil {
		return GitOperationResult{Success: false, Message: err.Error()}
	}
	return gitCheckoutOurs(base, path)
}

// GitCheckoutTheirs resolves a conflict using "theirs" version.
func (a *App) GitCheckoutTheirs(path string) GitOperationResult {
	base, err := a.activeWorkspaceBase()
	if err != nil {
		return GitOperationResult{Success: false, Message: err.Error()}
	}
	return gitCheckoutTheirs(base, path)
}

// GitTags returns the list of tags.
func (a *App) GitTags() []TagView {
	base, err := a.activeWorkspaceBase()
	if err != nil {
		return nil
	}
	return gitTags(base)
}

// GitCreateTag creates a tag.
func (a *App) GitCreateTag(name string, message string) GitOperationResult {
	base, err := a.activeWorkspaceBase()
	if err != nil {
		return GitOperationResult{Success: false, Message: err.Error()}
	}
	return gitCreateTag(base, name, message)
}

// GitDeleteTag deletes a tag.
func (a *App) GitDeleteTag(name string) GitOperationResult {
	base, err := a.activeWorkspaceBase()
	if err != nil {
		return GitOperationResult{Success: false, Message: err.Error()}
	}
	return gitDeleteTag(base, name)
}

// GitRevert creates a revert commit for the given hash.
func (a *App) GitRevert(hash string, noCommit bool) GitOperationResult {
	base, err := a.activeWorkspaceBase()
	if err != nil {
		return GitOperationResult{Success: false, Message: err.Error()}
	}
	return gitRevert(base, hash, noCommit)
}

// GitShowCommit returns the full diff of a specific commit.
func (a *App) GitShowCommit(hash string) GitDiffView {
	base, err := a.activeWorkspaceBase()
	if err != nil {
		return GitDiffView{Err: err.Error()}
	}
	return gitShowCommit(base, hash)
}

// GitFileHistory returns the commit log for a specific file.
func (a *App) GitFileHistory(path string, n int) []CommitView {
	base, err := a.activeWorkspaceBase()
	if err != nil {
		return nil
	}
	return gitFileHistory(base, path, n)
}

// GitGenerateCommitMessage uses the current AI model to generate a commit
// message based on staged changes. Returns the generated message or an error.
func (a *App) GitGenerateCommitMessage() (string, error) {
	base, err := a.activeWorkspaceBase()
	if err != nil {
		return "", fmt.Errorf("no workspace: %w", err)
	}

	summary := gitDiffSummaryForCommit(base)
	if summary == "" {
		return "", fmt.Errorf("no changes to generate a commit message from")
	}

	// Resolve the current model and provider.
	a.mu.RLock()
	tab := a.activeTabLocked()
	a.mu.RUnlock()
	if tab == nil {
		return "", fmt.Errorf("no active tab")
	}

	root := a.activeWorkspaceRoot()
	cfg, err := config.LoadForRoot(root)
	if err != nil {
		return "", fmt.Errorf("load config: %w", err)
	}

	modelRef := tab.model
	if modelRef == "" {
		modelRef = cfg.DefaultModel
	}
	entry, ok := cfg.ResolveModel(modelRef)
	if !ok {
		return "", fmt.Errorf("cannot resolve model %q", modelRef)
	}
	apiKey := entry.APIKey()
	if apiKey == "" {
		return "", fmt.Errorf("provider %q has no API key configured", entry.Name)
	}

	return generateCommitMessage(a.reqCtx(), entry.BaseURL, apiKey, entry.Model, summary)
}

// --- data model bindings (Tasks 21-22) ---

// ScheduledTaskView is the JSON-serialisable form of a scheduled task for the frontend.
type ScheduledTaskView struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Cron       string `json:"cron"`
	Skill      string `json:"skill"`
	Parameters string `json:"parameters"`
	Enabled    bool   `json:"enabled"`
	LastRun    int64  `json:"lastRun"`
	NextRun    int64  `json:"nextRun"`
	CreatedAt  int64  `json:"createdAt"`
}

// TodoView is the JSON-serialisable form of a todo for the frontend.
type TodoView struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	DueDate     string `json:"dueDate"`
	Priority    string `json:"priority"`
	Status      string `json:"status"`
	Source      string `json:"source"`
	CreatedAt   int64  `json:"createdAt"`
	UpdatedAt   int64  `json:"updatedAt"`
}

// NotificationView is the JSON-serialisable form of a notification for the frontend.
type NotificationView struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	Read      bool   `json:"read"`
	CreatedAt int64  `json:"createdAt"`
}

// HomePageData aggregates the data shown on the home/welcome screen.
type HomePageData struct {
	RecentTasks     []SessionMeta    `json:"recentTasks"`
	SuggestedSkills []SuggestedSkill `json:"suggestedSkills"`
	DailyTip        string           `json:"dailyTip"`
}

// SuggestedSkill is one skill recommended for the user's role.
type SuggestedSkill struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// ds returns the data store, or nil if it failed to initialise.
func (a *App) ds() *datastore.Store {
	if a == nil {
		return nil
	}
	return a.dataStore
}

// GetHomePageData returns recent tasks, suggested skills, and a daily tip for
// the home screen.
func (a *App) GetHomePageData() HomePageData {
	out := HomePageData{
		RecentTasks:     a.GetRecentTasks(5),
		SuggestedSkills: a.GetSuggestedSkills(""),
		DailyTip:        i18n.M.DailyTip,
	}
	return out
}

// GetRecentTasks returns the most recent session history entries up to limit.
func (a *App) GetRecentTasks(limit int) []SessionMeta {
	if limit <= 0 {
		limit = 5
	}
	all := a.ListSessions()
	if len(all) > limit {
		all = all[:limit]
	}
	return all
}

// GetSuggestedSkills returns skills recommended for the given user role.
// If userRole is empty, it returns all available skills.
func (a *App) GetSuggestedSkills(userRole string) []SuggestedSkill {
	a.mu.RLock()
	ctrl := a.activeCtrlLocked()
	a.mu.RUnlock()
	if ctrl == nil {
		return []SuggestedSkill{}
	}
	var out []SuggestedSkill
	for _, s := range ctrl.AllSkills() {
		if !ctrl.SkillEnabled(s.Name) {
			continue
		}
		out = append(out, SuggestedSkill{Name: s.Name, Description: s.Description})
	}
	if out == nil {
		out = []SuggestedSkill{}
	}
	return out
}

// GetNotifications returns unread notifications.
func (a *App) GetNotifications() []NotificationView {
	ds := a.ds()
	if ds == nil {
		return []NotificationView{}
	}
	ns, err := ds.GetUnreadNotifications()
	if err != nil {
		return []NotificationView{}
	}
	out := make([]NotificationView, len(ns))
	for i, n := range ns {
		out[i] = notificationViewFromModel(n)
	}
	return out
}

// MarkNotificationRead marks a notification as read by ID.
func (a *App) MarkNotificationRead(id string) error {
	ds := a.ds()
	if ds == nil {
		return fmt.Errorf("data store not available")
	}
	return ds.MarkNotificationRead(id)
}

// ListScheduledTasks returns all scheduled tasks.
func (a *App) ListScheduledTasks() []ScheduledTaskView {
	ds := a.ds()
	if ds == nil {
		return []ScheduledTaskView{}
	}
	tasks, err := ds.ListScheduledTasks()
	if err != nil {
		return []ScheduledTaskView{}
	}
	out := make([]ScheduledTaskView, len(tasks))
	for i, t := range tasks {
		out[i] = scheduledTaskViewFromModel(t)
	}
	return out
}

// CreateScheduledTask creates a new scheduled task.
func (a *App) CreateScheduledTask(name, cron, skill, params string) error {
	ds := a.ds()
	if ds == nil {
		return fmt.Errorf("data store not available")
	}
	t := datastore.ScheduledTask{
		ID:         datastore.NewID(),
		Name:       strings.TrimSpace(name),
		Cron:       strings.TrimSpace(cron),
		Skill:      strings.TrimSpace(skill),
		Parameters: datastore.ParseJSONObject(params),
		Enabled:    true,
		CreatedAt:  time.Now().UnixMilli(),
	}
	return ds.CreateScheduledTask(t)
}

// UpdateScheduledTask updates an existing scheduled task.
func (a *App) UpdateScheduledTask(id, name, cron, skill, params string, enabled bool) error {
	ds := a.ds()
	if ds == nil {
		return fmt.Errorf("data store not available")
	}
	t := datastore.ScheduledTask{
		ID:         strings.TrimSpace(id),
		Name:       strings.TrimSpace(name),
		Cron:       strings.TrimSpace(cron),
		Skill:      strings.TrimSpace(skill),
		Parameters: datastore.ParseJSONObject(params),
		Enabled:    enabled,
	}
	// Preserve existing timestamps by loading the current task.
	existing, err := ds.ListScheduledTasks()
	if err != nil {
		return err
	}
	for _, e := range existing {
		if e.ID == t.ID {
			t.LastRun = e.LastRun
			t.NextRun = e.NextRun
			t.CreatedAt = e.CreatedAt
			break
		}
	}
	return ds.UpdateScheduledTask(t)
}

// DeleteScheduledTask deletes a scheduled task by ID.
func (a *App) DeleteScheduledTask(id string) error {
	ds := a.ds()
	if ds == nil {
		return fmt.Errorf("data store not available")
	}
	return ds.DeleteScheduledTask(id)
}

// ListTodos returns all todos.
func (a *App) ListTodos() []TodoView {
	ds := a.ds()
	if ds == nil {
		return []TodoView{}
	}
	todos, err := ds.ListTodos()
	if err != nil {
		return []TodoView{}
	}
	out := make([]TodoView, len(todos))
	for i, t := range todos {
		out[i] = todoViewFromModel(t)
	}
	return out
}

// CreateTodo creates a new todo item.
func (a *App) CreateTodo(title, description, dueDate, priority string) error {
	ds := a.ds()
	if ds == nil {
		return fmt.Errorf("data store not available")
	}
	now := time.Now().UnixMilli()
	t := datastore.Todo{
		ID:          datastore.NewID(),
		Title:       strings.TrimSpace(title),
		Description: strings.TrimSpace(description),
		DueDate:     strings.TrimSpace(dueDate),
		Priority:    datastore.ValidatePriority(priority),
		Status:      "pending",
		Source:      "user",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	return ds.CreateTodo(t)
}

// UpdateTodo updates an existing todo item.
func (a *App) UpdateTodo(id, title, description, dueDate, priority, status string) error {
	ds := a.ds()
	if ds == nil {
		return fmt.Errorf("data store not available")
	}
	t := datastore.Todo{
		ID:          strings.TrimSpace(id),
		Title:       strings.TrimSpace(title),
		Description: strings.TrimSpace(description),
		DueDate:     strings.TrimSpace(dueDate),
		Priority:    datastore.ValidatePriority(priority),
		Status:      datastore.ValidateTodoStatus(status),
		UpdatedAt:   time.Now().UnixMilli(),
	}
	// Preserve CreatedAt and Source by loading the current todo.
	existing, err := ds.ListTodos()
	if err != nil {
		return err
	}
	for _, e := range existing {
		if e.ID == t.ID {
			t.CreatedAt = e.CreatedAt
			t.Source = e.Source
			break
		}
	}
	return ds.UpdateTodo(t)
}

// DeleteTodo deletes a todo item by ID.
func (a *App) DeleteTodo(id string) error {
	ds := a.ds()
	if ds == nil {
		return fmt.Errorf("data store not available")
	}
	return ds.DeleteTodo(id)
}

func scheduledTaskViewFromModel(t datastore.ScheduledTask) ScheduledTaskView {
	return ScheduledTaskView{
		ID:         t.ID,
		Name:       t.Name,
		Cron:       t.Cron,
		Skill:      t.Skill,
		Parameters: t.Parameters,
		Enabled:    t.Enabled,
		LastRun:    t.LastRun,
		NextRun:    t.NextRun,
		CreatedAt:  t.CreatedAt,
	}
}

func todoViewFromModel(t datastore.Todo) TodoView {
	return TodoView{
		ID:          t.ID,
		Title:       t.Title,
		Description: t.Description,
		DueDate:     t.DueDate,
		Priority:    t.Priority,
		Status:      t.Status,
		Source:      t.Source,
		CreatedAt:   t.CreatedAt,
		UpdatedAt:   t.UpdatedAt,
	}
}

func notificationViewFromModel(n datastore.Notification) NotificationView {
	return NotificationView{
		ID:        n.ID,
		Kind:      n.Kind,
		Title:     n.Title,
		Body:      n.Body,
		Read:      n.Read,
		CreatedAt: n.CreatedAt,
	}
}

// WorkflowView is the wire format for workflow objects sent to the frontend.
type WorkflowView struct {
	Name          string             `json:"name"`
	Description   string             `json:"description"`
	Nodes         []WorkflowNodeView `json:"nodes"`
	Edges         []WorkflowEdgeView `json:"edges"`
	Trigger       string             `json:"trigger,omitempty"`
	CronExpr      string             `json:"cronExpr,omitempty"`
	EventType     string             `json:"eventType,omitempty"`
	MatchRules    map[string]string  `json:"matchRules,omitempty"`
	Version       int                `json:"version,omitempty"`
	AllowedSkills []string           `json:"allowedSkills,omitempty"`
	CreatedAt     int64              `json:"createdAt"`
	UpdatedAt     int64              `json:"updatedAt"`
}

// WorkflowNodeView is the wire format for workflow nodes sent to the frontend.
type WorkflowNodeView struct {
	ID              string `json:"id"`
	Label           string `json:"label"`
	Kind            string `json:"kind"`
	Config          string `json:"config"`
	Model           string `json:"model,omitempty"`
	Effort          string `json:"effort,omitempty"`
	RequireApproval bool   `json:"requireApproval,omitempty"`
	PositionX       int    `json:"positionX"`
	PositionY       int    `json:"positionY"`
}

// WorkflowEdgeView is the wire format for workflow edges sent to the frontend.
type WorkflowEdgeView struct {
	ID     string `json:"id"`
	Source string `json:"source"`
	Target string `json:"target"`
	Label  string `json:"label,omitempty"`
}

// SaveWorkflow persists a new or updated workflow. The name must be unique and filename-safe.
func (a *App) SaveWorkflow(w WorkflowView) error {
	if a.workflowStore == nil {
		return fmt.Errorf("workflow store not initialized")
	}
	nodes := make([]workflow.WorkflowNode, len(w.Nodes))
	for i, n := range w.Nodes {
		nodes[i] = workflow.WorkflowNode{
			ID:              n.ID,
			Label:           n.Label,
			Kind:            n.Kind,
			Config:          n.Config,
			Model:           n.Model,
			Effort:          n.Effort,
			RequireApproval: n.RequireApproval,
			PositionX:       n.PositionX,
			PositionY:       n.PositionY,
		}
	}
	edges := make([]workflow.WorkflowEdge, len(w.Edges))
	for i, e := range w.Edges {
		edges[i] = workflow.WorkflowEdge{
			ID:     e.ID,
			Source: e.Source,
			Target: e.Target,
			Label:  e.Label,
		}
	}
	wf := workflow.Workflow{
		Name:        w.Name,
		Description: w.Description,
		Nodes:       nodes,
		Edges:       edges,
		Trigger:     workflow.TriggerType(w.Trigger),
		TriggerConfig: workflow.TriggerConfig{
			CronExpr:   w.CronExpr,
			EventType:  w.EventType,
			MatchRules: w.MatchRules,
		},
		Version:       workflow.CurrentWorkflowVersion,
		AllowedSkills: w.AllowedSkills,
		CreatedAt:     w.CreatedAt,
		UpdatedAt:     w.UpdatedAt,
	}
	return a.workflowStore.Save(wf)
}

// LoadWorkflow reads a workflow by name. Returns error if not found.
func (a *App) LoadWorkflow(name string) (WorkflowView, error) {
	if a.workflowStore == nil {
		return WorkflowView{}, fmt.Errorf("workflow store not initialized")
	}
	wf, err := a.workflowStore.Load(name)
	if err != nil {
		return WorkflowView{}, err
	}
	return workflowViewFromModel(wf), nil
}

// ListWorkflows returns all workflows, sorted by creation time descending.
func (a *App) ListWorkflows() ([]WorkflowView, error) {
	if a.workflowStore == nil {
		return nil, fmt.Errorf("workflow store not initialized")
	}
	workflows, err := a.workflowStore.List()
	if err != nil {
		return nil, err
	}
	views := make([]WorkflowView, 0, len(workflows))
	for _, wf := range workflows {
		views = append(views, workflowViewFromModel(wf))
	}
	return views, nil
}

// DeleteWorkflow removes a workflow by name. Returns error if not found.
func (a *App) DeleteWorkflow(name string) error {
	if a.workflowStore == nil {
		return fmt.Errorf("workflow store not initialized")
	}
	return a.workflowStore.Delete(name)
}

// RunWorkflow creates a new tab and executes the workflow DAG in topological
// order. Each node becomes one turn submitted to the controller:
//   - skill  → "/<skill_name> <args>" (routes to run_skill, same path Recipes use)
//   - prompt → the config text (with ${input} substituted from the workflow input)
//   - tool   → a natural-language request to use the described tool
//
// Per-node Model/Effort overrides are applied between turns via SetModelForTab.
// condition and parallel nodes are not yet evaluated (P2) — they are skipped
// with a Notice so the rest of the graph still runs.
//
// Execution happens in a background goroutine so the UI stays responsive; the
// caller receives nil as soon as the tab is created. Step-level status flows
// to the transcript as Notice events.
func (a *App) RunWorkflow(name string, input string) error {
	if a.workflowStore == nil {
		return fmt.Errorf("workflow store not initialized")
	}
	wf, err := a.workflowStore.Load(name)
	if err != nil {
		return err
	}

	// Validate the graph and produce an execution order.
	order, err := workflow.TopoSort(wf)
	if err != nil {
		return fmt.Errorf("workflow %q: %w", name, err)
	}
	if len(order) == 0 {
		return fmt.Errorf("workflow %q has no nodes", name)
	}

	// Create a fresh tab for this run so each execution has its own transcript.
	tabTitle := fmt.Sprintf("Workflow: %s", wf.Name)
	topic, topicErr := a.CreateTopic("global", "", tabTitle)
	if topicErr != nil {
		return fmt.Errorf("create tab for workflow: %w", topicErr)
	}

	go a.executeWorkflowSteps(topic.ID, wf, order, input)
	return nil
}

// executeWorkflowSteps walks the workflow DAG in topological order, executing
// each reachable node. It runs on its own goroutine launched by RunWorkflow.
//
// P2/P3 graph-traversal semantics:
//   - reachable[nodeID] starts true only for root nodes (in-degree 0). A node
//     runs only when at least one of its predecessors marked it reachable.
//   - skill/prompt/tool nodes submit a turn, wait for idle, capture the last
//     assistant reply into outputs[nodeID], and mark ALL successors reachable.
//   - condition nodes evaluate their config expression (with and/or/not/parens);
//     only successors on the matching branch (edge label yes/true or no/false)
//     become reachable.
//   - parallel nodes spawn one independent sub-agent tab per branch head and
//     run them concurrently. Each branch head's output is captured into
//     outputs; branch heads are marked executed so the main loop skips them;
//     their successors are marked reachable so downstream nodes continue in
//     the main tab. Multi-node branches resume sequentially in the main tab
//     after the fan-out completes (the branch head's output is already in
//     outputs, so ${ref} substitution still works).
//   - executed[nodeID] tracks nodes already run by the parallel handler so
//     the main loop doesn't re-run them.
//
// ${nodeId.output} and ${input} references in node configs are substituted
// from outputs / the workflow input before the turn is submitted.
//
// P3 projection: every executed node emits a StepProgress event (in_progress
// → completed) so the AgentCanvas graph renders workflow progress live.
func (a *App) executeWorkflowSteps(tabID string, wf workflow.Workflow, order []workflow.WorkflowNode, input string) {
	ctx := a.ctx
	outputs := make(map[string]string, len(order))
	reachable := make(map[string]bool, len(order))
	executed := make(map[string]bool, len(order))

	// Build adjacency list and in-degree from edges. order is already
	// topologically sorted (validated by TopoSort in RunWorkflow).
	adjacency := make(map[string][]workflow.WorkflowEdge, len(order))
	indegree := make(map[string]int, len(order))
	for _, n := range order {
		indegree[n.ID] = 0
	}
	for _, e := range wf.Edges {
		adjacency[e.Source] = append(adjacency[e.Source], e)
		indegree[e.Target]++
	}
	// Seed reachable with root nodes (in-degree 0).
	for _, n := range order {
		if indegree[n.ID] == 0 {
			reachable[n.ID] = true
		}
	}

	executedCount := 0
	total := len(order)
	for _, node := range order {
		select {
		case <-ctx.Done():
			a.workflowNotice(tabID, "⏹ workflow %q aborted: %v", wf.Name, ctx.Err())
			return
		default:
		}

		if executed[node.ID] {
			// Already run by the parallel handler — skip without re-emitting
			// step events (the handler already did).
			continue
		}
		if !reachable[node.ID] {
			// This node sits on a branch that no condition selected. Skip
			// it silently — no Notice, no StepProgress — so the transcript
			// isn't cluttered with pruned nodes.
			continue
		}

		switch node.Kind {
		case "condition":
			a.runConditionNode(ctx, tabID, node, outputs, input, adjacency, reachable)
			executedCount++
		case "parallel":
			a.runParallelNode(ctx, tabID, node, order, outputs, input, adjacency, reachable, executed, wf.AllowedSkills)
			executedCount++
		default:
			// skill / prompt / tool
			if a.runExecutableNode(ctx, tabID, node, outputs, input, adjacency, reachable, wf.AllowedSkills) {
				executedCount++
			}
		}
	}

	a.workflowNotice(tabID, "✅ workflow %q finished — %d/%d nodes executed", wf.Name, executedCount, total)
}

// runConditionNode evaluates the node's expression, stores "true"/"false" as
// the node's output, and marks only the matching branch's successors reachable.
func (a *App) runConditionNode(ctx context.Context, tabID string, node workflow.WorkflowNode,
	outputs map[string]string, input string, adjacency map[string][]workflow.WorkflowEdge, reachable map[string]bool) {
	ctrl := a.ctrlByTabID(tabID)
	if ctrl != nil {
		ctrl.EmitWorkflowStep(node.ID, node.Label, "in_progress")
	}
	result, err := workflow.EvaluateCondition(node.Config, outputs, input)
	if err != nil {
		a.workflowNotice(tabID, "❌ condition %q failed: %v", node.Label, err)
		if ctrl != nil {
			ctrl.EmitWorkflowStep(node.ID, node.Label, "completed")
		}
		// On error treat as false so a single-branch graph still terminates.
		result = false
	}
	outputs[node.ID] = strconv.FormatBool(result)
	a.workflowNotice(tabID, "🔀 condition %q → %v", node.Label, result)
	// Mark reachable only for successors on the selected branch.
	for _, e := range adjacency[node.ID] {
		if workflow.BranchLabelFor(e.Label, result) {
			reachable[e.Target] = true
		}
	}
	if ctrl != nil {
		ctrl.EmitWorkflowStep(node.ID, node.Label, "completed")
	}
}

// runParallelNode spawns one independent sub-agent tab per branch head and runs
// them concurrently. Each branch head's output is captured into outputs; branch
// heads are marked in `executed` so the main loop skips them; their successors
// are marked reachable so downstream nodes continue in the main tab.
//
// Multi-node branches resume sequentially in the main tab after the fan-out
// completes — the branch head's output is already in `outputs`, so ${ref}
// substitution in subsequent nodes still works. This gives true concurrency
// for the fan-out (the independent first step of each branch), which is the
// common parallel pattern; deeply nested multi-step branches can be expressed
// as nested sub-workflows when needed.
//
// Each goroutine creates its own tab via CreateTopic (a tab IS an independent
// agent session — the desktop equivalent of spawn_agent). The main tab's
// controller emits StepProgress for the parallel node itself; each branch's
// StepProgress is emitted by its own sub-tab's controller.
func (a *App) runParallelNode(ctx context.Context, tabID string, node workflow.WorkflowNode,
	order []workflow.WorkflowNode, outputs map[string]string, input string,
	adjacency map[string][]workflow.WorkflowEdge, reachable map[string]bool,
	executed map[string]bool, allowedSkills []string) {
	ctrl := a.ctrlByTabID(tabID)
	if ctrl != nil {
		ctrl.EmitWorkflowStep(node.ID, node.Label, "in_progress")
	}

	branches := adjacency[node.ID]
	if len(branches) == 0 {
		a.workflowNotice(tabID, "⋮⋮ parallel %q — no branches", node.Label)
		if ctrl != nil {
			ctrl.EmitWorkflowStep(node.ID, node.Label, "completed")
		}
		return
	}
	a.workflowNotice(tabID, "⋮⋮ parallel %q — fanning out to %d branches (true concurrency)", node.Label, len(branches))

	// Index order by ID so goroutines can look up their branch head node.
	byID := make(map[string]*workflow.WorkflowNode, len(order))
	for i := range order {
		byID[order[i].ID] = &order[i]
	}

	type branchResult struct {
		branchID string
		output   string
		err      error
	}
	results := make([]branchResult, len(branches))

	var wg sync.WaitGroup
	for i, e := range branches {
		wg.Add(1)
		go func(idx int, edge workflow.WorkflowEdge) {
			defer wg.Done()
			br := branchResult{branchID: edge.Target}

			branchNode, ok := byID[edge.Target]
			if !ok {
				br.err = fmt.Errorf("branch head %q not found in order", edge.Target)
				results[idx] = br
				return
			}

			// P3 permission whitelist for skill branch heads.
			if branchNode.Kind == "skill" {
				if skillName, ok := workflow.SkillNameFromConfig(branchNode.Config); ok {
					if !workflow.IsSkillAllowed(skillName, allowedSkills) {
						br.err = fmt.Errorf("skill %q not in workflow whitelist", skillName)
						results[idx] = br
						return
					}
				}
			}

			// Build the branch head's input using the workflow-level outputs
			// captured so far (snapshot, not the live map — concurrent writers
			// must not race).
			stepInput, supported := workflow.BuildNodeInputWithRefs(*branchNode, input, outputs)
			if !supported {
				br.err = fmt.Errorf("branch %q: %s", branchNode.Label, stepInput)
				results[idx] = br
				return
			}

			// Create an independent sub-agent tab for this branch.
			subTabTitle := fmt.Sprintf("%s / %s", node.Label, branchNode.Label)
			topic, topicErr := a.CreateTopic("global", "", subTabTitle)
			if topicErr != nil {
				br.err = fmt.Errorf("create sub-tab for branch %q: %w", branchNode.Label, topicErr)
				results[idx] = br
				return
			}

			// Per-node model override on the sub-tab.
			if m := strings.TrimSpace(branchNode.Model); m != "" {
				if err := a.SetModelForTab(topic.ID, m); err != nil {
					a.workflowNotice(tabID, "⚠️ branch %q: model switch to %q failed (%v) — continuing with current model",
						branchNode.Label, m, err)
				}
			}

			subCtrl := a.ctrlByTabID(topic.ID)
			if subCtrl != nil {
				subCtrl.EmitWorkflowStep(branchNode.ID, branchNode.Label, "in_progress")
			}

			// P3 approval gate for side-effecting branch heads.
			if branchNode.RequireApproval && subCtrl != nil {
				subject := branchNode.Label
				if stepInput != "" {
					preview := stepInput
					if len(preview) > 120 {
						preview = preview[:120] + "…"
					}
					subject = fmt.Sprintf("%s — %s", branchNode.Label, preview)
				}
				allowed, err := subCtrl.RequestApproval(ctx, "workflow_parallel_"+branchNode.Kind, subject)
				if err != nil || !allowed {
					if err != nil {
						br.err = fmt.Errorf("approval error: %w", err)
					} else {
						br.err = fmt.Errorf("user denied approval")
					}
					results[idx] = br
					if subCtrl != nil {
						subCtrl.EmitWorkflowStep(branchNode.ID, branchNode.Label, "completed")
					}
					return
				}
			}

			display := fmt.Sprintf("▶ [parallel:%s] %s", branchNode.Kind, branchNode.Label)
			a.SubmitDisplayToTab(topic.ID, display, stepInput)
			a.waitTabIdle(ctx, topic.ID)

			if sc := a.ctrlByTabID(topic.ID); sc != nil {
				br.output = sc.LastAssistantText()
				sc.EmitWorkflowStep(branchNode.ID, branchNode.Label, "completed")
			}
			results[idx] = br
		}(i, e)
	}
	wg.Wait()

	// Merge branch outputs back into the workflow-level outputs map and
	// mark branch heads executed so the main loop skips them. Downstream
	// successors of each branch head become reachable.
	for i := range results {
		r := results[i]
		if r.err != nil {
			a.workflowNotice(tabID, "⚠️ branch %d (%q) failed: %v", i, r.branchID, r.err)
			// Still mark the branch head executed + successors reachable so
			// the workflow continues (with an empty output for this branch).
		} else {
			outputs[r.branchID] = r.output
			a.workflowNotice(tabID, "✓ branch %q completed (%d chars)", r.branchID, len(r.output))
		}
		executed[r.branchID] = true
		for _, e := range adjacency[r.branchID] {
			reachable[e.Target] = true
		}
	}

	if ctrl != nil {
		ctrl.EmitWorkflowStep(node.ID, node.Label, "completed")
	}
}

// runExecutableNode handles skill/prompt/tool nodes: apply the per-node model
// override, substitute ${...} refs, submit the turn, wait for idle, capture
// the assistant reply into outputs, and mark all successors reachable.
// Returns true when the node actually ran (false when it was unsupported,
// blocked by the whitelist, or denied by the user).
//
// P3 controls run before the turn is submitted:
//   - AllowedSkills: when non-empty, skill nodes whose skill name isn't listed
//     are refused (Notice + skip).
//   - RequireApproval: the node is gated behind ApprovalModal; a deny skips it.
func (a *App) runExecutableNode(ctx context.Context, tabID string, node workflow.WorkflowNode,
	outputs map[string]string, input string, adjacency map[string][]workflow.WorkflowEdge,
	reachable map[string]bool, allowedSkills []string) bool {
	ctrl := a.ctrlByTabID(tabID)

	// P3 permission whitelist: skill nodes are checked against AllowedSkills.
	if node.Kind == "skill" {
		if skillName, ok := workflow.SkillNameFromConfig(node.Config); ok {
			if !workflow.IsSkillAllowed(skillName, allowedSkills) {
				a.workflowNotice(tabID, "⛔ %q skipped: skill %q not in workflow whitelist", node.Label, skillName)
				return false
			}
		}
	}

	// Per-node model override. SetModelForTab requires the controller to be
	// idle, which holds here: we wait after every executable node.
	if m := strings.TrimSpace(node.Model); m != "" {
		if err := a.SetModelForTab(tabID, m); err != nil {
			a.workflowNotice(tabID, "⚠️ %q: model switch to %q failed (%v) — continuing with current model",
				node.Label, m, err)
		}
	}

	stepInput, supported := workflow.BuildNodeInputWithRefs(node, input, outputs)
	if !supported {
		a.workflowNotice(tabID, "⏭️ %q skipped: %s", node.Label, stepInput)
		return false
	}

	// P3 approval gate: side-effecting nodes flagged RequireApproval prompt
	// the user before running. A deny skips the node (and its branch, since
	// we return without marking successors reachable).
	if node.RequireApproval && ctrl != nil {
		subject := node.Label
		if stepInput != "" {
			// Truncate long inputs so the modal stays readable.
			preview := stepInput
			if len(preview) > 120 {
				preview = preview[:120] + "…"
			}
			subject = fmt.Sprintf("%s — %s", node.Label, preview)
		}
		allowed, err := ctrl.RequestApproval(ctx, "workflow_"+node.Kind, subject)
		if err != nil {
			a.workflowNotice(tabID, "⛔ %q skipped: approval error %v", node.Label, err)
			return false
		}
		if !allowed {
			a.workflowNotice(tabID, "⛔ %q skipped: user denied approval", node.Label)
			return false
		}
	}

	if ctrl != nil {
		ctrl.EmitWorkflowStep(node.ID, node.Label, "in_progress")
	}
	display := fmt.Sprintf("▶ [%s] %s", node.Kind, node.Label)
	a.SubmitDisplayToTab(tabID, display, stepInput)

	// Wait for the turn to finish so the next node can switch models or
	// read this node's output.
	a.waitTabIdle(ctx, tabID)

	// Capture the model's reply as this node's output for ${nodeID.output}.
	if ctrl = a.ctrlByTabID(tabID); ctrl != nil {
		outputs[node.ID] = ctrl.LastAssistantText()
		ctrl.EmitWorkflowStep(node.ID, node.Label, "completed")
	}

	// Mark all successors reachable (non-condition nodes don't branch).
	for _, e := range adjacency[node.ID] {
		reachable[e.Target] = true
	}
	return true
}

// waitTabIdle blocks until the tab's controller reports it is no longer running
// a turn, or until ctx is cancelled. Polling interval is 200ms, which is well
// below typical agent-step latency but light enough not to waste CPU.
func (a *App) waitTabIdle(ctx context.Context, tabID string) {
	const poll = 200 * time.Millisecond
	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			ctrl := a.ctrlByTabID(tabID)
			if ctrl == nil || !ctrl.Running() {
				return
			}
		}
	}
}

// workflowNotice surfaces a status line in the tab's transcript as a Notice
// event. Silently no-ops if the tab or controller is gone (e.g. user closed
// the tab mid-workflow).
func (a *App) workflowNotice(tabID, format string, args ...any) {
	ctrl := a.ctrlByTabID(tabID)
	if ctrl == nil {
		return
	}
	ctrl.Notice(fmt.Sprintf(format, args...))
}

// workflowViewFromModel converts an internal workflow.Workflow to a WorkflowView.
func workflowViewFromModel(wf workflow.Workflow) WorkflowView {
	nodes := make([]WorkflowNodeView, len(wf.Nodes))
	for i, n := range wf.Nodes {
		nodes[i] = WorkflowNodeView{
			ID:              n.ID,
			Label:           n.Label,
			Kind:            n.Kind,
			Config:          n.Config,
			Model:           n.Model,
			Effort:          n.Effort,
			RequireApproval: n.RequireApproval,
			PositionX:       n.PositionX,
			PositionY:       n.PositionY,
		}
	}
	edges := make([]WorkflowEdgeView, len(wf.Edges))
	for i, e := range wf.Edges {
		edges[i] = WorkflowEdgeView{
			ID:     e.ID,
			Source: e.Source,
			Target: e.Target,
			Label:  e.Label,
		}
	}
	return WorkflowView{
		Name:          wf.Name,
		Description:   wf.Description,
		Nodes:         nodes,
		Edges:         edges,
		Trigger:       string(wf.Trigger),
		CronExpr:      wf.TriggerConfig.CronExpr,
		EventType:     wf.TriggerConfig.EventType,
		MatchRules:    wf.TriggerConfig.MatchRules,
		Version:       wf.Version,
		AllowedSkills: wf.AllowedSkills,
		CreatedAt:     wf.CreatedAt,
		UpdatedAt:     wf.UpdatedAt,
	}
}
