package toolresult

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSanitizeEscapedKnownSecretsAndYAMLBlocks(t *testing.T) {
	secret := "alpha\"beta\\gamma"
	t.Setenv("SERVICE_TOKEN", secret)
	encoded, _ := json.Marshal(map[string]string{"message": secret})
	for _, input := range []string{string(encoded), "password: |\n  fake-multiline-secret\nnext: ok", "token: >-\n  fake-multiline-secret\n  second line\nnext: ok"} {
		out := newSanitizer([]string{secret})(input)
		if strings.Contains(out, "alpha") || strings.Contains(out, "fake-multiline-secret") || strings.Contains(out, "second line") {
			t.Fatalf("escaped or multiline secret leaked")
		}
		if strings.Contains(input, "next: ok") && !strings.Contains(out, "next: ok") {
			t.Fatal("unrelated YAML field lost")
		}
	}
}

func TestSanitizeFailureOutput(t *testing.T) {
	t.Setenv("SERVICE_API_KEY", "environment-fake-credential")
	sanitize := newSanitizer([]string{"explicit-fake-credential"})
	input := strings.Repeat("FAILED tests/test_checkout.py::test_credentials\n", 15000) + `client={'api_key': 'fake sensitive value', "password":"line 1\nline 2"}; Authorization: Bearer not-a-real-token
url=https://alice:fake-url-password@example.com
environment-fake-credential explicit-fake-credential
-----BEGIN PRIVATE KEY-----
fake private material
-----END PRIVATE KEY-----`
	out := sanitize(input)
	for _, value := range []string{"fake sensitive value", "line 1", "not-a-real-token", "fake-url-password", "environment-fake-credential", "explicit-fake-credential", "fake private material"} {
		if strings.Contains(out, value) {
			t.Errorf("credential not redacted: %q", value)
		}
	}
	if !strings.Contains(out, "FAILED tests/test_checkout.py::test_credentials") {
		t.Fatal("failure identity lost")
	}
}

func TestSanitizeLeavesOrdinaryReadsUnchanged(t *testing.T) {
	input := "cat src/main.go\nsed -n '1,40p' src/main.go\nnl -ba README.md\ncommit 01234567890123456789012345678901\n"
	if got := newSanitizer(nil)(input); got != input {
		t.Fatalf("ordinary read changed: %q", got)
	}
}

func TestSanitizeLeavesTelemetryIdentifiersUnchanged(t *testing.T) {
	input := `{"max_tokens": 200, "tokenCount": 3, "input_tokens": 1}`
	if got := newSanitizer(nil)(input); got != input {
		t.Fatalf("telemetry identifiers changed: %q", got)
	}
}

func TestSanitizeDoesNotMaskShortEnvironmentValues(t *testing.T) {
	t.Setenv("SERVICE_TOKEN", "code")
	input := "status=code"
	if got := newSanitizer(nil)(input); got != input {
		t.Fatalf("short environment value changed: %q", got)
	}
}

func TestSanitizeMasksRecognizedTokenCredential(t *testing.T) {
	for _, input := range []string{
		`{"access_token":"not-a-real-token"}`,
		`{"github_token":"not-a-real-token"}`,
	} {
		out := newSanitizer(nil)(input)
		if strings.Contains(out, "not-a-real-token") {
			t.Fatalf("recognized token credential retained: %q", out)
		}
	}
}

func TestSanitizeHeadersAndCommandFlags(t *testing.T) {
	input := "Cookie: session=not-real-cookie; csrf=not-real-csrf\nAuthorization: Custom not-real-auth\npytest --password 'not real pass' --api-key not-real-key"
	out := newSanitizer(nil)(input)
	for _, value := range []string{"not-real-cookie", "not-real-csrf", "not-real-auth", "not real pass", "not-real-key"} {
		if strings.Contains(out, value) {
			t.Errorf("sensitive header/argument retained: %s", value)
		}
	}
}

