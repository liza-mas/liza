package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// D56 scenarios: a C commit may flip a driver assertion and delete a golden.
// The demo package's fake driver asserts, in driver.txt, the production value
// it was written for, as an in-driver assertion that fails over other trees.
const (
	demoDir      = "internal/oracle/demo"
	driverPath   = demoDir + "/driver.txt"
	markerPath   = demoDir + "/marker.txt"
	staticDriver = demoDir + "/static.txt"
	lateDriver   = demoDir + "/late.txt"
	staticGolden = demoDir + "/testdata/golden/human/static.txt"
	lateGolden   = demoDir + "/testdata/golden/human/late.txt"
	demoPattern  = "./" + demoDir
)

type call struct {
	commit             string
	baseline           bool
	packages, excluded []string
}

type recorder struct{ calls []call }

// regen is the fake regenerator. With honour, it runs no excluded package, as
// the real one does; without, it models the engine not excluding C packages.
func (r *recorder) regen(honour bool) regenerator {
	return func(_ context.Context, dir, commit string, baseline bool, packages, excluded []string) (map[string][]byte, error) {
		r.calls = append(r.calls, call{commit, baseline, packages, excluded})
		if honour && slices.Contains(excluded, demoPattern) {
			return map[string][]byte{}, nil
		}
		read := func(name string) (string, bool) {
			b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
			return string(b), err == nil
		}
		source, _ := read("source.txt")
		marker, _ := read(markerPath)
		if driver, ok := read(driverPath); ok && strings.SplitN(driver, "\n", 2)[0] != "expects:"+source {
			return nil, fmt.Errorf("--- FAIL: TestOracleDemo driver expects another production value")
		}
		values := map[string][]byte{sampleGolden: []byte(source + "/" + marker + "\n")}
		if _, ok := read(staticDriver); ok {
			values[staticGolden] = []byte("static\n")
		}
		if _, ok := read(lateDriver); ok {
			values[lateGolden] = []byte("late\n")
		}
		return values, nil
	}
}

func (r *recorder) expect(t *testing.T, want ...call) {
	t.Helper()
	if !reflect.DeepEqual(r.calls, want) {
		t.Fatalf("regenerator calls:\n got %v\nwant %v", r.calls, want)
	}
}

func (s *scenario) recordPath(path, reason string) {
	s.t.Helper()
	b, err := os.ReadFile(filepath.Join(s.dir, ledgerPath))
	if err != nil {
		s.t.Fatal(err)
	}
	s.put(ledgerPath, string(b)+"changed "+path+" "+reason+"\n")
}

func (s *scenario) remove(names ...string) {
	s.t.Helper()
	for _, name := range names {
		if err := os.Remove(filepath.Join(s.dir, filepath.FromSlash(name))); err != nil {
			s.t.Fatal(err)
		}
	}
}

// newD56 returns a scenario whose last commit, the pre-C commit, holds a
// second golden next to the first.
func newD56(t *testing.T) (*scenario, string) {
	t.Helper()
	s := newScenario(t)
	s.put(staticDriver, "on")
	s.put(staticGolden, "static\n")
	return s, s.commit("add static oracle")
}

// editC1 stages the first C change: production flips, the driver asserts the
// flip and the golden follows, with its record.
func (s *scenario) editC1() {
	s.t.Helper()
	s.put("source.txt", "new")
	s.put(driverPath, "expects:new\n")
	s.put(sampleGolden, "new/a\n")
	s.record("C1")
}

func (s *scenario) c1() string {
	s.t.Helper()
	s.editC1()
	return s.commit("C1 flips the driver assertion")
}

func TestRegenD56FlippedDriverAssertion(t *testing.T) {
	s, pre := newD56(t)
	c1 := s.c1()
	var rec recorder
	s.run(rec.regen(true), 0, nil,
		"ok "+sampleGolden+" chain pre "+pre+" C1 "+c1+"\n",
		"ok "+staticGolden+" chain pre "+pre+"\n",
		"ok "+demoDir+" package pre "+pre+" C1 "+c1+"\n")
	rec.expect(t,
		call{"working", true, []string{"./internal/oracle/..."}, []string{demoPattern}},
		call{pre, true, []string{demoPattern}, nil},
		call{c1, false, []string{demoPattern}, nil})
	// Without the exclusion the working drivers run over starting production,
	// where the flipped assertion fails: the defect this correction removes.
	var unexcluded recorder
	s.run(unexcluded.regen(false), 2, nil, "error regeneration working", "driver expects another production value")
}

func TestRegenD56PreChangeState(t *testing.T) {
	t.Run("unreproduced pre-C golden", func(t *testing.T) {
		s, _ := newD56(t)
		s.put(staticGolden, "tampered\n")
		pre := s.commit("golden edited before the first C point")
		s.c1()
		s.check2(t, 1, "unexplained "+staticGolden+": pre-change reproduction differs "+pre)
	})
	t.Run("golden first committed after the first point", func(t *testing.T) {
		s, pre := newD56(t)
		s.editC1()
		s.put(lateDriver, "on")
		s.put(lateGolden, "late\n")
		s.recordPath(lateGolden, "C1")
		s.commit("C1 adds a golden")
		s.check2(t, 1, "unexplained "+lateGolden+": pre-change blob absent "+pre)
	})
}

