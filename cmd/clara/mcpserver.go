package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/WebexCommunity/webex-go-sdk/v2/attachmentactions"
	"github.com/brightpuddle/clara/internal/auth"
	"github.com/brightpuddle/clara/internal/config"
	chromemcp "github.com/brightpuddle/clara/internal/mcpserver/chrome"
	dbmcp "github.com/brightpuddle/clara/internal/mcpserver/db"
	llmmcp "github.com/brightpuddle/clara/internal/mcpserver/llm"
	searchmcp "github.com/brightpuddle/clara/internal/mcpserver/search"
	shellmcp "github.com/brightpuddle/clara/internal/mcpserver/shell"
	taskwmcp "github.com/brightpuddle/clara/internal/mcpserver/taskwarrior"
	tmuxmcp "github.com/brightpuddle/clara/internal/mcpserver/tmux"
	webmcp "github.com/brightpuddle/clara/internal/mcpserver/web"
	webexmcp "github.com/brightpuddle/clara/internal/mcpserver/webex"
	zkmcp "github.com/brightpuddle/clara/internal/mcpserver/zk"
	"github.com/brightpuddle/clara/internal/registry"
	"github.com/brightpuddle/clara/internal/store"
	"github.com/mark3labs/mcp-go/server"
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
)

func addMCPServers(reg *registry.Registry, logger zerolog.Logger) error {
	for _, srv := range cfg.MCPServers {
		var mcpSrv *registry.MCPServer
		description := srv.Description
		if description == "" {
			description = fallbackDescription(srv.Name)
		}

		if srv.IsHTTPServer() {
			mcpSrv = registry.NewHTTPMCPServer(
				srv.Name,
				description,
				srv.URL,
				srv.Token,
				srv.SkipVerify,
				logger,
			)
		} else {
			args, err := srv.CommandArgs()
			if err != nil {
				return err
			}
			if len(args) == 0 {
				return fmt.Errorf("empty command for MCP server %q", srv.Name)
			}
			mcpSrv = registry.NewMCPServer(
				srv.Name,
				description,
				args[0],
				args[1:],
				srv.ResolvedEnv(),
				cfg.MCPCommandSearchPathList(),
				logger,
			)
		}
		if err := reg.AddServer(mcpSrv); err != nil {
			return err
		}
	}
	return nil
}

func fallbackDescription(serverName string) string {
	switch serverName {
	case "db":
		return "Built-in SQLite MCP server with query, exec, and vector search tools."
	case "llm":
		return "Built-in LLM MCP server with Gemini-backed text and vision generation."
	case "chrome":
		return "Built-in Chrome browser automation: navigate, click, fill, upload files, read page content, screenshot, and manage tabs."
	case "shell":
		return "Built-in shell MCP server for running local commands."
	case "taskwarrior":
		return "Built-in Taskwarrior MCP server for managing tasks."
	case "tmux":
		return "Built-in tmux MCP server for managing terminal sessions."
	case "zk":
		return "Built-in Zettelkasten MCP server for managing Obsidian/Markdown notes."
	case "web":
		return "Built-in web search server: search the internet via DuckDuckGo."
	default:
		return ""
	}
}

var mcpserverCmd = &cobra.Command{
	Use:   "mcpserver",
	Short: "Start a built-in MCP server on stdio",
}


var mcpserverDBCmd = &cobra.Command{
	Use:   "db [database-path]",
	Short: "Start the built-in SQLite database MCP server",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runMCPDB,
}

var mcpserverLLMCmd = &cobra.Command{
	Use:   "llm",
	Short: "Start the built-in LLM provider MCP server",
	RunE:  runMCPLLM,
}

var mcpserverRunCmd = &cobra.Command{
	Use:   "run",
	Short: "Start a stdio MCP server exposing Clara's tools and intents",
	RunE:  runMCPRun,
}

var mcpserverTaskwarriorCmd = &cobra.Command{
	Use:   "taskwarrior",
	Short: "Start the built-in Taskwarrior MCP server",
	RunE:  runMCPTaskwarrior,
}

var mcpserverTmuxCmd = &cobra.Command{
	Use:   "tmux",
	Short: "Start the built-in tmux MCP server",
	RunE:  runMCPTmux,
}

var mcpserverWebexAccessToken string
var mcpserverWebexBotToken string
var mcpserverWebexWebhookAddr string
var mcpserverWebexCmd = &cobra.Command{
	Use:   "webex",
	Short: "Start the built-in Webex MCP server",
	RunE:  runMCPWebex,
}

var mcpserverZKIndexPath string
var mcpserverZKCmd = &cobra.Command{
	Use:   "zk [vault-root]",
	Short: "Start the built-in ZK (Zettelkasten) MCP server",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runMCPZK,
}
var mcpserverSearchIndexPath string
var mcpserverSearchMailDir string
var mcpserverSearchCmd = &cobra.Command{
	Use:   "search",
	Short: "Start the built-in Search MCP server",
	RunE:  runMCPSearch,
}