// TestSanitizePreservesOrdinarySourceByteForByte pins the round-2 regressions:
// operator runs and function calls are not assignments, and placeholder-shaped
// values are not credentials. Every input must survive the sanitizer unchanged.
func TestSanitizePreservesOrdinarySourceByteForByte(t *testing.T) {
	for _, input := range []string{
		`token := os.Getenv("GH_TOKEN")`,
		`token = os.Getenv("GH_TOKEN")`,
		`if token == "" {`,
		`password : something`,
		`password: something`,
		`max_token=4096`,
		`pwd: ~`,
		`pwd: /home/user`,
		`export PWD=/home/user/project`,
		`OLDPWD=/home/user/old`,
		`token = cfg.Token`,
		`api_key = settings.api_key`,
		`page_token=next-page`,
		`next_page_token: CiAKGjBpNDd2Nmp`,
		`DB_PASSWORD=$DB_PASSWORD`,
		`token = generateToken(user)`,
		`token=getToken()`,
		`api_key = loadApiKey(cfg)`,
		`secret = computeSecret(salt, pw)`,
		`token = create_access_token(data)`,
		`USE_TOKEN=true`,
		`session_token=None`,
		`map[string]any{"token": AuthToken}`,
		`Client(api_key=api_key, token=token)`,
		`requests.post(url, auth_token=auth_token)`,
		`token = newToken`,
		`opts.Token = accessToken`,
		`const password = userPassword;`,
		`password = user_password`,
		`token_type=bearer`,
		`tokenCount=3`,
		`token = ""`,
		`token = ''`,
		`Bearer token`,
		`basic auth`,
		`--token-count 3`,
	} {
		if got := newSanitizer(nil)(input); got != input {
			t.Errorf("ordinary source was changed: %q -> %q", input, got)
		}
	}
}

// TestSanitizeStillMasksRealCredentials guards the positive side of the
// same rules: genuine credential shapes must still be redacted after the
// corruption fixes.
func TestSanitizeStillMasksRealCredentials(t *testing.T) {
	for _, input := range []string{
		`access_token="not-a-real-token"`,
		`API_KEY=sk-notarealapikey123456`,
		`Authorization: Bearer not-a-real-bearer-token`,
		`password: Hunter2`,
		`github_token="not-a-real-token"`,
	} {
		out := newSanitizer(nil)(input)
		for _, secret := range []string{"not-a-real-token", "sk-notarealapikey123456", "not-a-real-bearer-token", "Hunter2"} {
			if strings.Contains(out, secret) {
				t.Errorf("real credential retained in %q -> %q", input, out)
			}
		}
	}
}

// TestSanitizeMasksPasswordsWithSymbols pins the round-3 false negatives:
// .env-style values with symbols or all-lowercase passphrases must redact, and
// the redaction keeps the source's own spacing after the operator.
func TestSanitizeMasksPasswordsWithSymbols(t *testing.T) {
	for input, want := range map[string]string{
		`DB_PASSWORD=P@ssw0rd`:                  `DB_PASSWORD=[REDACTED]`,
		`password=Hunter2!`:                     `password=[REDACTED]`,
		`PASSWORD=s3cr3t$x9`:                    `PASSWORD=[REDACTED]`,
		`api_key: Xy9#kLm2pQ`:                   `api_key: [REDACTED]`,
		`token=abc%2Fdef123`:                    `token=[REDACTED]`,
		`DB_PASSWORD=correcthorsebatterystaple`: `DB_PASSWORD=[REDACTED]`,
		`access_token="not-a-real-token"`:       `access_token=[REDACTED]`,
	} {
		if got := newSanitizer(nil)(input); got != want {
			t.Errorf("%q -> %q, want %q", input, got, want)
		}
	}
}

// TestSanitizeMasksCredentialKeyVariants pins the camelCase, run-together,
// suffixed, `=>`, and JWT forms found in the round-5 review.
func TestSanitizeMasksCredentialKeyVariants(t *testing.T) {
	jwt := "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJ1c2VyMTIzNDUifQ.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c"
	for input, secret := range map[string]string{
		`clientSecret=live_abcd1234`:          "live_abcd1234",
		`{"clientSecret":"live_abcd1234"}`:    "live_abcd1234",
		`PGPASSWORD=Sup3rS3cret`:              "Sup3rS3cret",
		`'password' => 'Sup3rSecret!'`:        "Sup3rSecret!",
		`:password => "Sup3rSecret!"`:         "Sup3rSecret!",
		`SECRET_KEY_BASE=abc123def456`:        "abc123def456",
		`SECRET_KEY=django-insecure-abc`:      "django-insecure-abc",
		`JWT_SECRET=my.jwt.secret`:            "my.jwt.secret",
		`ACCESS_TOKEN=` + jwt:                 jwt,
		`token: ` + jwt:                       jwt,
		`API_TOKEN=abc123` + "\nDEBUG=true\n": "abc123",
	} {
		out := newSanitizer(nil)(input)
		if strings.Contains(out, secret) {
			t.Errorf("credential retained: %q -> %q", input, out)
		}
	}
	if got := newSanitizer(nil)(`{"env":"API_TOKEN=abc123\nDEBUG=true"}`); got != `{"env":"API_TOKEN=[REDACTED]\nDEBUG=true"}` {
		t.Errorf("value ran past an escaped newline or quote: %q", got)
	}
}

