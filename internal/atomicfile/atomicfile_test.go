package atomicfile

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestCopyReplacesDestinationAndLeavesNoTemporaryFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.json")
	dst := filepath.Join(dir, "dst.json")
	if err := os.WriteFile(src, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("old content"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Copy(src, dst); err != nil {
		t.Fatalf("Copy() error = %v", err)
	}
	if got, err := os.ReadFile(dst); err != nil || string(got) != "new" {
		t.Fatalf("dst = %q, %v; want %q", got, err, "new")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("directory entries = %v, want only src and dst", entries)
	}
}

func TestWriteCreatesDirectoryReplacesDestinationAndLeavesNoTemporaryFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested")
	dst := filepath.Join(dir, "dst.json")

	if err := Write(dst, []byte("first")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if err := Write(dst, []byte("second")); err != nil {
		t.Fatalf("Write() replace error = %v", err)
	}

	if got, err := os.ReadFile(dst); err != nil || string(got) != "second" {
		t.Fatalf("dst = %q, %v; want %q", got, err, "second")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("directory entries = %v, want only dst", entries)
	}
}

func TestCopyMissingSourceKeepsDestination(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "dst.json")
	if err := os.WriteFile(dst, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := Copy(filepath.Join(dir, "missing.json"), dst)
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Copy() error = %v, want not-exist", err)
	}
	if got, readErr := os.ReadFile(dst); readErr != nil || string(got) != "old" {
		t.Fatalf("dst = %q, %v; want unchanged", got, readErr)
	}
}
