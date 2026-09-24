package toolresult

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const DefaultThresholdBytes = 32 * 1024
const DefaultDigestBytes = 4 * 1024
const MaxReadBytes = 64 * 1024

type Config struct {
	ThresholdBytes int
	DigestBytes    int
}

type Result struct {
	Tool         string `json:"tool"`
	Command      string `json:"command"`
	Role         string `json:"role,omitempty"`
	CommandClass string `json:"command_class,omitempty"`
	TaskID       string `json:"task_id,omitempty"`
	AgentID      string `json:"agent_id,omitempty"`
	SessionID    string `json:"session_id,omitempty"`
	ExitCode     *int   `json:"exit_code"`
	Truncated    bool   `json:"truncated"`
	Content      string `json:"content"`
	// ContentJSON marks Content as one encoded JSON value, so redaction runs
	// on its decoded strings instead of on the encoding.
	ContentJSON bool `json:"content_json,omitempty"`
}

// Store retains sanitized evidence, independently of the task blackboard.
// Calls can run concurrently, including in separate CLI processes.
type Store struct {
	path     string
	config   Config
	sanitize func(string) string
}

type Digest struct {
	Tool            string `json:"tool"`
	Command         string `json:"command"`
	ExitCode        *int   `json:"exit_code"`
	OriginalBytes   int    `json:"original_bytes"`
	StoredBytes     int    `json:"stored_bytes"`
	Truncated       bool   `json:"truncated"`
	SourceTruncated bool   `json:"source_truncated"`
	Duplicate       bool   `json:"duplicate"`
	ArtifactID      string `json:"artifact_id"`
	ArtifactPath    string `json:"artifact_path"`
	ContentHash     string `json:"content_hash"`
	Excerpt         string `json:"excerpt,omitempty"`
}

// Event joins to provider usage by task/agent/session; it is not a token estimate
// or proof of task completion. Each invocation gets its own durable record.
type Event struct {
	ID                      string    `json:"id"`
	Timestamp               time.Time `json:"timestamp"`
	Tool                    string    `json:"tool"`
	Role                    string    `json:"role,omitempty"`
	CommandClass            string    `json:"command_class,omitempty"`
	TaskID                  string    `json:"task_id,omitempty"`
	AgentID                 string    `json:"agent_id,omitempty"`
	SessionID               string    `json:"session_id,omitempty"`
	ExitCode                *int      `json:"exit_code"`
	SourceTruncated         bool      `json:"source_truncated"`
	OriginalBytes           int       `json:"original_bytes"`
	ContextBytes            int       `json:"context_bytes"`
	StoredBytes             int       `json:"stored_bytes"`
	Externalized            bool      `json:"externalized"`
	Duplicate               bool      `json:"duplicate"`
	DuplicateBytesPrevented int       `json:"duplicate_bytes_prevented"`
	ContentHash             string    `json:"content_hash,omitempty"`
}

type Stats struct {
	Count                   int   `json:"count"`
	P95ResultBytes          int   `json:"p95_result_bytes"`
	DuplicateBytesPrevented int64 `json:"duplicate_bytes_prevented"`
	ContextBytes            int64 `json:"context_bytes"`
	OriginalBytes           int64 `json:"original_bytes"`
	ExternalizedCount       int   `json:"externalized_count"`
	DuplicateCount          int   `json:"duplicate_count"`
}

func New(path string, config Config, secrets []string) (*Store, error) {
	if config.ThresholdBytes == 0 {
		config.ThresholdBytes = DefaultThresholdBytes
	}
	if config.DigestBytes == 0 {
		config.DigestBytes = DefaultDigestBytes
	}
	if config.ThresholdBytes < 1 || config.DigestBytes < 1024 || config.DigestBytes > config.ThresholdBytes {
		return nil, errors.New("tool-result requires threshold >= digest >= 1024 bytes")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, errors.New("invalid tool-result root")
	}
	sanitize := newSanitizer(secrets)
	if sanitize(abs) != abs {
		return nil, errors.New("tool-result root contains sensitive text")
	}
	if err := os.MkdirAll(abs, 0700); err != nil {
		return nil, errors.New("cannot create tool-result root")
	}
	info, err := os.Lstat(abs)
	if err != nil || !info.IsDir() {
		return nil, errors.New("tool-result root must be a real directory")
	}
	root, err := os.OpenRoot(abs)
	if err != nil {
		return nil, errors.New("cannot open tool-result root")
	}
	defer root.Close()
	if err := root.MkdirAll("events", 0700); err != nil {
		return nil, errors.New("cannot create tool-result events directory")
	}
	return &Store{path: abs, config: config, sanitize: sanitize}, nil
}

func (s *Store) Process(result Result) (string, error) { return s.process(result, false) }

