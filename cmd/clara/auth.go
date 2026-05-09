package main

import (
	"fmt"
	"os"

	"github.com/brightpuddle/clara/internal/auth"
	"github.com/brightpuddle/clara/internal/store"
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
)

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Manage authentication for external services",
}

var (
	webexClientID     string
	webexClientSecret string
)

var authWebexCmd = &cobra.Command{
	Use:   "webex",
	Short: "Authenticate Clara with Webex using OAuth",
	RunE: func(cmd *cobra.Command, args []string) error {
		// If not provided via flags/env, try to pull from the config file.
		if webexClientID == "" || webexClientSecret == "" {
			if wcfg, ok := cfg.Integrations["webex"]; ok {
				if id, ok := wcfg["client_id"].(string); ok && webexClientID == "" {
					webexClientID = id
				}
				if secret, ok := wcfg["client_secret"].(string); ok && webexClientSecret == "" {
					webexClientSecret = secret
				}
			}
		}

		if webexClientID == "" || webexClientSecret == "" {
			return fmt.Errorf("Webex client-id and client-secret are required (provide via flags, environment variables, or config.yaml)")
		}

		db, err := store.Open(cfg.DBPath(), zerolog.Nop())
		if err != nil {
			return err
		}
		defer db.Close()

		log := zerolog.New(os.Stderr).With().Timestamp().Logger()
		return auth.AuthorizeWebex(cmd.Context(), webexClientID, webexClientSecret, db, log)
	},
}

func init() {
	authWebexCmd.Flags().
		StringVar(&webexClientID, "client-id", os.Getenv("WEBEX_CLIENT_ID"), "Webex Client ID")
	authWebexCmd.Flags().
		StringVar(&webexClientSecret, "client-secret", os.Getenv("WEBEX_CLIENT_SECRET"), "Webex Client Secret")

	authCmd.AddCommand(authWebexCmd)
	rootCmd.AddCommand(authCmd)
}
