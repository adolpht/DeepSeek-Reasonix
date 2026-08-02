//go:build !windows

package credential

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// defaultStorePath returns the credential store path for non-Windows platforms.
// The file is AES-encrypted with a machine-specific key derived from the user's
// home directory path (not as strong as DPAPI/keychain, but better than plaintext).
func defaultStorePath() string {
	cfgDir, err := os.UserConfigDir()
	if err != nil {
		cfgDir = filepath.Join(os.TempDir(), "rexion")
	}
	return filepath.Join(cfgDir, "rexion", "credentials.enc")
}

// New creates or opens the credential store at the default path.
func New() *Store {
	return &Store{path: storePath()}
}

// Get retrieves a credential by key.
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

// Set stores a credential.
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
		return nil
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

// machineKey derives a 32-byte AES key from a machine-specific identifier.
// This is NOT as strong as DPAPI/keychain, but prevents casual reading.
func machineKey() []byte {
	hostname, _ := os.Hostname()
	home, _ := os.UserHomeDir()
	uid := fmt.Sprintf("%s:%s:%s", hostname, home, "rexion-credential-v1")
	h := sha256.Sum256([]byte(uid))
	return h[:]
}

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

	enc, err := base64.StdEncoding.DecodeString(string(raw))
	if err != nil {
		return nil, fmt.Errorf("credential: base64 decode: %w", err)
	}

	decrypted, err := aesDecrypt(enc, machineKey())
	if err != nil {
		return nil, fmt.Errorf("credential: decrypt: %w", err)
	}

	var data map[string]string
	if err := json.Unmarshal(decrypted, &data); err != nil {
		return nil, fmt.Errorf("credential: json unmarshal: %w", err)
	}
	return data, nil
}

func (s *Store) writeStore(data map[string]string) error {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("credential: json marshal: %w", err)
	}

	encrypted, err := aesEncrypt(jsonData, machineKey())
	if err != nil {
		return fmt.Errorf("credential: encrypt: %w", err)
	}

	encoded := base64.StdEncoding.EncodeToString(encrypted)

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("credential: mkdir: %w", err)
	}

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

func aesEncrypt(plaintext, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

func aesDecrypt(ciphertext, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}
	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	return gcm.Open(nil, nonce, ciphertext, nil)
}
