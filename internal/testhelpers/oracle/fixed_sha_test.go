package oracle

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// Independent fixed recipes: SHA-1 and SHA-256 of the empty byte sequence.
// Keep the deliberately different case spellings in the expected output.
const (
	fixedSHA40 = "da39a3ee5e6b4b0d3255bfef95601890afd80709"
	fixedSHA64 = "E3B0C44298FC1C149AFBF4C8996FB92427AE41E4649B934CA495991B7852B855"
)

func TestFixedSHAPreservation(t *testing.T) {
	n := NewNormaliser()
	for _, sha := range []string{fixedSHA40, fixedSHA64, fixedSHA40, fixedSHA64} {
		if err := n.KeepSHA(sha); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct{ name, input, want string }{
		{"repeated", fixedSHA40 + " " + fixedSHA64 + " " + fixedSHA40, fixedSHA40 + " " + fixedSHA64 + " " + fixedSHA40},
		{"one-digit", "da39a3ee5e6b4b0d3255bfef95601890afd80708 E3B0C44298FC1C149AFBF4C8996FB92427AE41E4649B934CA495991B7852B854", "<ID> <ID>"},
		{"case", strings.ToUpper(fixedSHA40) + " " + strings.ToLower(fixedSHA64), "<ID> <ID>"},
		{"other-ids", "0123456789abcdef0123456789abcdef01234567 0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef 123e4567-e89b-12d3-a456-426614174000", "<ID> <ID> <ID>"},
		{"time", "2026-10-23T18:40:00+02:00 2026-10-23 18:40:00.5 -0500", "<TIME> <TIME>"},
		{"boundaries", "(" + fixedSHA40 + "). " + fixedSHA64 + "/file", "(" + fixedSHA40 + "). " + fixedSHA64 + "/file"},
		{"longer-run", fixedSHA40 + "0", fixedSHA40 + "0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := n.Text(tc.input); got != tc.want {
				t.Fatalf("Text = %q, want %q", got, tc.want)
			}
			if got := n.Text(tc.want); got != tc.want {
				t.Fatalf("normalisation is not idempotent: %q", got)
			}
		})
	}
	t.Run("invalid-registration", func(t *testing.T) {
		for i, invalid := range []string{
			"", strings.Repeat("a", 39), strings.Repeat("a", 41),
			strings.Repeat("a", 63), strings.Repeat("a", 65),
			" " + fixedSHA40, fixedSHA40 + "\n", fixedSHA64 + " ",
			"g" + fixedSHA40[1:], fixedSHA64[:63] + "z",
			strings.Repeat("a", 38) + "é",
		} {
			err := n.KeepSHA(invalid)
			if err == nil || err.Error() != "oracle: SHA must be exactly 40 or 64 ASCII hex characters" {
				t.Fatalf("invalid registration %d must return the value-free validation error", i)
			}
			// Invalid calls must not damage prior registrations or retain new IDs.
			if got := n.Text(fixedSHA40 + " " + fixedSHA64 + " " + strings.Repeat("a", 40)); got != fixedSHA40+" "+fixedSHA64+" <ID>" {
				t.Fatalf("invalid registration %d changed the allowlist: %q", i, got)
			}
		}
	})
	t.Run("precedence", func(t *testing.T) {
		var n Normaliser
		if err := n.KeepSHA(fixedSHA40); err != nil {
			t.Fatal(err)
		}
		n.Dir("/case/project", TokenProject)
		n.Literal(fixedSHA40, "<commit:recipe>")
		n.Literal("volatile", "123e4567-e89b-12d3-a456-426614174000")
		if got := n.Text("/case/project/child " + fixedSHA40 + " volatile 2026-10-23T18:40:00Z"); got != "<PROJECT>/child <commit:recipe> <ID> <TIME>" {
			t.Fatalf("explicit literal/directory/time/UUID ordering changed: %q", got)
		}
	})
	t.Run("independent-concurrent-readers", func(t *testing.T) {
		first, second := NewNormaliser(), NewNormaliser()
		if err := first.KeepSHA(fixedSHA40); err != nil {
			t.Fatal(err)
		}
		if err := second.KeepSHA(fixedSHA64); err != nil {
			t.Fatal(err)
		}
		input := fixedSHA40 + " " + fixedSHA64
		if got := NewNormaliser().Text(input); got != "<ID> <ID>" {
			t.Fatalf("registration leaked into a new instance: %q", got)
		}
		start := make(chan struct{})
		var readers sync.WaitGroup
		for range 8 {
			for _, tc := range []struct {
				n    *Normaliser
				want string
			}{{first, fixedSHA40 + " <ID>"}, {second, "<ID> " + fixedSHA64}} {
				readers.Go(func() {
					<-start
					for range 20 {
						if got := tc.n.Text(input); got != tc.want {
							t.Errorf("independent read = %q, want %q", got, tc.want)
						}
					}
				})
			}
		}
		close(start)
		readers.Wait()
	})
}

