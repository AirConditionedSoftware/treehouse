package cmd

import (
	"fmt"
	"os"

	"github.com/AirConditionedSoftware/treehouse/internal/config"
	"github.com/spf13/cobra"
)

// globalConfigFileName is how the global config file is named in output.
// config keeps its own unexported constant for the path; this is the label.
const globalConfigFileName = "config.json"

var schemaGlobal bool

var schemaCmd = &cobra.Command{
	Use:   "schema",
	Short: "Print the JSON Schema for a .thrc, or with --global the config file",
	Long: `Print the JSON Schema th ships for a repository's .thrc — what an editor
reads to offer completion, hover documentation, and validation while the
file is being written. The schema is built into the binary; nothing is
hosted and nothing is fetched.

The schema goes to stdout and the track it describes to stderr, so
th schema | jq and th schema > thrc.schema.json both work. --global prints
the schema for the global config file instead.

th schema install writes both schemas to ~/.th and points VS Code's user
settings at them, which is what this output is for.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		data, name, version := config.ThrcSchema(), config.LocalFileName, config.CurrentLocalVersion()
		if schemaGlobal {
			data, name, version = config.GlobalSchema(), globalConfigFileName, config.CurrentGlobalVersion()
		}
		fmt.Fprintf(os.Stderr, "JSON Schema for %s at config schema v%d\n", name, version)

		if _, err := os.Stdout.Write(data); err != nil {
			return err
		}
		// The bytes are the machine contract, so they go out verbatim; only a
		// missing final newline is added, as th config does.
		if len(data) == 0 || data[len(data)-1] != '\n' {
			fmt.Println()
		}
		return nil
	},
}

func init() {
	schemaCmd.Flags().BoolVar(&schemaGlobal, "global", false, "print the schema for the global config file instead of a .thrc")
	rootCmd.AddCommand(schemaCmd)
}
