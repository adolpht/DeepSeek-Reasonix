package main

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"rexion/internal/credential"
	"rexion/internal/memory"
)

// DataVaultLocation describes one user-data location on disk.
type DataVaultLocation struct {
	Label     string `json:"label"`
	Path      string `json:"path"`
	Kind      string `json:"kind"`
	Size      int64  `json:"size,omitempty"`
	Removable bool   `json:"removable"`
}

// DataVaultView is the full data inventory for the DataVault panel.
type DataVaultView struct {
	Locations []DataVaultLocation `json:"locations"`
	TotalSize int64               `json:"totalSize"`
}

// DataVault returns an inventory of all user-data locations.
func (a *App) DataVault() DataVaultView {
	out := DataVaultView{Locations: []DataVaultLocation{}}
	var total int64

	add := func(label, path, kind string, removable bool) {
		if path == "" {
			return
		}
		var size int64
		filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
			if err == nil && !info.IsDir() {
				size += info.Size()
			}
			return nil
		})
		total += size
		out.Locations = append(out.Locations, DataVaultLocation{
			Label: label, Path: path, Kind: kind, Size: size, Removable: removable,
		})
	}

	// Sessions directory (SQLite + JSONL)
	if dir, err := agentSessionDir(); err == nil {
		add("Sessions", dir, "session", true)
	}

	// Memory directory
	if memDir, err := memory.MemoryDir(); err == nil {
		add("Memory", memDir, "memory", true)
	}

	// Config directory
	if cfgDir, err := os.UserConfigDir(); err == nil {
		rexionCfg := filepath.Join(cfgDir, "Rexion")
		if _, err := os.Stat(rexionCfg); err == nil {
			add("Config", rexionCfg, "config", false)
		}
	}

	// Credential store
	credStore := credential.New()
	if keys, err := credStore.List(); err == nil && len(keys) > 0 {
		add("Credentials (encrypted)", credStorePath(), "credential", true)
	}

	// Project .rexion directories (if in a project workspace)
	a.mu.RLock()
	for _, tab := range a.tabs {
		if tab != nil && tab.WorkspaceRoot != "" {
			rexionDir := filepath.Join(tab.WorkspaceRoot, ".rexion")
			if _, err := os.Stat(rexionDir); err == nil {
				add("Project data ("+filepath.Base(tab.WorkspaceRoot)+")", rexionDir, "session", true)
			}
		}
	}
	a.mu.RUnlock()

	out.TotalSize = total
	return out
}

// DataVaultExport creates a zip archive of all user data and returns the path.
func (a *App) DataVaultExport() (string, error) {
	inventory := a.DataVault()

	cfgDir, _ := os.UserConfigDir()
	if cfgDir == "" {
		cfgDir = os.TempDir()
	}
	exportDir := filepath.Join(cfgDir, "Rexion", "exports")
	os.MkdirAll(exportDir, 0700)

	ts := time.Now().Format("2006-01-02_150405")
	outPath := filepath.Join(exportDir, fmt.Sprintf("rexion_data_%s.zip", ts))

	f, err := os.Create(outPath)
	if err != nil {
		return "", fmt.Errorf("datavault: create zip: %w", err)
	}
	defer f.Close()

	w := zip.NewWriter(f)
	defer w.Close()

	for _, loc := range inventory.Locations {
		filepath.Walk(loc.Path, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(filepath.Dir(loc.Path), path)
			if err != nil {
				return nil
			}
			// Prefix with the location label to organize within the zip.
			zipPath := filepath.Join(sanitizeForZip(loc.Label), rel)
			zf, err := w.Create(zipPath)
			if err != nil {
				return nil
			}
			src, err := os.Open(path)
			if err != nil {
				return nil
			}
			defer src.Close()
			io.Copy(zf, src)
			return nil
		})
	}

	return outPath, nil
}

// DataVaultPurge removes all removable user data (sessions, memory, credentials).
// Config and non-removable data are preserved.
func (a *App) DataVaultPurge() error {
	inventory := a.DataVault()
	for _, loc := range inventory.Locations {
		if !loc.Removable {
			continue
		}
		if err := os.RemoveAll(loc.Path); err != nil {
			return fmt.Errorf("datavault: purge %s: %w", loc.Label, err)
		}
	}
	return nil
}

// agentSessionDir returns the directory where agent sessions are stored.
func agentSessionDir() (string, error) {
	cfgDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cfgDir, "Rexion", "sessions"), nil
}

// credStorePath returns the credential store file path for display purposes.
func credStorePath() string {
	return credential.New().Path()
}

// sanitizeForZip replaces characters that are problematic in zip paths.
func sanitizeForZip(s string) string {
	s = strings.ReplaceAll(s, " ", "_")
	s = strings.ReplaceAll(s, ":", "-")
	return s
}
