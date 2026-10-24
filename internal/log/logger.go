package log

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/liza-mas/liza/internal/filelock"
	"gopkg.in/yaml.v3"
)

const (
	defaultTailReadBytes int64 = 64 * 1024
	maxTailReadBytes     int64 = 256 * 1024
	logFileMode                = 0644
	// tailWindowGrowthFactor doubles the tail read window after each miss.
	tailWindowGrowthFactor = 2
)

// Entry represents a single log entry
type Entry struct {
	Timestamp time.Time `yaml:"timestamp"`
	Agent     string    `yaml:"agent"`
	Action    string    `yaml:"action"`
	Task      *string   `yaml:"task,omitempty"`
	Detail    string    `yaml:"detail"`
}

// Validate checks if the entry has required fields
func (e *Entry) Validate() error {
	if e.Agent == "" {
		return fmt.Errorf("agent is required")
	}
	if e.Action == "" {
		return fmt.Errorf("action is required")
	}
	return nil
}

// Logger provides append operations to log.yaml guarded by a file lock.
type Logger struct {
	logPath  string
	fileLock *filelock.FileLock
}

// New creates a new Logger for the given log file path
func New(logPath string) *Logger {
	return &Logger{
		logPath:  logPath,
		fileLock: filelock.New(logPath),
	}
}

// WithLockTimeout returns a new Logger with a custom lock timeout
func (l *Logger) WithLockTimeout(timeout time.Duration) *Logger {
	return &Logger{
		logPath:  l.logPath,
		fileLock: filelock.New(l.logPath).WithTimeout(timeout),
	}
}

// Append adds a log entry to the log file.
// The timestamp is automatically set to the current UTC time.
func (l *Logger) Append(entry Entry) error {
	if err := entry.Validate(); err != nil {
		return fmt.Errorf("invalid entry: %w", err)
	}

	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now().UTC()
	}

	sequenceItem, err := yaml.Marshal([]Entry{entry})
	if err != nil {
		return fmt.Errorf("failed to marshal log entry: %w", err)
	}

	return l.fileLock.WithLockOperation("append", func() error {
		file, err := os.OpenFile(l.logPath, os.O_CREATE|os.O_RDWR|os.O_APPEND, logFileMode)
		if err != nil {
			return fmt.Errorf("failed to open log file for append: %w", err)
		}
		defer file.Close()

		info, err := file.Stat()
		if err != nil {
			return fmt.Errorf("failed to stat log file: %w", err)
		}

		if info.Size() > 0 {
			lastByte := make([]byte, 1)
			if _, err := file.ReadAt(lastByte, info.Size()-1); err != nil {
				return fmt.Errorf("failed to read log file tail: %w", err)
			}
			if lastByte[0] != '\n' {
				if _, err := file.Write([]byte("\n")); err != nil {
					return fmt.Errorf("failed to append separator newline: %w", err)
				}
			}
		}

		if _, err := file.Write(sequenceItem); err != nil {
			return fmt.Errorf("failed to append log entry: %w", err)
		}

		return nil
	})
}

// Read reads all entries from the log file.
// This is a convenience method for testing and debugging.
func (l *Logger) Read() ([]Entry, error) {
	var entries []Entry

	data, err := os.ReadFile(l.logPath)
	if err != nil {
		if os.IsNotExist(err) {
			return []Entry{}, nil
		}
		return nil, fmt.Errorf("failed to read log file: %w", err)
	}

	if len(data) == 0 {
		return []Entry{}, nil
	}

	if err := yaml.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("failed to parse log file: %w", err)
	}

	return entries, nil
}

// GetLastTimestamp returns the timestamp of the most recent log entry.
// Returns zero time if the log file doesn't exist or is empty.
func (l *Logger) GetLastTimestamp() (time.Time, error) {
	file, err := os.Open(l.logPath)
	if err != nil {
		if os.IsNotExist(err) {
			return time.Time{}, nil
		}
		return time.Time{}, fmt.Errorf("failed to open log file: %w", err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to stat log file: %w", err)
	}
	if info.Size() == 0 {
		return time.Time{}, nil
	}

	size := info.Size()
	window := min(defaultTailReadBytes, size)

	for {
		start := size - window
		buf := make([]byte, window)
		n, readErr := file.ReadAt(buf, start)
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return time.Time{}, fmt.Errorf("failed to read log file tail: %w", readErr)
		}
		buf = buf[:n]

		entryData := extractLastEntryYAML(buf, start == 0)
		if len(entryData) > 0 {
			var entries []Entry
			if err := yaml.Unmarshal(entryData, &entries); err == nil && len(entries) > 0 {
				return entries[len(entries)-1].Timestamp, nil
			}
		}
		if start == 0 {
			var entries []Entry
			if err := yaml.Unmarshal(bytes.TrimSpace(buf), &entries); err == nil {
				if len(entries) == 0 {
					return time.Time{}, nil
				}
				return entries[len(entries)-1].Timestamp, nil
			}
		}

		if start == 0 || window >= maxTailReadBytes || window >= size {
			break
		}
		window = min(window*tailWindowGrowthFactor, maxTailReadBytes, size)
	}

	return time.Time{}, fmt.Errorf("failed to parse last log entry from bounded tail window")
}

func extractLastEntryYAML(data []byte, atFileStart bool) []byte {
	if len(data) == 0 {
		return nil
	}

	if idx := bytes.LastIndex(data, []byte("\n- ")); idx >= 0 {
		return bytes.TrimSpace(data[idx+1:])
	}

	if atFileStart {
		trimmed := bytes.TrimLeft(data, " \t\r\n")
		if bytes.HasPrefix(trimmed, []byte("- ")) {
			return bytes.TrimSpace(trimmed)
		}
	}

	return nil
}