// check2 runs the demo fake regenerator and requires an exit code and report.
func (s *scenario) check2(t *testing.T, want int, fragments ...string) string {
	t.Helper()
	var rec recorder
	return s.run(rec.regen(true), want, nil, fragments...)
}

func TestRegenD56LaterPoints(t *testing.T) {
	setup := func(t *testing.T) (*scenario, string, string) {
		s, pre := newD56(t)
		c1 := s.c1()
		s.put(driverPath, "expects:new\n# tightened\n")
		s.recordPath(driverPath, "C2")
		return s, pre, c1
	}
	t.Run("each point is regenerated at its own commit", func(t *testing.T) {
		s, pre, c1 := setup(t)
		c2 := s.commit("driver-only C2")
		s.put(markerPath, "b")
		s.put(sampleGolden, "new/b\n")
		s.record("determinism")
		c3 := s.commit("determinism")
		var rec recorder
		s.run(rec.regen(true), 0, nil,
			"ok "+sampleGolden+" chain pre "+pre+" C1 "+c1+" determinism "+c3+"\n",
			"ok "+staticGolden+" chain pre "+pre+"\n",
			"ok "+demoDir+" package pre "+pre+" C1 "+c1+" C2 "+c2+" determinism "+c3+"\n")
		rec.expect(t,
			call{"working", true, []string{"./internal/oracle/..."}, []string{demoPattern}},
			call{pre, true, []string{demoPattern}, nil},
			call{c1, false, []string{demoPattern}, nil},
			call{c2, false, []string{demoPattern}, nil},
			call{c3, false, []string{demoPattern}, nil})
	})
	t.Run("reproduction differs", func(t *testing.T) {
		s, _, _ := setup(t)
		s.put(sampleGolden, "wrong\n")
		s.record("C2")
		c2 := s.commit("wrong blob")
		s.check2(t, 1, "unexplained "+sampleGolden+": change point reproduction differs "+c2)
	})
	t.Run("still-committed golden left unproduced", func(t *testing.T) {
		s, _, _ := setup(t)
		s.remove(staticDriver)
		s.recordPath(staticDriver, "C2")
		c2 := s.commit("driver removed, golden kept")
		s.check2(t, 1, "unexplained "+staticGolden+": change point reproduction differs "+c2)
	})
}

func TestRegenD56TerminalDeletion(t *testing.T) {
	t.Run("reproduced deletion", func(t *testing.T) {
		s, pre := newD56(t)
		s.editC1()
		s.remove(staticGolden, staticDriver)
		s.recordPath(staticGolden, "C1")
		c1 := s.commit("C1 deletes a golden")
		s.check2(t, 0, "ok "+staticGolden+" chain pre "+pre+" C1 "+c1+" deleted\n")
	})
	t.Run("deletion still produced", func(t *testing.T) {
		s, _ := newD56(t)
		s.editC1()
		s.remove(staticGolden)
		s.recordPath(staticGolden, "C1")
		c1 := s.commit("C1 deletes a golden its driver still writes")
		s.check2(t, 1, "unexplained "+staticGolden+": deletion not reproduced "+c1)
	})
	t.Run("deletion without record", func(t *testing.T) {
		s, _ := newD56(t)
		s.c1()
		s.remove(staticGolden, staticDriver)
		c2 := s.commit("unrecorded deletion")
		s.check2(t, 1, "unexplained "+staticGolden+": change without record "+c2)
	})
	t.Run("deletion under a determinism record", func(t *testing.T) {
		s, _ := newD56(t)
		s.c1()
		s.remove(staticGolden, staticDriver)
		s.recordPath(staticGolden, "determinism")
		s.commit("determinism deletion")
		s.check2(t, 1, "missing "+staticGolden)
	})
	t.Run("working-tree-only deletion", func(t *testing.T) {
		s, _ := newD56(t)
		s.c1()
		s.remove(staticGolden)
		s.check2(t, 1, "missing "+staticGolden)
	})
}