// ProcessForced persists a digest even below the usual threshold when an upstream
// transport imposes a stricter output limit.
func (s *Store) ProcessForced(result Result) (string, error) { return s.process(result, true) }

// Sanitize applies the store redaction policy before transport-specific truncation.
func (s *Store) Sanitize(value string) string { return s.sanitize(value) }

func (s *Store) sanitizeContent(content string, isJSON bool) string {
	if isJSON {
		return sanitizeJSON(s.sanitize, content)
	}
	return s.sanitize(content)
}

// ThresholdBytes is the largest live result preview the store can safely return
// without externalizing it.
func (s *Store) ThresholdBytes() int { return s.config.ThresholdBytes }

func (s *Store) process(result Result, force bool) (string, error) {
	originalBytes := len(result.Content)
	content := s.sanitizeContent(result.Content, result.ContentJSON)
	result.Tool, result.Command = s.sanitize(result.Tool), s.sanitize(result.Command)
	if result.Tool == "" {
		return "", errors.New("tool-result tool identity is required")
	}
	root, err := os.OpenRoot(s.path)
	if err != nil {
		return "", errors.New("cannot open tool-result store")
	}
	defer root.Close()
	event := Event{ID: randomID(), Timestamp: time.Now().UTC(), Tool: result.Tool,
		Role: s.sanitize(result.Role), CommandClass: s.sanitize(result.CommandClass),
		TaskID: s.sanitize(result.TaskID), AgentID: s.sanitize(result.AgentID), SessionID: s.sanitize(result.SessionID),
		ExitCode: result.ExitCode, SourceTruncated: result.Truncated, OriginalBytes: originalBytes}
	output := content
	if force || originalBytes > s.config.ThresholdBytes || len(content) > s.config.ThresholdBytes {
		// Normalize line endings only; preserve whitespace and substantive evidence.
		content = strings.ReplaceAll(content, "\r\n", "\n")
		hash := fmt.Sprintf("%x", sha256.Sum256([]byte(content)))
		event.ContentHash, event.Externalized, event.StoredBytes = hash, true, len(content)
		digest := Digest{Tool: result.Tool, Command: result.Command, ExitCode: result.ExitCode,
			OriginalBytes: originalBytes, StoredBytes: len(content), Truncated: true,
			SourceTruncated: result.Truncated, ArtifactID: hash, ContentHash: hash,
			ArtifactPath: filepath.Join(s.path, hash+".txt")}
		// Ensure metadata fits before creating any artifact. Never silently lose identity.
		if _, err := boundedDigest(digest, "", s.config.DigestBytes); err != nil {
			return "", err
		}
		duplicate, err := persistOnce(root, hash+".txt", []byte(content))
		if err != nil {
			return "", errors.New("cannot persist sanitized tool-result artifact")
		}
		event.Duplicate, digest.Duplicate = duplicate, duplicate
		excerpt := content
		if duplicate {
			excerpt = ""
		}
		output, err = boundedDigest(digest, excerpt, s.config.DigestBytes)
		if err != nil {
			return "", err
		}
		if duplicate {
			event.DuplicateBytesPrevented = max(0, len(content)-len(output))
		}
	}
	event.ContextBytes = len(output)
	data, err := json.Marshal(event)
	if err != nil {
		return "", errors.New("cannot encode tool-result event")
	}
	if _, err = persistOnce(root, "events/"+event.ID+".json", data); err != nil {
		return "", errors.New("cannot persist tool-result telemetry")
	}
	return output, nil
}

