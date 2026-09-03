package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/AirConditionedSoftware/treehouse/internal/config"
	"github.com/AirConditionedSoftware/treehouse/internal/gitx"
	"github.com/spf13/cobra"
)

var (
	addBase         string
	addPath         string
	addNoPrefix     bool
	addCopyHooks    bool
	addNoCopyHooks  bool
	addCopyFile     []string
	addNoCopyFiles  bool
	addLinkFile     []string
	addNoLinkFiles  bool
	addOpen         bool
	addNoOpen       bool
	addNoPreCreate  bool
	addNoPostCreate bool
	addCD           bool
	addNoCD         bool
)

type branchKind int

const (
	kindNew branchKind = iota
	kindLocal
	kindRemote
)

var addCmd = &cobra.Command{
	Use:   "add <branch>",
	Short: "Create a worktree for a branch",
	Long: `Create a worktree for a branch. The location is derived from the config's
worktree_dir template unless --path is given.

If the branch exists locally it is checked out as-is; if it exists on origin
a local branch tracking it is created; otherwise a new branch is created from
--base, the config's default_base, or the current HEAD. New branches get the
config's branch_prefix if one is set (th add fix-login -> peter/fix-login);
--no-prefix skips it.

The created path is the only output on stdout, so shell integration like
cd "$(th add my-branch)" works.

th add pr <number> creates a worktree from a GitHub pull request instead;
see th add pr --help.`,
	Args: cobra.ExactArgs(1),
	RunE: runAdd,
}

// addContext is the shared setup th add and th add pr both need: the repo's
// worktrees, the main worktree path anchoring config resolution, and the
// resolved settings.
type addContext struct {
	wts      []gitx.Worktree
	mainPath string
	res      config.Resolved
	repo     string
}

func newAddContext() (*addContext, error) {
	wts, err := gitx.ListWorktrees(".")
	if err != nil {
		return nil, err
	}
	mainPath := wts[0].Path

	res, err := config.Resolve(mainPath)
	if err != nil {
		return nil, err
	}
	if err := finalizeLocalMigration(res); err != nil {
		return nil, err
	}
	repo := res.RepoName
	if repo == "" {
		repo = filepath.Base(mainPath)
	}
	applyDisplayConfig(res.Settings)
	return &addContext{wts: wts, mainPath: mainPath, res: res, repo: repo}, nil
}

func runAdd(cmd *cobra.Command, args []string) error {
	arg := args[0]

	ctx, err := newAddContext()
	if err != nil {
		return err
	}
	settings := ctx.res.Settings

	prefix := settings.EffectivePrefix()
	if addNoPrefix || (prefix != "" && strings.HasPrefix(arg, prefix)) {
		prefix = ""
	}

	// An existing branch — with or without the prefix — is used as-is; the
	// prefix only names branches that don't exist yet.
	branch := arg
	kind := kindNew
	switch {
	case gitx.LocalBranchExists(".", arg):
		kind = kindLocal
	case prefix != "" && gitx.LocalBranchExists(".", prefix+arg):
		branch, kind = prefix+arg, kindLocal
	case gitx.RemoteBranchExists(".", arg):
		kind = kindRemote
	case prefix != "" && gitx.RemoteBranchExists(".", prefix+arg):
		branch, kind = prefix+arg, kindRemote
	default:
		branch = prefix + arg
	}

	return finishAdd(ctx, branch, func(target string) error {
		var err error
		switch kind {
		case kindLocal:
			fmt.Fprintf(os.Stderr, "Creating worktree for existing branch %q\n", branch)
			_, err = gitx.Run(".", "worktree", "add", target, branch)
		case kindRemote:
			fmt.Fprintf(os.Stderr, "Creating worktree for %q tracking origin/%s\n", branch, branch)
			_, err = gitx.Run(".", "worktree", "add", "--track", "-b", branch, target, "origin/"+branch)
		default:
			base := addBase
			if base == "" {
				base = settings.DefaultBase
			}
			if base == "" {
				base = "HEAD"
			}
			fmt.Fprintf(os.Stderr, "Creating worktree with new branch %q from %s\n", branch, base)
			_, err = gitx.Run(".", "worktree", "add", "-b", branch, target, base)
		}
		return err
	})
}

