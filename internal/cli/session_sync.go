package cli

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"rexion/internal/agent"
	"rexion/internal/config"
	"rexion/internal/i18n"
)

// sessionCommand dispatches the `rexion session <subcommand>` family.
// Subcommands: push, pull, list-remote, push-all.
func sessionCommand(args []string) int {
	if len(args) == 0 {
		sessionUsage()
		return 2
	}
	sub := args[0]
	rest := args[1:]
	switch sub {
	case "push":
		return sessionPush(rest)
	case "pull":
		return sessionPull(rest)
	case "list-remote":
		return sessionListRemote(rest)
	case "push-all":
		return sessionPushAll(rest)
	case "help", "--help", "-h":
		sessionUsage()
		return 0
	default:
		fmt.Fprintf(os.Stderr, i18n.M.UnknownCommandFmt+"\n\n", sub)
		sessionUsage()
		return 2
	}
}

func sessionUsage() {
	fmt.Print(`Usage:
  rexion session push [session-id]    Push a session to the sync backend
  rexion session pull <remote-id>     Pull a remote session into local store
  rexion session list-remote          List sessions on the sync backend
  rexion session push-all             Push all local sessions

Flags:
  --backend=file|http   Sync backend (default: file)
  --device-name=NAME    Override the device name stamped on pushes
`)
}

// sessionStore creates a Store from the loaded config, mirroring how boot.Build
// resolves the backend (auto-detect: SQLite if the db exists, JSONL otherwise).
func sessionStore() (agent.Store, error) {
	sessionDir := config.SessionDir()
	if sessionDir == "" {
		return nil, fmt.Errorf("cannot resolve session directory")
	}
	cfg, _ := config.Load()
	storeCfg := agent.StoreConfig{
		Dir:  sessionDir,
		Path: agent.DefaultSQLitePath(sessionDir),
	}
	if cfg != nil {
		storeCfg.Backend = cfg.Store.Backend
		storeCfg.Path = cfg.Store.Path
		storeCfg.AutoMigrate = cfg.Store.AutoMigrate
		if storeCfg.Path == "" {
			storeCfg.Path = agent.DefaultSQLitePath(sessionDir)
		}
	}
	return agent.NewStore(storeCfg)
}

// rexionRoot returns the Rexion config root (~/.config/Rexion), derived from
// the config file path so it stays consistent with the rest of the app.
func rexionRoot() string {
	p := config.UserConfigPath()
	if p == "" {
		return ""
	}
	return filepath.Dir(p)
}

// devicePath returns the path to the persistent device identity file.
func devicePath() string {
	root := rexionRoot()
	if root == "" {
		return ""
	}
	return filepath.Join(root, "device.json")
}

// defaultSyncDir returns the default file-backend sync directory.
func defaultSyncDir() string {
	root := rexionRoot()
	if root == "" {
		return ""
	}
	return filepath.Join(root, "sync")
}

// newSyncBackend constructs the requested backend by name ("file" or "http").
// An empty name defaults to "file".
func newSyncBackend(name string) (agent.SyncBackend, error) {
	if name == "" {
		name = "file"
	}
	switch name {
	case "file":
		return agent.NewFileBackend(defaultSyncDir())
	case "http":
		return agent.NewHTTPBackend("", "")
	default:
		return nil, fmt.Errorf("unknown backend: %s (use file or http)", name)
	}
}

// newSyncManager wires up the store + backend + device identity into a manager.
func newSyncManager(backendName string) (*agent.SyncManager, error) {
	store, err := sessionStore()
	if err != nil {
		return nil, err
	}
	backend, err := newSyncBackend(backendName)
	if err != nil {
		store.Close()
		return nil, err
	}
	return agent.NewSyncManager(store, backend, devicePath())
}

// currentSessionID returns the most recent local session ID, or "" if there
// are none. Used as the default for `session push` with no argument.
func currentSessionID() string {
	sessions, err := agent.ListSessions(config.SessionDir())
	if err != nil || len(sessions) == 0 {
		return ""
	}
	return agent.BranchID(sessions[0].Path)
}

