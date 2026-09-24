// Package toolresult budgets agent-visible tool results without discarding evidence.
package toolresult

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/liza-mas/liza/internal/secretmask"
)

// A credential key name: a credential word, optionally behind a namespace or
// camelCase prefix (`DB_PASSWORD`, `PGPASSWORD`, `clientSecret`) and followed
// only by a key/base/value/string suffix (`SECRET_KEY_BASE`, `secretKey`,
// `SecretString`). A leading `_` is allowed for `.npmrc`'s `_authToken`,
// `_password`, and `_auth`. `pwd` is deliberately
// absent because `PWD=`/`OLDPWD=` are working directories, and counters such
// as `max_tokens` or `tokenCount` never match.
const credentialKeyBody = `(?:_?[a-z0-9]+[_.-]?)*_?(?:(?:api|access|private)[_-]?key|secret|password|passwd|authorization|cookie|token)(?:[_.-]?(?:key|base|value|string))*|_auth`

// In text the key must start at a word boundary so it never matches inside
// another word.
const credentialKeyPattern = `\b(?:` + credentialKeyBody + `)`

var credentialKeyName = regexp.MustCompile(`(?i)^(?:` + credentialKeyBody + `)$`)

// A value following a credential key and `:`, `=`, or `=>`: a quoted literal,
// or a bare run up to whitespace, a quote, a backslash, or a structural
// delimiter. The bare run cannot start with an operator character, so operator
// runs (:=, ==, !=, <=, >=) never produce a match; redactAssignments decides
// whether a bare run is code.
var credentialAssignments = regexp.MustCompile(`(?i)(["']?` + credentialKeyPattern + `["']?[ \t]*(?:=>|[:=])[ \t]*)("(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*'|[^\s=:<>!"'` + "`" + `\\,;{}\[\]()][^\s"'` + "`" + `\\,;{}\[\]()]*)`)
var codeReference = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)+$`)
var plainIdentifier = regexp.MustCompile(`^[A-Za-z_]+$`)

// A Kubernetes/ECS-style environment entry whose credential name and value sit
// on consecutive `name:`/`value:` lines.
var credentialJSONNameValue = regexp.MustCompile(`(?is)("name"\s*:\s*"(?:` + credentialKeyBody + `)"\s*,\s*"value"\s*:\s*)"(?:\\.|[^"\\])+"`)
var credentialNameValue = regexp.MustCompile(`(?im)^([ \t]*-?[ \t]*name:[ \t]*["']?(?:` + credentialKeyBody + `)["']?[ \t]*\r?\n[ \t]*value:[ \t]*)([^\s][^\r\n]*)`)
var plainNumber = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)?$`)
var bearerValue = regexp.MustCompile(`(?i)\b(?:bearer|basic)\s+([^\s,;}]+)`)
var credentialHeader = regexp.MustCompile(`(?im)^(\s*(?:authorization|proxy-authorization|cookie|set-cookie)\s*:\s*)[^\r\n]+`)
var credentialFlag = regexp.MustCompile(`(?i)(--[a-z0-9_-]*(?:api[_-]?key|secret|token|password|passwd|pwd)[a-z0-9_-]*\s+)((?:"(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*')|[^\s]+)`)
var privateKey = regexp.MustCompile(`(?s)-----BEGIN (?:[A-Z0-9]+ )*PRIVATE KEY-----.*?(?:-----END (?:[A-Z0-9]+ )*PRIVATE KEY-----|$)`)
var urlCredentials = regexp.MustCompile(`(?i)([a-z][a-z0-9+.-]*://)[^\s/@:]*:[^\s/@]+@`)
var knownToken = regexp.MustCompile(`\b(?:sk-[a-zA-Z0-9_-]{16,}|AKIA[0-9A-Z]{16}|AIza[0-9A-Za-z_-]{35}|gh[pousr]_[a-zA-Z0-9]{20,}|npm_[A-Za-z0-9]{36}|EAA[A-Za-z0-9]{100,}|eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,})\b`)
var credentialBlock = regexp.MustCompile(`(?i)^[ \t]*["']?` + credentialKeyPattern + `["']?\s*:\s*[|>][0-9+-]*\s*(?:#.*)?$`)

func newSanitizer(secrets []string) func(string) string {
	masker := secretmask.New()
	values := append([]string(nil), secrets...)
	for _, entry := range os.Environ() {
		key, value, ok := strings.Cut(entry, "=")
		// Keep the threshold aligned with secretmask: very short environment
		// values are common words and redacting them corrupts ordinary output.
		if ok && len(value) >= 8 && secretmask.IsSecretKey(key) {
			values = append(values, value)
		}
	}
	// Textual tool results often contain JSON representations rather than raw
	// secret bytes. Mask that representation before any excerpt or persistence.
	for _, value := range append([]string(nil), values...) {
		encoded, _ := json.Marshal(value)
		values = append(values, string(encoded[1:len(encoded)-1]))
	}
	sort.Slice(values, func(i, j int) bool { return len(values[i]) > len(values[j]) })
	return func(text string) string {
		text = masker.MaskText(text)
		for _, value := range values {
			if value != "" {
				text = strings.ReplaceAll(text, value, "[REDACTED]")
			}
		}
		text = privateKey.ReplaceAllString(text, "[REDACTED PRIVATE KEY]")
		text = urlCredentials.ReplaceAllString(text, "${1}[REDACTED]@")
		text = credentialHeader.ReplaceAllString(text, "${1}[REDACTED]")
		text = redactFlags(text)
		text = redactCredentialBlocks(text)
		// Header values must be removed before assignment matching: otherwise
		// `Authorization: Bearer secret` would leave the secret behind.
		text = redactBearerValues(text)
		text = credentialJSONNameValue.ReplaceAllString(text, "${1}\"[REDACTED]\"")
		text = redactNameValues(text)
		text = redactAssignments(text)
		return knownToken.ReplaceAllString(text, "[REDACTED]")
	}
}

// redactBearerValues redacts `bearer|basic` continuations only when the value
// is plausibly a credential: at least twelve characters with real entropy.
// Short or plain words such as "basic auth" or "Bearer token" stay untouched.
func redactBearerValues(text string) string {
	var out strings.Builder
	last := 0
	for _, loc := range bearerValue.FindAllStringIndex(text, -1) {
		start, end := loc[0], loc[1]
		match := text[start:end]
		value := match[strings.LastIndex(match, " ")+1:]
		out.WriteString(text[last:start])
		if len(value) >= 12 && credentialShapedToken(value) {
			out.WriteString("[REDACTED]")
		} else {
			out.WriteString(match)
		}
		last = end
	}
	out.WriteString(text[last:])
	return out.String()
}

// redactAssignments redacts credential-key assignments. Quoted literals always
// redact unless empty. A bare value passes through byte-for-byte when it is a
// call, a number, a boolean or null literal, a `$VAR` reference, or a
// pagination cursor. A tight `key=value` (.env, properties, shell, query
// strings, `env` output) otherwise redacts, except a keyword argument
// (`f(api_key=api_key)`) whose value is code-shaped. Spaced, colon, and `=>`
// forms pass code-shaped values (`ident.ident` member accesses and digit-free
// identifiers such as `userPassword` or `AuthToken`) and redact
// credential-shaped tokens only, so source code, prose, and placeholders
// survive.
func redactAssignments(text string) string {
	var out strings.Builder
	last := 0
	for _, m := range credentialAssignments.FindAllStringSubmatchIndex(text, -1) {
		start, end, valueStart := m[0], m[1], m[4]
		if end < last {
			continue
		}
		out.WriteString(text[last:start])
		prefix := text[start:valueStart]
		if isQuotedLiteral(text[valueStart:end]) == "" {
			end = extendBareValue(text, end, prefix)
			if !isCall(text, end) && end < len(text) && text[end] == '(' {
				// Parentheses inside a bare value are data, not a call: consume
				// them and the following characters so `Pa(ss)w0rd` redacts
				// whole instead of leaking `(ss)w0rd` past the marker.
				if widened := extendParenValue(text, end); widened > end {
					end = widened
				}
			}
		}
		match, value := text[start:end], text[valueStart:end]
		codeShaped := codeReference.MatchString(value) || plainIdentifier.MatchString(value)
		redact := false
		if flagKey(prefix) || paginationKey(prefix) {
			redact = false
		} else if quoted := isQuotedLiteral(value); quoted != "" {
			redact = quoted != "empty"
		} else if !isCall(text, end) && !bareValueIsNotCredential(prefix, value) {
			if tightAssignment(prefix) {
				memberAccess := codeReference.MatchString(value) && !environmentKey(prefix)
				redact = !memberAccess && !(codeShaped && (keywordArgument(text, start, end) || codeAssignment(text, start, prefix)))
			} else {
				redact = !spacedCode(text, end, prefix, value) && credentialShapedToken(value)
			}
		}
		if redact {
			out.WriteString(prefix + "[REDACTED]")
		} else {
			out.WriteString(match)
		}
		last = end
	}
	out.WriteString(text[last:])
	return out.String()
}

// extendBareValue widens a bare value the class cut short. An upper-case
// environment value (`DB_PASSWORD=Pa(ss)w"0rd`) is data up to whitespace, a
// `;` or `&`, or a `,` followed by whitespace or another `KEY=`; any
// other value continues through an interior quote or backtick followed by more
// value characters (`password=Xk9"mQ2`), while a closing quote that ends a
// string literal (`"?token=abc123"`) still stops it. A quote that opened the
// key (`"TOKEN=x"`) always closes the value.
func extendBareValue(text string, end int, prefix string) int {
	var opener byte
	if prefix[0] == '"' || prefix[0] == '\'' {
		opener = prefix[0]
	}
	environment := tightAssignment(prefix) && environmentKey(prefix)
	stop := " \t\r\n,;{}[]()\"'`\\"
	if environment {
		stop = " \t\r\n\"'`\\;&,"
	}
	start := end
	for end < len(text) {
		c := text[end]
		if c == opener || strings.IndexByte(" \t\r\n\\", c) >= 0 {
			break
		}
		switch {
		case c == '"' || c == '\'' || c == '`':
			// Only an interior quote continues the value; a quote before a
			// delimiter or the end closes a surrounding string literal.
			if end+1 >= len(text) || strings.IndexByte(" \t\r\n,;)]}\\\"'`", text[end+1]) >= 0 {
				return trimUnmatchedClosers(text, start, end, environment)
			}
		case c == ';' || c == '&' || c == ',':
			// `;` and `&` separate commands and list entries; a comma ends an
			// environment value when whitespace, the end, or another `KEY=`
			// follows it.
			if !environment || c != ',' || end+1 >= len(text) || strings.IndexByte(" \t\r\n", text[end+1]) >= 0 || nextAssignment.MatchString(text[end+1:]) {
				return trimUnmatchedClosers(text, start, end, environment)
			}
		case !environment:
			return end
		}
		end++
		for end < len(text) && strings.IndexByte(stop, text[end]) < 0 {
			end++
		}
	}
	return trimUnmatchedClosers(text, start, end, environment)
}

var nextAssignment = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)

// trimUnmatchedClosers drops closing brackets an environment value swallowed
// from its surroundings (`(export TOKEN=x)`), keeping balanced ones
// (`Pa(ss)w0rd`).
func trimUnmatchedClosers(text string, valueStart, end int, environment bool) int {
	if !environment {
		return end
	}
	for end > valueStart {
		value := text[valueStart:end]
		last := value[len(value)-1]
		open := map[byte]byte{')': '(', ']': '[', '}': '{'}[last]
		if open == 0 || strings.Count(value, string(open)) >= strings.Count(value, string(last)) {
			break
		}
		end--
	}
	return end
}

// spacedCode reports whether a spaced, colon, or `=>` value is code: a member
// access, or an identifier under a non-environment key that starts lower-case
// (`userPassword`) or is followed by code punctuation (`{"token": AuthToken}`).
// A capitalized word ending the line (`password: CorrectHorse`) or any
// identifier under an environment key (`POSTGRES_PASSWORD: MySecret`) is data.
func spacedCode(text string, end int, prefix, value string) bool {
	if codeReference.MatchString(value) {
		return true
	}
	if !plainIdentifier.MatchString(value) || upperKey.MatchString(assignmentKey(prefix)) {
		return false
	}
	if value[0] >= 'a' && value[0] <= 'z' || value[0] == '_' {
		return true
	}
	after := strings.TrimLeft(text[end:], " \t")
	return after != "" && strings.IndexByte(",;)}", after[0]) >= 0
}

// assignmentKey returns the bare key of an assignment prefix such as
// `  "POSTGRES_PASSWORD": ` or `token => `.
func assignmentKey(prefix string) string {
	return strings.Trim(prefix, " \t\"':=>")
}

// tightAssignment reports a `key=value` prefix with no whitespace around `=`.
func tightAssignment(prefix string) bool {
	return strings.HasSuffix(prefix, "=") && !strings.ContainsAny(prefix, " \t")
}

// keywordArgument reports whether an assignment is a call's keyword argument
// rather than configuration: an unquoted key right after `(`, after `,` inside
// a parenthesis still open on the same line, or alone on its line inside a
// call split one argument per line (`api_key=api_key,` after a line ending in
// `(` or `,`). A quoted key is data, as in `["PATH=/usr/bin","TOKEN=x"]`.
func keywordArgument(text string, start, end int) bool {
	if text[start] == '"' || text[start] == '\'' {
		return false
	}
	before := strings.TrimRight(text[:start], " \t")
	if strings.HasSuffix(before, "(") {
		return true
	}
	if strings.HasSuffix(before, ",") {
		line := before[strings.LastIndexByte(before, '\n')+1:]
		return strings.Count(line, "(") > strings.Count(line, ")")
	}
	if !strings.HasSuffix(before, "\n") && before != "" {
		return false
	}
	after := strings.TrimLeft(text[end:], " \t")
	if strings.HasPrefix(after, "\n") || strings.HasPrefix(after, "\r\n") {
		after = strings.TrimLeft(after, " \t\r\n")
		if !strings.HasPrefix(after, ")") {
			return false
		}
	}
	if after == "" || (after[0] != ',' && after[0] != ')') {
		return false
	}
	previous := strings.TrimRight(before, " \t\r\n")
	return strings.HasSuffix(previous, "(") || strings.HasSuffix(previous, ",")
}

// codeAssignment reports a tight assignment written as code: a `let`, `const`,
// or `var` declaration, or a `this.`/`self.` member.
func codeAssignment(text string, start int, prefix string) bool {
	lower := strings.ToLower(prefix)
	if strings.HasPrefix(lower, "this.") || strings.HasPrefix(lower, "self.") {
		return true
	}
	before := strings.TrimRight(text[:start], " \t")
	for _, keyword := range []string{"let", "const", "var"} {
		if strings.HasSuffix(before, keyword) {
			rest := before[:len(before)-len(keyword)]
			if rest == "" || !isWordByte(rest[len(rest)-1]) {
				return true
			}
		}
	}
	return false
}

// environmentKey reports an upper-case environment-variable key
// (`JWT_SECRET=`), whose value is data even when it looks like `a.b.c`.
func environmentKey(prefix string) bool {
	key := strings.TrimSuffix(strings.TrimLeft(prefix, "\"'"), "=")
	return upperKey.MatchString(key)
}

var upperKey = regexp.MustCompile(`^[A-Z0-9_.-]*[A-Z][A-Z0-9_.-]*$`)

func isWordByte(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// flagKeyPattern matches a boolean-flag name that merely mentions a credential
// word: `hasPassword`, `isToken`, `REQUIRES_SECRET`.
var flagKeyPattern = regexp.MustCompile(`^["']?(?:(?:has|is|requires|show|enable)[A-Z_.-]|(?:HAS|IS|REQUIRES|SHOW|ENABLE)_)`)

func flagKey(key string) bool {
	return flagKeyPattern.MatchString(key)
}

// isCall reports whether a bare value is the callee of a real call: its `(`
// reaches a matching `)` with only whitespace, a delimiter, or the end of the
// line after it. Parentheses inside a value (`password=Pa(ss)w0rd`) are
// therefore data, not a call, and redactAssignments redacts the value whole.
func isCall(text string, end int) bool {
	if end >= len(text) || text[end] != '(' {
		return false
	}
	depth := 0
	for i := end; i < len(text); i++ {
		switch text[i] {
		case '(':
			depth++
		case ')':
			if depth--; depth == 0 {
				rest := strings.TrimLeft(text[i+1:], " \t")
				if rest == "" {
					return true
				}
				return rest[0] == '\n' || rest[0] == '\r' || strings.ContainsRune(",;)}][{", rune(rest[0]))
			}
		case '\n', '\r':
			return false
		}
	}
	return false
}

// extendParenValue extends a bare value that stopped at `(` over the balanced
// parenthesis group and the following value characters, mirroring the value
// class of credentialAssignments. isCall reports false before this runs, so
// the parentheses belong to the value (`Pa(ss)w0rd`), not to a call.
func extendParenValue(text string, from int) int {
	end := from
	depth := 0
	for i := from; i < len(text); i++ {
		c := text[i]
		if depth > 0 {
			switch c {
			case '(':
				depth++
			case ')':
				depth--
				if depth == 0 {
					end = i + 1
				}
			}
			continue
		}
		switch {
		case c == '(':
			depth = 1
		case strings.ContainsRune(",;{}[]() \t\r\n", rune(c)):
			return end
		default:
			end = i + 1
		}
	}
	return end
}

// bareValueIsNotCredential reports whether an unquoted value is a number, a
// boolean or null literal, a variable reference, or a pagination cursor rather
// than a secret.
func bareValueIsNotCredential(prefix, value string) bool {
	switch strings.ToLower(value) {
	case "true", "false", "null", "none", "nil":
		return true
	}
	return plainNumber.MatchString(value) || strings.HasPrefix(value, "$") || paginationKey(prefix)
}

// credentialPairValue redacts the value of a `{"name": "DB_PASSWORD",
// "value": "..."}` entry (Kubernetes, ECS), whose credential name is data.
func credentialPairValue(entry map[string]any) bool {
	for _, field := range [][2]string{{"name", "value"}, {"Name", "Value"}} {
		name, nameOK := entry[field[0]].(string)
		value, valueOK := entry[field[1]].(string)
		if nameOK && valueOK && value != "" && value != "[REDACTED]" && credentialKeyName.MatchString(name) && !paginationKey(name) {
			entry[field[1]] = "[REDACTED]"
			return true
		}
	}
	return false
}

// paginationKey reports whether a key names a pagination cursor, which is an
// opaque position rather than a credential.
func paginationKey(key string) bool {
	normalized := strings.ToLower(strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			return r
		}
		return -1
	}, key))
	// Exact cursor names only: a credential that merely ends in "page token"
	// (`fb_page_token`) is still a credential.
	// An upper-case environment key (`SYNC_TOKEN=`) is configuration, never a
	// cursor.
	if upperKey.MatchString(assignmentKey(key)) && strings.ContainsAny(assignmentKey(key), "_") {
		return false
	}
	switch strings.TrimLeft(normalized, "0123456789") {
	case "pagetoken", "nextpagetoken", "prevpagetoken", "previouspagetoken", "startpagetoken", "newstartpagetoken",
		"nexttoken", "continuationtoken", "nextcontinuationtoken", "nextforwardtoken", "nextbackwardtoken",
		"startingtoken", "resumetoken", "paginationtoken", "synctoken", "nextsynctoken":
		return true
	}
	return false
}