var mcpserverWebsearchCmd = &cobra.Command{
	Use:   "web",
	Short: "Start the built-in Websearch MCP server",
	RunE:  runMCPWebsearch,
}

var mcpserverChromeCmd = &cobra.Command{
	Use:   "chrome",
	Short: "Start the built-in Chrome browser automation MCP server",
	Long: fmt.Sprintf(
		"Start the Clara built-in Chrome browser automation MCP server on stdio.\n\n%s",
		chromemcp.Description,
	),
	RunE:              runMCPChrome,
	SilenceUsage:      true,
	PersistentPreRunE: skipConfigLoad,
}

var mcpserverChromeNativeHostCmd = &cobra.Command{
	Use:    "chrome-native-host",
	Short:  "Internal: Chrome Native Messaging host proxy",
	Hidden: true,
	RunE:   runMCPChromeNativeHost,
}

var mcpserverShellCmd = &cobra.Command{
	Use:   "shell",
	Short: "Start the built-in shell MCP server",
	Long: fmt.Sprintf(
		"Start the Clara built-in shell MCP server on stdio.\n\n%s",
		shellmcp.Description,
	),
	RunE:              runMCPShell,
	SilenceUsage:      true,
	PersistentPreRunE: skipConfigLoad,
}

func init() {

	mcpserverWebexCmd.Flags().StringVar(
		&mcpserverWebexAccessToken,
		"access-token",
		os.Getenv("WEBEX_ACCESS_TOKEN"),
		"Webex access token (defaults to WEBEX_ACCESS_TOKEN env var)",
	)
	mcpserverWebexCmd.Flags().StringVar(
		&mcpserverWebexBotToken,
		"bot-token",
		os.Getenv("WEBEX_BOT_TOKEN"),
		"Webex bot token (defaults to WEBEX_BOT_TOKEN env var)",
	)
	mcpserverWebexCmd.Flags().StringVar(
		&mcpserverWebexWebhookAddr,
		"webhook-addr",
		"",
		"Address to listen on for Webex webhooks (e.g. :8080)",
	)
	mcpserverZKCmd.Flags().StringVar(
		&mcpserverZKIndexPath,
		"index-path",
		"",
		"Path to the SQLite search index file",
	)

	mcpserverSearchCmd.Flags().StringVar(
		&mcpserverSearchIndexPath,
		"index-path",
		"",
		"Path to the SQLite search index file",
	)
	mcpserverSearchCmd.Flags().StringVar(
		&mcpserverSearchMailDir,
		"mail-dir",
		"",
		"Path to the Mail directory to index (e.g. ~/Library/Mail)",
	)

	mcpserverCmd.AddCommand(
		mcpserverDBCmd,
		mcpserverLLMCmd,
		mcpserverRunCmd,
		mcpserverTaskwarriorCmd,
		mcpserverTmuxCmd,
		mcpserverWebexCmd,
		mcpserverZKCmd,
		mcpserverSearchCmd,
		mcpserverWebsearchCmd,
		mcpserverChromeCmd,
		mcpserverShellCmd,
	)

	mcpserverChromeCmd.AddCommand(mcpserverChromeNativeHostCmd)
}


func runMCPDB(cmd *cobra.Command, args []string) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	path := ""
	if len(args) == 1 {
		path = resolveMCPDBPath(args[0])
	}

	svc, err := dbmcp.Open(path, buildMCPLogger("db"))
	if err != nil {
		return err
	}
	defer svc.Close()

	return serveMCP(ctx, svc.NewServer())
}

func runMCPTaskwarrior(cmd *cobra.Command, args []string) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	return serveMCP(ctx, taskwmcp.New(buildMCPLogger("taskwarrior")).NewServer())
}

func runMCPTmux(cmd *cobra.Command, args []string) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	return serveMCP(ctx, tmuxmcp.New(buildMCPLogger("tmux")).NewServer())
}

