package oracle

import (
	"sort"
	"strings"
	"testing"
)

func envNames(goos string, env []string) []string {
	var names []string
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		if goos == "windows" {
			name = strings.ToUpper(name)
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func envValue(env []string, name string) (string, bool) {
	for _, kv := range env {
		if n, v, _ := strings.Cut(kv, "="); n == name {
			return v, true
		}
	}
	return "", false
}

func TestChildEnvExactNameSetPerPlatform(t *testing.T) {
	base := []string{"HOME", "PATH", "TMPDIR"}
	win := []string{"SYSTEMROOT", "TEMP", "TMP", "USERPROFILE"}
	goNames := []string{"GOCACHE", "GOFLAGS", "GOMODCACHE"}
	cases := []struct {
		goos string
		goTC bool
		want []string
	}{
		{"linux", false, base},
		{"darwin", false, base},
		{"windows", false, append(append([]string{}, base...), win...)},
		{"linux", true, append(append([]string{}, base...), goNames...)},
		{"darwin", true, append(append([]string{}, base...), goNames...)},
		{"windows", true, append(append(append([]string{}, base...), win...), goNames...)},
	}
	for _, c := range cases {
		env, err := childEnv(c.goos, EnvBase{Home: "/h", Path: "/p", Tmp: "/t", Go: c.goTC}, goCaches{"/gc", "/gm"}, "/sysroot", nil)
		if err != nil {
			t.Fatalf("%s go=%v: %v", c.goos, c.goTC, err)
		}
		want := append([]string{}, c.want...)
		sort.Strings(want)
		got := envNames(c.goos, env)
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("%s go=%v: names %v, want %v", c.goos, c.goTC, got, want)
		}
	}
}

func TestChildEnvWindowsValues(t *testing.T) {
	env, err := childEnv("windows", EnvBase{Home: `C:\h`, Path: `C:\p`, Tmp: `C:\t`}, goCaches{}, `C:\Windows`, nil)
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{
		"HOME": `C:\h`, "USERPROFILE": `C:\h`, "TMPDIR": `C:\t`, "TMP": `C:\t`, "TEMP": `C:\t`, "SYSTEMROOT": `C:\Windows`,
	} {
		if got, ok := envValue(env, name); !ok || got != want {
			t.Errorf("%s: value differs from its source variable (present=%v)", name, ok)
		}
	}
}

func TestChildEnvGoToolchainForm(t *testing.T) {
	env, err := childEnv("linux", EnvBase{Home: "/h", Path: "/p", Tmp: "/t", Go: true}, goCaches{"/gc", "/gm"}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"GOCACHE": "/gc", "GOMODCACHE": "/gm", "GOFLAGS": "-buildvcs=false"} {
		if got, _ := envValue(env, name); got != want {
			t.Errorf("%s has an unexpected value", name)
		}
	}
}

func TestChildEnvExtraAppendedAndPolicyRedefinitionRejected(t *testing.T) {
	base := EnvBase{Home: "/h", Path: "/p", Tmp: "/t"}
	env, err := childEnv("linux", base, goCaches{}, "", []string{"ORACLE_X=1"})
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := envValue(env, "ORACLE_X"); !ok || v != "1" {
		t.Error("extra pair missing")
	}
	for _, c := range []struct{ goos, entry, name string }{
		{"linux", "HOME=secret-value", "HOME"},
		{"linux", "TMPDIR=secret-value", "TMPDIR"},
		{"windows", "userprofile=secret-value", "userprofile"},
		{"windows", "Path=secret-value", "Path"},
		{"windows", "SystemRoot=secret-value", "SystemRoot"},
	} {
		_, err := childEnv(c.goos, base, goCaches{}, "", []string{c.entry})
		if err == nil {
			t.Errorf("%s: %s redefinition accepted", c.goos, c.name)
			continue
		}
		if !strings.Contains(err.Error(), c.name) {
			t.Errorf("%s: error does not name %s", c.goos, c.name)
		}
		if strings.Contains(err.Error(), "secret-value") {
			t.Errorf("%s: error carries a value", c.goos)
		}
	}
	if _, err := childEnv("linux", base, goCaches{}, "", []string{"path=lowercase-is-distinct-on-linux"}); err != nil {
		t.Errorf("names are case-sensitive on linux: %v", err)
	}
	if _, err := childEnv("linux", base, goCaches{}, "", []string{"NOEQUALS"}); err == nil {
		t.Error("an entry without = was accepted")
	}
}

func TestChildEnvResolvesParentGoCaches(t *testing.T) {
	env, err := ChildEnv(EnvBase{Home: "/h", Path: "/p", Tmp: "/t", Go: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"GOCACHE", "GOMODCACHE"} {
		if v, ok := envValue(env, name); !ok || v == "" {
			t.Errorf("%s not resolved", name)
		}
	}
}