func boundedDigest(digest Digest, content string, budget int) (string, error) {
	if !fitDigestCommand(&digest, budget) {
		return "", errors.New("tool-result identity metadata exceeds digest budget; increase digest and threshold")
	}
	lo, hi := 0, min(len(content), budget)
	for lo < hi {
		mid := (lo + hi + 1) / 2
		digest.Excerpt = validPrefix(content, mid)
		data, _ := json.Marshal(digest)
		if len(data)+1 <= budget {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	digest.Excerpt = validPrefix(content, lo)
	data, _ := json.Marshal(digest)
	return string(data) + "\n", nil
}

// fitDigestCommand preserves the result boundary when an otherwise valid command
// would consume the entire digest budget (for example, a heredoc script). The
// complete command remains with the caller; the model-facing digest retains a
// UTF-8-safe prefix and an explicit truncation marker.
func fitDigestCommand(digest *Digest, budget int) bool {
	encoded, _ := json.Marshal(*digest)
	if len(encoded)+1 <= budget {
		return true
	}
	if digest.Command == "" {
		return false
	}
	original := digest.Command
	lo, hi := 0, len(original)
	for lo < hi {
		mid := (lo + hi + 1) / 2
		digest.Command = validPrefix(original, mid) + "…"
		encoded, _ = json.Marshal(*digest)
		if len(encoded)+1 <= budget {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	digest.Command = validPrefix(original, lo) + "…"
	encoded, _ = json.Marshal(*digest)
	return len(encoded)+1 <= budget
}

func validPrefix(text string, n int) string {
	if n > len(text) {
		n = len(text)
	}
	for n > 0 && n < len(text) && !utf8.RuneStart(text[n]) {
		n--
	}
	return strings.ToValidUTF8(text[:n], "�")
}

func randomID() string    { return hex.EncodeToString(randomBytes()) }
func randomBytes() []byte { var b [16]byte; _, _ = rand.Read(b[:]); return b[:] }

// Link publishes a fully written, synced file without overwriting a competing
// writer. Existing evidence must match exactly before it can be referenced.
func persistOnce(root *os.Root, name string, data []byte) (bool, error) {
	tmp := ".pending-" + randomID()
	f, err := root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return false, err
	}
	defer root.Remove(tmp)
	_, writeErr := f.Write(data)
	if writeErr == nil {
		writeErr = f.Sync()
	}
	closeErr := f.Close()
	if writeErr != nil {
		return false, writeErr
	}
	if closeErr != nil {
		return false, closeErr
	}
	if err := root.Link(tmp, name); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return false, err
		}
		info, err := root.Lstat(name)
		if err != nil || !info.Mode().IsRegular() {
			return false, errors.New("artifact is not a regular file")
		}
		existing, err := root.ReadFile(name)
		if err != nil {
			return false, err
		}
		if string(existing) != string(data) {
			return false, errors.New("artifact integrity mismatch")
		}
		return true, nil
	}
	// Persist the published directory entry as well as the contents on platforms
	// with directory fsync. Windows does not support syncing directory handles.
	if runtime.GOOS != "windows" {
		dir, err := root.Open(filepath.Dir(name))
		if err != nil {
			return false, err
		}
		err = dir.Sync()
		closeErr := dir.Close()
		if err != nil {
			return false, err
		}
		if closeErr != nil {
			return false, closeErr
		}
	}
	return false, nil
}

var contentHashPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

func (s *Store) Read(hash string, offset, limit int64) (string, error) {
	if !contentHashPattern.MatchString(hash) || offset < 0 || limit < 1 || limit > MaxReadBytes {
		return "", errors.New("read requires a SHA-256 hash, nonnegative offset and limit 1..65536")
	}
	root, err := os.OpenRoot(s.path)
	if err != nil {
		return "", errors.New("cannot open tool-result store")
	}
	defer root.Close()
	info, err := root.Lstat(hash + ".txt")
	if err != nil || !info.Mode().IsRegular() {
		return "", errors.New("artifact missing or not a regular file")
	}
	f, err := root.Open(hash + ".txt")
	if err != nil {
		return "", errors.New("cannot open tool-result artifact")
	}
	defer f.Close()
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return "", errors.New("invalid artifact offset")
	}
	b, err := io.ReadAll(io.LimitReader(f, limit))
	if err != nil {
		return "", errors.New("cannot read tool-result artifact")
	}
	return string(b), nil
}

func (s *Store) Stats() (Stats, error) {
	var stats Stats
	root, err := os.OpenRoot(s.path)
	if err != nil {
		return stats, errors.New("cannot open tool-result store")
	}
	defer root.Close()
	dir, err := root.Open("events")
	if err != nil {
		return stats, errors.New("cannot open tool-result events")
	}
	defer dir.Close()
	entries, err := dir.ReadDir(-1)
	if err != nil {
		return stats, errors.New("cannot list tool-result events")
	}
	sizes := make([]int, 0, len(entries))
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		if !entry.Type().IsRegular() {
			return stats, errors.New("invalid tool-result event file")
		}
		data, err := root.ReadFile("events/" + entry.Name())
		if err != nil {
			return stats, errors.New("cannot read tool-result event")
		}
		var event Event
		if err := json.Unmarshal(data, &event); err != nil {
			return stats, errors.New("invalid tool-result event")
		}
		stats.Count++
		stats.ContextBytes += int64(event.ContextBytes)
		stats.OriginalBytes += int64(event.OriginalBytes)
		stats.DuplicateBytesPrevented += int64(event.DuplicateBytesPrevented)
		if event.Externalized {
			stats.ExternalizedCount++
		}
		if event.Duplicate {
			stats.DuplicateCount++
		}
		sizes = append(sizes, event.OriginalBytes)
	}
	sort.Ints(sizes)
	if len(sizes) > 0 {
		stats.P95ResultBytes = sizes[(95*len(sizes)+99)/100-1]
	}
	return stats, nil
}
