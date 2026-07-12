package agent

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// SyncBackend is the transport-neutral interface for pushing and pulling
// serialized sessions across devices. Implementations include FileBackend
// (shared directory / USB) and HTTPBackend (cloud relay).
type SyncBackend interface {
	// Push uploads a serialized session payload and returns the remote ID
	// under which it was stored.
	Push(sessionID string, data []byte) (remoteID string, err error)
	// Pull downloads a previously pushed session by its remote ID.
	Pull(remoteID string) ([]byte, error)
	// List returns the entries currently available on the backend.
	List() ([]SyncEntry, error)
	// Delete removes a remote session entry.
	Delete(remoteID string) error
}

// SyncEntry is the metadata record for a session stored on a sync backend.
// It identifies the session, the device that pushed it, and when.
type SyncEntry struct {
	RemoteID   string    `json:"remote_id"`
	SessionID  string    `json:"session_id"`
	DeviceName string    `json:"device_name"`
	PushedAt   time.Time `json:"pushed_at"`
	Size       int64     `json:"size"`
}

// DeviceInfo is the persistent device identity. It is auto-generated on first
// use and stored at ~/.config/Rexion/device.json so every push is attributed to
// the same device name across sessions.
type DeviceInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// syncPayload is the on-the-wire (and on-disk) wrapper that couples sync
// metadata with the serialized session bytes. Data is a raw JSON message so
// the session payload stays nested without double-encoding.
type syncPayload struct {
	Entry SyncEntry       `json:"sync_entry"`
	Data  json.RawMessage `json:"data"`
}

// --- Device identity ---

// LoadOrCreateDevice loads the device identity from the Rexion config dir,
// generating and persisting a fresh one on first run. The ID is a short random
// hex string; the Name defaults to the hostname so users can recognise their
// devices in the remote list. devicePath is typically
// ~/.config/Rexion/device.json.
func LoadOrCreateDevice(devicePath string) (DeviceInfo, error) {
	if devicePath == "" {
		return DeviceInfo{}, fmt.Errorf("sync: empty device path")
	}
	if raw, err := os.ReadFile(devicePath); err == nil {
		var d DeviceInfo
		if err := json.Unmarshal(raw, &d); err == nil && d.ID != "" {
			return d, nil
		}
		// Fall through to create on malformed file.
	} else if !os.IsNotExist(err) {
		return DeviceInfo{}, fmt.Errorf("sync: read device: %w", err)
	}
	d := DeviceInfo{
		ID:   newDeviceID(),
		Name: defaultDeviceName(),
	}
	if err := saveDevice(devicePath, d); err != nil {
		return DeviceInfo{}, fmt.Errorf("sync: save device: %w", err)
	}
	return d, nil
}

// saveDevice writes the device identity atomically to devicePath.
func saveDevice(devicePath string, d DeviceInfo) error {
	if err := os.MkdirAll(filepath.Dir(devicePath), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(devicePath), ".device.*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return os.Rename(tmpPath, devicePath)
}

// newDeviceID returns a 16-char random hex string.
func newDeviceID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// defaultDeviceName returns the hostname, falling back to "device" on error.
func defaultDeviceName() string {
	host, err := os.Hostname()
	if err != nil || host == "" {
		return "device"
	}
	return host
}

// --- FileBackend ---

// FileBackend stores sync payloads as individual JSON files in a shared
// directory (e.g. a USB mount or a cloud-drive folder synced by Dropbox /
// OneDrive). Each file is <remoteID>.json containing a syncPayload.
type FileBackend struct {
	dir        string
	deviceName string
}

// NewFileBackend creates a file-system sync backend rooted at dir. The
// directory is created if it doesn't exist.
func NewFileBackend(dir string) (*FileBackend, error) {
	if dir == "" {
		return nil, fmt.Errorf("file backend: empty directory")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("file backend: create dir: %w", err)
	}
	return &FileBackend{dir: dir}, nil
}

// SetDeviceName stamps the device name on subsequently pushed entries.
func (b *FileBackend) SetDeviceName(name string) { b.deviceName = name }

func (b *FileBackend) path(remoteID string) string {
	return filepath.Join(b.dir, remoteID+".json")
}

// Push writes the session payload to <dir>/<remoteID>.json.
func (b *FileBackend) Push(sessionID string, data []byte) (string, error) {
	remoteID := newRemoteID()
	payload := syncPayload{
		Entry: SyncEntry{
			RemoteID:   remoteID,
			SessionID:  sessionID,
			DeviceName: b.deviceName,
			PushedAt:   time.Now().UTC(),
			Size:       int64(len(data)),
		},
		Data: json.RawMessage(data),
	}
	raw, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "", fmt.Errorf("file push: marshal: %w", err)
	}
	raw = append(raw, '\n')
	path := b.path(remoteID)
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		return "", fmt.Errorf("file push: write: %w", err)
	}
	return remoteID, nil
}

