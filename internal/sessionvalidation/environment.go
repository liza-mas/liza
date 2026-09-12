// Package sessionvalidation checks declared prerequisites in an explicit process
// environment. It never infers a provider's execution policy.
package sessionvalidation

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

var fingerprintKey = randomBytes(32)
var fingerprintDomain = hex.EncodeToString(randomBytes(16))

func randomBytes(n int) []byte {
	value := make([]byte, n)
	if _, err := rand.Read(value); err != nil {
		panic("validation preflight: randomness unavailable")
	}
	return value
}

// Domain identifies this process's private fingerprint comparison domain. A
// persisted fingerprint from another domain cannot establish environment equality.
func Domain() string { return fingerprintDomain }

// Fingerprint returns an opaque, process-local HMAC of the effective environment.
// The random key stays private; the digest contains no recoverable environment values.
func Fingerprint(env []string) string {
	canonical, _ := json.Marshal(normalizeEnvironment(env))
	mac := hmac.New(sha256.New, fingerprintKey)
	_, _ = mac.Write(canonical)
	return hex.EncodeToString(mac.Sum(nil))
}

// ResolveEnvironment applies env-file overlays to a copy of base. optional permits
// missing catalog defaults only; every other read failure is fatal. Files use the
// existing KEY=VALUE format, without shell expansion or quote interpretation.
func ResolveEnvironment(base []string, root string, files []string, optional bool) ([]string, error) {
	env := append([]string{}, base...)
	for i, file := range files {
		path := file
		if !filepath.IsAbs(path) {
			path = filepath.Join(root, path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			if optional && os.IsNotExist(err) {
				continue
			}
			return nil, &Error{Code: "env_file_unavailable", CommandIndex: -1, CheckIndex: i}
		}
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			if idx := strings.Index(line, " #"); idx >= 0 {
				line = strings.TrimRight(line[:idx], " ")
			}
			if strings.Contains(line, "=") {
				env = append(env, line)
			}
		}
	}
	return normalizeEnvironment(env), nil
}

// normalizeEnvironment matches exec.Cmd's last-value-wins environment semantics,
// and sorts entries so representation order does not change the fingerprint.
func normalizeEnvironment(env []string) []string {
	values := make(map[string]string, len(env))
	for _, entry := range env {
		key, value, ok := strings.Cut(entry, "=")
		if !ok || key == "" {
			continue
		}
		if runtime.GOOS == "windows" {
			key = strings.ToUpper(key)
		}
		values[key] = value
	}
	result := make([]string, 0, len(values))
	for key, value := range values {
		result = append(result, key+"="+value)
	}
	sort.Strings(result)
	return result
}

func environmentValue(env []string, name string) string {
	for i := len(env) - 1; i >= 0; i-- {
		key, value, ok := strings.Cut(env[i], "=")
		if ok && (key == name || runtime.GOOS == "windows" && strings.EqualFold(key, name)) {
			return value
		}
	}
	return ""
}

// LookPath resolves against env and cwd exclusively, including relative PATH
// entries. The returned absolute path avoids exec.Command's ambient PATH lookup.
func LookPath(name, cwd string, env []string) (string, error) {
	missing := &Error{Code: "executable_missing", CommandIndex: -1, CheckIndex: -1}
	if name == "" || !filepath.IsAbs(cwd) {
		return "", missing
	}
	candidates := []string{name}
	if !filepath.IsAbs(name) && !strings.ContainsAny(name, `/\`) {
		candidates = nil
		for _, dir := range filepath.SplitList(environmentValue(env, "PATH")) {
			candidates = append(candidates, filepath.Join(dir, name))
		}
	}
	for _, candidate := range candidates {
		if !filepath.IsAbs(candidate) {
			candidate = filepath.Join(cwd, candidate)
		}
		extensions := []string{""}
		if runtime.GOOS == "windows" && filepath.Ext(candidate) == "" {
			extensions = filepath.SplitList(environmentValue(env, "PATHEXT"))
			if len(extensions) == 0 {
				// Match os/exec's Windows defaults when PATHEXT is absent.
				extensions = []string{".com", ".exe", ".bat", ".cmd"}
			}
		}
		for _, extension := range extensions {
			path := candidate + extension
			info, err := os.Stat(path)
			if err == nil && info.Mode().IsRegular() && (runtime.GOOS == "windows" || info.Mode().Perm()&0111 != 0) {
				return path, nil
			}
		}
	}
	return "", missing
}
