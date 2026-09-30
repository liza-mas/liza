package runtimeinputs

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/liza-mas/liza/internal/paths"
)

// KeyFileName is the operator key's file name in the global runtime directory.
const KeyFileName = "runtime-input.key"

const keyBytes = 32

// Key is the persistent operator-owned HMAC key materialization identities
// are computed with (INVARIANTS §9: persisted digests of environment values
// are keyed). It never enters state, a child environment or an error.
type Key struct {
	secret []byte
	// ID names the key in ledger records without revealing it.
	ID string
}

// DefaultKeyPath is <home>/<global dir>/runtime-input.key.
func DefaultKeyPath() (string, error) {
	dir, err := paths.GlobalLizaDir()
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrKeyUnavailable, err)
	}
	return filepath.Join(dir, KeyFileName), nil
}

// LoadKey reads an existing key. A missing or malformed key is
// ErrKeyUnavailable; callers never generate a replacement while the ledger
// holds instances, since a new key could not recognize spent materializations.
func LoadKey(path string) (*Key, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("%w: key file does not exist", ErrKeyUnavailable)
		}
		return nil, fmt.Errorf("%w: key file unreadable", ErrKeyUnavailable)
	}
	secret, err := hex.DecodeString(strings.TrimSpace(string(data)))
	if err != nil || len(secret) != keyBytes {
		return nil, fmt.Errorf("%w: key file is malformed", ErrKeyUnavailable)
	}
	return newKey(secret), nil
}

// CreateKey writes a fresh key with mode 0600, refusing to overwrite one.
func CreateKey(path string) (*Key, error) {
	secret := make([]byte, keyBytes)
	if _, err := rand.Read(secret); err != nil {
		return nil, fmt.Errorf("%w: randomness unavailable", ErrKeyUnavailable)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("%w: cannot create key directory", ErrKeyUnavailable)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, fmt.Errorf("%w: cannot create key file", ErrKeyUnavailable)
	}
	_, writeErr := file.WriteString(hex.EncodeToString(secret) + "\n")
	syncErr := file.Sync()
	closeErr := file.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		_ = os.Remove(path)
		return nil, fmt.Errorf("%w: cannot write key file", ErrKeyUnavailable)
	}
	return newKey(secret), nil
}

func newKey(secret []byte) *Key {
	id := sha256.Sum256(append([]byte("runtime-input-key-id\x00"), secret...))
	return &Key{secret: secret, ID: hex.EncodeToString(id[:8])}
}

// Identity is the keyed digest of a canonical materialization: each declared
// name with its value, except that a file-artifact name contributes its
// content digest instead of its path. Formatting of the envelope and the
// location of a file therefore never change identity.
func (k *Key) Identity(values map[string]string, fileDigests map[string]string) string {
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	mac := hmac.New(sha256.New, k.secret)
	for _, name := range names {
		if digest, isFile := fileDigests[name]; isFile {
			_, _ = fmt.Fprintf(mac, "F\x00%s\x00%s\n", name, digest)
			continue
		}
		_, _ = fmt.Fprintf(mac, "V\x00%s\x00%s\n", name, values[name])
	}
	return hex.EncodeToString(mac.Sum(nil))
}
