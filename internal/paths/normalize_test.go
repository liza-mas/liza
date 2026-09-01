package paths

import "testing"

func TestNormalizeSpecRef(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "clean relative path unchanged",
			input: "specs/plan.md",
			want:  "specs/plan.md",
		},
		{
			name:  "worktree prefix stripped",
			input: ".worktrees/task-1/specs/plan.md",
			want:  "specs/plan.md",
		},
		{
			name:  "absolute path with worktree stripped",
			input: "/home/user/project/.worktrees/task-1/specs/plan.md",
			want:  "specs/plan.md",
		},
		{
			name:  "empty string unchanged",
			input: "",
			want:  "",
		},
		{
			name:  "worktrees as substring no false positive",
			input: "docs/about.worktrees.md",
			want:  "docs/about.worktrees.md",
		},
		{
			name:  "worktree with nested path",
			input: ".worktrees/plan-task-2/specs/sub/deep/plan.md",
			want:  "specs/sub/deep/plan.md",
		},
		{
			name:  "worktree segment only no trailing path",
			input: ".worktrees/task-id",
			want:  ".worktrees/task-id",
		},
		{
			name:  "path without worktree prefix unchanged",
			input: "/absolute/path/specs/plan.md",
			want:  "/absolute/path/specs/plan.md",
		},
		{
			name:  "worktree prefix with fragment anchor",
			input: ".worktrees/task-1/specs/plan.md#section",
			want:  "specs/plan.md#section",
		},
		{
			name:  "traversal after worktree not laundered",
			input: ".worktrees/task-1/../README.md",
			want:  ".worktrees/task-1/../README.md",
		},
		{
			name:  "fragment-only after worktree not laundered",
			input: ".worktrees/task-1/#anchor",
			want:  ".worktrees/task-1/#anchor",
		},
		{
			name:  "absolute remainder after worktree not laundered",
			input: ".worktrees/task-1//etc/passwd",
			want:  ".worktrees/task-1//etc/passwd",
		},
		{
			name:  "empty remainder after worktree not laundered",
			input: "prefix/.worktrees/task-1/",
			want:  "prefix/.worktrees/task-1/",
		},
		{
			name:  "windows absolute remainder after worktree not laundered",
			input: `.worktrees/task-1/C:\specs\plan.md`,
			want:  `.worktrees/task-1/C:\specs\plan.md`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeSpecRef(tt.input)
			if got != tt.want {
				t.Errorf("NormalizeSpecRef(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsAbsAnyPlatform(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{path: "/etc/passwd", want: true},
		{path: `\Windows\System32`, want: true},
		{path: `C:\repo\file`, want: true},
		{path: "D:/repo/file", want: true},
		{path: "specs/plan.md", want: false},
		{path: "docs:C", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := IsAbsAnyPlatform(tt.path); got != tt.want {
				t.Fatalf("IsAbsAnyPlatform(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}
