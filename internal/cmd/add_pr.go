package cmd

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/AirConditionedSoftware/treehouse/internal/forge"
	"github.com/AirConditionedSoftware/treehouse/internal/gitx"
	"github.com/spf13/cobra"
)

var addPrCmd = &cobra.Command{
	Use:   "pr <number|url>",
	Short: "Create a worktree from a GitHub pull request",
	Long: `Create a worktree from a GitHub pull request, given its number, #number, or
web URL. The PR must belong to this repository's origin.

With the gh CLI available, the worktree is created on the PR's actual head
branch; for a same-repo PR it tracks origin, so pushed fixups reach the PR.
Without gh, the PR head is fetched directly via refs/pull/<n>/head into a
branch named pr-<n>.

The configured branch_prefix never applies — the branch belongs to the PR
author. The created path is the only output on stdout.`,
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) != 1 {
			return errors.New(`expected a pull request number or URL (for a branch literally named "pr", use git worktree add)`)
		}
		return nil
	},
	RunE: runAddPr,
}

func runAddPr(cmd *cobra.Command, args []string) error {
	num, err := forge.ParsePRArg(args[0])
	if err != nil {
		return err
	}

	ctx, err := newAddContext()
	if err != nil {
		return err
	}

	f, err := forge.Detect(".")
	if err != nil {
		return err
	}

	pr, ghErr := f.ResolvePR(".", num)
	if ghErr != nil {
		fmt.Fprintf(os.Stderr, "gh unavailable for PR #%d (%v); falling back to plain git\n", num, ghErr)
	} else if pr.State == "MERGED" || pr.State == "CLOSED" {
		fmt.Fprintf(os.Stderr, "Warning: PR #%d is %s\n", num, strings.ToLower(pr.State))
	}

	// The head branch name comes from the PR author, so the configured
	// branch_prefix never applies. Without gh there is no name to use.
	branch := fmt.Sprintf("pr-%d", num)
	if ghErr == nil {
		branch = pr.HeadRef
	}
	sameRepo := ghErr == nil && !pr.IsCrossRepo

	return finishAdd(ctx, branch, func(target string) error {
		if sameRepo {
			// Update origin/<branch> so a re-run sees new PR commits; a
			// failure (branch deleted on origin) falls through to the
			// refs/pull route below.
			_, _ = gitx.Run(".", "fetch", "origin", branch)
		}

		// A branch left over from an earlier checkout of this PR is reused
		// as-is: without an upstream (fork and no-gh cases) a forced update
		// could destroy local commits.
		if gitx.LocalBranchExists(".", branch) {
			fmt.Fprintf(os.Stderr, "Creating worktree for existing branch %q\n", branch)
			if sameRepo {
				local, lerr := gitx.Run(".", "rev-parse", "refs/heads/"+branch)
				remote, rerr := gitx.Run(".", "rev-parse", "refs/remotes/origin/"+branch)
				if lerr == nil && rerr == nil && local != remote {
					fmt.Fprintf(os.Stderr, "note: local branch %q differs from origin/%s; run git pull inside the worktree to update\n", branch, branch)
				}
			} else {
				fmt.Fprintf(os.Stderr, "note: branch %q already exists locally; using it as-is (delete it and re-run to refresh from the PR)\n", branch)
			}
			_, err := gitx.Run(".", "worktree", "add", target, branch)
			return err
		}

		if sameRepo && gitx.RemoteBranchExists(".", branch) {
			fmt.Fprintf(os.Stderr, "Creating worktree for %q tracking origin/%s\n", branch, branch)
			_, err := gitx.Run(".", "worktree", "add", "--track", "-b", branch, target, "origin/"+branch)
			return err
		}

		if ghErr == nil && pr.IsCrossRepo {
			fmt.Fprintf(os.Stderr, "PR #%d comes from a fork (%s); pushes from this worktree will not reach it — use gh pr checkout for that workflow\n", num, pr.HeadOwner)
		}

		pullRef := f.PullHeadRef(num)
		fmt.Fprintf(os.Stderr, "Fetching %s from origin\n", pullRef)
		if _, err := gitx.Run(".", "fetch", "origin", pullRef); err != nil {
			return fmt.Errorf("PR #%d could not be fetched from origin (does it exist?): %w", num, err)
		}
		fmt.Fprintf(os.Stderr, "Creating worktree with branch %q from PR #%d\n", branch, num)
		_, err := gitx.Run(".", "worktree", "add", "-b", branch, target, "FETCH_HEAD")
		return err
	})
}

func init() {
	addCmd.AddCommand(addPrCmd)
}
