// Package config loads treehouse's JSON configuration. The file location
// comes from $TH_CONFIG, defaulting to ~/.th/config.json; a missing file at
// the default location means built-in defaults apply.
package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// EnvVar overrides the config file location.
const EnvVar = "TH_CONFIG"

// homeConfigDirName is treehouse's directory under the home directory,
// holding the global config file and the trust store.
const homeConfigDirName = ".th"

// globalFileName is the global config file inside homeConfigDirName.
const globalFileName = "config.json"

// DefaultWorktreeDir places worktrees when no config sets worktree_dir.
const DefaultWorktreeDir = "~/worktrees/{repo}/{branch}"

// DefaultPrefixSeparator joins branch_prefix to the branch name when no
// config sets prefix_separator.
const DefaultPrefixSeparator = "/"

// Settings are the options that can be set globally and overridden per repo.
type Settings struct {
	// WorktreeDir is a path template; {repo} and {branch} are substituted
	// and a leading ~ expands to the home directory.
	WorktreeDir string `json:"worktree_dir,omitempty"`
	// DefaultBase is the ref new branches start from when the branch exists
	// neither locally nor on origin. Empty means the current HEAD.
	DefaultBase string `json:"default_base,omitempty"`
	// BranchPrefix is prepended to branch names that th add creates,
	// joined with PrefixSeparator: "peter" -> "peter/fix-login".
	BranchPrefix string `json:"branch_prefix,omitempty"`
	// PrefixSeparator joins BranchPrefix to the branch name. Empty means
	// DefaultPrefixSeparator.
	PrefixSeparator string `json:"prefix_separator,omitempty"`
	// CopyHooks copies the repo's git hooks into newly created worktrees.
	// Only relevant when core.hooksPath points inside the worktree (e.g.
	// husky's .husky); plain .git/hooks is already shared by all worktrees.
	// A pointer so a per-repo false can override a global true.
	CopyHooks *bool `json:"copy_hooks,omitempty"`
	// CopyFiles are paths or globs, relative to the main worktree, of
	// untracked files (e.g. ".env") to copy into newly created worktrees.
	// A repo entry's list replaces the global one; [] disables copying.
	CopyFiles []string `json:"copy_files,omitempty"`
	// LinkFiles are paths or globs, relative to the main worktree, of
	// untracked files or directories to symlink (rather than copy) into
	// newly created worktrees — e.g. a shared node_modules, so it costs
	// nothing and stays shared. A repo entry's list replaces the global
	// one; [] disables linking.
	LinkFiles []string `json:"link_files,omitempty"`
	// VSCode groups the VS Code integration's settings. A pointer so an
	// unset object stays out of the JSON entirely; layers merge it field by
	// field, so a .thrc setting one key keeps the rest of the inherited
	// object.
	VSCode *VSCode `json:"vscode,omitempty"`
	// FullPaths shows absolute paths in human-facing output instead of
	// abbreviating the home directory to ~. Same effect as --full-paths.
	FullPaths *bool `json:"full_paths,omitempty"`
	// AutoCD asks the th shell wrapper (see th shell-init) to cd into a
	// worktree that th add creates. Defaults to true — installing the
	// wrapper is the real opt-in; without TH_CD_FILE in the environment
	// the setting has no effect. It never applies when the add opens VS
	// Code (vscode.open or --open): the open wins and the terminal stays
	// put. A pointer so a per-repo false can override a global (or
	// default) true; nil means true.
	AutoCD *bool `json:"auto_cd,omitempty"`
	// PreCreate commands run in the main worktree before th add creates a
	// worktree, in order, via sh -c, with TH_WORKTREE pointing at the
	// not-yet-existing target. The first failure aborts the add. Worktree
	// metadata is passed as TH_* environment variables rather than
	// interpolated into the command. A repo entry's list replaces the
	// global one; [] disables.
	PreCreate []string `json:"pre_create,omitempty"`
	// PostCreate commands run inside a newly created worktree, in order,
	// via sh -c, like PreCreate. A failure is reported but the worktree
	// survives. A repo entry's list replaces the global one; [] disables.
	PostCreate []string `json:"post_create,omitempty"`
	// PreRemove commands run inside a worktree about to be removed, in
	// order, via sh -c, like PreCreate. The first failure blocks the
	// removal. A repo entry's list replaces the global one; [] disables.
	PreRemove []string `json:"pre_remove,omitempty"`
	// PostRemove commands run in the main worktree after a worktree is
	// removed, in order, via sh -c, with TH_WORKTREE pointing at the
	// now-deleted path. A failure is reported but the removal stands.
	// A repo entry's list replaces the global one; [] disables.
	PostRemove []string `json:"post_remove,omitempty"`
	// Run is the repository's one foreground command for th run (a dev
	// server, a test watcher), executed via sh -c in the current worktree's
	// root with the terminal's stdio attached. A single string, not a list:
	// it is one process to attach to, and compound commands come free via
	// sh -c. The TH_* variables reach it as environment variables, never
	// interpolated into the command. Empty falls through to the layer
	// below; a .thrc-supplied command needs approval before it runs.
	Run string `json:"run,omitempty"`
}