// sanitizeJSON applies the text policy to the decoded string values of a JSON
// document rather than to its encoding: in encoded form every newline is the
// two characters `\n`, so line-anchored rules never fire and a bare value can
// run across what were separate lines. String values under a credential key
// are redacted outright. The document is returned byte-for-byte when nothing
// changes, and falls back to the text policy when it is not valid JSON.
func sanitizeJSON(sanitize func(string) string, raw string) string {
	// Every top-level value is sanitized, so trailing values cannot carry an
	// unsanitized secret past the first one.
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	var documents []any
	for {
		var document any
		err := decoder.Decode(&document)
		if err == io.EOF && len(documents) > 0 {
			break
		}
		if err != nil {
			return sanitize(raw)
		}
		documents = append(documents, document)
	}
	changed := false
	var walk func(any) any
	walk = func(value any) any {
		switch typed := value.(type) {
		case string:
			clean := sanitize(typed)
			changed = changed || clean != typed
			return clean
		case []any:
			for i := range typed {
				typed[i] = walk(typed[i])
			}
		case map[string]any:
			if credentialPairValue(typed) {
				changed = true
			}
			for key, item := range typed {
				if text, ok := item.(string); ok && text != "" && text != "[REDACTED]" && credentialKeyName.MatchString(key) && !paginationKey(key) && !flagKey(key) {
					typed[key], changed = "[REDACTED]", true
					continue
				}
				typed[key] = walk(item)
			}
		}
		return value
	}
	for i := range documents {
		documents[i] = walk(documents[i])
	}
	if !changed {
		return raw
	}
	var encoded bytes.Buffer
	encoder := json.NewEncoder(&encoded)
	encoder.SetEscapeHTML(false)
	for _, document := range documents {
		if encoder.Encode(document) != nil {
			return sanitize(raw)
		}
	}
	return strings.TrimSuffix(encoded.String(), "\n")
}