// These fixtures are already A9-preprocessed: lease and identity tokens must
// survive, while unregistered raw IDs and remaining timestamps still normalise.
func fixedSHASurfaces() []struct {
	kind                 Kind
	input, before, after string
	want                 string
} {
	return []struct {
		kind                 Kind
		input, before, after string
		want                 string
	}{
		{
			kind:   State,
			before: "commit: " + fixedSHA40 + "  \r\nlease: <T1+1800s>\ngeneration: <generation:1>\nat: 2026-10-23T18:40:00Z\n",
			after:  "commit: " + fixedSHA40 + "\ndigest: " + fixedSHA64 + "\nrequest: 0123456789abcdef0123456789abcdef01234567\n",
			want:   "# before\ncommit: " + fixedSHA40 + "\nlease: <T1+1800s>\ngeneration: <generation:1>\nat: <TIME>\n# after\ncommit: " + fixedSHA40 + "\ndigest: " + fixedSHA64 + "\nrequest: <ID>\n",
		},
		{
			kind:  JSON,
			input: `{"z":[{"sha":"` + fixedSHA64 + `","repeat":"` + fixedSHA40 + `"}],"n":1.0,"lease":"<T1+1800s>","generation":"<generation:1>","commit":"` + fixedSHA40 + `","code":"not_found","at":"2026-10-23T18:40:00+02:00","id":"123e4567-e89b-12d3-a456-426614174000"}`,
			want:  "{\n  \"at\": \"<TIME>\",\n  \"code\": \"not_found\",\n  \"commit\": \"" + fixedSHA40 + "\",\n  \"generation\": \"<generation:1>\",\n  \"id\": \"<ID>\",\n  \"lease\": \"<T1+1800s>\",\n  \"n\": 1.0,\n  \"z\": [\n    {\n      \"repeat\": \"" + fixedSHA40 + "\",\n      \"sha\": \"" + fixedSHA64 + "\"\n    }\n  ]\n}\n",
		},
		{
			kind:  Human,
			input: "\x1b[1mCOMMIT\x1b[0m    LEASE\n\n" + fixedSHA40 + "\t<T1+1800s>\n" + fixedSHA64 + "  <generation:1>\n" + fixedSHA40 + "  0123456789abcdef0123456789abcdef01234567  2026-10-23T18:40:00Z\n",
			want:  "COMMIT LEASE\n" + fixedSHA40 + " <T1+1800s>\n" + fixedSHA64 + " <generation:1>\n" + fixedSHA40 + " <ID> <TIME>\n",
		},
	}
}

func TestFixedSHAKinds(t *testing.T) {
	n := NewNormaliser()
	for _, sha := range []string{fixedSHA40, fixedSHA64} {
		if err := n.KeepSHA(sha); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range fixedSHASurfaces() {
		t.Run(string(tc.kind), func(t *testing.T) {
			got := n.StateForm([]byte(tc.before), []byte(tc.after))
			if tc.kind != State {
				var err error
				got, err = n.Form(tc.kind, []byte(tc.input))
				if err != nil {
					t.Fatal(err)
				}
			}
			if string(got) != tc.want {
				t.Fatalf("%s bytes = %q, want %q", tc.kind, got, tc.want)
			}
		})
	}
}

func TestFixedSHAGoldenComparison(t *testing.T) {
	t.Chdir(t.TempDir())
	n := NewNormaliser()
	for _, sha := range []string{fixedSHA40, fixedSHA64} {
		if err := n.KeepSHA(sha); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range fixedSHASurfaces() {
		t.Run(string(tc.kind), func(t *testing.T) {
			check := func(tb testing.TB, normaliser *Normaliser, name, input, before, after string) {
				tb.Helper()
				if tc.kind == State {
					if normaliser == nil {
						CheckState(tb, name, []byte(before), []byte(after))
					} else {
						normaliser.CheckState(tb, name, []byte(before), []byte(after))
					}
				} else if normaliser == nil {
					Check(tb, tc.kind, name, []byte(input))
				} else {
					normaliser.Check(tb, tc.kind, name, []byte(input))
				}
			}
			t.Setenv(WriteEnv, "1")
			check(t, n, "fixed", tc.input, tc.before, tc.after)
			path := filepath.Join("testdata", "golden", string(tc.kind), "fixed")
			assertBytes := func(path, want string) {
				t.Helper()
				got, err := os.ReadFile(path)
				if err != nil || string(got) != want {
					t.Fatalf("golden bytes = %q, want %q, error %v", got, want, err)
				}
			}
			assertBytes(path, tc.want)
			t.Setenv(WriteEnv, "")
			check(t, n, "fixed", tc.input, tc.before, tc.after)
			for _, sha := range []string{fixedSHA40, fixedSHA64} {
				t.Run(fmt.Sprintf("changed-%d", len(sha)), func(t *testing.T) {
					changed := "0" + sha[1:]
					mutate := func(s string) string { return strings.ReplaceAll(s, sha, changed) }
					msg := expectFailure(t, func(tb testing.TB) {
						check(tb, n, "fixed", mutate(tc.input), mutate(tc.before), mutate(tc.after))
					})
					for _, want := range []string{"fixed", "kind " + string(tc.kind), "differs at line", sha, "<ID>"} {
						if !strings.Contains(msg, want) {
							t.Errorf("expected SHA mismatch with %q, got %q", want, msg)
						}
					}
					assertBytes(path, tc.want)
				})
			}
			// Both package helpers and unconfigured instances retain D-3 defaults.
			defaultWant := strings.NewReplacer(fixedSHA40, "<ID>", fixedSHA64, "<ID>").Replace(tc.want)
			t.Setenv(WriteEnv, "1")
			check(t, nil, "default", tc.input, tc.before, tc.after)
			assertBytes(filepath.Join("testdata", "golden", string(tc.kind), "default"), defaultWant)
			t.Setenv(WriteEnv, "")
			check(t, NewNormaliser(), "default", tc.input, tc.before, tc.after)
			check(t, nil, "default", tc.input, tc.before, tc.after)
		})
	}
}

func TestProviderCorrectionA9Start(t *testing.T) {
	runCorrectionAtStart(t, "^(TestFixedSHAPreservation|TestFixedSHAKinds|TestFixedSHAGoldenComparison)$")
}