func runMCPWebex(cmd *cobra.Command, args []string) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	log := buildMCPLogger("webex")
	
	// Prioritize: 1. Explicit Bot Token, 2. Explicit Access Token, 3. OAuth/Env
	token := mcpserverWebexBotToken
	if token == "" {
		token = mcpserverWebexAccessToken
	}

	if token == "" {
		db, err := store.Open(cfg.DBPath(), zerolog.Nop())
		if err == nil {
			defer db.Close()
			var tokens auth.WebexTokens
			if err := db.GetKV(ctx, auth.WebexKVKey, &tokens); err == nil {
				if time.Now().Add(time.Minute).After(tokens.Expiry) {
					log.Info().Msg("refreshing webex access token")
					newTokens, err := auth.RefreshToken(ctx, &tokens)
					if err == nil {
						_ = db.SetKV(ctx, auth.WebexKVKey, newTokens)
						token = newTokens.AccessToken
					} else {
						log.Warn().Err(err).Msg("failed to refresh webex token")
					}
				} else {
					token = tokens.AccessToken
				}
			}
		}
	}

	svc := webexmcp.New(token, nil, log)
	if err := svc.Start(ctx); err != nil {
		return err
	}
	defer svc.Stop()

	// Optional Webhook server for external proxying
	if mcpserverWebexWebhookAddr != "" {
		mux := http.NewServeMux()
		mux.HandleFunc("/webhook", func(w http.ResponseWriter, r *http.Request) {
			var action attachmentactions.AttachmentAction
			if err := json.NewDecoder(r.Body).Decode(&action); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			svc.HandleWebhook(&action)
			w.WriteHeader(http.StatusOK)
		})
		srv := &http.Server{Addr: mcpserverWebexWebhookAddr, Handler: mux}
		log.Info().Str("addr", mcpserverWebexWebhookAddr).Msg("starting webex webhook proxy server")
		go func() {
			if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Error().Err(err).Msg("webhook server failed")
			}
		}()
		go func() {
			<-ctx.Done()
			srv.Shutdown(context.Background())
		}()
	}

	return serveMCP(ctx, svc.NewServer())
}

func buildMCPLogger(component string) zerolog.Logger {
	return zerolog.New(os.Stderr).With().Timestamp().Str("component", "mcp_"+component).Logger()
}

func runMCPZK(cmd *cobra.Command, args []string) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	path := ""
	if len(args) == 1 {
		path = resolvePath(args[0])
	}

	indexPath := mcpserverZKIndexPath
	if indexPath == "" {
		indexPath = filepath.Join(config.DefaultDataDir(), "indexes", "zk", "index.db")
	} else {
		indexPath = resolveMCPDBPath(indexPath)
	}

	// Ensure parent directory exists for index
	if err := os.MkdirAll(filepath.Dir(indexPath), 0755); err != nil {
		return fmt.Errorf("create index directory: %w", err)
	}

	s, err := zkmcp.New(path, indexPath, buildMCPLogger("zk"))
	if err != nil {
		return err
	}
	return serveMCP(ctx, s)
}

func resolvePath(path string) string {
	path = os.ExpandEnv(strings.TrimSpace(path))
	if path == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
		return path
	}
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

func runMCPSearch(cmd *cobra.Command, args []string) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	indexPath := mcpserverSearchIndexPath
	if indexPath == "" {
		indexPath = filepath.Join(config.DefaultDataDir(), "indexes", "mail", "index.db")
	} else {
		indexPath = resolveMCPDBPath(indexPath)
	}

	mailDir := mcpserverSearchMailDir
	if mailDir == "" {
		home, _ := os.UserHomeDir()
		mailDir = filepath.Join(home, "Library/Mail")
	}

	// Ensure parent directory exists for index
	if err := os.MkdirAll(filepath.Dir(indexPath), 0755); err != nil {
		return fmt.Errorf("create index directory: %w", err)
	}

	// Check if index exists before creating server to decide on background indexing
	_, statErr := os.Stat(indexPath)
	indexExists := statErr == nil

	s, err := searchmcp.New(indexPath, buildMCPLogger("search"))
	if err != nil {
		return err
	}

	// Index if it's a new index
	if !indexExists && mailDir != "" {
		log := buildMCPLogger("search_index")
		go func() {
			if err := s.IndexMail(context.Background(), mailDir); err != nil {
				log.Error().Err(err).Msg("mail indexing failed")
			}
		}()
	}

	return serveMCP(ctx, s.MCPServer)
}

func runMCPWebsearch(cmd *cobra.Command, args []string) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	return serveMCP(ctx, webmcp.New(buildMCPLogger("web")).NewServer())
}

func runMCPShell(cmd *cobra.Command, args []string) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	return serveMCP(ctx, shellmcp.New(buildMCPLogger("shell")))
}

func runMCPLLM(cmd *cobra.Command, args []string) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	log := buildMCPLogger("llm")

	return serveMCP(
		ctx,
		llmmcp.New(llmmcp.Options{
			Config: cfg,
			Logger: log,
		}).NewServer(),
	)
}

func runMCPRun(cmd *cobra.Command, args []string) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	return runGateway(ctx)
}

func runMCPChrome(cmd *cobra.Command, _ []string) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	log := buildMCPLogger("chrome")
	return chromemcp.New(log).Run(ctx)
}

func runMCPChromeNativeHost(cmd *cobra.Command, _ []string) error {
	log := zerolog.New(os.Stderr).With().Timestamp().Logger()
	return chromemcp.RunNativeHost(cmd.Context(), log)
}

func serveMCP(ctx context.Context, srv *server.MCPServer) error {
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.ServeStdio(srv)
	}()

	select {
	case <-ctx.Done():
		return nil
	case err := <-errCh:
		return err
	}
}

func skipConfigLoad(cmd *cobra.Command, args []string) error {
	return nil
}

func resolveMCPDBPath(path string) string {
	if path == "" || path == ":memory:" || filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(config.DefaultDataDir(), path)
}