// finishAdd runs everything shared by th add and th add pr once the branch is
// decided: the already-checked-out guard, the target-directory computation,
// pre_create, create to make the worktree there, and the post-create
// pipeline (hooks, copy_files, post_create, workspace file, VS Code). The
// created path is the only stdout output.
func finishAdd(ctx *addContext, branch string, create func(target string) error) error {
	settings := ctx.res.Settings
	mainPath := ctx.mainPath
	repo := ctx.repo

	for _, w := range ctx.wts {
		if w.Branch == branch {
			return fmt.Errorf("branch %q is already checked out at %s", branch, displayPath(w.Path))
		}
	}

	target := addPath
	if target == "" {
		var err error
		if target, err = settings.WorktreePath(repo, branch); err != nil {
			return err
		}
	}
	if abs, err := filepath.Abs(target); err == nil {
		target = abs
	}
	if _, err := os.Stat(target); err == nil {
		return fmt.Errorf("target directory already exists: %s", displayPath(target))
	}

	// pre_create runs in the main worktree — the target doesn't exist yet;
	// TH_WORKTREE carries the intended path. A failure aborts the add
	// before anything is created.
	preCmds := settings.PreCreate
	if addNoPreCreate {
		preCmds = nil
	}
	preCmds, err := gateRepoHook(mainPath, ctx.res, "pre_create", preCmds)
	if err != nil {
		return err
	}
	if len(preCmds) > 0 {
		if err := runHook("pre_create", mainPath, target, mainPath, repo, branch, preCmds); err != nil {
			return err
		}
	}

	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}

	if err := create(target); err != nil {
		return err
	}

	openTarget, err := provisionWorktree(ctx, target, branch, !addNoPostCreate)
	if err != nil {
		return fmt.Errorf("worktree created at %s, but %w", displayPath(target), err)
	}

	wantOpen := settings.VSCodeOpenEnabled()
	if addOpen {
		wantOpen = true
	}
	if addNoOpen {
		wantOpen = false
	}
	if wantOpen {
		openInVSCode(openTarget)
	}

	wantCD := settings.AutoCDEnabled()
	if addCD {
		wantCD = true
	}
	if addNoCD {
		wantCD = false
	}
	// An effective VS Code open always wins, even over an explicit --cd:
	// the user works in the opened window and the terminal stays put.
	if wantOpen {
		wantCD = false
	}
	if wantCD {
		writeCDFile(target)
	}

	fmt.Println(target)
	return nil
}

// provisionWorktree runs the config-driven provisioning th add and th refresh
// share on the worktree at target: the hooks copy, copy_files/link_files
// placement, post_create (when runPost is true), and the workspace file. It
// returns the path VS Code should open (the workspace file when one was
// written, else target). Errors name the failed step with no "worktree
// created" framing; callers add their own.
func provisionWorktree(ctx *addContext, target, branch string, runPost bool) (string, error) {
	settings := ctx.res.Settings
	mainPath := ctx.mainPath
	repo := ctx.repo

	wantHooks := settings.CopyHooksEnabled()
	if addCopyHooks {
		wantHooks = true
	}
	if addNoCopyHooks {
		wantHooks = false
	}
	if wantHooks {
		if err := copyHooksTo(mainPath, target); err != nil {
			return "", fmt.Errorf("copying hooks failed: %w", err)
		}
	}

	patterns := settings.CopyFiles
	if addNoCopyFiles {
		patterns = nil
	}
	patterns = append(append([]string{}, patterns...), addCopyFile...)
	linkPatterns := settings.LinkFiles
	if addNoLinkFiles {
		linkPatterns = nil
	}
	linkPatterns = append(append([]string{}, linkPatterns...), addLinkFile...)
	if len(patterns) > 0 || len(linkPatterns) > 0 {
		if err := copyFilesTo(mainPath, target, patterns, linkPatterns); err != nil {
			return "", fmt.Errorf("placing files failed: %w", err)
		}
	}

	postCmds := settings.PostCreate
	if !runPost {
		postCmds = nil
	}
	// Commands the repository supplied are gated; commands from the
	// user-owned global config are not.
	postCmds, err := gateRepoHook(mainPath, ctx.res, "post_create", postCmds)
	if err != nil {
		return "", err
	}
	if len(postCmds) > 0 {
		if err := runHook("post_create", target, target, mainPath, repo, branch, postCmds); err != nil {
			return "", err
		}
	}

	openTarget := target
	if settings.VSCodeWorkspaceFileEnabled() && branch != "" {
		wsPath, err := writeWorkspaceFile(target, settings, repo, branch)
		if err != nil {
			return "", fmt.Errorf("writing the workspace file failed: %w", err)
		}
		fmt.Fprintf(os.Stderr, "Wrote %s\n", displayPath(wsPath))
		openTarget = wsPath
	} else if settings.VSCodeWorkspaceFileEnabled() {
		// Unreachable from th add (a branch is always checked out there);
		// th refresh can meet a detached worktree.
		fmt.Fprintf(os.Stderr, "No branch checked out at %s; skipping the workspace file\n", displayPath(target))
	} else if settings.VSCodeSettings().WindowColor != "" {
		fmt.Fprintln(os.Stderr, "vscode.window_color has no effect without vscode.workspace_file")
	}
	return openTarget, nil
}

