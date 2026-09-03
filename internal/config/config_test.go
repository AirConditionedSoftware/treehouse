package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func boolPtr(b bool) *bool { return &b }

// noRepoHooks is the RepoHooks map a .thrc that sets no hooks resolves to;
// repoHooks marks the named hooks as repo-sourced on top of it.
func noRepoHooks() map[string]bool {
	return map[string]bool{"pre_create": false, "post_create": false, "pre_remove": false, "post_remove": false, "run": false}
}

func repoHooks(hooks ...string) map[string]bool {
	m := noRepoHooks()
	for _, h := range hooks {
		m[h] = true
	}
	return m
}

func TestPathExplicit(t *testing.T) {
	t.Setenv(EnvVar, "/somewhere/custom.json")
	p, explicit, err := Path()
	if err != nil {
		t.Fatal(err)
	}
	if p != "/somewhere/custom.json" || !explicit {
		t.Errorf("Path() = %q, explicit=%v; want /somewhere/custom.json, true", p, explicit)
	}
}

func TestPathDefault(t *testing.T) {
	t.Setenv(EnvVar, "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	p, explicit, err := Path()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, ".th", "config.json")
	if p != want || explicit {
		t.Errorf("Path() = %q, explicit=%v; want %q, false", p, explicit, want)
	}
}

func TestLoadMissingDefaultIsEmpty(t *testing.T) {
	t.Setenv(EnvVar, "")
	t.Setenv("HOME", t.TempDir())
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() with no config file: %v", err)
	}
	if cfg.WorktreeDir != "" || cfg.DefaultBase != "" || len(cfg.Repos) != 0 {
		t.Errorf("Load() = %+v; want empty config", cfg)
	}
}

func TestLoadMissingExplicitFails(t *testing.T) {
	t.Setenv(EnvVar, filepath.Join(t.TempDir(), "nope.json"))
	if _, err := Load(); err == nil {
		t.Error("Load() with missing $TH_CONFIG file should fail")
	}
}

