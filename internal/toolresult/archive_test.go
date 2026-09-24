package toolresult

import (
	"archive/zip"
	"bufio"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// Optional real-run regression. Nothing from the archive is logged or copied
// into the repository; the production sanitizer runs before temporary storage.
func TestRealRunArchiveResults(t *testing.T) {
	path := os.Getenv("TOOL_RESULT_EVIDENCE_ARCHIVE")
	if path == "" {
		t.Skip("set TOOL_RESULT_EVIDENCE_ARCHIVE to the user-provided run archive")
	}
	z, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal("cannot open evidence archive")
	}
	defer z.Close()
	var count int
	s := testStore(t)
	for _, file := range z.File {
		if !strings.HasSuffix(file.Name, "/coder-1-20260710-025708.txt") && !strings.HasSuffix(file.Name, "/code-reviewer-3-20260710-012121.txt") {
			continue
		}
		r, err := file.Open()
		if err != nil {
			t.Fatal("cannot open evidence member")
		}
		scanner := bufio.NewScanner(r)
		scanner.Buffer(make([]byte, 65536), 8*1024*1024)
		for scanner.Scan() {
			var record struct {
				Params struct {
					Update struct {
						SessionUpdate string `json:"sessionUpdate"`
						ToolCallID    string `json:"toolCallId"`
						RawOutput     struct {
							FormattedOutput string `json:"formatted_output"`
							ExitCode        *int   `json:"exit_code"`
						} `json:"rawOutput"`
					} `json:"update"`
				} `json:"params"`
			}
			if json.Unmarshal(scanner.Bytes(), &record) != nil {
				continue
			}
			u := record.Params.Update
			if u.SessionUpdate != "tool_call_update" || len(u.RawOutput.FormattedOutput) < 32768 {
				continue
			}
			result := Result{Tool: "exec", Command: "archived tool result", ExitCode: u.RawOutput.ExitCode, Content: u.RawOutput.FormattedOutput, TaskID: "DEV-778-archive-replay", SessionID: u.ToolCallID}
			one, err := s.Process(result)
			if err != nil {
				t.Fatal(err)
			}
			d := parseDigest(t, one)
			if len(one) > s.config.DigestBytes || d.OriginalBytes != len(result.Content) || d.ExitCode == nil || *d.ExitCode != *result.ExitCode {
				t.Fatal("real output digest metadata mismatch")
			}
			two, err := s.Process(result)
			if err != nil {
				t.Fatal(err)
			}
			if repeated := parseDigest(t, two); !repeated.Duplicate || repeated.Excerpt != "" || repeated.ContentHash != d.ContentHash {
				t.Fatal("real output duplicate reinserted")
			}
			full, err := os.ReadFile(d.ArtifactPath)
			if err != nil {
				t.Fatal(err)
			}
			expected := strings.ReplaceAll(s.sanitize(result.Content), "\r\n", "\n")
			if string(full) != expected {
				t.Fatal("real sanitized evidence was lost")
			}
			part, err := s.Read(d.ContentHash, 0, 4096)
			if err != nil || part != string(full[:min(len(full), 4096)]) {
				t.Fatal("real artifact targeted retrieval failed")
			}
			count++
		}
		if err := scanner.Err(); err != nil {
			t.Fatal("cannot scan evidence member")
		}
		r.Close()
	}
	if count == 0 {
		t.Fatal("no matching real oversized results in archive")
	}
	t.Logf("replayed %d real oversized results: bounded, retrievable, deduplicated", count)
}
