package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// AC1 fallback: the harness normalizer erases fixture timestamps and IDs.
// Keep this comparator local; it implements the same golden tree/write protocol.
type replacement struct{ path, token string }
type selective struct{ paths []replacement }

func (n *selective) dir(path, token string) {
	paths := []string{path}
	if real, err := filepath.EvalSymlinks(path); err == nil {
		paths = append(paths, real)
	}
	for _, p := range paths {
		for _, form := range []string{p, filepath.ToSlash(p), strings.ReplaceAll(p, "/", "\\")} {
			n.paths = append(n.paths, replacement{form, token})
		}
	}
	sort.SliceStable(n.paths, func(i, j int) bool { return len(n.paths[i].path) > len(n.paths[j].path) })
}

func (n selective) text(s string) string {
	for _, p := range n.paths {
		re := regexp.MustCompile(regexp.QuoteMeta(p.path) + `($|[\\/\s"'` + "`" + `:),}])`)
		s = re.ReplaceAllStringFunc(s, func(match string) string { return p.token + match[len(p.path):] })
	}
	// Only tails belonging to a registered path get separator normalization.
	re := regexp.MustCompile(`<(PROJECT|HOME|TMP)>([\\/][^\s"'<>` + "`" + `]*|)`)
	return re.ReplaceAllStringFunc(s, func(match string) string { return strings.ReplaceAll(match, "\\", "/") })
}

var ansi = regexp.MustCompile("\x1b\\[[0-?]*[ -/]*[@-~]")
var analyzed = regexp.MustCompile(`(?m)^\*\*Analyzed:\*\* [^\r\n]+`)
var pointer = regexp.MustCompile(`^0x[0-9a-fA-F]+$`)

func layout(s string) string {
	var lines []string
	for _, line := range strings.Split(ansi.ReplaceAllString(s, ""), "\n") {
		if words := strings.Fields(line); len(words) > 0 {
			lines = append(lines, strings.Join(words, " "))
		}
	}
	return strings.Join(lines, "\n") + "\n"
}

// fmt's value report is an unlabelled struct. Parse its ten top-level fields,
// retaining every delimiter and byte outside the three allocated pointer slots.
func usagePointers(s string) (string, error) {
	trimmed := strings.TrimSpace(s)
	if !strings.HasPrefix(trimmed, "{") || !strings.HasSuffix(trimmed, "}") {
		return "", fmt.Errorf("unexpected usage-value report shape")
	}
	start := strings.Index(s, "{")
	end := start + len(trimmed) - 1
	type span struct{ start, end int }
	var fields []span
	depth, begin := 0, -1
	for i := start + 1; i < end; i++ {
		c := s[i]
		if depth == 0 && (c == ' ' || c == '\t' || c == '\n' || c == '\r') {
			if begin >= 0 {
				fields = append(fields, span{begin, i})
				begin = -1
			}
			continue
		}
		if begin < 0 {
			begin = i
		}
		switch c {
		case '{', '[':
			depth++
		case '}', ']':
			depth--
		}
		if depth < 0 {
			return "", fmt.Errorf("unexpected usage-value report shape")
		}
	}
	if begin >= 0 {
		fields = append(fields, span{begin, end})
	}
	if depth != 0 || len(fields) != 10 {
		return "", fmt.Errorf("unexpected usage-value report shape")
	}
	for i, f := range fields {
		value := s[f.start:f.end]
		switch i {
		case 0:
			if !strings.HasPrefix(value, "{") || !strings.HasSuffix(value, "}") {
				return "", fmt.Errorf("unexpected usage-value window shape")
			}
		case 1, 2, 4, 8, 9:
			if !strings.HasPrefix(value, "[") || !strings.HasSuffix(value, "]") {
				return "", fmt.Errorf("unexpected usage-value sequence shape")
			}
		case 3, 5, 6:
			if value != "<nil>" && !pointer.MatchString(value) && value != pointerMarker(i) {
				return "", fmt.Errorf("unexpected usage-value pointer shape")
			}
		case 7:
			if value != "<nil>" {
				return "", fmt.Errorf("unexpected usage-value baseline shape")
			}
		}
	}
	for _, i := range []int{6, 5, 3} {
		f := fields[i]
		if pointer.MatchString(s[f.start:f.end]) {
			s = s[:f.start] + pointerMarker(i) + s[f.end:]
		}
	}
	return s, nil
}

func pointerMarker(i int) string {
	return map[int]string{3: "<PerCompletedTask:pointer>", 5: "<Provenance:pointer>", 6: "<SuppressedCalls:pointer>"}[i]
}

