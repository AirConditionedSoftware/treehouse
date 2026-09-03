package gitx

import (
	"reflect"
	"testing"
)

func TestParseWorktrees(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []Worktree
	}{
		{
			name: "empty",
			in:   "",
			want: nil,
		},
		{
			name: "main plus branch worktree",
			in: "worktree /repos/myapp\n" +
				"HEAD 1234567890abcdef1234567890abcdef12345678\n" +
				"branch refs/heads/main\n" +
				"\n" +
				"worktree /trees/myapp/fix-login\n" +
				"HEAD abcdef1234567890abcdef1234567890abcdef12\n" +
				"branch refs/heads/fix-login\n",
			want: []Worktree{
				{Path: "/repos/myapp", Head: "1234567890abcdef1234567890abcdef12345678", Branch: "main"},
				{Path: "/trees/myapp/fix-login", Head: "abcdef1234567890abcdef1234567890abcdef12", Branch: "fix-login"},
			},
		},
		{
			name: "detached",
			in: "worktree /trees/x\n" +
				"HEAD abcdef1234567890abcdef1234567890abcdef12\n" +
				"detached\n",
			want: []Worktree{
				{Path: "/trees/x", Head: "abcdef1234567890abcdef1234567890abcdef12", Detached: true},
			},
		},
		{
			name: "bare",
			in:   "worktree /repos/bare.git\nbare\n",
			want: []Worktree{
				{Path: "/repos/bare.git", Bare: true},
			},
		},
		{
			name: "locked with and without reason",
			in: "worktree /trees/a\n" +
				"HEAD abcdef1234567890abcdef1234567890abcdef12\n" +
				"branch refs/heads/a\n" +
				"locked\n" +
				"\n" +
				"worktree /trees/b\n" +
				"HEAD abcdef1234567890abcdef1234567890abcdef12\n" +
				"branch refs/heads/b\n" +
				"locked on a usb drive\n",
			want: []Worktree{
				{Path: "/trees/a", Head: "abcdef1234567890abcdef1234567890abcdef12", Branch: "a", Locked: true},
				{Path: "/trees/b", Head: "abcdef1234567890abcdef1234567890abcdef12", Branch: "b", Locked: true, LockedReason: "on a usb drive"},
			},
		},
		{
			name: "prunable with reason",
			in: "worktree /trees/gone\n" +
				"HEAD abcdef1234567890abcdef1234567890abcdef12\n" +
				"branch refs/heads/gone\n" +
				"prunable gitdir file points to non-existent location\n",
			want: []Worktree{
				{Path: "/trees/gone", Head: "abcdef1234567890abcdef1234567890abcdef12", Branch: "gone", Prunable: true, PrunableReason: "gitdir file points to non-existent location"},
			},
		},
		{
			name: "branch name containing slash",
			in: "worktree /trees/feat\n" +
				"HEAD abcdef1234567890abcdef1234567890abcdef12\n" +
				"branch refs/heads/feature/login\n",
			want: []Worktree{
				{Path: "/trees/feat", Head: "abcdef1234567890abcdef1234567890abcdef12", Branch: "feature/login"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseWorktrees(tt.in)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseWorktrees:\n got %+v\nwant %+v", got, tt.want)
			}
		})
	}
}
