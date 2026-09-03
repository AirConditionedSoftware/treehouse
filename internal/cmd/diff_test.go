package cmd

import "testing"

func TestDiffCommands(t *testing.T) {
	tests := []struct {
		name     string
		old, new []string
		want     string
	}{
		{
			name: "identical lists have no markers",
			old:  []string{"direnv allow", "npm ci"},
			new:  []string{"direnv allow", "npm ci"},
			want: "    direnv allow\n    npm ci",
		},
		{
			name: "pure addition",
			old:  []string{"direnv allow"},
			new:  []string{"direnv allow", "npm ci"},
			want: "    direnv allow\n  + npm ci",
		},
		{
			name: "pure removal",
			old:  []string{"direnv allow", "npm ci"},
			new:  []string{"direnv allow"},
			want: "    direnv allow\n  - npm ci",
		},
		{
			name: "replacement",
			old:  []string{"direnv allow", "npm install"},
			new:  []string{"direnv allow", "npm ci"},
			want: "    direnv allow\n  - npm install\n  + npm ci",
		},
		{
			name: "first approval marks everything added",
			old:  nil,
			new:  []string{"direnv allow", "npm ci"},
			want: "  + direnv allow\n  + npm ci",
		},
		{
			name: "emptied list marks everything removed",
			old:  []string{"direnv allow", "npm ci"},
			new:  nil,
			want: "  - direnv allow\n  - npm ci",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := diffCommands(tt.old, tt.new); got != tt.want {
				t.Errorf("diffCommands(%q, %q) =\n%s\nwant:\n%s", tt.old, tt.new, got, tt.want)
			}
		})
	}
}

// TestDiffFileLines covers the file-content wrapper th migrate previews a
// schema rewrite with: the trailing newline must not become a phantom line,
// and a v1-to-v2 rewrite must read as the flat key leaving and the nested
// ones arriving.
func TestDiffFileLines(t *testing.T) {
	tests := []struct {
		name     string
		old, new []byte
		want     string
	}{
		{
			name: "an unchanged file marks nothing",
			old: []byte(`{
  "version": 2
}
`),
			new: []byte(`{
  "version": 2
}
`),
			want: `    {
      "version": 2
    }`,
		},
		{
			name: "a v1 to v2 rewrite shows the key moving",
			old: []byte(`{
  "vscode_workspace_file": true
}
`),
			new: []byte(`{
  "version": 2,
  "vscode": {
    "workspace_file": true
  }
}
`),
			want: `    {
  -   "vscode_workspace_file": true
  +   "version": 2,
  +   "vscode": {
  +     "workspace_file": true
  +   }
    }`,
		},
		{
			name: "an empty original marks every line added",
			old:  nil,
			new: []byte(`{
  "version": 2
}
`),
			want: `  + {
  +   "version": 2
  + }`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := diffFileLines(tt.old, tt.new); got != tt.want {
				t.Errorf("diffFileLines(%q, %q) =\n%s\nwant:\n%s", tt.old, tt.new, got, tt.want)
			}
		})
	}
}