// VSCode is the VS Code integration's settings, the "vscode" object in
// config.json and .thrc.
type VSCode struct {
	// Open opens the worktree in VS Code after th add creates it.
	Open *bool `json:"open,omitempty"`
	// WorkspaceFile writes a .code-workspace file into each new worktree,
	// named "<workspace_prefix><branch>.code-workspace".
	WorkspaceFile *bool `json:"workspace_file,omitempty"`
	// WorkspacePrefix is prepended to the workspace file's name.
	WorkspacePrefix string `json:"workspace_prefix,omitempty"`
	// WindowTitle is written verbatim as the workspace file's
	// settings["window.title"], so VS Code title variables like
	// ${activeEditorShort} pass through. Empty means the repo name.
	WindowTitle string `json:"window_title,omitempty"`
	// WindowColor colors each worktree window's title bar and status bar
	// via workbench.colorCustomizations in the generated workspace file.
	// "auto" derives a stable color from the repo and branch; "#rrggbb"
	// uses that color everywhere. Empty means no coloring. Requires
	// workspace_file.
	WindowColor string `json:"window_color,omitempty"`
	// WorkspacePaths are extra folders added to generated .code-workspace
	// files after the worktree itself. A repo entry's list replaces the
	// global one.
	WorkspacePaths []WorkspacePath `json:"workspace_paths,omitempty"`
}

// WorkspacePath is one extra folder for generated .code-workspace files.
type WorkspacePath struct {
	// Name is the folder's display name in VS Code. Optional.
	Name string `json:"name,omitempty"`
	// Path is the folder's location; a leading ~ expands to the home
	// directory, anything else is written as given.
	Path string `json:"path"`
}

// RepoConfig is one repos entry: settings for the repository whose main
// worktree lives at Path.
type RepoConfig struct {
	// Name is what {repo} expands to in templates for this repo. Empty
	// means the directory basename of the main worktree.
	Name string `json:"name,omitempty"`
	// Path is the filesystem path of the repo's main worktree.
	Path string `json:"path"`
	Settings
}

// File is the full global-config schema: top-level defaults plus per-repo
// overrides matched by the main worktree's path.
type File struct {
	// Version is the config schema version; absent means 1. th stamps the
	// current version whenever it migrates the file (see migrate.go).
	Version int `json:"version"`
	Settings
	Repos []RepoConfig `json:"repos,omitempty"`
	// UpdateCheck lets th --version query GitHub for a newer release.
	// Deliberately top-level only — not part of Settings — so a repo's
	// .thrc can never turn on network calls.
	UpdateCheck *bool `json:"update_check,omitempty"`
}

// UpdateCheckEnabled reports whether update_check is set and true.
func (f *File) UpdateCheckEnabled() bool {
	return f.UpdateCheck != nil && *f.UpdateCheck
}

// LocalFileName is the repo-local config file, read from the root of a
// repository's main worktree only — never from a linked worktree.
const LocalFileName = ".thrc"

// LocalConfig is the .thrc schema: the same settings as a repos entry,
// for the repository the file lives in. It is parsed with unknown fields
// rejected, so "repos" and "path" — meaningless in a file that is itself
// the repo — fail loudly.
type LocalConfig struct {
	// Version is the config schema version; absent means 1. First field so
	// a file th writes leads with it.
	Version int `json:"version"`
	// Name is what {repo} expands to for this repo. It overrides the name
	// of a matching global repos entry. Empty means the entry's name, or
	// the directory basename of the main worktree.
	Name string `json:"name,omitempty"`
	Settings
}