func (n selective) normalize(path string, got observation) ([]byte, error) {
	got.Stdout, got.Stderr = n.text(got.Stdout), n.text(got.Stderr)
	if strings.HasPrefix(path, "json/") {
		value, err := decodeJSON(got.Stdout)
		if err != nil {
			return nil, err
		}
		value = normalizeStrings(value, n)
		return canonicalJSON(map[string]any{"exit_code": got.ExitCode, "stdout": value, "stderr": got.Stderr})
	}
	if path == "human/analysis-report.txt" {
		got.Stdout = analyzed.ReplaceAllString(got.Stdout, "**Analyzed:** <TIME>")
	}
	if path == "human/usage-value.txt" {
		var err error
		got.Stderr, err = usagePointers(got.Stderr)
		if err != nil {
			return nil, err
		}
	}
	if strings.HasPrefix(path, "state/") {
		return []byte(fmt.Sprintf("exit_code: %d\n# before\n%s\n# after\n%s", got.ExitCode, strings.TrimRight(got.Stdout, "\r\n"), strings.TrimRight(got.Stderr, "\r\n")+"\n")), nil
	}
	return []byte(fmt.Sprintf("exit_code: %d\n# stdout\n%s# stderr\n%s", got.ExitCode, layout(got.Stdout), layout(got.Stderr))), nil
}

func normalizeStrings(value any, n selective) any {
	switch v := value.(type) {
	case string:
		return n.text(v)
	case []any:
		for i := range v {
			v[i] = normalizeStrings(v[i], n)
		}
	case map[string]any:
		for k := range v {
			v[k] = normalizeStrings(v[k], n)
		}
	}
	return value
}

func decodeJSON(s string) (any, error) {
	d := json.NewDecoder(strings.NewReader(s))
	d.UseNumber()
	var value any
	if err := d.Decode(&value); err != nil {
		return nil, err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("expected exactly one JSON value")
	}
	return value, nil
}

func canonicalJSON(value any) ([]byte, error) {
	b, err := json.MarshalIndent(value, "", "  ")
	return append(b, '\n'), err
}

func compareGolden(path string, want, got []byte) error {
	if !bytes.Equal(want, got) {
		return fmt.Errorf("golden mismatch: testdata/golden/%s", path)
	}
	return nil
}

