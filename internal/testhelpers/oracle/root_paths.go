package oracle

import "strings"

// ReplaceRoot replaces complete occurrences of the supplied literal root with
// token. Callers resolve the root's spelling before calling. An empty root is
// a no-op. Name bytes (letters, digits, dot, hyphen and underscore) on either
// side prevent a match; slash and backslash suffixes are retained verbatim.
// No other bytes are normalised, including invalid UTF-8 and final newlines.
func ReplaceRoot(text, root, token string) string {
	if root == "" {
		return text
	}
	var out strings.Builder
	written := 0
	for search := 0; search < len(text); {
		i := strings.Index(text[search:], root)
		if i < 0 {
			break
		}
		i += search
		end := i + len(root)
		if (i > 0 && isNameByte(text[i-1])) || (end < len(text) && isNameByte(text[end])) {
			search = i + 1
			continue
		}
		out.WriteString(text[written:i])
		out.WriteString(token)
		written, search = end, end
	}
	out.WriteString(text[written:])
	return out.String()
}