// TestSanitizeJSONRedactsDecodedStrings pins the round-5 blocker: JSON results
// are sanitized on their decoded strings, so line-anchored rules fire, the
// document stays valid, and lines are neither merged nor dropped.
func TestSanitizeJSONRedactsDecodedStrings(t *testing.T) {
	sanitize := newSanitizer(nil)
	for content, want := range map[string]string{
		"# db settings\npassword: Hunter2Secret99 # prod\n":         "# db settings\npassword: [REDACTED] # prod\n",
		"API_TOKEN=abc123\nDEBUG=true\nPORT=8080\nLOG_LEVEL=info\n": "API_TOKEN=[REDACTED]\nDEBUG=true\nPORT=8080\nLOG_LEVEL=info\n",
		"API_TOKEN=abc123\nDEBUG=true\n# the end of file\n":         "API_TOKEN=[REDACTED]\nDEBUG=true\n# the end of file\n",
		"    password = userPassword\n    return password\n":        "    password = userPassword\n    return password\n",
		"Authorization: Custom not-real-auth\nnext\n":               "Authorization: [REDACTED]\nnext\n",
	} {
		raw, _ := json.Marshal(map[string]any{"type": "text", "file": map[string]any{"content": content, "numLines": 2}})
		out := sanitizeJSON(sanitize, string(raw))
		var decoded struct {
			File struct {
				Content string `json:"content"`
			} `json:"file"`
		}
		if err := json.Unmarshal([]byte(out), &decoded); err != nil {
			t.Fatalf("sanitized JSON is invalid: %v: %s", err, out)
		}
		if decoded.File.Content != want {
			t.Errorf("%q -> %q, want %q", content, decoded.File.Content, want)
		}
	}
	if got := sanitizeJSON(sanitize, `{"ok":true} "{\"password\":\"Hunter2secret\"}"`); strings.Contains(got, "Hunter2secret") {
		t.Errorf("trailing JSON value escaped sanitization: %q", got)
	}
	raw := `{"b": "plain <text>", "a": [1, 2.50, true]}`
	if got := sanitizeJSON(sanitize, raw); got != raw {
		t.Errorf("unchanged document was rewritten: %q", got)
	}
	if got := sanitizeJSON(sanitize, `{"headers":{"Authorization":"x-custom"},"tokenCount":3,"next_page_token":"abc"}`); !strings.Contains(got, `"Authorization":"[REDACTED]"`) || !strings.Contains(got, `"tokenCount":3`) || !strings.Contains(got, `"next_page_token":"abc"`) {
		t.Errorf("credential keys not handled: %s", got)
	}
}