func TestLoadUnknownFieldFails(t *testing.T) {
	p := filepath.Join(t.TempDir(), "th.json")
	if err := os.WriteFile(p, []byte(`{"worktre_dir": "/x"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(EnvVar, p)
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "worktre_dir") {
		t.Errorf("Load() with unknown field: err = %v; want unknown-field error", err)
	}
}

func TestLoadAndFor(t *testing.T) {
	p := filepath.Join(t.TempDir(), "th.json")
	content := `{
  "version": 2,
  "worktree_dir": "/global/{repo}/{branch}",
  "default_base": "main",
  "branch_prefix": "peter",
  "copy_hooks": true,
  "copy_files": [".env"],
  "repos": [
    {"name": "myapp", "path": "/code/myapp", "worktree_dir": "/special/{branch}", "default_base": "develop", "branch_prefix": "team", "prefix_separator": "_", "copy_hooks": false, "copy_files": [], "vscode": {"open": true, "workspace_file": true, "workspace_prefix": "acs-", "window_title": "myapp ${activeEditorShort}", "window_color": "auto", "workspace_paths": [{"name": "docs", "path": "~/notes/myapp"}, {"path": "/shared/lib"}]}, "full_paths": true, "post_create": ["make setup"]},
    {"path": "/code/partial", "default_base": "trunk"}
  ]
}`
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(EnvVar, p)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		path     string
		want     Settings
		wantName string
	}{
		{"/code/myapp", Settings{WorktreeDir: "/special/{branch}", DefaultBase: "develop", BranchPrefix: "team", PrefixSeparator: "_", CopyHooks: boolPtr(false), CopyFiles: []string{}, VSCode: &VSCode{Open: boolPtr(true), WorkspaceFile: boolPtr(true), WorkspacePrefix: "acs-", WindowTitle: "myapp ${activeEditorShort}", WindowColor: "auto", WorkspacePaths: []WorkspacePath{{Name: "docs", Path: "~/notes/myapp"}, {Path: "/shared/lib"}}}, FullPaths: boolPtr(true), PostCreate: []string{"make setup"}}, "myapp"},
		{"/code/partial", Settings{WorktreeDir: "/global/{repo}/{branch}", DefaultBase: "trunk", BranchPrefix: "peter", CopyHooks: boolPtr(true), CopyFiles: []string{".env"}}, ""},
		{"/code/unlisted", Settings{WorktreeDir: "/global/{repo}/{branch}", DefaultBase: "main", BranchPrefix: "peter", CopyHooks: boolPtr(true), CopyFiles: []string{".env"}}, ""},
	}
	for _, tt := range tests {
		got, name := cfg.ForPath(tt.path)
		if !reflect.DeepEqual(got, tt.want) || name != tt.wantName {
			t.Errorf("ForPath(%q) = %+v, %q; want %+v, %q", tt.path, got, name, tt.want, tt.wantName)
		}
	}
	if s, _ := cfg.ForPath("/code/myapp"); s.CopyHooksEnabled() {
		t.Error("myapp copy_hooks=false should override the global true")
	}
	if s, _ := cfg.ForPath("/code/unlisted"); !s.CopyHooksEnabled() {
		t.Error("unlisted repo should inherit the global copy_hooks=true")
	}
	if s, _ := cfg.ForPath("/code/myapp"); len(s.CopyFiles) != 0 {
		t.Errorf("myapp copy_files=[] should clear the inherited list, got %v", s.CopyFiles)
	}
	if s, _ := cfg.ForPath("/code/myapp"); !s.VSCodeOpenEnabled() || !s.VSCodeWorkspaceFileEnabled() {
		t.Error("myapp should have vscode.open and vscode.workspace_file enabled")
	}
	if s, _ := cfg.ForPath("/code/unlisted"); s.VSCodeOpenEnabled() || s.VSCodeWorkspaceFileEnabled() {
		t.Error("unlisted repo should not have vscode options enabled")
	}
}

func TestUpdateCheckTopLevelOnly(t *testing.T) {
	p := filepath.Join(t.TempDir(), "th.json")
	if err := os.WriteFile(p, []byte(`{"version": 2, "update_check": true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(EnvVar, p)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.UpdateCheckEnabled() {
		t.Error("update_check: true should enable the check")
	}
	if (&File{}).UpdateCheckEnabled() {
		t.Error("update check must be off by default")
	}

	// A repo's .thrc must not be able to switch on network calls.
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, LocalFileName), []byte(`{"update_check": true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Resolve(repo); err == nil || !strings.Contains(err.Error(), "update_check") {
		t.Errorf("Resolve with update_check in .thrc: err = %v; want unknown-field rejection", err)
	}
}

func TestForPathBuiltinDefaults(t *testing.T) {
	cfg := &File{}
	got, name := cfg.ForPath("/nowhere")
	if got.WorktreeDir != DefaultWorktreeDir || got.DefaultBase != "" || name != "" {
		t.Errorf("ForPath() on empty config = %+v, %q", got, name)
	}
}

func TestLoadWorkspacePathMissingPathFails(t *testing.T) {
	p := filepath.Join(t.TempDir(), "th.json")
	content := `{"version": 2, "repos": [{"path": "/x", "vscode": {"workspace_paths": [{"name": "docs"}]}}]}`
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(EnvVar, p)
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "vscode.workspace_paths") {
		t.Errorf("Load() with pathless workspace_paths entry: err = %v; want missing-path error", err)
	}
}

func TestLoadWindowColorValidation(t *testing.T) {
	valid := []string{`{"version": 2}`, `{"version": 2, "vscode": {"window_color": "auto"}}`, `{"version": 2, "vscode": {"window_color": "#aabbcc"}}`, `{"version": 2, "vscode": {"window_color": "#AABBCC"}}`}
	for _, content := range valid {
		p := filepath.Join(t.TempDir(), "th.json")
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Setenv(EnvVar, p)
		if _, err := Load(); err != nil {
			t.Errorf("Load() with %s: unexpected error %v", content, err)
		}
	}

	invalid := []string{"red", "#12", "#gggggg", "#aabbccdd", "Auto"}
	for _, v := range invalid {
		p := filepath.Join(t.TempDir(), "th.json")
		if err := os.WriteFile(p, []byte(`{"version": 2, "vscode": {"window_color": "`+v+`"}}`), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Setenv(EnvVar, p)
		if _, err := Load(); err == nil || !strings.Contains(err.Error(), "vscode.window_color") {
			t.Errorf("Load() with vscode.window_color %q: err = %v; want a vscode.window_color error", v, err)
		}
	}

	// The repos-entry and .thrc sites validate too.
	p := filepath.Join(t.TempDir(), "th.json")
	if err := os.WriteFile(p, []byte(`{"version": 2, "repos": [{"path": "/x", "vscode": {"window_color": "red"}}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(EnvVar, p)
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), `repos[0] vscode.window_color`) {
		t.Errorf("Load() with invalid repos entry color: err = %v; want repos[0] vscode.window_color error", err)
	}

	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, LocalFileName), []byte(`{"version": 2, "vscode": {"window_color": "red"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(`{"version": 2}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Resolve(repo); err == nil || !strings.Contains(err.Error(), "repo config") || !strings.Contains(err.Error(), "vscode.window_color") {
		t.Errorf("Resolve with invalid .thrc color: err = %v; want repo config vscode.window_color error", err)
	}
}

func TestLoadRepoMissingPathFails(t *testing.T) {
	p := filepath.Join(t.TempDir(), "th.json")
	if err := os.WriteFile(p, []byte(`{"repos": [{"name": "myapp"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(EnvVar, p)
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "path") {
		t.Errorf("Load() with pathless repos entry: err = %v; want missing-path error", err)
	}
}

// writeGlobalForRepo writes a global config whose repos entry matches main,
// points $TH_CONFIG at it, and returns the settings that entry resolves to
// with no .thrc present.
func writeGlobalForRepo(t *testing.T, main string) Settings {
	t.Helper()
	p := filepath.Join(t.TempDir(), "th.json")
	content := fmt.Sprintf(`{
  "version": 2,
  "worktree_dir": "/global/{repo}/{branch}",
  "default_base": "main",
  "branch_prefix": "peter",
  "copy_hooks": true,
  "copy_files": [".env"],
  "repos": [
    {"name": "entryname", "path": %q, "default_base": "develop", "prefix_separator": "_", "copy_files": [".env", ".env.local"], "vscode": {"window_color": "auto"}, "post_create": ["global setup"], "run": "make dev"}
  ]
}`, main)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(EnvVar, p)
	return Settings{
		WorktreeDir:     "/global/{repo}/{branch}",
		DefaultBase:     "develop",
		BranchPrefix:    "peter",
		PrefixSeparator: "_",
		CopyHooks:       boolPtr(true),
		CopyFiles:       []string{".env", ".env.local"},
		VSCode:          &VSCode{WindowColor: "auto"},
		PostCreate:      []string{"global setup"},
		Run:             "make dev",
	}
}

func TestResolveLayers(t *testing.T) {
	main := t.TempDir()
	entry := writeGlobalForRepo(t, main)
	localPath := filepath.Join(main, LocalFileName)

	tests := []struct {
		name  string
		local string // .thrc content; "" means no file
		want  Resolved
	}{
		{
			name:  "no local file matches ForPath",
			local: "",
			want:  Resolved{Settings: entry, RepoName: "entryname"},
		},
		{
			name:  "empty local file changes nothing but provenance",
			local: `{"version": 2}`,
			want:  Resolved{Settings: entry, RepoName: "entryname", LocalFile: localPath, RepoHooks: noRepoHooks()},
		},
		{
			name:  "local overrides global top-level and repos entry",
			local: `{"version": 2, "name": "localname", "worktree_dir": "/local/{branch}", "branch_prefix": "team", "copy_hooks": false, "copy_files": ["only.local"], "vscode": {"workspace_file": true}, "post_create": ["npm ci"]}`,
			want: Resolved{
				Settings: Settings{
					WorktreeDir:     "/local/{branch}",
					DefaultBase:     "develop", // from the repos entry
					BranchPrefix:    "team",
					PrefixSeparator: "_", // from the repos entry
					CopyHooks:       boolPtr(false),
					CopyFiles:       []string{"only.local"},
					// Deep-merged: workspace_file from .thrc, window_color
					// still the repos entry's.
					VSCode:     &VSCode{WorkspaceFile: boolPtr(true), WindowColor: "auto"},
					PostCreate: []string{"npm ci"},
					Run:        "make dev", // from the repos entry
				},
				RepoName:  "localname",
				LocalFile: localPath,
				RepoHooks: repoHooks("post_create"),
			},
		},
		{
			name:  "empty post_create list clears and still counts as repo-sourced",
			local: `{"version": 2, "post_create": []}`,
			want: Resolved{
				Settings: Settings{
					WorktreeDir:     entry.WorktreeDir,
					DefaultBase:     entry.DefaultBase,
					BranchPrefix:    entry.BranchPrefix,
					PrefixSeparator: entry.PrefixSeparator,
					CopyHooks:       boolPtr(true),
					CopyFiles:       []string{".env", ".env.local"},
					VSCode:          entry.VSCode,
					PostCreate:      []string{},
					Run:             "make dev",
				},
				RepoName:  "entryname",
				LocalFile: localPath,
				RepoHooks: repoHooks("post_create"),
			},
		},
		{
			name:  "lifecycle hooks from .thrc set RepoHooks per hook",
			local: `{"version": 2, "pre_create": ["check-vpn"], "pre_remove": ["docker compose down"], "post_remove": []}`,
			want: Resolved{
				Settings: Settings{
					WorktreeDir:     entry.WorktreeDir,
					DefaultBase:     entry.DefaultBase,
					BranchPrefix:    entry.BranchPrefix,
					PrefixSeparator: entry.PrefixSeparator,
					CopyHooks:       boolPtr(true),
					CopyFiles:       []string{".env", ".env.local"},
					VSCode:          entry.VSCode,
					PreCreate:       []string{"check-vpn"},
					PostCreate:      []string{"global setup"}, // from the repos entry
					PreRemove:       []string{"docker compose down"},
					PostRemove:      []string{},
					Run:             "make dev", // from the repos entry
				},
				RepoName:  "entryname",
				LocalFile: localPath,
				RepoHooks: repoHooks("pre_create", "pre_remove", "post_remove"),
			},
		},
		{
			name:  "run from .thrc overrides and is repo-sourced",
			local: `{"version": 2, "run": "npm run dev"}`,
			want: Resolved{
				Settings: Settings{
					WorktreeDir:     entry.WorktreeDir,
					DefaultBase:     entry.DefaultBase,
					BranchPrefix:    entry.BranchPrefix,
					PrefixSeparator: entry.PrefixSeparator,
					CopyHooks:       boolPtr(true),
					CopyFiles:       []string{".env", ".env.local"},
					VSCode:          entry.VSCode,
					PostCreate:      []string{"global setup"},
					Run:             "npm run dev",
				},
				RepoName:  "entryname",
				LocalFile: localPath,
				RepoHooks: repoHooks("run"),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.local == "" {
				if err := os.RemoveAll(localPath); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(localPath, []byte(tt.local), 0o644); err != nil {
				t.Fatal(err)
			}
			got, err := Resolve(main)
			if err != nil {
				t.Fatalf("Resolve(%q): %v", main, err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Resolve() = %+v; want %+v", got, tt.want)
			}
		})
	}
}

func TestResolveWithoutLocalMatchesForPath(t *testing.T) {
	main := t.TempDir()
	writeGlobalForRepo(t, main)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	want, wantName := cfg.ForPath(main)
	got, err := Resolve(main)
	if err != nil {
		t.Fatalf("Resolve(%q): %v", main, err)
	}
	if !reflect.DeepEqual(got.Settings, want) || got.RepoName != wantName {
		t.Errorf("Resolve() = %+v, %q; want ForPath result %+v, %q", got.Settings, got.RepoName, want, wantName)
	}
	if got.LocalFile != "" || got.RepoHooks != nil {
		t.Errorf("Resolve() with no .thrc: LocalFile = %q, RepoHooks = %v; want \"\", nil", got.LocalFile, got.RepoHooks)
	}
}

func TestResolveNoGlobalConfig(t *testing.T) {
	t.Setenv(EnvVar, "")
	t.Setenv("HOME", t.TempDir())
	main := t.TempDir()
	content := `{"version": 2, "name": "solo", "branch_prefix": "team", "post_create": ["make setup"]}`
	if err := os.WriteFile(filepath.Join(main, LocalFileName), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Resolve(main)
	if err != nil {
		t.Fatalf("Resolve(%q): %v", main, err)
	}
	want := Resolved{
		Settings:  Settings{WorktreeDir: DefaultWorktreeDir, BranchPrefix: "team", PostCreate: []string{"make setup"}},
		RepoName:  "solo",
		LocalFile: filepath.Join(main, LocalFileName),
		RepoHooks: repoHooks("post_create"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Resolve() = %+v; want %+v", got, want)
	}
}

func TestResolveLocalErrors(t *testing.T) {
	tests := []struct {
		name, content, wantSubstr string
	}{
		{"repos key rejected", `{"repos": [{"path": "/x"}]}`, "repos"},
		{"path key rejected", `{"path": "/x"}`, "path"},
		{"unknown field rejected", `{"worktre_dir": "/x"}`, "worktre_dir"},
		{"malformed json", `{"branch_prefix": `, "repo config"},
		{"pathless workspace_paths", `{"version": 2, "vscode": {"workspace_paths": [{"name": "docs"}]}}`, "vscode.workspace_paths"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(EnvVar, "")
			t.Setenv("HOME", t.TempDir())
			main := t.TempDir()
			if err := os.WriteFile(filepath.Join(main, LocalFileName), []byte(tt.content), 0o644); err != nil {
				t.Fatal(err)
			}
			_, err := Resolve(main)
			if err == nil {
				t.Fatalf("Resolve() with %s should fail", tt.content)
			}
			if !strings.Contains(err.Error(), tt.wantSubstr) || !strings.Contains(err.Error(), LocalFileName) {
				t.Errorf("Resolve() err = %v; want it to mention %q and %q", err, tt.wantSubstr, LocalFileName)
			}
		})
	}
}

func TestResolveGlobalConfigErrorPropagates(t *testing.T) {
	t.Setenv(EnvVar, filepath.Join(t.TempDir(), "nope.json"))
	if _, err := Resolve(t.TempDir()); err == nil {
		t.Error("Resolve() with a missing $TH_CONFIG file should fail")
	}
}

func TestEffectivePrefix(t *testing.T) {
	tests := []struct {
		prefix, sep, want string
	}{
		{"", "", ""},
		{"", "-", ""},
		{"peter", "", "peter/"},
		{"peter/", "", "peter/"},
		{"peter", "-", "peter-"},
		{"peter-", "-", "peter-"},
		{"team", "_", "team_"},
	}
	for _, tt := range tests {
		s := Settings{BranchPrefix: tt.prefix, PrefixSeparator: tt.sep}
		if got := s.EffectivePrefix(); got != tt.want {
			t.Errorf("EffectivePrefix(prefix=%q, sep=%q) = %q; want %q", tt.prefix, tt.sep, got, tt.want)
		}
	}
}

func TestWorktreePath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	tests := []struct {
		tmpl, repo, branch, want string
	}{
		{"/trees/{repo}/{branch}", "myapp", "fix-login", "/trees/myapp/fix-login"},
		{"/trees/{repo}/{branch}", "myapp", "feature/login", "/trees/myapp/feature-login"},
		{"~/trees/{branch}", "myapp", "x", filepath.Join(home, "trees", "x")},
	}
	for _, tt := range tests {
		got, err := Settings{WorktreeDir: tt.tmpl}.WorktreePath(tt.repo, tt.branch)
		if err != nil {
			t.Fatalf("WorktreePath(%q, %q, %q): %v", tt.tmpl, tt.repo, tt.branch, err)
		}
		if got != tt.want {
			t.Errorf("WorktreePath(%q, %q, %q) = %q; want %q", tt.tmpl, tt.repo, tt.branch, got, tt.want)
		}
	}
}

func TestResolveDetailedProvenance(t *testing.T) {
	main := t.TempDir()
	writeGlobalForRepo(t, main)
	local := `{"version": 2, "name": "localname", "branch_prefix": "team", "copy_files": ["only.local"]}`
	if err := os.WriteFile(filepath.Join(main, LocalFileName), []byte(local), 0o644); err != nil {
		t.Fatal(err)
	}

	resolved, prov, err := ResolveDetailed(main)
	if err != nil {
		t.Fatalf("ResolveDetailed(%q): %v", main, err)
	}
	plain, err := Resolve(main)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(resolved, plain) {
		t.Errorf("ResolveDetailed settings = %+v; want Resolve's %+v", resolved, plain)
	}
	if prov.ReposIndex != 0 || prov.ReposPath != main {
		t.Errorf("matched entry = repos[%d] (%q); want repos[0] (%q)", prov.ReposIndex, prov.ReposPath, main)
	}
	wantFields := map[string]string{
		"name":                SourceLocal, // .thrc name over the entry's
		"worktree_dir":        SourceTopLevel,
		"default_base":        "repos[0]",
		"branch_prefix":       SourceLocal, // over the top-level "peter"
		"prefix_separator":    "repos[0]",
		"copy_hooks":          SourceTopLevel,
		"copy_files":          SourceLocal, // over the entry's list
		"vscode.window_color": "repos[0]",
		"post_create":         "repos[0]",
		"run":                 "repos[0]",
	}
	if !reflect.DeepEqual(prov.Fields, wantFields) {
		t.Errorf("Fields = %v; want %v", prov.Fields, wantFields)
	}
	if got := prov.Source("vscode.open"); got != SourceDefault {
		t.Errorf("Source(vscode.open) = %q; want %q", got, SourceDefault)
	}
}

func TestResolveGlobal(t *testing.T) {
	p := filepath.Join(t.TempDir(), "th.json")
	if err := os.WriteFile(p, []byte(`{"default_base": "main"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(EnvVar, p)
	resolved, prov, err := ResolveGlobal()
	if err != nil {
		t.Fatal(err)
	}
	want := Resolved{Settings: Settings{WorktreeDir: DefaultWorktreeDir, DefaultBase: "main"}}
	if !reflect.DeepEqual(resolved, want) {
		t.Errorf("ResolveGlobal() = %+v; want %+v", resolved, want)
	}
	if !reflect.DeepEqual(prov.Fields, map[string]string{"default_base": SourceTopLevel}) {
		t.Errorf("Fields = %v; want default_base from top-level only", prov.Fields)
	}
	if prov.ReposIndex != -1 {
		t.Errorf("ReposIndex = %d; want -1", prov.ReposIndex)
	}
}

// useGlobalMigrations and useLocalMigrations swap a registry for the
// duration of one test, restoring it afterwards. Both registries ship empty,
// so a fake step is the only way to exercise the migrate-on-load paths.
func useGlobalMigrations(t *testing.T, steps []migration) {
	t.Helper()
	saved := globalMigrations
	globalMigrations = steps
	t.Cleanup(func() { globalMigrations = saved })
}

func useLocalMigrations(t *testing.T, steps []migration) {
	t.Helper()
	saved := localMigrations
	localMigrations = steps
	t.Cleanup(func() { localMigrations = saved })
}

// renameStep is a fake v1 -> v2 step: it renames a key, so the pre-migration
// document has a field the strict decoder would reject. Migrating before
// decoding is the whole point.
func renameStep(from, to string) migration {
	return migration{from: 1, apply: func(m map[string]any) (map[string]any, error) {
		if v, ok := m[from]; ok {
			delete(m, from)
			m[to] = v
		}
		return m, nil
	}}
}

func TestLoadMigratesAndBacksUp(t *testing.T) {
	useGlobalMigrations(t, []migration{renameStep("old_base", "default_base")})
	dir := t.TempDir()
	p := filepath.Join(dir, "th.json")
	original := []byte(`{"old_base": "develop"}`)
	if err := os.WriteFile(p, original, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(EnvVar, p)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load(): %v", err)
	}
	if cfg.Version != 2 || cfg.DefaultBase != "develop" {
		t.Errorf("Load() = %+v; want version 2 and the migrated default_base", cfg)
	}

	// The file is rewritten in place, stamped, in the engine's format.
	want := "{\n  \"default_base\": \"develop\",\n  \"version\": 2\n}\n"
	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Errorf("migrated config =\n%q\nwant\n%q", got, want)
	}
	if fi, err := os.Stat(p); err != nil {
		t.Fatal(err)
	} else if fi.Mode().Perm() != 0o600 {
		t.Errorf("migrated config mode = %v; want the original 0600", fi.Mode().Perm())
	}

	// The backup lands next to the file — in the $TH_CONFIG directory, not
	// wherever ~/.th happens to be.
	backups, err := filepath.Glob(p + ".v1.*.bak")
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 1 {
		t.Fatalf("backups = %v; want exactly one %s.v1.<ts>.bak", backups, filepath.Base(p))
	}
	if b, err := os.ReadFile(backups[0]); err != nil {
		t.Fatal(err)
	} else if string(b) != string(original) {
		t.Errorf("backup = %q; want the pre-migration bytes %q", b, original)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Errorf("directory holds %d entries; want the config and its backup only", len(entries))
	}
}

// TestLoadMigratesV1FlatVSCodeConfig is the real registry end to end: a
// version-absent config with the flat VS Code keys, as every config in the
// wild is written, comes back nested and is rewritten on disk, original
// safely backed up.
func TestLoadMigratesV1FlatVSCodeConfig(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "th.json")
	original := []byte(`{"branch_prefix": "peter", "vscode_open": true, "vscode_window_color": "auto", "repos": [{"path": "/code/myapp", "vscode_workspace_file": true, "workspace_paths": [{"path": "/shared/lib"}]}]}`)
	if err := os.WriteFile(p, original, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(EnvVar, p)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load(): %v", err)
	}
	want := &File{
		Version:  CurrentGlobalVersion(),
		Settings: Settings{BranchPrefix: "peter", VSCode: &VSCode{Open: boolPtr(true), WindowColor: "auto"}},
		Repos: []RepoConfig{{
			Path:     "/code/myapp",
			Settings: Settings{VSCode: &VSCode{WorkspaceFile: boolPtr(true), WorkspacePaths: []WorkspacePath{{Path: "/shared/lib"}}}},
		}},
	}
	if !reflect.DeepEqual(cfg, want) {
		t.Errorf("Load() = %+v; want %+v", cfg, want)
	}

	// What is on disk is what Load returned: nested, stamped, no flat keys.
	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decodeGlobalStrict(t, got), *want) {
		t.Errorf("migrated config on disk =\n%s\nwant the shape Load returned %+v", got, want)
	}
	var m map[string]any
	if err := json.Unmarshal(got, &m); err != nil {
		t.Fatal(err)
	}
	assertNoFlatVSCodeKeys(t, "top level", m)
	assertNoFlatVSCodeKeys(t, "repos[0]", reposEntry(t, m, 0))

	backups, err := filepath.Glob(p + ".v1.*.bak")
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 1 {
		t.Fatalf("backups = %v; want exactly one %s.v1.<ts>.bak", backups, filepath.Base(p))
	}
	if b, err := os.ReadFile(backups[0]); err != nil {
		t.Fatal(err)
	} else if string(b) != string(original) {
		t.Errorf("backup = %q; want the pre-migration bytes %q", b, original)
	}
}

func TestLoadTooNewFailsWithoutTouchingDisk(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "th.json")
	original := []byte(`{"version": 99, "from_the_future": true}`)
	if err := os.WriteFile(p, original, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(EnvVar, p)

	_, err := Load()
	var tooNew *TooNewError
	if !errors.As(err, &tooNew) {
		t.Fatalf("Load() of a future config: err = %v; want *TooNewError", err)
	}
	if !strings.Contains(err.Error(), "upgrade th") || !strings.Contains(err.Error(), p) {
		t.Errorf("err = %v; want it to name the file and say to upgrade th", err)
	}
	assertUntouched(t, dir, p, original)
}

func TestLoadLocalTooNewFailsWithoutTouchingDisk(t *testing.T) {
	main := t.TempDir()
	path := filepath.Join(main, LocalFileName)
	original := []byte(`{"version": 99}`)
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatal(err)
	}

	_, _, pending, err := loadLocal(main)
	var tooNew *TooNewError
	if !errors.As(err, &tooNew) {
		t.Fatalf("loadLocal() of a future .thrc: err = %v; want *TooNewError", err)
	}
	if pending != nil {
		t.Errorf("loadLocal() returned a pending migration for a future file: %+v", pending)
	}
	if !strings.Contains(err.Error(), "repo config") || !strings.Contains(err.Error(), "upgrade th") {
		t.Errorf("err = %v; want a repo config error saying to upgrade th", err)
	}
	assertUntouched(t, main, path, original)
}

// TestLoadCurrentVersionLeavesFileUntouched is the promise to a file already
// on the current schema: it is not rewritten, not re-encoded — deliberately
// odd formatting and all — and no backup appears beside it. Built from
// CurrentGlobalVersion() so it keeps meaning the same thing as the registry
// grows. A version-absent file is a v1 file now, migrated on load; that path
// is TestLoadMigratesV1FlatVSCodeConfig's.
func TestLoadCurrentVersionLeavesFileUntouched(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "th.json")
	content := fmt.Sprintf("{\"version\":   %d,\n\t\"default_base\": \"main\"}", CurrentGlobalVersion())
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(EnvVar, p)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load(): %v", err)
	}
	if cfg.DefaultBase != "main" || cfg.Version != CurrentGlobalVersion() {
		t.Errorf("Load() = %+v; want default_base main and version %d", cfg, CurrentGlobalVersion())
	}
	assertUntouched(t, dir, p, []byte(content))
}

func TestResolveDetailedReportsPendingLocalMigration(t *testing.T) {
	useLocalMigrations(t, []migration{renameStep("old_prefix", "branch_prefix")})
	t.Setenv(EnvVar, "")
	t.Setenv("HOME", t.TempDir())
	main := t.TempDir()
	path := filepath.Join(main, LocalFileName)
	original := []byte(`{"name": "solo", "old_prefix": "team"}`)
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatal(err)
	}

	resolved, _, err := ResolveDetailed(main)
	if err != nil {
		t.Fatalf("ResolveDetailed(%q): %v", main, err)
	}
	// The run sees the migrated shape even though disk still has the old one.
	if resolved.BranchPrefix != "team" || resolved.RepoName != "solo" {
		t.Errorf("ResolveDetailed() = %+v; want the migrated branch_prefix and name", resolved)
	}
	p := resolved.LocalMigration
	if p == nil {
		t.Fatal("ResolveDetailed() reported no pending migration for an out-of-date .thrc")
	}
	if p.Path != path || p.From != 1 || p.To != 2 {
		t.Errorf("pending = {Path: %q, From: %d, To: %d}; want {%q, 1, 2}", p.Path, p.From, p.To, path)
	}
	if string(p.Original) != string(original) {
		t.Errorf("pending.Original = %q; want %q", p.Original, original)
	}
	wantMigrated := "{\n  \"branch_prefix\": \"team\",\n  \"name\": \"solo\",\n  \"version\": 2\n}\n"
	if string(p.Migrated) != wantMigrated {
		t.Errorf("pending.Migrated =\n%q\nwant\n%q", p.Migrated, wantMigrated)
	}
	// loadLocal never writes: persisting is the caller's decision.
	assertUntouched(t, main, path, original)
}