// Path returns the config file location and whether it was set explicitly
// via $TH_CONFIG.
func Path() (path string, explicit bool, err error) {
	if p := os.Getenv(EnvVar); p != "" {
		return p, true, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", false, err
	}
	return filepath.Join(home, homeConfigDirName, globalFileName), false, nil
}

// Load reads the config file. A missing file at the default location is not
// an error; a missing file at an explicit $TH_CONFIG location is, so a typo'd
// path fails loudly instead of being silently ignored. A file written for an
// older schema is migrated and rewritten in place, backup first.
func Load() (*File, error) {
	path, explicit, err := Path()
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) && !explicit {
		return &File{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("config %s: %w", path, err)
	}
	migrated, pending, err := migratePending("config "+path, path, raw, globalMigrations)
	if err != nil {
		return nil, err
	}
	if pending != nil {
		// Backup first, then rename the migrated content over the file: a
		// failed backup leaves the original as the only, untouched copy.
		if _, err := pending.WriteBackup(); err != nil {
			return nil, err
		}
		if err := pending.Persist(); err != nil {
			return nil, err
		}
	}
	dec := json.NewDecoder(bytes.NewReader(migrated))
	dec.DisallowUnknownFields()
	var cfg File
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("config %s: %w", path, err)
	}
	if err := validateVSCode("config "+path, "", cfg.VSCode); err != nil {
		return nil, err
	}
	for i, r := range cfg.Repos {
		if r.Path == "" {
			return nil, fmt.Errorf("config %s: repos[%d] is missing \"path\"", path, i)
		}
		if err := validateVSCode("config "+path, fmt.Sprintf("repos[%d] ", i), r.VSCode); err != nil {
			return nil, err
		}
	}
	return &cfg, nil
}

// validateVSCode checks one vscode object: workspace_paths entries carry a
// "path", and window_color is "", "auto", or a 6-digit "#rrggbb" hex color
// (case-insensitive). A nil object has nothing to check. desc names the file
// ("config <path>" or "repo config <path>"), where locates the object within
// the file ("" for top level, "repos[N] " for an entry).
func validateVSCode(desc, where string, v *VSCode) error {
	if v == nil {
		return nil
	}
	for i, wp := range v.WorkspacePaths {
		if wp.Path == "" {
			return fmt.Errorf("%s: %svscode.workspace_paths[%d] is missing \"path\"", desc, where, i)
		}
	}
	if c := v.WindowColor; c != "" && c != "auto" && !isHexColor(c) {
		return fmt.Errorf("%s: %svscode.window_color %q is invalid; use \"auto\" or \"#rrggbb\"", desc, where, c)
	}
	return nil
}

// isHexColor reports whether v is "#" followed by exactly six hex digits.
func isHexColor(v string) bool {
	if len(v) != 7 || v[0] != '#' {
		return false
	}
	for _, c := range v[1:] {
		if !('0' <= c && c <= '9' || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F') {
			return false
		}
	}
	return true
}

// loadLocal reads <mainPath>/.thrc. A missing file is not an error; the
// returned path is reported either way, for provenance. Broken JSON and
// unknown fields fail loudly, like a broken global config.
//
// A file written for an older schema is migrated in memory only — the .thrc
// belongs to the repository, so rewriting it is the user's call. The
// migration comes back as a *PendingMigration (nil when the file was already
// current) for an interactive caller to persist.
func loadLocal(mainPath string) (*LocalConfig, string, *PendingMigration, error) {
	path := filepath.Join(mainPath, LocalFileName)
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, path, nil, nil
	}
	if err != nil {
		return nil, path, nil, fmt.Errorf("repo config %s: %w", path, err)
	}
	migrated, pending, err := migratePending("repo config "+path, path, raw, localMigrations)
	if err != nil {
		return nil, path, nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(migrated))
	dec.DisallowUnknownFields()
	var local LocalConfig
	if err := dec.Decode(&local); err != nil {
		return nil, path, nil, fmt.Errorf("repo config %s: %w", path, err)
	}
	if err := validateVSCode("repo config "+path, "", local.VSCode); err != nil {
		return nil, path, nil, err
	}
	return &local, path, pending, nil
}

