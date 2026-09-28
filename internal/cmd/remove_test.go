package cmd

import "testing"

// TestBranchOutcomeNote pins the parenthesized suffix of the "Removed
// worktree" line for every outcome resolveBranch can produce. These strings
// are th's oldest human contract; the machine fields were added around them,
// so a change here is a change users would see.
func TestBranchOutcomeNote(t *testing.T) {
	tests := []struct {
		name    string
		outcome branchOutcome
		want    string
	}{
		{
			name:    "detached or vanished branch says nothing",
			outcome: branchOutcome{},
			want:    "",
		},
		{
			name:    "no action recorded says nothing",
			outcome: branchOutcome{Branch: "fix-login"},
			want:    "",
		},
		{
			name:    "kept without --delete-branch",
			outcome: branchOutcome{Branch: "fix-login", Action: branchKept},
			want:    `(branch "fix-login" kept)`,
		},
		{
			name:    "kept because it is the default branch",
			outcome: branchOutcome{Branch: "main", Action: branchKept, Reason: reasonDefaultBranch},
			want:    `(branch "main" kept: default branch)`,
		},
		{
			name:    "kept because git refused off a terminal",
			outcome: branchOutcome{Branch: "fix-login", Action: branchKept, Reason: reasonNotFullyMerged},
			want:    `(branch "fix-login" kept: not fully merged; use git branch -D to force)`,
		},
		{
			name:    "kept because the prompt was declined",
			outcome: branchOutcome{Branch: "fix-login", Action: branchKept, Reason: reasonDeclined},
			want:    `(branch "fix-login" kept)`,
		},
		{
			name:    "kept because the forced delete failed",
			outcome: branchOutcome{Branch: "fix-login", Action: branchKept, Reason: reasonDeleteFailed, Detail: "git branch -D: fatal: nope"},
			want:    `(branch "fix-login" kept: git branch -D: fatal: nope)`,
		},
		{
			name:    "deleted with git branch -d",
			outcome: branchOutcome{Branch: "fix-login", Action: branchDeleted},
			want:    `(branch "fix-login" deleted)`,
		},
		{
			name:    "deleted after confirming the force",
			outcome: branchOutcome{Branch: "fix-login", Action: branchDeleted, Reason: reasonForced},
			want:    `(branch "fix-login" deleted)`,
		},
		{
			name:    "branch names are quoted, not interpolated",
			outcome: branchOutcome{Branch: `weird "name"`, Action: branchKept},
			want:    `(branch "weird \"name\"" kept)`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.outcome.note(); got != tt.want {
				t.Errorf("note() = %q; want %q", got, tt.want)
			}
		})
	}
}