// TestResolveMigratesV1ThrcInMemory is the real registry on the local track:
// a .thrc with the flat VS Code keys resolves as the nested shape, and the
// file itself is left for the user to decide about.
func TestResolveMigratesV1ThrcInMemory(t *testing.T) {
	t.Setenv(EnvVar, "")
	t.Setenv("HOME", t.TempDir())
	main := t.TempDir()
	path := filepath.Join(main, LocalFileName)
	original := []byte(`{"vscode_workspace_file": true, "vscode_window_color": "auto"}`)
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatal(err)
	}

	resolved, err := Resolve(main)
	if err != nil {
		t.Fatalf("Resolve(%q): %v", main, err)
	}
	want := &VSCode{WorkspaceFile: boolPtr(true), WindowColor: "auto"}
	if !reflect.DeepEqual(resolved.VSCode, want) {
		t.Errorf("Resolve() vscode = %+v; want %+v", resolved.VSCode, want)
	}
	if !resolved.VSCodeWorkspaceFileEnabled() {
		t.Error("the migrated vscode.workspace_file should be enabled")
	}
	p := resolved.LocalMigration
	if p == nil {
		t.Fatal("Resolve() reported no pending migration for a v1 .thrc")
	}
	if p.Path != path || p.From != 1 || p.To != CurrentLocalVersion() {
		t.Errorf("pending = {Path: %q, From: %d, To: %d}; want {%q, 1, %d}", p.Path, p.From, p.To, path, CurrentLocalVersion())
	}
	assertUntouched(t, main, path, original)
}