// workspaceFilePath returns where the th-generated .code-workspace for a
// worktree lives: next to the worktree directory (a sibling, so it never
// shows up as an untracked file inside it). "" for branchless worktrees.
func workspaceFilePath(settings config.Settings, worktreePath, branch string) string {
	if branch == "" {
		return ""
	}
	name := settings.VSCodeSettings().WorkspacePrefix + config.SanitizeBranch(branch) + ".code-workspace"
	return filepath.Join(filepath.Dir(worktreePath), name)
}

// writeWorkspaceFile writes a .code-workspace file next to the worktree with
// a folders entry for it and window.title set to the configured value (taken
// verbatim, VS Code title variables included) or the repo name. When
// vscode.window_color is set, workbench.colorCustomizations colors the
// window's title and status bars so each worktree is visibly distinct.
func writeWorkspaceFile(worktreePath string, settings config.Settings, repo, branch string) (string, error) {
	vs := settings.VSCodeSettings()
	title := vs.WindowTitle
	if title == "" {
		title = repo
	}
	wsPath := workspaceFilePath(settings, worktreePath, branch)
	if wsPath == "" {
		return "", fmt.Errorf("cannot derive a workspace file name for %s", displayPath(worktreePath))
	}
	folders := []map[string]string{{"path": worktreePath}}
	for _, wp := range vs.WorkspacePaths {
		p, err := config.ExpandTilde(wp.Path)
		if err != nil {
			return "", err
		}
		entry := map[string]string{"path": p}
		if wp.Name != "" {
			entry["name"] = wp.Name
		}
		folders = append(folders, entry)
	}
	wsSettings := map[string]any{"window.title": title}
	if bg := effectiveWindowColor(vs.WindowColor, repo, branch); bg != "" {
		wsSettings["workbench.colorCustomizations"] = windowColorCustomizations(bg)
	}
	ws := map[string]any{
		"folders":  folders,
		"settings": wsSettings,
	}
	data, err := json.MarshalIndent(ws, "", "  ")
	if err != nil {
		return "", err
	}
	return wsPath, os.WriteFile(wsPath, append(data, '\n'), 0o644)
}

// openInVSCode launches VS Code on path. Failing to open is a warning, not
// an error — the worktree itself is fine.
func openInVSCode(path string) {
	codeBin, err := exec.LookPath("code")
	if err != nil {
		fmt.Fprintln(os.Stderr, `VS Code CLI "code" not found on PATH; not opening`)
		return
	}
	if out, err := exec.Command(codeBin, path).CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "opening in VS Code failed: %v\n%s", err, out)
		return
	}
	fmt.Fprintf(os.Stderr, "Opened %s in VS Code\n", displayPath(path))
}

