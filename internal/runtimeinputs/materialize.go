package runtimeinputs

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/liza-mas/liza/internal/models"
)

// Materialization is one envelope read against its declaration. Values live
// only in memory; the ledger stores Identity and Names. File digests
// enter the keyed identity only: an unkeyed content digest in state would
// let a low-entropy file be guessed offline.
type Materialization struct {
	Identity string
	Values   map[string]string
	// Files maps each file-artifact name to the SHA-256 of its content.
	Files map[string]string
	Names []string
}

// Materialize reads the envelope at path once and checks it against the
// declaration: exactly the declared names, secret values of at least
// MinSecretValueBytes, and file artifacts that are regular files outside
// every forbidden root. It returns the keyed identity of the result.
func Materialize(key *Key, declaration models.RuntimeInput, envelope string, forbiddenRoots []string) (*Materialization, error) {
	data, err := readLocatedFile(envelope, forbiddenRoots, MaxEnvelopeBytes)
	if err != nil {
		return nil, err
	}
	values, err := ParseEnvelope(data)
	if err != nil {
		return nil, err
	}
	for name := range values {
		if !slices.Contains(declaration.Env, name) {
			return nil, fmt.Errorf("%w: %s is not declared by input %s", ErrInvalid, name, declaration.ID)
		}
	}
	for _, name := range declaration.Env {
		if _, ok := values[name]; !ok {
			return nil, fmt.Errorf("%w: input %s requires %s", ErrInvalid, declaration.ID, name)
		}
		if declaration.Secret && len(values[name]) < MinSecretValueBytes {
			return nil, fmt.Errorf("%w: secret %s is shorter than %d bytes", ErrInvalid, name, MinSecretValueBytes)
		}
	}
	files := make(map[string]string, len(declaration.Files))
	for _, name := range declaration.Files {
		digest, err := hashLocatedFile(values[name], forbiddenRoots)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		files[name] = digest
	}
	names := slices.Clone(declaration.Env)
	sort.Strings(names)
	return &Materialization{Identity: key.Identity(values, files), Values: values, Files: files, Names: names}, nil
}

// SecretValues returns the values declared-set masking must hide.
func (m *Materialization) SecretValues() []string {
	values := make([]string, 0, len(m.Values))
	for _, name := range m.Names {
		values = append(values, m.Values[name])
	}
	return values
}

// Environment returns NAME=VALUE entries in name order.
func (m *Materialization) Environment() []string {
	entries := make([]string, 0, len(m.Names))
	for _, name := range m.Names {
		entries = append(entries, name+"="+m.Values[name])
	}
	return entries
}

// CheckLocation requires an absolute, clean path whose resolved location lies
// outside every forbidden root (the repository and its worktrees).
func CheckLocation(path string, forbiddenRoots []string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return fmt.Errorf("%w: path must be absolute and clean", ErrInvalid)
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return unavailable(err)
	}
	for _, root := range forbiddenRoots {
		if root == "" {
			continue
		}
		base, err := filepath.EvalSymlinks(root)
		if err != nil {
			base = filepath.Clean(root)
		}
		if resolved == base || strings.HasPrefix(resolved, base+string(filepath.Separator)) {
			return fmt.Errorf("%w: artifacts must live outside the repository and its worktrees", ErrInvalid)
		}
	}
	return nil
}

func readLocatedFile(path string, forbiddenRoots []string, limit int64) ([]byte, error) {
	file, err := openLocatedFile(path, forbiddenRoots)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, unavailable(err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%w: file exceeds %d bytes", ErrInvalid, limit)
	}
	return data, nil
}

func hashLocatedFile(path string, forbiddenRoots []string) (string, error) {
	file, err := openLocatedFile(path, forbiddenRoots)
	if err != nil {
		return "", err
	}
	defer func() { _ = file.Close() }()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", unavailable(err)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func openLocatedFile(path string, forbiddenRoots []string) (*os.File, error) {
	if err := CheckLocation(path, forbiddenRoots); err != nil {
		return nil, err
	}
	// Refuse a FIFO or device before opening it: opening a FIFO blocks until
	// a writer appears, before any command timeout applies. The check after
	// opening covers a file swapped in between.
	if info, err := os.Stat(path); err != nil {
		return nil, unavailable(err)
	} else if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: not a regular file", ErrInvalid)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, unavailable(err)
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, unavailable(err)
	}
	if !info.Mode().IsRegular() {
		_ = file.Close()
		return nil, fmt.Errorf("%w: not a regular file", ErrInvalid)
	}
	return file, nil
}

// unavailable classifies a read failure without quoting the OS error, which
// names the path.
func unavailable(err error) error {
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%w: file does not exist", ErrUnavailable)
	}
	if errors.Is(err, fs.ErrPermission) {
		return fmt.Errorf("%w: file is not readable", ErrUnavailable)
	}
	return fmt.Errorf("%w: file could not be read", ErrUnavailable)
}