// Resolved is the effective configuration for one repository, plus where the
// values came from.
type Resolved struct {
	Settings
	// RepoName is the .thrc name, else the global repos entry's name,
	// else "" (the caller falls back to the directory basename).
	RepoName string
	// LocalFile is the path of the .thrc that was loaded, "" if none.
	LocalFile string
	// LocalMigration is non-nil when the loaded .thrc predates the current
	// schema: the settings above are already migrated, the file on disk is
	// not. Interactive commands offer to persist it; everything else uses
	// the migrated settings for the run and leaves the file alone.
	LocalMigration *PendingMigration
	// RepoHooks records, per hook (by its JSON name — the lifecycle hooks
	// and the run command), whether the effective commands came from .thrc
	// rather than the user-owned global config, and so need approval before
	// they run. Nil when no .thrc was loaded.
	RepoHooks map[string]bool
}

// HookFromRepo reports whether the named hook's effective commands came
// from the repo's .thrc rather than the user-owned global config.
func (r Resolved) HookFromRepo(hook string) bool {
	return r.RepoHooks[hook]
}

// Source labels for Provenance: the layer an effective value came from.
const (
	SourceDefault  = "default"
	SourceTopLevel = "top-level"
	SourceLocal    = LocalFileName
)

// Provenance records which layer set each effective value.
type Provenance struct {
	// Fields maps a setting's JSON name to the label of the layer that
	// last set it: SourceTopLevel, "repos[N]", or SourceLocal. Settings
	// no layer set are absent — their values are built-in defaults.
	Fields map[string]string
	// ReposIndex is the index of the repos entry that matched, -1 if none.
	ReposIndex int
	// ReposPath is the matching repos entry's configured path, "" if none.
	ReposPath string
}

// Source returns the label of the layer that set the field with the given
// JSON name, SourceDefault when no layer did.
func (p Provenance) Source(field string) string {
	if s, ok := p.Fields[field]; ok {
		return s
	}
	return SourceDefault
}

// mark records label as the source of every field the layer explicitly
// sets, and of "name" when the layer names the repo.
func (p Provenance) mark(s Settings, name, label string) {
	for _, f := range s.setFields() {
		p.Fields[f] = label
	}
	if name != "" {
		p.Fields["name"] = label
	}
}

// Resolve returns the effective settings for the repository whose main
// worktree is at mainPath, layering built-in defaults, the global config's
// top-level settings, its matching repos entry, and finally the repo's own
// .thrc. Each layer overrides field by field: empty strings fall through,
// lists and booleans that are set replace the layer below.
func Resolve(mainPath string) (Resolved, error) {
	resolved, _, err := ResolveDetailed(mainPath)
	return resolved, err
}

// ResolveDetailed is Resolve, additionally reporting where each effective
// value came from.
func ResolveDetailed(mainPath string) (Resolved, Provenance, error) {
	prov := Provenance{Fields: map[string]string{}, ReposIndex: -1}
	cfg, err := Load()
	if err != nil {
		return Resolved{}, prov, err
	}
	settings, name, idx := cfg.forPath(mainPath)
	prov.mark(cfg.Settings, "", SourceTopLevel)
	if idx >= 0 {
		r := cfg.Repos[idx]
		prov.mark(r.Settings, r.Name, fmt.Sprintf("repos[%d]", idx))
		prov.ReposIndex, prov.ReposPath = idx, r.Path
	}
	resolved := Resolved{Settings: settings, RepoName: name}

	local, path, pending, err := loadLocal(mainPath)
	if err != nil {
		return Resolved{}, prov, err
	}
	if local == nil {
		return resolved, prov, nil
	}
	resolved.LocalFile = path
	resolved.LocalMigration = pending
	resolved.Settings.merge(local.Settings)
	prov.mark(local.Settings, local.Name, SourceLocal)
	if local.Name != "" {
		resolved.RepoName = local.Name
	}
	// An explicit [] clears the inherited commands and still counts as
	// repo-sourced, so the approval gate sees the repo's decision. run is a
	// string: only a non-empty value overrides, so only that counts.
	resolved.RepoHooks = map[string]bool{
		"pre_create":  local.PreCreate != nil,
		"post_create": local.PostCreate != nil,
		"pre_remove":  local.PreRemove != nil,
		"post_remove": local.PostRemove != nil,
		"run":         local.Run != "",
	}
	return resolved, prov, nil
}

