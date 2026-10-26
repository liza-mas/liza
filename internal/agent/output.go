package agent

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// Permissions of the files and directories the runtime writes under the project:
// agent output, streamed output and checkpoint summaries.
const (
	runtimeDirMode  os.FileMode = 0o755
	runtimeFileMode os.FileMode = 0o644
)

// saveTimestampedFile writes content to dir/<agentID>-<timestamp>.<ext> and returns the path.
func saveTimestampedFile(dir, agentID, ext, content string) (string, error) {
	if err := os.MkdirAll(dir, runtimeDirMode); err != nil {
		return "", fmt.Errorf("create directory %s: %w", dir, err)
	}

	timestamp := time.Now().UTC().Format("20060102-150405")
	filePath := filepath.Join(dir, fmt.Sprintf("%s-%s.%s", agentID, timestamp, ext))

	if err := os.WriteFile(filePath, []byte(content), runtimeFileMode); err != nil {
		return "", fmt.Errorf("write file %s: %w", filePath, err)
	}

	return filePath, nil
}

func saveOutput(outputsDir, agentID, ext, output string, masker *SecretMasker) (string, error) {
	masked := output
	if masker != nil {
		masked = masker.MaskText(output)
	}
	return saveTimestampedFile(outputsDir, agentID, ext, masked)
}

type flushingWriter interface {
	io.Writer
	Flush() error
}

type passthroughFlushWriter struct {
	io.Writer
}

func (w passthroughFlushWriter) Flush() error {
	return nil
}

type streamingOutputFile struct {
	dir       string
	agentID   string
	ext       string
	timestamp string
	masker    *SecretMasker

	file   *os.File
	writer flushingWriter
	err    error
	failed bool
}

func newStreamingOutputFile(dir, agentID, ext, timestamp string, masker *SecretMasker) *streamingOutputFile {
	return &streamingOutputFile{
		dir:       dir,
		agentID:   agentID,
		ext:       ext,
		timestamp: timestamp,
		masker:    masker,
	}
}

func (w *streamingOutputFile) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if w.failed {
		return len(p), nil
	}
	if err := w.ensureOpen(); err != nil {
		w.recordError(err)
		return len(p), nil
	}
	if _, err := w.writer.Write(p); err != nil {
		w.recordError(err)
	}
	return len(p), nil
}

func (w *streamingOutputFile) Close() error {
	if w.writer != nil {
		w.recordError(w.writer.Flush())
	}
	if w.file != nil {
		w.recordError(w.file.Close())
	}
	return w.err
}

func (w *streamingOutputFile) ensureOpen() error {
	if w.writer != nil {
		return nil
	}
	if err := os.MkdirAll(w.dir, runtimeDirMode); err != nil {
		return fmt.Errorf("create directory %s: %w", w.dir, err)
	}

	filePath := filepath.Join(w.dir, fmt.Sprintf("%s-%s.%s", w.agentID, w.timestamp, w.ext))
	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, runtimeFileMode)
	if err != nil {
		return fmt.Errorf("open output file %s: %w", filePath, err)
	}

	w.file = file
	if w.masker != nil {
		w.writer = w.masker.NewStreamingWriter(file)
	} else {
		w.writer = passthroughFlushWriter{Writer: file}
	}
	return nil
}

func (w *streamingOutputFile) recordError(err error) {
	if err != nil && w.err == nil {
		w.err = err
	}
	if err != nil {
		w.failed = true
	}
}
