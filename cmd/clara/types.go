package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	dbbuiltin "github.com/brightpuddle/clara/internal/builtin/db"
	fsbuiltin "github.com/brightpuddle/clara/internal/builtin/fs"
	notifybuiltin "github.com/brightpuddle/clara/internal/builtin/notify"
	shellbuiltin "github.com/brightpuddle/clara/internal/builtin/shell"
	"github.com/brightpuddle/clara/internal/ipc"
	"github.com/brightpuddle/clara/internal/registry"
	"github.com/brightpuddle/clara/internal/typestub"
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
)

var (
	typesOutPath string
	typesStdout  bool
)

var typesCmd = &cobra.Command{
	Use:   "types",
	Short: "Manage and generate TypeScript type definitions for Clara MCP tools",
}

var typesGenCmd = &cobra.Command{
	Use:   "gen",
	Short: "Generate TypeScript ambient declarations (.d.ts) for Clara tools",
	Long: `Generate TypeScript ambient declarations (clara.d.ts) for all registered
MCP tools and CloudEvents.

If the Clara daemon is running, type stubs are fetched directly from active tools.
If the daemon is offline, type stubs are generated from Clara's built-in tools.

By default, the definitions are saved to ~/.config/clara/sdk/clara.d.ts.
You can specify an explicit output path with -o / --out or print to stdout with --stdout.

Examples:
  clara types gen
  clara types gen --out ./scripts/clara.d.ts
  clara types gen --stdout`,
	RunE:         runTypesGen,
	SilenceUsage: true,
}

func init() {
	typesGenCmd.Flags().StringVarP(&typesOutPath, "out", "O", "", "output file path (defaults to ~/.config/clara/sdk/clara.d.ts)")
	typesGenCmd.Flags().BoolVar(&typesStdout, "stdout", false, "write type declarations to stdout instead of file")

	typesCmd.AddCommand(typesGenCmd)
	rootCmd.AddCommand(typesCmd)
}

func runTypesGen(cmd *cobra.Command, args []string) error {
	var dtsContent string

	// 1. Try to query running daemon for live tool signatures
	if isRunning(cfg.ControlSocketPath()) {
		resp, err := sendRequest(cfg.ControlSocketPath(), ipc.Request{
			Method: ipc.MethodTypesGen,
		})
		if err == nil && resp.Data != nil {
			if m, ok := resp.Data.(map[string]any); ok {
				if dts, ok := m["dts"].(string); ok && dts != "" {
					dtsContent = dts
				}
			}
		}
	}

	// 2. If daemon not running or returned empty, generate from built-ins offline
	if dtsContent == "" {
		reg := registry.New(zerolog.Nop())
		ctx := context.Background()

		_ = shellbuiltin.Register(ctx, nil, reg, zerolog.Nop())
		_ = fsbuiltin.Register(ctx, nil, reg, zerolog.Nop())
		_ = dbbuiltin.Register(ctx, nil, reg, zerolog.Nop())
		_ = notifybuiltin.Register(ctx, cfg.Notify, reg, zerolog.Nop())

		// Also register configured MCP server placeholders if available in config
		for _, srv := range cfg.MCPServers {
			mcpSrv := buildMCPServer(srv, zerolog.Nop())
			_ = reg.AddServer(mcpSrv)
		}

		dtsContent = typestub.GenerateTypeScript(reg)
	}

	if typesStdout {
		fmt.Print(dtsContent)
		return nil
	}

	targetPath := typesOutPath
	if targetPath == "" {
		targetPath = filepath.Join(cfg.SDKDir(), "clara.d.ts")
	}

	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return fmt.Errorf("create directory %q: %w", filepath.Dir(targetPath), err)
	}

	if err := os.WriteFile(targetPath, []byte(dtsContent), 0o644); err != nil {
		return fmt.Errorf("write type declarations to %q: %w", targetPath, err)
	}

	fmt.Printf("Generated TypeScript declarations at %s\n", targetPath)
	return nil
}