// Pull reads and returns the session payload for remoteID.
func (b *FileBackend) Pull(remoteID string) ([]byte, error) {
	raw, err := os.ReadFile(b.path(remoteID))
	if err != nil {
		return nil, fmt.Errorf("file pull: %w", err)
	}
	var payload syncPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("file pull: unmarshal: %w", err)
	}
	return []byte(payload.Data), nil
}

// List scans the sync directory and returns metadata for every stored entry.
func (b *FileBackend) List() ([]SyncEntry, error) {
	entries, err := os.ReadDir(b.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("file list: %w", err)
	}
	var out []SyncEntry
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(b.dir, e.Name()))
		if err != nil {
			continue
		}
		var payload syncPayload
		if err := json.Unmarshal(raw, &payload); err != nil {
			continue
		}
		out = append(out, payload.Entry)
	}
	return out, nil
}

// Delete removes the sync file for remoteID.
func (b *FileBackend) Delete(remoteID string) error {
	err := os.Remove(b.path(remoteID))
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("file delete: %w", err)
	}
	return nil
}

// --- HTTPBackend ---

// HTTPBackend talks to a cloud relay server. The base URL is read from the
// REXION_SYNC_URL environment variable; an optional Bearer token from
// REXION_SYNC_TOKEN is sent for authentication. Uses only net/http.
type HTTPBackend struct {
	baseURL    string
	token      string
	deviceName string
	client     *http.Client
}

// NewHTTPBackend creates an HTTP sync backend. If baseURL is empty the
// REXION_SYNC_URL env var is used; if token is empty the REXION_SYNC_TOKEN
// env var is used. Returns an error if no URL is configured.
func NewHTTPBackend(baseURL, token string) (*HTTPBackend, error) {
	if baseURL == "" {
		baseURL = os.Getenv("REXION_SYNC_URL")
	}
	if baseURL == "" {
		return nil, fmt.Errorf("http backend: REXION_SYNC_URL is not set")
	}
	if token == "" {
		token = os.Getenv("REXION_SYNC_TOKEN")
	}
	return &HTTPBackend{
		baseURL: baseURL,
		token:   token,
		client:  &http.Client{Timeout: 30 * time.Second},
	}, nil
}

// SetDeviceName stamps the device name on subsequently pushed entries.
func (b *HTTPBackend) SetDeviceName(name string) { b.deviceName = name }