// TestSanitizeRoundTwoReviewInputs pins the second review round: quoted and
// lowercase tight assignments, camelCase suffix keys, and name/value pairs
// redact, while keyword arguments stay intact.
func TestSanitizeRoundTwoReviewInputs(t *testing.T) {
	sanitize := newSanitizer(nil)
	for input, secret := range map[string]string{
		`      - "POSTGRES_PASSWORD=example"`:                   "example",
		`      - 'DB_PASSWORD=supersecret'`:                     "supersecret",
		`docker run -e "DB_PASSWORD=supersecret" img`:           "supersecret",
		`spring.datasource.password=supersecret`:                "supersecret",
		"protocol=https\nusername=bob\npassword=supersecret":    "supersecret",
		`GET /login?user=bob&password=letmein HTTP/1.1`:         "letmein",
		`token=abcdefghijklmnop`:                                "abcdefghijklmnop",
		`secretKey: 'abc123XYZ'`:                                "abc123XYZ",
		`const secretKey = 'wJalrXUtnFEMI/K7MDENG';`:            "wJalrXUtnFEMI",
		`passwordValue = 'Hunter2'`:                             "Hunter2",
		`{"secretKey":"abc123XYZ"}`:                             "abc123XYZ",
		"env:\n  - name: DB_PASSWORD\n    value: supersecret\n": "supersecret",
	} {
		if out := sanitize(input); strings.Contains(out, secret) {
			t.Errorf("credential retained: %q -> %q", input, out)
		}
	}
	for raw, secret := range map[string]string{
		`{"env":[{"name":"API_TOKEN","value":"supersecretvalue"}]}`: "supersecretvalue",
		`[{"Name":"DB_PASSWORD","Value":"Hunter22x"}]`:              "Hunter22x",
	} {
		if out := sanitizeJSON(sanitize, raw); strings.Contains(out, secret) {
			t.Errorf("name/value credential retained: %q -> %q", raw, out)
		}
	}
	for _, input := range []string{
		`{"env":[{"name":"LOG_LEVEL","value":"debug"}]}`,
		"  - name: LOG_LEVEL\n    value: debug\n",
		`f(token=cfg.token)`,
	} {
		if out := sanitize(input); out != input {
			t.Errorf("ordinary input changed: %q -> %q", input, out)
		}
	}
}

// TestSanitizeRoundThreeReviewInputs pins the third review round: comma- and
// quote-led data is not a keyword argument, JSON-text name/value pairs and YAML
// block values redact, and tight code assignments and flag keys stay intact.
func TestSanitizeRoundThreeReviewInputs(t *testing.T) {
	sanitize := newSanitizer(nil)
	for input, secret := range map[string]string{
		`["PATH=/usr/bin","POSTGRES_PASSWORD=example"]`:                                         "example",
		`['PATH=/usr/bin', 'POSTGRES_PASSWORD=example']`:                                        "example",
		`user=bob,password=hunter`:                                                              "hunter",
		"{\"environment\": [\n {\n \"name\": \"DB_PASSWORD\",\n \"value\": \"hunter2\"\n }\n]}": "hunter2",
		"- name: SECRET\n  value: |\n    multi\n    line\nnext: ok":                             "multi",
	} {
		if out := sanitize(input); strings.Contains(out, secret) {
			t.Errorf("credential retained: %q -> %q", input, out)
		}
	}
	if out := sanitize("- name: SECRET\n  value: |\n    multi\n    line\nnext: ok"); !strings.Contains(out, "line") && !strings.HasSuffix(out, "next: ok") {
		t.Errorf("content after the block lost: %q", out)
	}
	for _, input := range []string{
		`token=os.environ["TOKEN"]`,
		`let password=req.body.password;`,
		`this.password=e.password`,
		`f(x, token=token)`,
		`{"hasPassword":"yes"}`,
	} {
		if out := sanitize(input); out != input {
			t.Errorf("ordinary input changed: %q -> %q", input, out)
		}
	}
	if out := sanitizeJSON(sanitize, `{"hasPassword":"yes","password":"x1"}`); !strings.Contains(out, `"hasPassword":"yes"`) || strings.Contains(out, `"x1"`) {
		t.Errorf("flag key handling: %s", out)
	}
}

