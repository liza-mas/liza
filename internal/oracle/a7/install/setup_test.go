package install

import (
	"path/filepath"
	"testing"
)

func TestOracleSetup(t *testing.T) {
	skipUnlessLinux(t)
	cases := []string{"default", "force-existing", "agent-tools", "claude", "codex", "copilot", "cursor", "gemini", "mistral", "opencode", "pi", "provider-kimi", "refuse-existing"}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			if name == "force-existing" || name == "refuse-existing" {
				f.invoke(t, "prerequisite-setup", "", false, 0, "setup", "--yes")
			}
			before := f.manifest(t, f.home)
			args, expected := []string{"setup", "--yes"}, 0
			switch name {
			case "default", "refuse-existing":
				if name == "refuse-existing" {
					expected = 1
				}
			case "force-existing":
				args = append(args, "--force")
			case "agent-tools":
				path := filepath.Join(f.root, "agent-tools.md")
				copyFixture(t, "agent-tools.md", path)
				args = append(args, "--agent-tools", path)
			case "provider-kimi":
				args, expected = append(args, "--provider", "kimi"), 1
			default:
				args = append(args, "--"+name)
			}
			f.invoke(t, "setup", "", false, expected, args...)
			f.checkHuman(t, "setup/"+name)
			f.checkManifest(t, "setup/"+name+".home", f.home, before)
		})
	}
}
