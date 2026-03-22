package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/brightpuddle/clara/internal/config"
	chromemcp "github.com/brightpuddle/clara/internal/mcpserver/chrome"
	dbmcp "github.com/brightpuddle/clara/internal/mcpserver/db"
	fsmcp "github.com/brightpuddle/clara/internal/mcpserver/fs"
	llmmcp "github.com/brightpuddle/clara/internal/mcpserver/llm"
	ollamamcp "github.com/brightpuddle/clara/internal/mcpserver/ollama"
	taskwmcp "github.com/brightpuddle/clara/internal/mcpserver/taskwarrior"
	zkmcp "github.com/brightpuddle/clara/internal/mcpserver/zk"
	"github.com/mark3labs/mcp-go/server"
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
)

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Start a built-in MCP server",
	Long: `Start a built-in MCP server on stdio.

Clara ships several MCP servers that can be used directly or configured as
external MCP servers in config.yaml. For example:

  mcp_servers:
    - name: fs
      command: clara
      args: [mcp, fs]
      description: "Built-in filesystem server"

    - name: db
      command: clara
      args: [mcp, db, ~/.local/share/clara/data.db]
      description: "Built-in SQLite tool server"

Available servers:
  fs    filesystem operations (read, write, list, search, move, delete)
  db    SQLite query, exec, and vector-search tools
  llm   LLM text and vision generation tools
  ollama    local Ollama-powered generation and embeddings
  taskwarrior    Taskwarrior CRUD, filtering, and due-task helpers
  zk    Zettelkasten Markdown vault (Obsidian, Zettlr, zk)`,
}

var mcpFsCmd = &cobra.Command{
	Use:   "fs",
	Short: "Start the built-in filesystem MCP server",
	Long: fmt.Sprintf(
		"Start the Clara built-in filesystem MCP server on stdio.\n\n%s",
		fsmcp.Description,
	),
	RunE:              runMCPFs,
	SilenceUsage:      true,
	PersistentPreRunE: skipConfigLoad,
}

var mcpDBCmd = &cobra.Command{
	Use:   "db [path]",
	Short: "Start the built-in SQLite MCP server",
	Long: fmt.Sprintf(
		"Start the Clara built-in SQLite MCP server on stdio.\n\n%s\n\nIf no path is provided, the server uses an in-memory database.",
		dbmcp.Description,
	),
	Args:              cobra.MaximumNArgs(1),
	RunE:              runMCPDB,
	SilenceUsage:      true,
	PersistentPreRunE: skipConfigLoad,
}

var mcpTaskwarriorCmd = &cobra.Command{
	Use:   "taskwarrior",
	Short: "Start the built-in Taskwarrior MCP server",
	Long: fmt.Sprintf(
		"Start the Clara built-in Taskwarrior MCP server on stdio.\n\n%s",
		taskwmcp.Description,
	),
	RunE:              runMCPTaskwarrior,
	SilenceUsage:      true,
	PersistentPreRunE: skipConfigLoad,
}

var (
	mcpOllamaEmbedModel    string
	mcpOllamaGenerateModel string
	mcpOllamaURL           string
)

var mcpOllamaCmd = &cobra.Command{
	Use:   "ollama",
	Short: "Start the built-in Ollama MCP server",
	Long: fmt.Sprintf(
		"Start the Clara built-in Ollama MCP server on stdio.\n\n%s",
		ollamamcp.Description,
	),
	RunE:              runMCPOllama,
	SilenceUsage:      true,
	PersistentPreRunE: skipConfigLoad,
}

var mcpZKCmd = &cobra.Command{
	Use:   "zk <vault-path>",
	Short: "Start the built-in Zettelkasten MCP server",
	Long: fmt.Sprintf(
		"Start the Clara built-in Zettelkasten MCP server on stdio.\n\n%s",
		zkmcp.Description,
	),
	Args:              cobra.ExactArgs(1),
	RunE:              runMCPZK,
	SilenceUsage:      true,
	PersistentPreRunE: skipConfigLoad,
}

var (
	mcpLLMDefaultProvider string
	mcpLLMGeminiAPIKey    string
	mcpLLMGeminiModel     string
	mcpLLMGeminiBaseURL   string
)

var mcpLLMCmd = &cobra.Command{
	Use:   "llm",
	Short: "Start the built-in LLM MCP server",
	Long: fmt.Sprintf(
		"Start the Clara built-in LLM MCP server on stdio.\n\n%s",
		llmmcp.Description,
	),
	RunE:              runMCPLLM,
	SilenceUsage:      true,
	PersistentPreRunE: skipConfigLoad,
}