// TestSanitizeRoundFourReviewInputs pins the fourth review round: key=value
// lists ending in `;` redact, multi-line keyword arguments and tight member
// accesses stay intact, and npm and Secrets Manager values redact. A tight
// letters-only value under a dotted lowercase key (`user.password=newPassword`)
// still redacts: it is indistinguishable from `spring.datasource.password=x`.
func TestSanitizeRoundFourReviewInputs(t *testing.T) {
	sanitize := newSanitizer(nil)
	for input, secret := range map[string]string{
		`Server=db;Database=app;User Id=sa;Password=postgres;`:                  "postgres",
		`jdbc:sqlserver://h:1433;databaseName=app;password=secret;encrypt=true`: "=secret",
		`export API_KEY=hunter; npm start`:                                      "hunter",
		`DB_PASSWORD=postgres; ./run.sh`:                                        "postgres",
		`//registry.npmjs.org/:_authToken=npm_AbCdEf1234567890`:                 "npm_AbCdEf1234567890",
		`{"SecretString": "hunter2plain", "Name": "prod/db"}`:                   "hunter2plain",
	} {
		if out := sanitize(input); strings.Contains(out, secret) {
			t.Errorf("credential retained: %q -> %q", input, out)
		}
	}
	for _, input := range []string{
		"client = OpenAI(\n    api_key=settings.OPENAI_API_KEY,\n    timeout=30,\n)",
		"boto3.client(\n    \"s3\",\n    aws_secret_access_key=creds.secret_key,\n)",
		"client = Client(\n    api_key=api_key,\n    token=token,\n)",
		`    token=response.data.token`,
		`secret=process.env.SECRET`,
	} {
		if out := sanitize(input); out != input {
			t.Errorf("ordinary input changed: %q -> %q", input, out)
		}
	}
}

// TestSanitizeRoundFiveReviewInputs pins the fifth review round: interior
// quotes, .npmrc `_password`/`_auth`, empty-user URL credentials, and env
// values with parentheses redact whole, while string literals that merely end
// after a credential value keep their closing quote.
func TestSanitizeRoundFiveReviewInputs(t *testing.T) {
	sanitize := newSanitizer(nil)
	for input, want := range map[string]string{
		`DB_PASSWORD=Xk9"mQ2!zR`:                         `DB_PASSWORD=[REDACTED]`,
		`DB_PASSWORD=Xk9'mQ2!zR`:                         `DB_PASSWORD=[REDACTED]`,
		"API_TOKEN=ab`cd12EF34":                          `API_TOKEN=[REDACTED]`,
		`password=Xk9"mQ2!zR`:                            `password=[REDACTED]`,
		`DB_PASSWORD=Pa(ss)w0rd`:                         `DB_PASSWORD=[REDACTED]`,
		`//registry.npmjs.org/:_password=aHVudGVyMg==`:   `//registry.npmjs.org/:_password=[REDACTED]`,
		`//registry.npmjs.org/:_auth=dXNlcjpodW50ZXIy`:   `//registry.npmjs.org/:_auth=[REDACTED]`,
		`REDIS_URL=redis://:S3cretPass@localhost:6379/0`: `REDIS_URL=redis://[REDACTED]@localhost:6379/0`,
		`url = "https://x.test/cb?token=abc123XY"`:       `url = "https://x.test/cb?token=[REDACTED]"`,
		`["PATH=/usr/bin","POSTGRES_PASSWORD=example"]`:  `["PATH=/usr/bin","POSTGRES_PASSWORD=[REDACTED]"]`,
		`docker run -e "DB_PASSWORD=supersecret" img`:    `docker run -e "DB_PASSWORD=[REDACTED]" img`,
		`{"env":"API_TOKEN=abc123\nDEBUG=true"}`:         `{"env":"API_TOKEN=[REDACTED]\nDEBUG=true"}`,
	} {
		if got := sanitize(input); got != want {
			t.Errorf("%q -> %q, want %q", input, got, want)
		}
	}
	for _, input := range []string{
		"client = OpenAI(\n    api_key=api_key\n)",
		`print(f"token={token}")`,
		`http://localhost:8080/path`,
	} {
		if out := sanitize(input); out != input {
			t.Errorf("ordinary input changed: %q -> %q", input, out)
		}
	}
}