// copyFilesTo places untracked files (e.g. .env) from the main worktree
// into the new one: copyPatterns copy, linkPatterns symlink back to the
// main worktree's path. Patterns are filepath.Glob globs relative to the
// main worktree; a matched directory copies recursively. Files git tracks
// are never touched — the checkout already put them in the worktree, and
// overwriting them would silently clobber it. Copies run before links, so
// a copy can never write through a symlink into the main worktree.
func copyFilesTo(mainPath, worktreePath string, copyPatterns, linkPatterns []string) error {
	tracked, err := gitx.TrackedFiles(mainPath)
	if err != nil {
		return err
	}

	copied := 0
	var copiedBytes int64
	for _, pat := range copyPatterns {
		if !filepath.IsLocal(pat) {
			return fmt.Errorf("copy_files pattern %q must stay inside the repository", pat)
		}
		matches, err := filepath.Glob(filepath.Join(mainPath, pat))
		if err != nil {
			return fmt.Errorf("copy_files pattern %q: %w", pat, err)
		}
		if len(matches) == 0 {
			fmt.Fprintf(os.Stderr, "copy_files: no match for %q\n", pat)
			continue
		}
		for _, src := range matches {
			rel, err := filepath.Rel(mainPath, src)
			if err != nil {
				return err
			}
			dst := filepath.Join(worktreePath, rel)
			info, err := os.Stat(src)
			if err != nil {
				return err
			}
			if !info.IsDir() && tracked[rel] {
				fmt.Fprintf(os.Stderr, "copy_files: skipping tracked %q (already in the worktree via git)\n", rel)
				continue
			}
			prog := newCopyProgress(rel)
			if info.IsDir() {
				skipped := 0
				skip := func(fileRel string) bool {
					if tracked[fileRel] {
						skipped++
						return true
					}
					return false
				}
				err = copyDir(src, dst, prog, mainPath, skip)
				if skipped > 0 {
					fmt.Fprintf(os.Stderr, "copy_files: skipped %d tracked file(s) under %q (already in the worktree via git)\n", skipped, rel)
				}
			} else {
				err = copyFile(src, dst, info.Mode().Perm(), prog)
			}
			if err != nil {
				return err
			}
			prog.done()
			copied += prog.files
			copiedBytes += prog.bytes
		}
	}

	linked := 0
	for _, pat := range linkPatterns {
		if !filepath.IsLocal(pat) {
			return fmt.Errorf("link_files pattern %q must stay inside the repository", pat)
		}
		matches, err := filepath.Glob(filepath.Join(mainPath, pat))
		if err != nil {
			return fmt.Errorf("link_files pattern %q: %w", pat, err)
		}
		if len(matches) == 0 {
			fmt.Fprintf(os.Stderr, "link_files: no match for %q\n", pat)
			continue
		}
		for _, src := range matches {
			rel, err := filepath.Rel(mainPath, src)
			if err != nil {
				return err
			}
			if trackedUnder(tracked, rel) {
				fmt.Fprintf(os.Stderr, "link_files: skipping %q — git tracks it, and a link would shadow the checkout\n", rel)
				continue
			}
			dst := filepath.Join(worktreePath, rel)
			if _, err := os.Lstat(dst); err == nil {
				fmt.Fprintf(os.Stderr, "link_files: %q already exists in the worktree; not linking\n", rel)
				continue
			}
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				return err
			}
			if err := os.Symlink(src, dst); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Linked %s -> %s\n", rel, displayPath(src))
			linked++
		}
	}

	switch {
	case copied > 0 && linked > 0:
		fmt.Fprintf(os.Stderr, "Copied %d file(s) (%s), linked %d into %s\n", copied, formatSize(copiedBytes, ""), linked, displayPath(worktreePath))
	case copied > 0:
		fmt.Fprintf(os.Stderr, "Copied %d file(s) (%s) into %s\n", copied, formatSize(copiedBytes, ""), displayPath(worktreePath))
	case linked > 0:
		fmt.Fprintf(os.Stderr, "Linked %d into %s\n", linked, displayPath(worktreePath))
	}
	return nil
}

// trackedUnder reports whether rel itself or anything under it is tracked.
// ls-files paths use forward slashes, as does filepath.Rel on the
// platforms th supports.
func trackedUnder(tracked map[string]bool, rel string) bool {
	if tracked[rel] {
		return true
	}
	prefix := rel + "/"
	for p := range tracked {
		if strings.HasPrefix(p, prefix) {
			return true
		}
	}
	return false
}

// copyHooksTo copies the main worktree's effective hooks directory into the
// new worktree. With the default .git/hooks both worktrees resolve to the
// same directory (git shares it via the common git dir), so there is nothing
// to copy; a core.hooksPath inside the worktree resolves per worktree and
// needs the copy.
func copyHooksTo(mainPath, worktreePath string) error {
	src, err := gitx.HooksDir(mainPath)
	if err != nil {
		return err
	}
	dst, err := gitx.HooksDir(worktreePath)
	if err != nil {
		return err
	}
	if samePath(src, dst) {
		fmt.Fprintf(os.Stderr, "Hooks at %s are shared by all worktrees; nothing to copy\n", displayPath(src))
		return nil
	}
	if _, err := os.Stat(src); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "Hooks directory %s does not exist; nothing to copy\n", displayPath(src))
		return nil
	}
	if err := copyDir(src, dst, nil, "", nil); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "Copied hooks to %s\n", displayPath(dst))
	return nil
}