// ResolveGlobal resolves only the repo-independent layers — built-in
// defaults and the config file's top-level settings — for use outside any
// repository.
func ResolveGlobal() (Resolved, Provenance, error) {
	prov := Provenance{Fields: map[string]string{}, ReposIndex: -1}
	cfg, err := Load()
	if err != nil {
		return Resolved{}, prov, err
	}
	s := Settings{WorktreeDir: DefaultWorktreeDir}
	s.merge(cfg.Settings)
	prov.mark(cfg.Settings, "", SourceTopLevel)
	return Resolved{Settings: s}, prov, nil
}

// ForPath returns the effective settings for the repository whose main
// worktree is at mainPath: built-in defaults, overlaid with the file's
// top-level settings, overlaid with the first repos entry whose path matches.
// The second return is the matching entry's name ("" when unnamed or no
// entry matches).
func (f *File) ForPath(mainPath string) (Settings, string) {
	s, name, _ := f.forPath(mainPath)
	return s, name
}

// forPath is ForPath, also returning the index of the repos entry that
// matched (-1 if none), for provenance.
func (f *File) forPath(mainPath string) (Settings, string, int) {
	s := Settings{WorktreeDir: DefaultWorktreeDir}
	s.merge(f.Settings)
	target := normalizePath(mainPath)
	for i, r := range f.Repos {
		if normalizePath(r.Path) == target {
			s.merge(r.Settings)
			return s, r.Name, i
		}
	}
	return s, "", -1
}

func (s *Settings) merge(over Settings) {
	if over.WorktreeDir != "" {
		s.WorktreeDir = over.WorktreeDir
	}
	if over.DefaultBase != "" {
		s.DefaultBase = over.DefaultBase
	}
	if over.BranchPrefix != "" {
		s.BranchPrefix = over.BranchPrefix
	}
	if over.PrefixSeparator != "" {
		s.PrefixSeparator = over.PrefixSeparator
	}
	if over.CopyHooks != nil {
		s.CopyHooks = over.CopyHooks
	}
	if over.CopyFiles != nil {
		s.CopyFiles = over.CopyFiles
	}
	if over.LinkFiles != nil {
		s.LinkFiles = over.LinkFiles
	}
	if over.VSCode != nil {
		// s.VSCode may alias a lower layer's struct; never mutate through it.
		merged := VSCode{}
		if s.VSCode != nil {
			merged = *s.VSCode
		}
		merged.merge(*over.VSCode)
		s.VSCode = &merged
	}
	if over.FullPaths != nil {
		s.FullPaths = over.FullPaths
	}
	if over.AutoCD != nil {
		s.AutoCD = over.AutoCD
	}
	if over.PreCreate != nil {
		s.PreCreate = over.PreCreate
	}
	if over.PostCreate != nil {
		s.PostCreate = over.PostCreate
	}
	if over.PreRemove != nil {
		s.PreRemove = over.PreRemove
	}
	if over.PostRemove != nil {
		s.PostRemove = over.PostRemove
	}
	if over.Run != "" {
		s.Run = over.Run
	}
}

// merge overlays over onto v field by field, with the same conditions
// Settings.merge uses, so a layer setting one vscode key keeps the rest of
// the object it inherited.
func (v *VSCode) merge(over VSCode) {
	if over.Open != nil {
		v.Open = over.Open
	}
	if over.WorkspaceFile != nil {
		v.WorkspaceFile = over.WorkspaceFile
	}
	if over.WorkspacePrefix != "" {
		v.WorkspacePrefix = over.WorkspacePrefix
	}
	if over.WindowTitle != "" {
		v.WindowTitle = over.WindowTitle
	}
	if over.WindowColor != "" {
		v.WindowColor = over.WindowColor
	}
	if over.WorkspacePaths != nil {
		v.WorkspacePaths = over.WorkspacePaths
	}
}