// assertUntouched checks that path still holds want and that dir gained no
// files — no rewrite, no backup, no leftover temp file.
func assertUntouched(t *testing.T, dir, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Errorf("%s = %q; want it untouched: %q", filepath.Base(path), got, want)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("directory holds %d entries; want just %s — no backup, no temp file", len(entries), filepath.Base(path))
	}
}

// TestSetFieldsCoversEveryField guards setFields and merge against drift:
// every JSON-tagged field of Settings, when set in an overriding layer,
// must both be applied by merge and reported by setFields.
func TestSetFieldsCoversEveryField(t *testing.T) {
	if got := (Settings{}).setFields(); len(got) != 0 {
		t.Errorf("zero Settings setFields() = %v; want empty", got)
	}
	// An all-zero vscode object sets nothing — the mirror image of merge,
	// which would apply none of its fields.
	if got := (Settings{VSCode: &VSCode{}}).setFields(); len(got) != 0 {
		t.Errorf("empty vscode object setFields() = %v; want empty", got)
	}
	rt := reflect.TypeOf(Settings{})
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		jsonName, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if jsonName == "" {
			t.Fatalf("field %s has no json tag", f.Name)
		}
		if f.Type == reflect.TypeOf(&VSCode{}) {
			// Setting this field to an all-zero object sets no fields at
			// all, so the assertions below cannot hold for it; the loop
			// over VSCode's own fields further down guards them instead.
			continue
		}
		var layer Settings
		fv := reflect.ValueOf(&layer).Elem().Field(i)
		switch f.Type.Kind() {
		case reflect.String:
			fv.SetString("x")
		case reflect.Pointer:
			fv.Set(reflect.New(f.Type.Elem()))
		case reflect.Slice:
			fv.Set(reflect.MakeSlice(f.Type, 0, 0))
		default:
			t.Fatalf("field %s: unhandled kind %s — extend this test, merge, and setFields", f.Name, f.Type.Kind())
		}
		fields := layer.setFields()
		if len(fields) != 1 || fields[0] != jsonName {
			t.Errorf("setFields() with only %s set = %v; want [%s]", f.Name, fields, jsonName)
		}
		var base Settings
		base.merge(layer)
		if !reflect.DeepEqual(base, layer) {
			t.Errorf("merge() does not apply %s", f.Name)
		}
	}

	// The same guard for the fields inside the vscode object, whose names
	// setFields reports dotted, as users write them.
	vt := reflect.TypeOf(VSCode{})
	for i := 0; i < vt.NumField(); i++ {
		f := vt.Field(i)
		jsonName, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if jsonName == "" {
			t.Fatalf("VSCode field %s has no json tag", f.Name)
		}
		var sub VSCode
		fv := reflect.ValueOf(&sub).Elem().Field(i)
		switch f.Type.Kind() {
		case reflect.String:
			fv.SetString("x")
		case reflect.Pointer:
			fv.Set(reflect.New(f.Type.Elem()))
		case reflect.Slice:
			fv.Set(reflect.MakeSlice(f.Type, 0, 0))
		default:
			t.Fatalf("VSCode field %s: unhandled kind %s — extend this test, merge, and setFields", f.Name, f.Type.Kind())
		}
		layer := Settings{VSCode: &sub}
		fields := layer.setFields()
		want := "vscode." + jsonName
		if len(fields) != 1 || fields[0] != want {
			t.Errorf("setFields() with only VSCode.%s set = %v; want [%s]", f.Name, fields, want)
		}
		var base Settings
		base.merge(layer)
		// DeepEqual follows pointers, so the clone merge makes compares
		// equal to the layer it came from.
		if !reflect.DeepEqual(base, layer) {
			t.Errorf("merge() does not apply VSCode.%s", f.Name)
		}
	}
}

// TestMergeVSCodeDoesNotAliasLayers pins the clone in Settings.merge: layers
// hand over a *VSCode and forPath merges two of them into one Settings, so
// merging must never write through the pointer it was handed — that would
// corrupt the loaded File for every later resolve.
func TestMergeVSCodeDoesNotAliasLayers(t *testing.T) {
	global := Settings{VSCode: &VSCode{WindowColor: "auto"}}
	entry := Settings{VSCode: &VSCode{WorkspaceFile: boolPtr(true)}}
	var s Settings
	s.merge(global)
	s.merge(entry)

	want := &VSCode{WorkspaceFile: boolPtr(true), WindowColor: "auto"}
	if !reflect.DeepEqual(s.VSCode, want) {
		t.Errorf("merged vscode = %+v; want %+v", s.VSCode, want)
	}
	if global.VSCode.WorkspaceFile != nil {
		t.Errorf("merging wrote back into the lower layer: %+v", global.VSCode)
	}
	if entry.VSCode.WindowColor != "" {
		t.Errorf("merging wrote back into the overriding layer: %+v", entry.VSCode)
	}
}
