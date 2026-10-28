package oracle

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
)

// EnvBase is the input of the child-environment policy.
type EnvBase struct {
	Home string
	Path string
	Tmp  string
	// Go marks a Go-toolchain child: it also gets the build caches and
	// GOFLAGS=-buildvcs=false.
	Go bool
}

// ChildEnv builds the exact environment of a child process; no other host
// variable reaches it. Every platform gets HOME, PATH and TMPDIR. Windows adds
// USERPROFILE (os.UserHomeDir), TMP and TEMP (os.TempDir) so the disposable
// directories take effect, and SYSTEMROOT, which os/exec would otherwise
// append from the host: naming it keeps the set exact. Go-toolchain children
// add GOCACHE, GOMODCACHE and GOFLAGS. Each extra is a NAME=value pair; one
// redefining a policy name is an error naming it.
func ChildEnv(base EnvBase, extra ...string) ([]string, error) {
	var caches goCaches
	if base.Go {
		var err error
		if caches, err = parentGoCaches(); err != nil {
			return nil, err
		}
	}
	return childEnv(runtime.GOOS, base, caches, os.Getenv("SYSTEMROOT"), extra)
}

type goCaches struct{ cache, modCache string }

func childEnv(goos string, base EnvBase, caches goCaches, systemRoot string, extra []string) ([]string, error) {
	env := []string{"HOME=" + base.Home, "PATH=" + base.Path, "TMPDIR=" + base.Tmp}
	if goos == "windows" {
		env = append(env,
			"USERPROFILE="+base.Home,
			"TMP="+base.Tmp,
			"TEMP="+base.Tmp,
			"SYSTEMROOT="+systemRoot,
		)
	}
	if base.Go {
		env = append(env,
			"GOCACHE="+caches.cache,
			"GOMODCACHE="+caches.modCache,
			"GOFLAGS=-buildvcs=false",
		)
	}
	policy := map[string]bool{}
	for _, kv := range env {
		policy[envKey(goos, kv)] = true
	}
	for i, kv := range extra {
		name, _, ok := strings.Cut(kv, "=")
		if !ok || name == "" {
			return nil, fmt.Errorf("oracle: extra environment entry %d is not NAME=value", i)
		}
		if policy[envKey(goos, kv)] {
			return nil, fmt.Errorf("oracle: extra environment entry redefines policy variable %s", name)
		}
	}
	return append(env, extra...), nil
}

// envKey is the comparison key of an entry's name: names are case-insensitive
// on Windows.
func envKey(goos, kv string) string {
	name, _, _ := strings.Cut(kv, "=")
	if goos == "windows" {
		return strings.ToUpper(name)
	}
	return name
}

var (
	goCachesOnce sync.Once
	goCachesVal  goCaches
	goCachesErr  error
)

// parentGoCaches resolves the parent's GOCACHE and GOMODCACHE once per
// process, so Go children find the caches without inheriting the variables
// that locate them.
func parentGoCaches() (goCaches, error) {
	goCachesOnce.Do(func() {
		out, err := exec.Command("go", "env", "GOCACHE", "GOMODCACHE").Output()
		if err != nil {
			goCachesErr = fmt.Errorf("oracle: resolve parent Go caches: %w", err)
			return
		}
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		if len(lines) != 2 || strings.TrimSpace(lines[0]) == "" || strings.TrimSpace(lines[1]) == "" {
			goCachesErr = fmt.Errorf("oracle: go env returned %d lines for GOCACHE and GOMODCACHE", len(lines))
			return
		}
		goCachesVal = goCaches{cache: strings.TrimSpace(lines[0]), modCache: strings.TrimSpace(lines[1])}
	})
	return goCachesVal, goCachesErr
}
