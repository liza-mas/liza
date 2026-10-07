package models

import "testing"

func lineageTask(id string, replanned bool, supersedes string) Task {
	task := Task{ID: id, RolePair: "architecture-pair", Status: TaskStatusMerged, ParentTasks: []string{"epic"}}
	if replanned {
		task.TransitionsExecuted = map[string]bool{"replanned": true}
	}
	if supersedes != "" {
		task.Supersedes = &supersedes
	}
	return task
}

func TestReplanSuccessor(t *testing.T) {
	t.Parallel()
	otherPair := lineageTask("a-replan-1", false, "a")
	otherPair.RolePair = "code-planning-pair"
	otherParents := lineageTask("a-replan-1", false, "a")
	otherParents.ParentTasks = []string{"other-epic"}
	for name, tc := range map[string]struct {
		tasks []Task
		want  string
	}{
		"missing":           {tasks: nil},
		"not replanned":     {tasks: []Task{lineageTask("a", false, ""), lineageTask("a-replan-1", false, "a")}},
		"no successor":      {tasks: []Task{lineageTask("a", true, "")}},
		"ambiguous":         {tasks: []Task{lineageTask("a", true, ""), lineageTask("a-replan-1", false, "a"), lineageTask("a-fix", false, "a")}},
		"cycle":             {tasks: []Task{lineageTask("a", true, "b"), lineageTask("b", true, "a")}},
		"other role pair":   {tasks: []Task{lineageTask("a", true, ""), otherPair}},
		"other parents":     {tasks: []Task{lineageTask("a", true, ""), otherParents}},
		"broken chain":      {tasks: []Task{lineageTask("a", true, ""), lineageTask("a-replan-1", true, "a")}},
		"direct successor":  {tasks: []Task{lineageTask("a", true, ""), lineageTask("a-replan-1", false, "a")}, want: "a-replan-1"},
		"chained successor": {tasks: []Task{lineageTask("a", true, ""), lineageTask("a-replan-1", true, "a"), lineageTask("a-replan-1-replan-1", false, "a-replan-1")}, want: "a-replan-1-replan-1"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			successor, ok := ReplanSuccessor(&State{Tasks: tc.tasks}, "a")
			if tc.want == "" {
				if ok {
					t.Fatalf("resolved %s, want fail closed", successor.ID)
				}
				return
			}
			if !ok || successor.ID != tc.want {
				t.Fatalf("successor = %v (ok=%v), want %s", successor, ok, tc.want)
			}
		})
	}
}