func (p project) check(t *testing.T, path string, got observation) {
	t.Helper()
	data, err := p.norm.normalize(path, got)
	if err != nil {
		t.Fatalf("testdata/golden/%s: %v", path, err)
	}
	file := filepath.Join("testdata", "golden", filepath.FromSlash(path))
	if os.Getenv("ORACLE_WRITE") == "1" {
		if err := os.MkdirAll(filepath.Dir(file), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, data, 0644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("testdata/golden/%s: missing/unreadable golden", path)
	}
	if err := compareGolden(path, want, data); err != nil {
		t.Error(err)
	}
}

func TestOracleCLIComparison(t *testing.T) {
	t.Parallel()
	n := selective{}
	root := t.TempDir()
	n.dir(root, "<TMP>")
	n.dir(filepath.Join(root, "nested"), "<PROJECT>")
	if got := n.text(filepath.Join(root, "nested", "file")); got != "<PROJECT>/file" {
		t.Fatalf("longest registered path: %s", got)
	}
	if got := n.text(root + "-fixture"); got != root+"-fixture" {
		t.Fatal("replaced unregistered prefix")
	}
	if got := n.text(strings.ReplaceAll(root, "/", "\\") + "\\file"); got != "<TMP>/file" {
		t.Fatalf("separator normalization: %s", got)
	}
	fixed := "2026-10-01T12:30:00.123+02:00 2026-10-01T10:30:00Z 123e4567-e89b-12d3-a456-426614174000 abcdef0123456789abcdef0123456789abcdef0123 0x123456789abcdef0123456789abcdef0123456789"
	if n.text(fixed) != fixed {
		t.Fatal("fixture values erased")
	}
	human := observation{0, fixed + "\nrow one\nrow two\n", "err\n"}
	base, err := n.normalize("human/synthetic.txt", human)
	if err != nil {
		t.Fatal(err)
	}
	for _, variant := range []observation{{1, human.Stdout, human.Stderr}, {0, human.Stderr, human.Stdout}, {0, strings.Replace(human.Stdout, "row one\nrow two", "row two\nrow one", 1), human.Stderr}, {0, strings.Replace(human.Stdout, "+02:00", "+03:00", 1), human.Stderr}, {0, strings.Replace(human.Stdout, "abcdef0", "abcdef1", 1), human.Stderr}} {
		got, err := n.normalize("human/synthetic.txt", variant)
		if err != nil || compareGolden("human/synthetic.txt", base, got) == nil {
			t.Fatal("lost human content/channel/status/order sensitivity")
		}
	}
	spaced := human
	spaced.Stdout = "\x1b[31m  " + strings.ReplaceAll(human.Stdout, " ", "  \t") + "\x1b[0m"
	got, err := n.normalize("human/synthetic.txt", spaced)
	if err != nil || !bytes.Equal(base, got) {
		t.Fatal("layout-only change did not compare equal")
	}
	for _, path := range []string{"json/synthetic.json", "state/synthetic.txt"} {
		a := observation{0, `{"b":[9007199254740993,2],"a":"` + fixed + `"}`, ""}
		if strings.HasPrefix(path, "state/") {
			a.Stdout = fixed + "\n"
			a.Stderr = a.Stdout
		}
		b, err := n.normalize(path, a)
		if err != nil {
			t.Fatal(err)
		}
		for _, token := range strings.Fields(fixed) {
			if !bytes.Contains(b, []byte(token)) {
				t.Fatalf("lost fixture token in %s", path)
			}
		}
		changed := a
		changed.Stdout = strings.Replace(a.Stdout, "123", "124", 1)
		c, err := n.normalize(path, changed)
		if err != nil || bytes.Equal(b, c) {
			t.Fatalf("lost identifier sensitivity in %s", path)
		}
	}
	j := observation{0, `{"b":[9007199254740993,2],"a":1}`, ""}
	b, _ := n.normalize("json/synthetic.json", j)
	for _, s := range []string{`{"a":1,"b":[2,9007199254740993]}`, `{"a":1,"b":[9007199254740992,2]}`} {
		j.Stdout = s
		c, _ := n.normalize("json/synthetic.json", j)
		if bytes.Equal(b, c) {
			t.Fatal("JSON number/array order erased")
		}
	}
	j.Stdout = ` { "a":1, "b":[9007199254740993,2] } `
	c, _ := n.normalize("json/synthetic.json", j)
	if !bytes.Equal(b, c) {
		t.Fatal("JSON key/layout canonicalization")
	}
	value := "{{2026-10-01 00:00:00 +0000 UTC} [{merged coder 1}] [{merged 1}] 0x111 [] 0x222 0x333 <nil> [] [fixture 0xabcdef0123456789]}\n"
	obs := observation{0, "", value}
	b, err = n.normalize("human/usage-value.txt", obs)
	if err != nil {
		t.Fatal(err)
	}
	other := obs
	other.Stderr = strings.NewReplacer("0x111", "0xaaa", "0x222", "0xbbb", "0x333", "0xccc").Replace(value)
	c, err = n.normalize("human/usage-value.txt", other)
	if err != nil || !bytes.Equal(b, c) {
		t.Fatal("pointer-only variation did not compare equal")
	}
	for _, pair := range [][2]string{{"0x111", "<nil>"}, {"2026-10-01", "2026-10-02"}, {"merged coder 1", "coder merged 1"}, {"coder 1", "coder 2"}, {"0xabcdef0123456789", "0xabcdef0123456788"}} {
		other.Stderr = strings.Replace(value, pair[0], pair[1], 1)
		c, err = n.normalize("human/usage-value.txt", other)
		if err != nil || bytes.Equal(b, c) {
			t.Fatal("usage-value asserted token erased")
		}
	}
	for _, bad := range []string{"not a report", strings.Replace(value, " [] 0x222", " 0x222 []", 1), strings.Replace(value, " <nil> []", " <nil>", 1)} {
		_, err := n.normalize("human/usage-value.txt", observation{Stderr: bad})
		if err == nil {
			t.Fatal("unexpected usage-value shape accepted")
		}
	}
	pointers, err := usagePointers(value)
	if err != nil {
		t.Fatal(err)
	}
	again, err := usagePointers(pointers)
	if err != nil || pointers != again {
		t.Fatal("pointer normalization not idempotent")
	}
	if n.text(n.text(root+"/file")) != n.text(root+"/file") || layout(layout(fixed)) != layout(fixed) {
		t.Fatal("normalization not idempotent")
	}
	otherSurface, _ := n.normalize("human/other.txt", obs)
	if !bytes.Contains(otherSurface, []byte("0x111")) {
		t.Fatal("pointer normalization escaped allocated surface")
	}
	stdoutPointer, _ := n.normalize("human/usage-value.txt", observation{Stdout: "0x111", Stderr: value})
	if !bytes.Contains(stdoutPointer, []byte("# stdout\n0x111")) {
		t.Fatal("stdout pointer erased")
	}
	a := observation{0, "**Analyzed:** 2026-10-01T12:00:00+02:00\n" + fixed, ""}
	b, _ = n.normalize("human/analysis-report.txt", a)
	a.Stdout = strings.Replace(a.Stdout, "12:00:00+02:00", "14:00:00Z", 1)
	c, _ = n.normalize("human/analysis-report.txt", a)
	if !bytes.Equal(b, c) || !bytes.Contains(b, []byte(fixed)) {
		t.Fatal("analysis stamp normalization altered fixed values")
	}
	if err := compareGolden("human/synthetic.txt", []byte("a"), []byte("b")); err == nil || !strings.Contains(err.Error(), "human/synthetic.txt") {
		t.Fatal("comparison failure must name golden")
	}
}
