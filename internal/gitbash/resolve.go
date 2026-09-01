// Package gitbash locates a Bash executable that can address native Windows
// files. On Windows that means Git Bash, not the similarly named WSL launcher.
package gitbash

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

var errNotFound = errors.New("git bash is not installed")

// Resolve returns a Bash executable suitable for running repository POSIX
// scripts. Windows installations are probed directly because Git for Windows
// normally adds Git\cmd, not Git\bin, to PATH and Windows also ships a WSL
// launcher named bash.exe.
func Resolve() (string, error) {
	return resolve(runtime.GOOS, os.Getenv, executableFile, exec.LookPath)
}

func resolve(goos string, getenv func(string) string, isFile func(string) bool, lookPath func(string) (string, error)) (string, error) {
	if goos != "windows" {
		path, err := lookPath("bash")
		if err != nil {
			return "", fmt.Errorf("%w on PATH: %v", errNotFound, err)
		}
		return path, nil
	}

	candidates := windowsInstallCandidates(getenv)
	if gitPath, err := lookPath("git"); err == nil {
		gitDir := filepath.Dir(gitPath)
		switch {
		case strings.EqualFold(filepath.Base(gitDir), "cmd"):
			candidates = append(candidates, filepath.Join(filepath.Dir(gitDir), "bin", "bash.exe"))
		case strings.EqualFold(filepath.Base(gitDir), "bin"):
			candidates = append(candidates, filepath.Join(gitDir, "bash.exe"))
		}
	}

	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		key := strings.ToLower(filepath.Clean(candidate))
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		if isFile(candidate) {
			return candidate, nil
		}
	}

	if path, err := lookPath("bash"); err == nil && !isWSLLauncher(path, getenv) {
		return path, nil
	}
	return "", fmt.Errorf("%w; install Git for Windows or add its bin directory to PATH", errNotFound)
}

func windowsInstallCandidates(getenv func(string) string) []string {
	var candidates []string
	if root := getenv("LOCALAPPDATA"); root != "" {
		candidates = append(candidates, filepath.Join(root, "Programs", "Git", "bin", "bash.exe"))
	}
	for _, name := range []string{"ProgramFiles", "ProgramFiles(x86)"} {
		if root := getenv(name); root != "" {
			candidates = append(candidates, filepath.Join(root, "Git", "bin", "bash.exe"))
		}
	}
	return candidates
}

func executableFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func isWSLLauncher(path string, getenv func(string) string) bool {
	clean := filepath.Clean(path)
	for _, name := range []string{"SystemRoot", "WINDIR"} {
		if root := getenv(name); root != "" && strings.EqualFold(clean, filepath.Join(root, "System32", "bash.exe")) {
			return true
		}
	}
	return strings.HasSuffix(strings.ToLower(filepath.ToSlash(clean)), "/windows/system32/bash.exe")
}
