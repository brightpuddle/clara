package main

import (
	"fmt"
	"path/filepath"

	"github.com/cockroachdb/errors"
	"github.com/spf13/cobra"

	"github.com/brightpuddle/clara/internal/config"
)

var secretsCmd = &cobra.Command{
	Use:   "secrets",
	Short: "Inspect secret references in the Clara config",
}

var secretsCheckCmd = &cobra.Command{
	Use:   "check",
	Short: "Verify every ${keychain:...} reference in the config resolves on this host",
	Long: `Lists each ${keychain:service} reference in the config file and reports
whether it can be read from the macOS keychain on this host. Values are never printed.`,
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		path := cfgFile
		if path == "" {
			path = config.DefaultConfigPath()
		}
		paths := []string{path}
		for _, ext := range []string{"yaml", "yml"} {
			matches, _ := filepath.Glob(filepath.Join(config.DefaultConfDDir(), "*."+ext))
			paths = append(paths, matches...)
		}
		refs, err := config.CheckKeychainRefs(paths...)
		if err != nil {
			return err
		}
		missing := 0
		for _, r := range refs {
			status := "ok"
			if r.Err != nil {
				status = "MISSING"
				missing++
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%-8s %s\n", status, r.Service)
		}
		if missing > 0 {
			return errors.Newf("%d of %d secret(s) not readable", missing, len(refs))
		}
		fmt.Fprintf(cmd.OutOrStdout(), "%d secret reference(s) resolved\n", len(refs))
		return nil
	},
}

func init() {
	secretsCmd.AddCommand(secretsCheckCmd)
	rootCmd.AddCommand(secretsCmd)
}