func (b *HTTPBackend) do(method, path string, body io.Reader) (*http.Response, error) {
	url := b.baseURL + path
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}
	if b.token != "" {
		req.Header.Set("Authorization", "Bearer "+b.token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return b.client.Do(req)
}

// Push POSTs the session payload to the relay and returns the server-assigned
// remote ID.
func (b *HTTPBackend) Push(sessionID string, data []byte) (string, error) {
	remoteID := newRemoteID()
	payload := syncPayload{
		Entry: SyncEntry{
			RemoteID:   remoteID,
			SessionID:  sessionID,
			DeviceName: b.deviceName,
			PushedAt:   time.Now().UTC(),
			Size:       int64(len(data)),
		},
		Data: json.RawMessage(data),
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("http push: marshal: %w", err)
	}
	resp, err := b.do(http.MethodPost, "/push", bytes.NewReader(raw))
	if err != nil {
		return "", fmt.Errorf("http push: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("http push: %s: %s", resp.Status, string(body))
	}
	// The server may assign a different remote ID; honour it if present.
	var result struct {
		RemoteID string `json:"remote_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err == nil && result.RemoteID != "" {
		return result.RemoteID, nil
	}
	return remoteID, nil
}

// Pull GETs the session payload from the relay.
func (b *HTTPBackend) Pull(remoteID string) ([]byte, error) {
	resp, err := b.do(http.MethodGet, "/pull/"+remoteID, nil)
	if err != nil {
		return nil, fmt.Errorf("http pull: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("http pull: %s: %s", resp.Status, string(body))
	}
	var payload syncPayload
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("http pull: decode: %w", err)
	}
	return []byte(payload.Data), nil
}

// List GETs the entry list from the relay.
func (b *HTTPBackend) List() ([]SyncEntry, error) {
	resp, err := b.do(http.MethodGet, "/list", nil)
	if err != nil {
		return nil, fmt.Errorf("http list: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("http list: %s: %s", resp.Status, string(body))
	}
	var result struct {
		Entries []SyncEntry `json:"entries"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("http list: decode: %w", err)
	}
	return result.Entries, nil
}

// Delete sends a DELETE request to remove the remote entry.
func (b *HTTPBackend) Delete(remoteID string) error {
	resp, err := b.do(http.MethodDelete, "/pull/"+remoteID, nil)
	if err != nil {
		return fmt.Errorf("http delete: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("http delete: %s: %s", resp.Status, string(body))
	}
	return nil
}

// --- SyncManager ---

// SyncManager coordinates session serialization, device identity, and backend
// transport. It is the high-level API the CLI and desktop call into; the
// individual backends are swappable so the same manager works for file-based
// and HTTP-based sync.
type SyncManager struct {
	backend SyncBackend
	store   Store
	device  DeviceInfo
}

// deviceStamper is implemented by backends that can stamp a device name on
// pushed entries. The SyncManager calls SetDeviceName before Push.
type deviceStamper interface {
	SetDeviceName(name string)
}

// NewSyncManager creates a manager backed by the given Store and SyncBackend.
// The device identity is loaded (or generated) from devicePath so pushes are
// attributed to a stable device name.
func NewSyncManager(store Store, backend SyncBackend, devicePath string) (*SyncManager, error) {
	device, err := LoadOrCreateDevice(devicePath)
	if err != nil {
		return nil, err
	}
	// Stamp the device name on the backend so pushes are attributed correctly.
	if ds, ok := backend.(deviceStamper); ok {
		ds.SetDeviceName(device.Name)
	}
	return &SyncManager{
		backend: backend,
		store:   store,
		device:  device,
	}, nil
}

// Device returns the current device identity.
func (m *SyncManager) Device() DeviceInfo { return m.device }

// SetDeviceName overrides the device name used to stamp pushes. This is a
// runtime-only change; it is not persisted to device.json.
func (m *SyncManager) SetDeviceName(name string) {
	if name = strings.TrimSpace(name); name != "" {
		m.device.Name = name
		if ds, ok := m.backend.(deviceStamper); ok {
			ds.SetDeviceName(name)
		}
	}
}

// StoreClose closes the underlying session store. Callers should defer this
// after NewSyncManager to avoid leaking the SQLite/JSONL file handle.
func (m *SyncManager) StoreClose() error {
	return m.store.Close()
}

// PushSession serializes a local session and pushes it to the backend. The
// device name from the manager's identity is stamped on the entry so the
// remote list can show which device pushed what.
func (m *SyncManager) PushSession(sessionID string) (SyncEntry, error) {
	data, err := SerializeSession(m.store, sessionID)
	if err != nil {
		return SyncEntry{}, err
	}
	remoteID, err := m.backend.Push(sessionID, data)
	if err != nil {
		return SyncEntry{}, err
	}
	entries, _ := m.backend.List()
	for _, e := range entries {
		if e.RemoteID == remoteID {
			return e, nil
		}
	}
	// Fallback: construct the entry locally if the backend's List didn't
	// return it (e.g. HTTP server assigns IDs asynchronously).
	return SyncEntry{
		RemoteID:   remoteID,
		SessionID:  sessionID,
		DeviceName: m.device.Name,
		PushedAt:   time.Now().UTC(),
		Size:       int64(len(data)),
	}, nil
}

// PullSession pulls a remote session and imports it into the local store. A
// new local session ID is returned — the import never overwrites an existing
// session.
func (m *SyncManager) PullSession(remoteID string) (string, error) {
	data, err := m.backend.Pull(remoteID)
	if err != nil {
		return "", err
	}
	return DeserializeSession(m.store, data)
}

// ListRemote returns the entries available on the backend.
func (m *SyncManager) ListRemote() ([]SyncEntry, error) {
	return m.backend.List()
}

// DeleteRemote removes a remote entry.
func (m *SyncManager) DeleteRemote(remoteID string) error {
	return m.backend.Delete(remoteID)
}

// PushAllSessions serializes and pushes every local session. Sessions that
// fail to serialize are skipped with a warning count; the returned entries
// contain one entry per successfully pushed session.
func (m *SyncManager) PushAllSessions() ([]SyncEntry, int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	threads, err := m.store.ListThreads(ctx, ListOpts{})
	if err != nil {
		return nil, 0, fmt.Errorf("push all: list threads: %w", err)
	}
	var entries []SyncEntry
	skipped := 0
	for _, t := range threads {
		entry, err := m.PushSession(t.ID)
		if err != nil {
			skipped++
			continue
		}
		entries = append(entries, entry)
	}
	return entries, skipped, nil
}

// newRemoteID returns a unique remote ID: a timestamp + random hex suffix,
// matching the session-path naming convention for sortability.
func newRemoteID() string {
	stamp := time.Now().UTC().Format("20060102-150405")
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%s-%s", stamp, hex.EncodeToString(b))
}