// redactFlags redacts secret-named command-line flags the same way
// redactAssignments does: quoted values always redact (unless empty), bare
// values only when credential-shaped. Telemetry flags such as `--token-count 3`
// or `--max-tokens 2048` therefore pass through byte-for-byte.
func redactFlags(text string) string {
	var out strings.Builder
	last := 0
	for _, m := range credentialFlag.FindAllStringSubmatchIndex(text, -1) {
		start, end := m[0], m[1]
		out.WriteString(text[last:start])
		match := text[start:end]
		value := text[m[4]:m[5]]
		keep := match
		if quoted := isQuotedLiteral(value); quoted != "" {
			if quoted != "empty" {
				keep = text[start:m[4]] + "[REDACTED]"
			}
		} else if len(value) >= 6 && credentialShapedToken(value) {
			keep = text[start:m[4]] + "[REDACTED]"
		}
		out.WriteString(keep)
		last = end
	}
	out.WriteString(text[last:])
	return out.String()
}

// isQuotedLiteral reports whether a matched value is a quoted string literal.
// Empty literals ("", ”) carry no secret and are reported as "empty" so the
// surrounding assignment survives byte-for-byte.
func isQuotedLiteral(value string) string {
	if len(value) >= 2 {
		if (value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'') {
			if len(value) == 2 {
				return "empty"
			}
			return "quoted"
		}
	}
	return ""
}