var mcpChromePort int

var mcpChromeCmd = &cobra.Command{
	Use:   "chrome",
	Short: "Start the built-in Chrome browser automation MCP server",
	Long: fmt.Sprintf(
		"Start the Clara built-in Chrome browser automation MCP server on stdio.\n\n%s\n\n"+
			"This server also listens on ws://localhost:<port> for the Clara Chrome\n"+
			"extension to connect. Load the extension from the 'extension/' directory\n"+
			"in Chrome developer mode (chrome://extensions → Load unpacked).",
		chromemcp.Description,
	),
	RunE:              runMCPChrome,
	SilenceUsage:      true,
	PersistentPreRunE: skipConfigLoad,
}

func init() {
	mcpOllamaCmd.Flags().StringVar(
		&mcpOllamaEmbedModel,
		"embed-model",
		ollamamcp.DefaultEmbedModel,
		"Ollama embedding model to use",
	)
	mcpOllamaCmd.Flags().StringVar(
		&mcpOllamaGenerateModel,
		"gen-model",
		ollamamcp.DefaultGenerateModel,
		"Ollama generation model to use",
	)
	mcpOllamaCmd.Flags().StringVar(
		&mcpOllamaURL,
		"url",
		ollamamcp.DefaultURL,
		"Base URL for the Ollama API",
	)

	mcpChromeCmd.Flags().IntVar(
		&mcpChromePort,
		"port",
		chromemcp.DefaultPort,
		"localhost port for the Chrome extension WebSocket connection",
	)
	mcpLLMCmd.Flags().StringVar(
		&mcpLLMDefaultProvider,
		"default-provider",
		llmmcp.DefaultProvider,
		"Default LLM provider to route requests to",
	)
	mcpLLMCmd.Flags().StringVar(
		&mcpLLMGeminiAPIKey,
		"gemini-api-key",
		os.Getenv("GEMINI_API_KEY"),
		"Gemini API key (defaults to GEMINI_API_KEY env var)",
	)
	mcpLLMCmd.Flags().StringVar(
		&mcpLLMGeminiModel,
		"gemini-model",
		llmmcp.DefaultGeminiModel,
		"Default Gemini model for text and vision generation",
	)
	mcpLLMCmd.Flags().StringVar(
		&mcpLLMGeminiBaseURL,
		"gemini-base-url",
		llmmcp.DefaultGeminiBaseURL,
		"Base URL for the Gemini Generative Language API",
	)

	mcpCmd.AddCommand(mcpFsCmd, mcpDBCmd, mcpLLMCmd, mcpOllamaCmd, mcpTaskwarriorCmd, mcpZKCmd, mcpChromeCmd)
}

func runMCPFs(cmd *cobra.Command, args []string) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	return serveMCP(ctx, fsmcp.New())
}

func runMCPDB(cmd *cobra.Command, args []string) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	path := ""
	if len(args) == 1 {
		path = resolveMCPDBPath(args[0])
	}

	svc, err := dbmcp.Open(path, zerolog.Nop())
	if err != nil {
		return err
	}
	defer svc.Close()

	return serveMCP(ctx, svc.NewServer())
}

func runMCPTaskwarrior(cmd *cobra.Command, args []string) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	return serveMCP(ctx, taskwmcp.New(zerolog.Nop()).NewServer())
}

func runMCPOllama(cmd *cobra.Command, args []string) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	return serveMCP(
		ctx,
		ollamamcp.New(mcpOllamaURL, mcpOllamaEmbedModel, mcpOllamaGenerateModel).NewServer(),
	)
}

func runMCPZK(cmd *cobra.Command, args []string) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	srv, err := zkmcp.New(args[0])
	if err != nil {
		return err
	}
	return serveMCP(ctx, srv)
}

func runMCPLLM(cmd *cobra.Command, args []string) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	return serveMCP(
		ctx,
		llmmcp.New(llmmcp.Options{
			DefaultProvider: mcpLLMDefaultProvider,
			GeminiAPIKey:    mcpLLMGeminiAPIKey,
			GeminiModel:     mcpLLMGeminiModel,
			GeminiBaseURL:   mcpLLMGeminiBaseURL,
		}).NewServer(),
	)
}

func runMCPChrome(cmd *cobra.Command, _ []string) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	log := zerolog.New(zerolog.NewConsoleWriter(func(w *zerolog.ConsoleWriter) {
		w.Out = os.Stderr
	})).With().Timestamp().Logger()
	return chromemcp.New(log).Run(ctx, mcpChromePort)
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
