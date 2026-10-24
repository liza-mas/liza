package install

import "testing"

func TestOracleProviderActivation(t *testing.T) {
	skipUnlessLinux(t)
	cases := append(append([]string{}, providers...), "provider-kimi")
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.materialiseRepo(t, true)
			f.invoke(t, "prerequisite-setup", "", false, 0, "setup", "--yes")
			beforeHome, beforeProject := f.manifest(t, f.home), f.manifest(t, f.project)
			args := []string{"init", "--yes"}
			if name == "provider-kimi" {
				args = append(args, "--provider", "kimi")
			} else {
				args = append(args, "--"+name)
			}
			f.invoke(t, "activation", "", name == "codex" || name == "cursor", 0, args...)
			prefix := "activation/" + name
			f.checkHuman(t, prefix)
			f.checkManifest(t, prefix+".home", f.home, beforeHome)
			f.checkManifest(t, prefix+".project", f.project, beforeProject)
			f.checkActivations(t, prefix)
		})
	}
}
