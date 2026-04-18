package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/brightpuddle/clara/internal/interpreter"
	"github.com/brightpuddle/clara/internal/orchestrator"
	"github.com/brightpuddle/clara/internal/registry"
	"github.com/brightpuddle/clara/internal/store"
	"github.com/cockroachdb/errors"
	"github.com/spf13/cobra"
)

var testCmd = &cobra.Command{
	Use:   "test [paths...]",
	Short: "Run Starlark tests (*_test.star)",
	RunE:  runTests,
}

func init() {
	rootCmd.AddCommand(testCmd)
}

func runTests(cmd *cobra.Command, args []string) error {
	paths := args
	if len(paths) == 0 {
		paths = []string{cfg.TasksDir()}
	}

	var testFiles []string
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			return err
		}
		if !info.IsDir() {
			// Check for _test.star suffix case-insensitively
			ext := filepath.Ext(p)
			if strings.EqualFold(ext, ".star") && strings.HasSuffix(strings.ToLower(p), "_test.star") {
				testFiles = append(testFiles, p)
			}
			continue
		}
		err = filepath.WalkDir(p, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			ext := filepath.Ext(path)
			if strings.EqualFold(ext, ".star") && strings.HasSuffix(strings.ToLower(path), "_test.star") {
				testFiles = append(testFiles, path)
			}
			return nil
		})
		if err != nil {
			return err
		}
	}

	if len(testFiles) == 0 {
		fmt.Println("No tests found.")
		return nil
	}

	logger := buildLogger()
	// Use isolated in-memory db for testing
	db, err := store.Open(":memory:", logger)
	if err != nil {
		return errors.Wrap(err, "open test database")
	}
	defer db.Close()

	reg := registry.New(logger)
	if err := addMCPServers(reg, logger); err != nil {
		return err
	}
	registerPermanentTUITools(reg, db, logger)

	ctx := context.Background()
	if err := reg.StartServers(ctx); err != nil {
		return errors.Wrap(err, "start MCP servers")
	}
	defer reg.StopServers()
	_ = reg.WaitReady(ctx)

	passed := 0
	failed := 0

	for _, file := range testFiles {
		fmt.Printf("=== RUN   %s\n", file)
		data, err := os.ReadFile(file)
		if err != nil {
			fmt.Printf("--- FAIL: %s (read error: %v)\n", file, err)
			failed++
			continue
		}

		namespaces := []string{"llm", "search", "clara_tui"}
		if cfg != nil {
			for _, srv := range cfg.MCPServers {
				namespaces = append(namespaces, srv.Name)
			}
		}

		intent, err := orchestrator.LoadIntentFile(file, data, namespaces)
		if err != nil {
			fmt.Printf("--- FAIL: %s (parse error: %v)\n", file, err)
			failed++
			continue
		}

		if len(intent.Tests) == 0 {
			fmt.Printf("--- SKIP: %s (no test_ functions found)\n", file)
			continue
		}

		for _, testName := range intent.Tests {
			fmt.Printf("    --- RUN   %s\n", testName)
			
			it := interpreter.NewStarlark(reg, logger)
			
			start := time.Now()
			err := it.Execute(ctx, intent, "", interpreter.RunOptions{
				Entrypoint: testName,
			})
			dur := time.Since(start)

			if err != nil {
				fmt.Printf("    --- FAIL: %s (%v)\n", testName, dur)
				failed++
			} else {
				fmt.Printf("    --- PASS: %s (%v)\n", testName, dur)
				passed++
			}
		}
	}

	fmt.Printf("\nTests: %d passed, %d failed\n", passed, failed)
	if failed > 0 {
		return errors.New("tests failed")
	}
	return nil
}