// TestSanitizeRoundSixReviewInputs pins the sixth review round: capitalized
// words and environment-key values in colon form redact, quoted pagination
// cursors survive, and environment values stop at list and command separators.
func TestSanitizeRoundSixReviewInputs(t *testing.T) {
	sanitize := newSanitizer(nil)
	for input, want := range map[string]string{
		`      POSTGRES_PASSWORD: MySecretPassword`:                `      POSTGRES_PASSWORD: [REDACTED]`,
		`password: CorrectHorseBatteryStaple`:                      `password: [REDACTED]`,
		`api_key: AbCdEfGhIjKlMnOpQrSt`:                            `api_key: [REDACTED]`,
		`DB_PASSWORD = MySecretPassword`:                           `DB_PASSWORD = [REDACTED]`,
		`spring.datasource.password: MySecretPassword`:             `spring.datasource.password: [REDACTED]`,
		`"nextPageToken": "CAESBggDEAEYAQ"`:                        `"nextPageToken": "CAESBggDEAEYAQ"`,
		`"NextToken": "eyJOZXh0VG9rZW4iOiBudWxsfQ=="`:              `"NextToken": "eyJOZXh0VG9rZW4iOiBudWxsfQ=="`,
		`export API_TOKEN=abc123XYZ;./deploy.sh`:                   `export API_TOKEN=[REDACTED];./deploy.sh`,
		`Server=db;PASSWORD=Pw123abc!;TrustServerCertificate=True`: `Server=db;PASSWORD=[REDACTED];TrustServerCertificate=True`,
		`if true; then TOKEN=Abc123Xyz; fi`:                        `if true; then TOKEN=[REDACTED]; fi`,
		`cfg = Config(TOKEN=Abc123Xyz, DEBUG=True)`:                `cfg = Config(TOKEN=[REDACTED], DEBUG=True)`,
		`(export DB_PASSWORD=secret123xyz)`:                        `(export DB_PASSWORD=[REDACTED])`,
		`DB_PASSWORD=Pa(ss)w0rd`:                                   `DB_PASSWORD=[REDACTED]`,
	} {
		if got := sanitize(input); got != want {
			t.Errorf("%q -> %q, want %q", input, got, want)
		}
	}
}

// TestSanitizePaginationCursorsAreExactKeys pins the seventh review round: only
// real cursor keys keep their values; credentials ending in "page token" do not.
func TestSanitizePaginationCursorsAreExactKeys(t *testing.T) {
	sanitize := newSanitizer(nil)
	for input, secret := range map[string]string{
		`"fb_page_token":"EAAGm0PX4ZCpsBA"`:      "EAAGm0PX4ZCpsBA",
		`SYNC_TOKEN=abcDEF123456`:                "abcDEF123456",
		"access=EAA" + strings.Repeat("x9Y", 40): "EAAx9Y",
	} {
		if out := sanitize(input); strings.Contains(out, secret) {
			t.Errorf("credential retained: %q -> %q", input, out)
		}
	}
	for _, input := range []string{`"nextPageToken": "CAESBggDEAEYAQ"`, `"pageToken": "CAES9x"`, `"continuationToken": "abc123XYZdef"`, `"nextSyncToken": "CPjx8x"`, `page_token=next-page`,
		`"nextForwardToken": "f/38451234/s"`, `"nextBackwardToken": "b/38451234/s"`, `{"NextContinuationToken": "1ueGcxLPRx1Tr="}`, `"syncToken": "CPDAlvWDx70C="`, `"startPageToken": "12345"`, `"PaginationToken": "Ab9x"`} {
		if out := sanitize(input); out != input {
			t.Errorf("cursor changed: %q -> %q", input, out)
		}
	}
}

// TestSanitizeMasksParenthesizedValues pins the round-5 review suggestion:
// parentheses inside a bare value are data, not a call, so `password=Pa(ss)w0rd`
// redacts whole instead of passing through in full or leaking its tail past
// the `[REDACTED]` marker. Real calls still pass through byte-for-byte.
func TestSanitizeMasksParenthesizedValues(t *testing.T) {
	for input, want := range map[string]string{
		`DB_PASSWORD=Pa(ss)w0rd`:   `DB_PASSWORD=[REDACTED]`,
		`password=Pa(ss)w0rd`:      `password=[REDACTED]`,
		`token=abc(de)fg`:          `token=[REDACTED]`,
		`PASSWORD=s3(x)cret`:       `PASSWORD=[REDACTED]`,
		`spring.password=Pa(ss)w0`: `spring.password=[REDACTED]`,
	} {
		if got := newSanitizer(nil)(input); got != want {
			t.Errorf("%q -> %q, want %q", input, got, want)
		}
	}
	for _, input := range []string{
		`token = generateToken(user)`,
		`token = outer(inner(x))`,
		`token = f(a); next`,
		`api_key = loadApiKey(cfg)`,
		`secret = computeSecret(salt, pw)`,
	} {
		if got := newSanitizer(nil)(input); got != input {
			t.Errorf("real call changed: %q -> %q", input, got)
		}
	}
}
