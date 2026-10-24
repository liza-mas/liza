package install

import "testing"

func TestOracleInitWorkspace(t *testing.T) {
	skipUnlessLinux(t)
	cases := []string{"default", "all-config", "refuse-uncommitted-spec", "refuse-reinit"}
	for _, provider := range providers {
		cases = append(cases, "default-cli-"+provider)
	}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.materialiseRepo(t, name != "refuse-uncommitted-spec")
			f.invoke(t, "prerequisite-setup", "", false, 0, "setup", "--yes")
			if name == "refuse-reinit" {
				f.invoke(t, "prerequisite-init", "", false, 0, "init", "Oracle goal", "--yes")
			}
			beforeState := readOptional(t, f.project, ".liza/state.yaml")
			beforeTree := f.manifest(t, f.project)
			beforeGit := f.gitSnapshot(t)
			args, stdin, expected := []string{"init", "Oracle goal", "--yes"}, "", 0
			switch name {
			case "all-config":
				args = append(args, "--branch", "integ", "--post-worktree-cmd", "true", "--entry-point", "general-objective",
					"--max-instances", "2", "--validation-execution", "local", "--auto-resume", "--no-follow-up",
					"--agent-env-deny", "FOO", "--copy-worktree-env-files")
			case "refuse-uncommitted-spec":
				expected = 1
			case "refuse-reinit":
				args, stdin, expected = []string{"init", "Oracle goal"}, "n\n", 1
			case "default":
			default:
				args = append(args, "--default-cli", name[len("default-cli-"):])
			}
			f.invoke(t, "init", stdin, false, expected, args...)
			prefix := "init/" + name
			f.checkHuman(t, prefix)
			f.checkState(t, prefix, beforeState, readOptional(t, f.project, ".liza/state.yaml"))
			f.checkManifest(t, prefix+".project", f.project, beforeTree)
			f.normaliser().CheckState(t, prefix+".git-effects", beforeGit, f.gitSnapshot(t))
			f.checkActivations(t, prefix)
		})
	}
}
