// Package archiveobject installs immutable content-addressed evidence.
// Envelope and domain validation remain with each archive's caller.
package archiveobject

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// ErrConflict means an existing digest-named object has different bytes.
var ErrConflict = errors.New("archive object conflict: existing object differs")

// Digest returns the SHA-256 of the object's exact bytes.
func Digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// IsDigest accepts only lowercase SHA-256, so references cannot direct I/O.
func IsDigest(digest string) bool {
	if len(digest) != sha256.Size*2 {
		return false
	}
	for _, ch := range digest {
		if !('0' <= ch && ch <= '9') && !('a' <= ch && ch <= 'f') {
			return false
		}
	}
	return true
}

// Path derives a safe object path beneath the caller's archive directory.
func Path(archiveDir, digest string) (string, error) {
	if !IsDigest(digest) {
		return "", fmt.Errorf("invalid archive object digest %q", digest)
	}
	return filepath.Join(archiveDir, "objects", digest[:2], digest+".json"), nil
}

// Write installs bytes without overwriting evidence and completes durability
// even on identical-object reuse. The optional directory barrier supports
// caller-local fault injection; nil uses SyncDir.
func Write(archiveDir string, data []byte, syncDir func(string) error) (string, error) {
	digest := Digest(data)
	path, err := Path(archiveDir, digest)
	if err != nil {
		return "", err
	}
	dir := filepath.Dir(path)
	existing, err := os.ReadFile(path)
	switch {
	case err == nil:
		if !bytes.Equal(existing, data) {
			return "", fmt.Errorf("%w: %s", ErrConflict, path)
		}
	case errors.Is(err, os.ErrNotExist):
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", fmt.Errorf("create archive directory: %w", err)
		}
		if err := install(dir, path, data); err != nil {
			return "", err
		}
	default:
		return "", fmt.Errorf("read archive object: %w", err)
	}
	if err := SyncFile(path); err != nil {
		return "", fmt.Errorf("sync archive object: %w", err)
	}
	if syncDir == nil {
		syncDir = SyncDir
	}
	for _, d := range []string{dir, filepath.Dir(dir), archiveDir, filepath.Dir(archiveDir)} {
		if err := syncDir(d); err != nil {
			return "", fmt.Errorf("sync archive directory %s: %w", d, err)
		}
	}
	return digest, nil
}

func install(dir, path string, data []byte) error {
	f, err := os.CreateTemp(dir, filepath.Base(path)+".tmp.*")
	if err != nil {
		return fmt.Errorf("create archive temp file: %w", err)
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err := f.Chmod(0o644); err != nil {
		f.Close()
		return fmt.Errorf("set archive object permissions: %w", err)
	}
	_, writeErr := f.Write(data)
	syncErr := f.Sync()
	closeErr := f.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		return fmt.Errorf("write archive object: %w", err)
	}
	if err := link(tmp, path); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return fmt.Errorf("install archive object: %w", err)
		}
		existing, readErr := os.ReadFile(path)
		if readErr != nil || !bytes.Equal(existing, data) {
			return fmt.Errorf("%w: %s", ErrConflict, path)
		}
	}
	return nil
}

func link(tmp, path string) error {
	if runtime.GOOS == "windows" {
		if _, err := os.Stat(path); err == nil {
			return os.ErrExist
		}
		return os.Rename(tmp, path)
	}
	return os.Link(tmp, path)
}

// SyncFile flushes an installed file without modifying its bytes.
func SyncFile(path string) error {
	flag := os.O_RDONLY
	if runtime.GOOS == "windows" {
		flag = os.O_RDWR // FlushFileBuffers needs a write-capable handle.
	}
	f, err := os.OpenFile(path, flag, 0)
	if err != nil {
		return err
	}
	return errors.Join(f.Sync(), f.Close())
}

// SyncDir is best-effort on Windows, where directories cannot be fsynced.
func SyncDir(dir string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	return errors.Join(f.Sync(), f.Close())
}