// credentialShapedToken reports whether an unquoted value has enough entropy
// to be a real credential: a letter plus an uppercase letter, a digit, or a
// sign. All-lowercase words and pure numbers (telemetry, placeholders) are
// preserved.
func credentialShapedToken(value string) bool {
	if len(value) < 6 {
		return false
	}
	hasLetter, hasEntropy := false, false
	for i := 0; i < len(value); i++ {
		switch c := value[i]; {
		case c >= 'a' && c <= 'z':
			hasLetter = true
		case c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
			hasLetter = true
			hasEntropy = true
		default:
			hasEntropy = true
		}
	}
	return hasLetter && hasEntropy
}

// redactNameValues redacts the value line of a credential `name:`/`value:`
// entry, including the indented body of a `|` or `>` block scalar.
func redactNameValues(text string) string {
	var out strings.Builder
	last := 0
	for _, m := range credentialNameValue.FindAllStringSubmatchIndex(text, -1) {
		valueStart, end := m[4], m[5]
		out.WriteString(text[last:valueStart])
		out.WriteString("[REDACTED]")
		last = end
		if value := text[valueStart:end]; value[0] == '|' || value[0] == '>' {
			lineStart := strings.LastIndexByte(text[:valueStart], '\n') + 1
			indent := len(text[lineStart:]) - len(strings.TrimLeft(text[lineStart:], " \t"))
			for last < len(text) && text[last] == '\n' {
				next := text[last+1:]
				lineEnd := strings.IndexByte(next, '\n')
				if lineEnd < 0 {
					lineEnd = len(next)
				}
				line := next[:lineEnd]
				if strings.TrimSpace(line) != "" && len(line)-len(strings.TrimLeft(line, " \t")) <= indent {
					break
				}
				last += 1 + lineEnd
			}
		}
	}
	out.WriteString(text[last:])
	return out.String()
}

func redactCredentialBlocks(text string) string {
	lines := strings.SplitAfter(text, "\n")
	var out strings.Builder
	for i := 0; i < len(lines); i++ {
		line := strings.TrimRight(lines[i], "\r\n")
		if !credentialBlock.MatchString(line) {
			out.WriteString(lines[i])
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " \t"))
		colon := strings.Index(line, ":")
		out.WriteString(line[:colon+1] + " [REDACTED]")
		if strings.HasSuffix(lines[i], "\n") {
			out.WriteByte('\n')
		}
		for i+1 < len(lines) {
			next := lines[i+1]
			nextIndent := len(next) - len(strings.TrimLeft(next, " \t"))
			if strings.TrimSpace(next) != "" && nextIndent <= indent {
				break
			}
			i++
		}
	}
	return out.String()
}
