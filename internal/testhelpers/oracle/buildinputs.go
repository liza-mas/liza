package oracle

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
)

// RegisterBuildInputs makes a binary that a test builds from source visible to
// Go's test cache. go test keys a cached result on the test binary and on the
// files and environment variables the test process itself reads; sources read
// by a `go build` subprocess are not among them, so after an edit to them a
// stale "(cached)" result would be replayed. RegisterBuildInputs lists the
// main-module packages that `go build pkgs` compiles from modRoot and opens
// their Go, cgo and embedded files, their directories, plus go.mod and go.sum
// from the test process, which records them as inputs. An opened directory is
// recorded with its listing, so adding or removing a source file invalidates
// the result too; that includes files the build ignores, such as tests, which
// only costs a re-run. env is the go command's environment
// (nil inherits the process's) and must select the same build as the real
// one. It uses go -C rather than a working directory: starting a process in a
// directory stats it, and a directory's mtime would become a volatile input.
// It returns the registered paths.
func RegisterBuildInputs(env []string, modRoot string, pkgs ...string) ([]string, error) {
	args := append([]string{"-C", modRoot, "list", "-deps", "-json=Dir,GoFiles,CgoFiles,EmbedFiles,Module"}, pkgs...)
	cmd := exec.Command("go", args...)
	cmd.Env = env
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return nil, fmt.Errorf("oracle: go list %v: %w\n%s", pkgs, err, boundedTail(string(exitErr.Stderr)))
		}
		return nil, fmt.Errorf("oracle: go list %v: %w", pkgs, err)
	}
	files := []string{filepath.Join(modRoot, "go.mod"), filepath.Join(modRoot, "go.sum")}
	var dirs []string
	seen := map[string]bool{}
	addDir := func(dir string) {
		if !seen[dir] {
			seen[dir] = true
			dirs = append(dirs, dir)
		}
	}
	dec := json.NewDecoder(bytes.NewReader(out))
	for {
		var p struct {
			Dir                           string
			GoFiles, CgoFiles, EmbedFiles []string
			Module                        *struct{ Main bool }
		}
		if err := dec.Decode(&p); err == io.EOF {
			break
		} else if err != nil {
			return nil, fmt.Errorf("oracle: decode go list output: %w", err)
		}
		if p.Module == nil || !p.Module.Main {
			continue // standard library and dependency modules are fixed by go.sum
		}
		addDir(p.Dir)
		for _, names := range [][]string{p.GoFiles, p.CgoFiles, p.EmbedFiles} {
			for _, name := range names {
				files = append(files, filepath.Join(p.Dir, name))
			}
		}
		for _, name := range p.EmbedFiles {
			addDir(filepath.Dir(filepath.Join(p.Dir, name)))
		}
	}
	for _, name := range append(files, dirs...) {
		f, err := os.Open(name)
		if errors.Is(err, fs.ErrNotExist) {
			continue // recorded all the same: a later creation changes the input
		} else if err != nil {
			return nil, fmt.Errorf("oracle: register build input: %w", err)
		}
		f.Close()
	}
	return files, nil
}