func TestRegenD56RejectsUnexplained(t *testing.T) {
	for _, tt := range []struct {
		name, want string
		alter      func(*scenario)
	}{
		{"untracked golden", "extra " + demoDir + "/testdata/golden/json/untracked.json", func(s *scenario) {
			s.put(demoDir+"/testdata/golden/json/untracked.json", "{}")
		}},
		{"unrecorded driver change", "unexplained " + driverPath + ": change without record", func(s *scenario) {
			s.put(driverPath, "expects:new\n# unrecorded\n")
			s.commit("driver edited without a record")
		}},
		{"merge changing a driver", "unexplained " + driverPath + ": merge changes oracle files", func(s *scenario) {
			s.git("checkout", "-q", "-b", "side")
			s.put("side.txt", "side")
			s.commit("side")
			s.git("checkout", "-q", "main")
			s.put("main.txt", "main")
			s.commit("main")
			s.git("merge", "--no-ff", "--no-commit", "side")
			s.put(driverPath, "expects:new\n# merged\n")
			s.commit("merge changes a driver")
		}},
		{"uncommitted driver edit", "unexplained " + driverPath + ": working tree differs from HEAD", func(s *scenario) {
			s.put(driverPath, "expects:new\n# uncommitted\n")
		}},
		{"driver record without change", "unexplained " + driverPath + ": record without change", func(s *scenario) {
			s.recordPath(driverPath, "C2")
			s.commit("record for an unchanged driver")
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s, _ := newD56(t)
			s.c1()
			tt.alter(s)
			s.check2(t, 1, tt.want)
		})
	}
}

func TestRegenD56PackageScope(t *testing.T) {
	t.Run("record in an unselected package", func(t *testing.T) {
		s := newScenario(t)
		other := "internal/oracle/other/testdata/golden/human/o.txt"
		s.put(other, "x\n")
		s.commit("add other oracle")
		s.put(other, "y\n")
		s.recordPath(other, "C1")
		s.commit("C1 in another package")
		var rec recorder
		s.run(rec.regen(true), 0, []string{"-pkg", "./internal/oracle/demo/..."}, "ok "+sampleGolden+" H1\n")
		rec.expect(t, call{"working", true, []string{"./internal/oracle/demo/..."}, nil})
	})
	t.Run("nested package does not make its parent a C package", func(t *testing.T) {
		parent := "internal/oracle/a3/testdata/golden/human/p.txt"
		control := "internal/oracle/a3/control/testdata/golden/human/c.txt"
		s := newScenario(t)
		s.put(parent, "p\n")
		s.put(control, "old\n")
		pre := s.commit("add a3 oracle")
		s.put("source.txt", "new")
		s.put(control, "new\n")
		s.recordPath(control, "C1")
		c1 := s.commit("C1 in the nested package")
		var rec recorder
		reg := func(ctx context.Context, dir, commit string, baseline bool, packages, excluded []string) (map[string][]byte, error) {
			rec.calls = append(rec.calls, call{commit, baseline, packages, excluded})
			source, err := os.ReadFile(filepath.Join(dir, "source.txt"))
			values := map[string][]byte{parent: []byte("p\n")}
			if !slices.Contains(excluded, "./internal/oracle/a3/control") {
				values[control] = append(source, '\n')
			}
			return values, err
		}
		s.run(reg, 0, []string{"-pkg", "./internal/oracle/a3/..."},
			"ok "+parent+" H1\n", "ok "+control+" chain pre "+pre+" C1 "+c1+"\n")
		rec.expect(t,
			call{"working", true, []string{"./internal/oracle/a3/..."}, []string{"./internal/oracle/a3/control"}},
			call{pre, true, []string{"./internal/oracle/a3/control"}, nil},
			call{c1, false, []string{"./internal/oracle/a3/control"}, nil})
	})
	t.Run("write-new writes nothing into a C package", func(t *testing.T) {
		s, _ := newD56(t)
		s.c1()
		inC := demoDir + "/testdata/golden/json/new.json"
		outside := "internal/oracle/other/testdata/golden/json/new.json"
		reg := func(context.Context, string, string, bool, []string, []string) (map[string][]byte, error) {
			return map[string][]byte{inC: []byte("c"), outside: []byte("o")}, nil
		}
		s.run(reg, 0, []string{"-mode", "write-new"}, "wrote "+outside)
		if _, err := os.Stat(filepath.Join(s.dir, filepath.FromSlash(inC))); !os.IsNotExist(err) {
			t.Fatal("write-new wrote into a C package")
		}
	})
}

// Exact and recursive filters select a record by the package owning its path,
// whether it names a golden, a driver or a fixture.
func TestRegenD56FiltersSelectDriverRecords(t *testing.T) {
	for _, pattern := range []string{demoPattern, demoPattern + "/..."} {
		t.Run("driver-only first point "+pattern, func(t *testing.T) {
			s, pre := newD56(t)
			s.put(driverPath, "expects:old\n")
			s.recordPath(driverPath, "C1")
			c1 := s.commit("driver-only C1")
			var rec recorder
			s.run(rec.regen(true), 0, []string{"-pkg", pattern},
				"ok "+sampleGolden+" chain pre "+pre+"\n", "ok "+demoDir+" package pre "+pre+" C1 "+c1+"\n")
		})
		t.Run("driver record without change "+pattern, func(t *testing.T) {
			s, _ := newD56(t)
			s.c1()
			s.recordPath(driverPath, "C2")
			s.commit("record for an unchanged driver")
			var rec recorder
			s.run(rec.regen(true), 1, []string{"-pkg", pattern}, "unexplained "+driverPath+": record without change")
		})
	}
}
