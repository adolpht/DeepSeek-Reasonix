//go:build windows

package credential

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

var (
	dllcrypt32   = syscall.NewLazyDLL("crypt32.dll")
	procProtect  = dllcrypt32.NewProc("CryptProtectData")
	procUnprotect = dllcrypt32.NewProc("CryptUnprotectData")
)

// DATA_BLOB maps the Windows DATA_BLOB struct.
type DATA_BLOB struct {
	CbData uint32
	PbData *byte
}

// defaultStorePath returns the Windows credential store directory.
func defaultStorePath() string {
	cfgDir, err := os.UserConfigDir()
	if err != nil {
		cfgDir = filepath.Join(os.TempDir(), "rexion")
	}
	return filepath.Join(cfgDir, "Rexion", "credentials.dat")
}

// New creates or opens the credential store at the default path.
func New() *Store {
	return &Store{path: storePath()}
}

// Get retrieves a credential by key from the DPAPI-protected store.
func (s *Store) Get(key string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, err := s.readStore()
	if err != nil {
		return "", fmt.Errorf("credential: read store: %w", err)
	}
	if v, ok := data[key]; ok {
		return v, nil
	}
	return "", fmt.Errorf("credential: key %q not found", key)
}

// Set stores a credential in the DPAPI-protected store.
func (s *Store) Set(key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := s.readStore()
	if err != nil {
		data = make(map[string]string)
	}
	data[key] = value
	return s.writeStore(data)
}

// Delete removes a credential from the store.
func (s *Store) Delete(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := s.readStore()
	if err != nil {
		return nil // already empty
	}
	delete(data, key)
	return s.writeStore(data)
}

// List returns all stored credential keys.
func (s *Store) List() ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, err := s.readStore()
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	return keys, nil
}

// readStore decrypts and reads the credential file.
func (s *Store) readStore() (map[string]string, error) {
	raw, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]string), nil
		}
		return nil, err
	}
	if len(raw) == 0 {
		return make(map[string]string), nil
	}

	// Decode base64 → DPAPI-encrypted blob → decrypt → JSON → map
	enc, err := base64.StdEncoding.DecodeString(string(raw))
	if err != nil {
		return nil, fmt.Errorf("credential: base64 decode: %w", err)
	}

	decrypted, err := dpapiUnprotect(enc)
	if err != nil {
		return nil, fmt.Errorf("credential: dpapi unprotect: %w", err)
	}

	var data map[string]string
	if err := json.Unmarshal(decrypted, &data); err != nil {
		return nil, fmt.Errorf("credential: json unmarshal: %w", err)
	}
	return data, nil
}

// writeStore encrypts and writes the credential file.
func (s *Store) writeStore(data map[string]string) error {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("credential: json marshal: %w", err)
	}

	encrypted, err := dpapiProtect(jsonData)
	if err != nil {
		return fmt.Errorf("credential: dpapi protect: %w", err)
	}

	encoded := base64.StdEncoding.EncodeToString(encrypted)

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("credential: mkdir: %w", err)
	}

	// Write atomically: temp file → rename
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, []byte(encoded), 0600); err != nil {
		return fmt.Errorf("credential: write temp: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("credential: rename: %w", err)
	}
	return nil
}

// dpapiProtect encrypts data using Windows DPAPI (CryptProtectData).
// The data is encrypted with the current user's credentials.
func dpapiProtect(data []byte) ([]byte, error) {
	in := DATA_BLOB{
		CbData: uint32(len(data)),
		PbData: &data[0],
	}
	var out DATA_BLOB

	r, _, err := procProtect.Call(
		uintptr(unsafe.Pointer(&in)),
		0, // optional entropy
		0, // reserved
		0, // prompt struct
		0, // flags
		uintptr(unsafe.Pointer(&out)),
	)
	if r == 0 {
		return nil, fmt.Errorf("CryptProtectData: %w", err)
	}
	defer syscall.LocalFree(syscall.Handle(uintptr(unsafe.Pointer(out.PbData))))

	result := make([]byte, out.CbData)
	copy(result, unsafe.Slice(out.PbData, out.CbData))
	return result, nil
}

// dpapiUnprotect decrypts data using Windows DPAPI (CryptUnprotectData).
func dpapiUnprotect(data []byte) ([]byte, error) {
	in := DATA_BLOB{
		CbData: uint32(len(data)),
		PbData: &data[0],
	}
	var out DATA_BLOB

	r, _, err := procUnprotect.Call(
		uintptr(unsafe.Pointer(&in)),
		0,
		0,
		0,
		0,
		uintptr(unsafe.Pointer(&out)),
	)
	if r == 0 {
		return nil, fmt.Errorf("CryptUnprotectData: %w", err)
	}
	defer syscall.LocalFree(syscall.Handle(uintptr(unsafe.Pointer(out.PbData))))

	result := make([]byte, out.CbData)
	copy(result, unsafe.Slice(out.PbData, out.CbData))
	return result, nil
}

// randomBytes generates cryptographically random bytes.
func randomBytes(n int) []byte {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return b
}
