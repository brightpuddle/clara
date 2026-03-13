package main

import (
	"context"
	"strings"

	bridgeclient "github.com/brightpuddle/clara/internal/bridge"
	"github.com/brightpuddle/clara/internal/config"
	"github.com/brightpuddle/clara/internal/registry"
	"github.com/brightpuddle/clara/internal/store"
	"github.com/cockroachdb/errors"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/rs/zerolog"
)

func registerBuiltinTools(reg *registry.Registry, db *store.Store, cfg *config.Config, log zerolog.Logger) {
	reg.RegisterWithSpec(dbQueryToolSpec(), db.QueryTool())
	reg.RegisterWithSpec(dbExecToolSpec(), db.ExecTool())
	reg.RegisterWithSpec(dbVecSearchToolSpec(), db.VecSearchTool())

	if strings.TrimSpace(cfg.Bridge.Path) == "" {
		return
	}

	reg.RegisterWithSpec(bridgeFetchRemindersToolSpec(), bridgeTool(cfg.Bridge.SocketPath, "fetch_reminders", log))
}

func dbQueryToolSpec() mcp.Tool {
	return mcp.NewTool("db.query",
		mcp.WithDescription("Execute a SQL query and return the results."),
		mcp.WithString("sql",
			mcp.Required(),
			mcp.Description("SQL query text to execute."),
		),
		mcp.WithArray("params",
			mcp.Description("Optional positional parameters bound to the SQL query."),
		),
	)
}

func dbExecToolSpec() mcp.Tool {
	return mcp.NewTool("db.exec",
		mcp.WithDescription("Execute a SQL statement and return the number of affected rows."),
		mcp.WithString("sql",
			mcp.Required(),
			mcp.Description("SQL statement text to execute."),
		),
		mcp.WithArray("params",
			mcp.Description("Optional positional parameters bound to the SQL statement."),
		),
	)
}

func dbVecSearchToolSpec() mcp.Tool {
	return mcp.NewTool("db.vec_search",
		mcp.WithDescription("Perform a vector similarity search over a sqlite-vec table."),
		mcp.WithString("table",
			mcp.Required(),
			mcp.Description("Name of the vec0 virtual table to query."),
		),
		mcp.WithArray("vector",
			mcp.Required(),
			mcp.Description("Query vector represented as a JSON array of floats or raw bytes."),
		),
		mcp.WithNumber("limit",
			mcp.Description("Maximum number of matches to return. Defaults to 10."),
		),
		mcp.WithNumber("min_score",
			mcp.Description("Optional maximum distance threshold for returned matches."),
		),
	)
}

func bridgeFetchRemindersToolSpec() mcp.Tool {
	return mcp.NewTool("bridge.fetch_reminders",
		mcp.WithDescription("Return incomplete reminders from EventKit, optionally filtered by reminder list name."),
		mcp.WithString("list_name",
			mcp.Description("Optional reminder list name to filter by."),
		),
	)
}

func bridgeTool(socketPath, name string, log zerolog.Logger) registry.Tool {
	return func(ctx context.Context, args map[string]any) (any, error) {
		client, err := bridgeclient.New(socketPath, log)
		if err != nil {
			return nil, errors.Wrapf(err, "connect to bridge socket %q", socketPath)
		}
		defer client.Close()

		result, err := client.CallTool(ctx, name, args)
		if err != nil {
			return nil, errors.Wrapf(err, "call bridge tool %q", name)
		}
		return result, nil
	}
}
