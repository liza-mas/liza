package providersignal

import "testing"

// The leading characters that mark a JSON event line keep their spelling.
func TestA9JSONLinePrefixesKeepSpelling(t *testing.T) {
	if jsonObjectLinePrefix != "{" || jsonArrayLinePrefix != "[" {
		t.Fatalf("prefixes = %q %q, want { [", jsonObjectLinePrefix, jsonArrayLinePrefix)
	}
}