// sessionPush handles `rexion session push [session-id]`.
func sessionPush(args []string) int {
	fs := flag.NewFlagSet("session push", flag.ContinueOnError)
	backend := fs.String("backend", "file", "sync backend: file or http")
	deviceName := fs.String("device-name", "", "override the device name")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	sessionID := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if sessionID == "" {
		sessionID = currentSessionID()
		if sessionID == "" {
			fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, "no sessions available to push")
			return 1
		}
		fmt.Fprintf(os.Stderr, "  %s %s\n", dim("using most recent session:"), sessionID)
	}

	mgr, err := newSyncManager(*backend)
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 1
	}
	defer mgr.StoreClose()

	if *deviceName != "" {
		mgr.SetDeviceName(*deviceName)
	}

	entry, err := mgr.PushSession(sessionID)
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 1
	}
	fmt.Printf("%s pushed session %s\n", green("✓"), entry.SessionID)
	fmt.Printf("  remote-id: %s\n", entry.RemoteID)
	fmt.Printf("  device:    %s\n", entry.DeviceName)
	fmt.Printf("  size:      %d bytes\n", entry.Size)
	fmt.Printf("  pushed-at: %s\n", entry.PushedAt.Local().Format("2006-01-02 15:04:05"))
	return 0
}

// sessionPull handles `rexion session pull <remote-id>`.
func sessionPull(args []string) int {
	fs := flag.NewFlagSet("session pull", flag.ContinueOnError)
	backend := fs.String("backend", "file", "sync backend: file or http")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	remoteID := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if remoteID == "" {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, "usage: rexion session pull <remote-id>")
		return 2
	}

	mgr, err := newSyncManager(*backend)
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 1
	}
	defer mgr.StoreClose()

	newID, err := mgr.PullSession(remoteID)
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 1
	}
	fmt.Printf("%s pulled remote %s into local session %s\n", green("✓"), remoteID, newID)
	return 0
}

// sessionListRemote handles `rexion session list-remote`.
func sessionListRemote(args []string) int {
	fs := flag.NewFlagSet("session list-remote", flag.ContinueOnError)
	backend := fs.String("backend", "file", "sync backend: file or http")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	mgr, err := newSyncManager(*backend)
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 1
	}
	defer mgr.StoreClose()

	entries, err := mgr.ListRemote()
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 1
	}
	if len(entries) == 0 {
		fmt.Println("(no remote sessions)")
		return 0
	}
	fmt.Printf("%-22s  %-20s  %-16s  %10s  %s\n", "REMOTE-ID", "SESSION-ID", "DEVICE", "SIZE", "PUSHED-AT")
	for _, e := range entries {
		fmt.Printf("%-22s  %-20s  %-16s  %10d  %s\n",
			e.RemoteID,
			truncateID(e.SessionID, 20),
			truncateID(e.DeviceName, 16),
			e.Size,
			e.PushedAt.Local().Format("2006-01-02 15:04"),
		)
	}
	return 0
}

// sessionPushAll handles `rexion session push-all`.
func sessionPushAll(args []string) int {
	fs := flag.NewFlagSet("session push-all", flag.ContinueOnError)
	backend := fs.String("backend", "file", "sync backend: file or http")
	deviceName := fs.String("device-name", "", "override the device name")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	mgr, err := newSyncManager(*backend)
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 1
	}
	defer mgr.StoreClose()

	if *deviceName != "" {
		mgr.SetDeviceName(*deviceName)
	}

	entries, skipped, err := mgr.PushAllSessions()
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 1
	}
	fmt.Printf("%s pushed %d session(s)", green("✓"), len(entries))
	if skipped > 0 {
		fmt.Printf(", %d skipped", skipped)
	}
	fmt.Println()
	for _, e := range entries {
		fmt.Printf("  %s → remote %s\n", e.SessionID, e.RemoteID)
	}
	return 0
}

// truncateID shortens an ID for table display.
func truncateID(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}