// copyDir copies regular files recursively, preserving permissions (hooks
// must stay executable) and skipping git's *.sample placeholders. prog may
// be nil. When skip is non-nil it is asked per file with the path relative
// to base (the main worktree); hook copying passes nil for both.
func copyDir(src, dst string, prog *copyProgress, base string, skip func(rel string) bool) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !d.Type().IsRegular() || strings.HasSuffix(p, ".sample") {
			return nil
		}
		if skip != nil {
			if baseRel, err := filepath.Rel(base, p); err == nil && skip(baseRel) {
				return nil
			}
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		return copyFile(p, target, info.Mode().Perm(), prog)
	})
}

// copyFile copies src to dst. On copy-on-write filesystems (APFS on macOS;
// Btrfs/XFS on Linux) the copy is a filesystem clone — a metadata-only
// operation, so big files land near-instantly with byte-identical output.
// Everywhere else it streams the bytes so large files don't sit in memory
// and prog (which may be nil) can tick while bytes move.
func copyFile(src, dst string, perm fs.FileMode, prog *copyProgress) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	// Any clone failure — dst already exists (darwin), cross-filesystem, no
	// CoW support — falls back silently: a real problem (unreadable src,
	// unwritable dst) fails again below with the streamed path's usual
	// error. A failed Stat also skips the clone so os.Open reports it.
	if info, err := os.Stat(src); err == nil && info.Mode().IsRegular() {
		if cloneFile(src, dst, perm) == nil {
			prog.advance(info.Size(), true)
			return nil
		}
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	if _, err := io.Copy(progressWriter{out, prog}, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	prog.advance(0, true)
	return nil
}

func init() {
	addCmd.Flags().StringVar(&addBase, "base", "", "base ref for a newly created branch (default: config default_base, then HEAD)")
	addCmd.Flags().BoolVar(&addNoPrefix, "no-prefix", false, "do not apply the configured branch_prefix")
	// Persistent so th add pr shares them; --base and --no-prefix stay above
	// because a PR checkout has no base and never gets the prefix.
	addCmd.PersistentFlags().StringVar(&addPath, "path", "", "override the config-derived worktree location")
	addCmd.PersistentFlags().BoolVar(&addCopyHooks, "copy-hooks", false, "copy the repo's git hooks into the new worktree")
	addCmd.PersistentFlags().BoolVar(&addNoCopyHooks, "no-copy-hooks", false, "do not copy hooks even if the config enables copy_hooks")
	addCmd.PersistentFlags().StringArrayVar(&addCopyFile, "copy-file", nil, "extra file or glob (relative to the main worktree) to copy into the new worktree; repeatable")
	addCmd.PersistentFlags().BoolVar(&addNoCopyFiles, "no-copy-files", false, "skip the config's copy_files list")
	addCmd.PersistentFlags().StringArrayVar(&addLinkFile, "link-file", nil, "extra file or glob (relative to the main worktree) to symlink into the new worktree; repeatable")
	addCmd.PersistentFlags().BoolVar(&addNoLinkFiles, "no-link-files", false, "skip the config's link_files list")
	addCmd.PersistentFlags().BoolVar(&addOpen, "open", false, "open the new worktree in VS Code")
	addCmd.PersistentFlags().BoolVar(&addNoOpen, "no-open", false, "do not open in VS Code even if the config enables vscode.open")
	addCmd.PersistentFlags().BoolVar(&addNoPreCreate, "no-pre-create", false, "skip the config's pre_create commands")
	addCmd.PersistentFlags().BoolVar(&addNoPostCreate, "no-post-create", false, "skip the config's post_create commands")
	addCmd.PersistentFlags().BoolVar(&addCD, "cd", false, "with th shell integration installed, cd into the new worktree (an effective VS Code open still wins)")
	addCmd.PersistentFlags().BoolVar(&addNoCD, "no-cd", false, "do not cd into the new worktree even if the config enables auto_cd")
	rootCmd.AddCommand(addCmd)
}
