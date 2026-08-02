// Package credential provides secure credential storage that supplements
// environment-variable-based API keys. On Windows it uses DPAPI; on other
// platforms it falls back to an encrypted file in the user's config directory.
//
// The Store is a key-value store where keys are the same env-var names used in
// api_key_env (e.g. "DEEPSEEK_API_KEY") and values are the secret strings.
// When os.Getenv returns empty for a key, the caller can fall back to
// Store.Get to check the secure store.
package credential

import "sync"

// Store is the process-level credential store. It is safe for concurrent use.
type Store struct {
	mu   sync.RWMutex
	path string
}

// Path returns the file path of the credential store.
func (s *Store) Path() string { return s.path }

// storePath returns the file path for the credential store on non-Windows
// platforms. On Windows, DPAPI-protected credentials are stored in the
// system credential store directory instead.
func storePath() string {
	return defaultStorePath()
}
