package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// TrustFileName is the approval store for repo-sourced hook commands. It
// lives at ~/.th/trust.json regardless of $TH_CONFIG: it is per-user
// machine state, not configuration, and must not travel with a config file.
const TrustFileName = "trust.json"

// trustFile is the ~/.th/trust.json schema, keyed by normalized main
// worktree path.
type trustFile struct {
	Repos map[string]trustRecord `json:"repos"`
}

// trustRecord is one repository's approvals: per hook, the exact commands
// approved and when. Approval is re-required whenever a hook's commands
// change. PostCreate and ApprovedAt are the layout from before hooks were
// stored per hook; they still answer for post_create and are folded into
// Hooks on the next write.
type trustRecord struct {
	PostCreate []string                `json:"post_create,omitempty"` // legacy; migrate on write
	ApprovedAt string                  `json:"approved_at,omitempty"`
	Hooks      map[string]hookApproval `json:"hooks,omitempty"`
}

// hookApproval is one hook's approved command list.
type hookApproval struct {
	Commands   []string `json:"commands"`
	ApprovedAt string   `json:"approved_at"`
}

func trustPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, homeConfigDirName, TrustFileName), nil
}

// readTrust loads the store best-effort: a missing, unreadable or corrupt
// file reads as empty, so a damaged store re-prompts instead of wedging th.
func readTrust() trustFile {
	path, err := trustPath()
	if err != nil {
		return trustFile{}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return trustFile{}
	}
	var tf trustFile
	if err := json.Unmarshal(data, &tf); err != nil {
		return trustFile{}
	}
	return tf
}

// ApprovedCommands returns the commands the user approved for the given
// hook of the repository whose main worktree is at mainPath, and whether an
// approval record exists at all. For "post_create" a record written before
// approvals were stored per hook still answers. It never fails: with no
// usable record the caller prompts for approval.
func ApprovedCommands(mainPath, hook string) ([]string, bool) {
	rec, ok := readTrust().Repos[normalizePath(mainPath)]
	if !ok {
		return nil, false
	}
	if ha, ok := rec.Hooks[hook]; ok {
		return ha.Commands, true
	}
	if hook == "post_create" && (rec.ApprovedAt != "" || rec.PostCreate != nil) {
		return rec.PostCreate, true
	}
	return nil, false
}

// ApproveCommands records cmds as approved for the given hook of the
// repository whose main worktree is at mainPath, replacing any previous
// approval for that hook and leaving the other hooks' approvals alone. A
// legacy record is migrated to the per-hook layout on the way. The store is
// written atomically (temp file plus rename) with mode 0600.
func ApproveCommands(mainPath, hook string, cmds []string) error {
	path, err := trustPath()
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("trust %s: %w", path, err)
	}

	tf := readTrust()
	if tf.Repos == nil {
		tf.Repos = make(map[string]trustRecord)
	}
	key := normalizePath(mainPath)
	rec := tf.Repos[key]
	if rec.Hooks == nil {
		rec.Hooks = make(map[string]hookApproval)
	}
	if rec.ApprovedAt != "" || rec.PostCreate != nil {
		if _, ok := rec.Hooks["post_create"]; !ok {
			rec.Hooks["post_create"] = hookApproval{Commands: rec.PostCreate, ApprovedAt: rec.ApprovedAt}
		}
		rec.PostCreate, rec.ApprovedAt = nil, ""
	}
	// Stored as given, so reading back compares equal to what was approved.
	rec.Hooks[hook] = hookApproval{
		Commands:   cmds,
		ApprovedAt: time.Now().UTC().Format(time.RFC3339),
	}
	tf.Repos[key] = rec

	data, err := json.MarshalIndent(tf, "", "  ")
	if err != nil {
		return fmt.Errorf("trust %s: %w", path, err)
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(dir, ".trust-*.json")
	if err != nil {
		return fmt.Errorf("trust %s: %w", path, err)
	}
	// Removing the temp file is a no-op once the rename has consumed it.
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("trust %s: %w", path, err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("trust %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("trust %s: %w", path, err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("trust %s: %w", path, err)
	}
	return nil
}
