package agent

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// tailReadSize is the maximum bytes to read from the end of an output file.
// Quota messages appear near the end; reading the full file is wasteful.
const tailReadSize = 8 * 1024

// latestAgentOutputContent reads tails from stdout/stderr files belonging to
// the same most-recent agent output timestamp. Provider startup failures may be
// emitted on stderr before a structured stdout stream exists.
func latestAgentOutputContent(outputsDir, agentID string) string {
	base := latestOutputBase(outputsDir, agentID)
	if base == "" {
		return ""
	}

	parts := []string{
		outputContent(base + ".txt"),
		outputContent(base + ".err"),
	}
	return strings.Join(parts, "\n")
}

func latestOutputBase(outputsDir, agentID string) string {
	matches := make([]string, 0, 2)
	for _, ext := range []string{".txt", ".err"} {
		pattern := filepath.Join(outputsDir, agentID+"-*"+ext)
		extMatches, err := filepath.Glob(pattern)
		if err != nil {
			continue
		}
		matches = append(matches, extMatches...)
	}
	if len(matches) == 0 {
		return ""
	}
	sort.Strings(matches)
	latest := matches[len(matches)-1]
	return strings.TrimSuffix(latest, filepath.Ext(latest))
}

// latestOutputContent reads the tail of the most recent agent output file with
// the requested extension. Returns empty string if no file is found or read fails.
func latestOutputContent(outputsDir, agentID, ext string) string {
	pattern := filepath.Join(outputsDir, agentID+"-*"+ext)
	matches, err := filepath.Glob(pattern)
	if err != nil || len(matches) == 0 {
		return ""
	}
	// Glob returns sorted by name; timestamp format ensures lexicographic = chronological.
	latest := matches[len(matches)-1]
	return outputContent(latest)
}

func outputContent(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return ""
	}

	size := info.Size()
	readSize := int64(tailReadSize)
	if size < readSize {
		readSize = size
	}
	buf := make([]byte, readSize)
	if _, err := f.ReadAt(buf, size-readSize); err != nil {
		return ""
	}
	return string(buf)
}