// setFields lists the JSON names of the fields s explicitly sets — the
// same per-field conditions merge uses to let a layer override the one
// below. A test keeps the two (and the Settings struct) in sync.
func (s Settings) setFields() []string {
	var fields []string
	set := func(name string, isSet bool) {
		if isSet {
			fields = append(fields, name)
		}
	}
	set("worktree_dir", s.WorktreeDir != "")
	set("default_base", s.DefaultBase != "")
	set("branch_prefix", s.BranchPrefix != "")
	set("prefix_separator", s.PrefixSeparator != "")
	set("copy_hooks", s.CopyHooks != nil)
	set("copy_files", s.CopyFiles != nil)
	set("link_files", s.LinkFiles != nil)
	// Dotted names, matching the file shape users write. An all-zero
	// vscode object sets nothing — the mirror image of merge.
	if v := s.VSCode; v != nil {
		set("vscode.open", v.Open != nil)
		set("vscode.workspace_file", v.WorkspaceFile != nil)
		set("vscode.workspace_prefix", v.WorkspacePrefix != "")
		set("vscode.window_title", v.WindowTitle != "")
		set("vscode.window_color", v.WindowColor != "")
		set("vscode.workspace_paths", v.WorkspacePaths != nil)
	}
	set("full_paths", s.FullPaths != nil)
	set("auto_cd", s.AutoCD != nil)
	set("pre_create", s.PreCreate != nil)
	set("post_create", s.PostCreate != nil)
	set("pre_remove", s.PreRemove != nil)
	set("post_remove", s.PostRemove != nil)
	set("run", s.Run != "")
	return fields
}

// FullPathsEnabled reports whether full_paths is set and true.
// AutoCDEnabled reports whether auto_cd is enabled. Unlike the other
// boolean settings this defaults to true when unset — the real opt-in is
// installing the th shell wrapper (th shell-init).
func (s Settings) AutoCDEnabled() bool {
	return s.AutoCD == nil || *s.AutoCD
}

func (s Settings) FullPathsEnabled() bool {
	return s.FullPaths != nil && *s.FullPaths
}

// CopyHooksEnabled reports whether copy_hooks is set and true.
func (s Settings) CopyHooksEnabled() bool {
	return s.CopyHooks != nil && *s.CopyHooks
}

// VSCodeSettings returns the effective vscode object, the zero value when
// no layer set one — so callers read the fields without a nil check.
func (s Settings) VSCodeSettings() VSCode {
	if s.VSCode == nil {
		return VSCode{}
	}
	return *s.VSCode
}

// VSCodeOpenEnabled reports whether vscode.open is set and true.
func (s Settings) VSCodeOpenEnabled() bool {
	return s.VSCode != nil && s.VSCode.Open != nil && *s.VSCode.Open
}

// VSCodeWorkspaceFileEnabled reports whether vscode.workspace_file is set
// and true.
func (s Settings) VSCodeWorkspaceFileEnabled() bool {
	return s.VSCode != nil && s.VSCode.WorkspaceFile != nil && *s.VSCode.WorkspaceFile
}

// EffectivePrefix returns BranchPrefix with the separator applied, e.g.
// "peter" -> "peter/". A prefix already ending in the separator is not
// doubled. Empty if no prefix is configured.
func (s Settings) EffectivePrefix() string {
	if s.BranchPrefix == "" {
		return ""
	}
	sep := s.PrefixSeparator
	if sep == "" {
		sep = DefaultPrefixSeparator
	}
	return strings.TrimSuffix(s.BranchPrefix, sep) + sep
}

// WorktreePath expands the WorktreeDir template for repo and branch.
func (s Settings) WorktreePath(repo, branch string) (string, error) {
	p := s.WorktreeDir
	p = strings.ReplaceAll(p, "{repo}", repo)
	p = strings.ReplaceAll(p, "{branch}", SanitizeBranch(branch))
	p, err := ExpandTilde(p)
	if err != nil {
		return "", err
	}
	return filepath.Clean(p), nil
}

// SanitizeBranch makes a branch name safe to use as a single path segment.
func SanitizeBranch(branch string) string {
	return strings.ReplaceAll(branch, "/", "-")
}

func ExpandTilde(p string) (string, error) {
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, strings.TrimPrefix(p, "~")), nil
	}
	return p, nil
}

// normalizePath makes paths comparable: ~ expanded, absolute, symlinks
// resolved. Best-effort — a path that can't be resolved is used as-is.
func normalizePath(p string) string {
	if e, err := ExpandTilde(p); err == nil {
		p = e
	}
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		p = resolved
	}
	return filepath.Clean(p)
}
