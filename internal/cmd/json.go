package cmd

import (
	"encoding/json"
	"os"
)

// printJSON writes v to stdout as one indented JSON document — the machine
// channel. Callers return right after; machine output keeps full paths.
func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
