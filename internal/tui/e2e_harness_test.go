package tui

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"testing"
	"time"

	"github.com/brightpuddle/clara/internal/config"
	"github.com/brightpuddle/clara/internal/ipc"
	"github.com/brightpuddle/clara/internal/orchestrator"
	"github.com/brightpuddle/clara/internal/registry"
	"github.com/brightpuddle/clara/internal/store"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/cockroachdb/errors"
	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/rs/zerolog"
)

type E2EHarness struct {
	T        *testing.T
	Config   *config.Config
	Store    *store.Store
	Registry *registry.Registry
	
	// Mock Daemon
	IPCServer *ipc.Server
	
	// Active TUI (if any)
	TUIProgram *tea.Program
	TUIModel   *appModel
	TUIClosed  chan struct{}

	ctx    context.Context
	cancel context.CancelFunc
}

func NewE2EHarness(t *testing.T) *E2EHarness {
	ctx, cancel := context.WithCancel(context.Background())
	
	dataDir := t.TempDir()
	
	id := randomHex(4)
	cfg := &config.Config{
		DataDir: dataDir,
		ControlSocketPathOverride:    fmt.Sprintf("/tmp/cl_ctrl_%s.sock", id),
		DynamicMCPSocketPathOverride: fmt.Sprintf("/tmp/cl_mcp_%s.sock", id),
	}
	
	log := zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr}).With().Timestamp().Logger()
	s, err := store.Open(cfg.DBPath(), log)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	
	reg := registry.New(log)
	
	h := &E2EHarness{
		T:        t,
		Config:   cfg,
		Store:    s,
		Registry: reg,
		ctx:      ctx,
		cancel:   cancel,
	}
	
	t.Cleanup(func() {
		h.Close()
	})
	
	h.registerDaemonTools()
	h.startIPCServer()
	
	// Wait for server to be ready
	for i := 0; i < 20; i++ {
		conn, err := net.Dial("unix", cfg.ControlSocketPath())
		if err == nil {
			conn.Close()
			return h
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Log("Warning: IPC server did not become ready in time")
	
	return h
}

func (h *E2EHarness) registerDaemonTools() {
	h.Registry.RegisterDefault("tui.notify.send", func(ctx context.Context, args map[string]any) (any, error) {
		msg, _ := args["message"].(string)
		runID, _ := ctx.Value(orchestrator.ContextKeyRunID).(string)
		intentID, _ := ctx.Value(orchestrator.ContextKeyIntentID).(string)
		
		id, err := h.Store.SaveTUIContent(ctx, store.TUIContentItem{
			RunID:    runID,
			IntentID: intentID,
			Kind:     "notification",
			Text:     msg,
		})
		if err != nil {
			return nil, err
		}
		
		if h.Registry.Has("tui.hud_send") {
			args["id"] = float64(id)
			args["run_id"] = runID
			args["intent_id"] = intentID
			return h.Registry.Call(ctx, "tui.hud_send", args)
		}
		return "notification recorded (TUI offline)", nil
	})

	h.Registry.RegisterDefault("tui.notify.send_interactive", func(ctx context.Context, args map[string]any) (any, error) {
		prompt, _ := args["prompt"].(string)
		var opts []string
		if raw, ok := args["options"].([]any); ok {
			for _, r := range raw {
				if s, ok := r.(string); ok {
					opts = append(opts, s)
				}
			}
		} else if raw, ok := args["options"].([]string); ok {
			opts = raw
		}

		runID, _ := ctx.Value(orchestrator.ContextKeyRunID).(string)
		intentID, _ := ctx.Value(orchestrator.ContextKeyIntentID).(string)
		// CLI calls might pass intent_id in args
		if intentID == "" {
			intentID, _ = args["intent_id"].(string)
		}

		if intentID != "" {
			answer, _ := h.Store.GetTUIAnswer(ctx, intentID, prompt)
			if answer != "" {
				_, _ = h.Store.SaveTUIContent(ctx, store.TUIContentItem{
					RunID:    runID,
					IntentID: intentID,
					Kind:     "qa",
					Text:     prompt,
					Options:  opts,
					Answer:   answer,
				})
				return fmt.Sprintf("Answer received: %s", answer), nil
			}
		}

		// Deduplication logic
		var id int64
		if intentID != "" {
			id, _ = h.Store.GetUnansweredTUIPrompt(ctx, intentID, prompt)
		}
		
		if id == 0 {
			id, _ = h.Store.SaveTUIContent(ctx, store.TUIContentItem{
				RunID:    runID,
				IntentID: intentID,
				Kind:     "qa",
				Text:     prompt,
				Options:  opts,
			})
		}

		if h.Registry.Has("tui.hud_send_interactive") {
			args["id"] = float64(id)
			args["run_id"] = runID
			args["intent_id"] = intentID
			return h.Registry.Call(ctx, "tui.hud_send_interactive", args)
		}

		// CLI BLOCKING LOGIC
		if runID == "" {
			for {
				select {
				case <-ctx.Done():
					// If CLI call is cancelled (e.g. Ctrl+C), remove the prompt from DB
					// so it doesn't appear in TUI later.
					_ = h.Store.DeleteTUIContent(context.Background(), id)
					return nil, ctx.Err()
				case <-time.After(500 * time.Millisecond):
					history, _ := h.Store.LoadTUIContentHistory(ctx, 100)
					for _, item := range history {
						if item.ID == id && item.Answer != "" {
							return fmt.Sprintf("Answer received: %s", item.Answer), nil
						}
					}
				}
			}
		}

		return nil, errors.New("workflow paused: waiting for TUI input")
	})
}

func (h *E2EHarness) startIPCServer() {
	handler := func(ctx context.Context, req *ipc.Request, w ipc.ResponseWriter) {
		switch req.Method {
		case ipc.MethodToolCall:
			name, _ := req.Params["name"].(string)
			args, _ := req.Params["args"].(map[string]any)
			res, err := h.Registry.Call(ctx, name, args)
			if err != nil {
				w.Write(&ipc.Response{Error: err.Error()})
			} else {
				w.Write(&ipc.Response{Data: res})
			}
		case ipc.MethodTUIHistory:
			limit, _ := req.Params["limit"].(float64)
			history, _ := h.Store.LoadTUIContentHistory(ctx, int(limit))
			var items []map[string]any
			for _, item := range history {
				var opts []any
				for _, o := range item.Options {
					opts = append(opts, o)
				}
				items = append(items, map[string]any{
					"ID":       float64(item.ID),
					"RunID":    item.RunID,
					"IntentID": item.IntentID,
					"Kind":     item.Kind,
					"Text":     item.Text,
					"Options":  opts,
					"Answer":   item.Answer,
				})
			}
			w.Write(&ipc.Response{Data: items})
		case ipc.MethodTUIAnswer:
			id, _ := req.Params["id"].(float64)
			answer, _ := req.Params["answer"].(string)
			err := h.Store.UpdateTUIContentAnswer(ctx, int64(id), answer)
			if err != nil {
				w.Write(&ipc.Response{Error: err.Error()})
			} else {
				w.Write(&ipc.Response{Message: "answer recorded"})
			}
		case ipc.MethodStart:
			w.Write(&ipc.Response{Message: "Harness: Start ignored (stateless)"})
		case ipc.MethodMCPRegister:
			token := "test-token"
			socketPath := h.Config.DynamicMCPSocketPath()
			
			var l net.Listener
			var listenErr error
			for i := 0; i < 5; i++ {
				_ = os.Remove(socketPath)
				l, listenErr = net.Listen("unix", socketPath)
				if listenErr == nil {
					break
				}
				time.Sleep(100 * time.Millisecond)
			}
			if listenErr != nil {
				w.Write(&ipc.Response{Error: fmt.Sprintf("harness failed to listen: %v", listenErr)})
				return
			}
			
			go func() {
				defer l.Close()
				
				if ul, ok := l.(*net.UnixListener); ok {
					ul.SetDeadline(time.Now().Add(2 * time.Second))
				}

				conn, err := l.Accept()
				if err != nil {
					return
				}
				
				var handshake map[string]string
				if err := json.NewDecoder(conn).Decode(&handshake); err != nil {
					return
				}
				
				json.NewEncoder(conn).Encode(map[string]string{"message": "OK"})
				
				transport := registry.NewConnTransport(conn)
				mcpClient := client.NewClient(transport)
				if err := mcpClient.Start(h.ctx); err != nil {
					return
				}
				
				time.Sleep(50 * time.Millisecond)

				_, err = mcpClient.Initialize(h.ctx, mcp.InitializeRequest{})
				if err != nil {
					return
				}
				
				toolsRes, err := mcpClient.ListTools(h.ctx, mcp.ListToolsRequest{})
				if err != nil {
					return
				}
				
				caps := &registry.ServerCapabilities{Name: "tui", Tools: toolsRes.Tools}
				h.Registry.RegisterConnectedClient("tui", mcpClient, caps, transport.Close)
			}()
			w.Write(&ipc.Response{Data: map[string]any{
				"name":        "tui",
				"token":       token,
				"socket_path": socketPath,
			}})
		default:
			w.Write(&ipc.Response{Error: "unsupported method"})
		}
	}

	srv, err := ipc.NewServer(h.Config.ControlSocketPath(), ipc.HandlerFunc(handler), zerolog.New(io.Discard))
	if err != nil {
		h.T.Fatalf("failed to create IPC server: %v", err)
	}
	h.IPCServer = srv
	go h.IPCServer.ListenAndServe(h.ctx)
}

// TUISnapshot returns a thread-safe copy of the TUI model's content state.
// Safe to call from any goroutine.
func (h *E2EHarness) TUISnapshot() modelSnapshot {
	if h.TUIModel == nil {
		return modelSnapshot{}
	}
	if snap, ok := h.TUIModel.snapshot.Load().(modelSnapshot); ok {
		return snap
	}
	return modelSnapshot{}
}

func (h *E2EHarness) StartTUI() error {
	return h.StartTUIWithHistory(0)
}

func (h *E2EHarness) StartTUIWithHistory(minItems int) error {
	theme := DefaultTheme()
	h.TUIModel = &appModel{
		cfg:     h.Config,
		client:  NewIPCClient(h.Config),
		theme:   theme,
		content: newContentModel(theme),
		prompt:  newPromptModel(theme),
		msgChan: make(chan tea.Msg, 100),
	}
	h.TUIProgram = tea.NewProgram(h.TUIModel, tea.WithInput(nil), tea.WithOutput(io.Discard))
	h.TUIClosed = make(chan struct{})
	go func() {
		h.TUIProgram.Run()
		close(h.TUIClosed)
	}()
	mcpSrv := NewTUIServer(h.TUIProgram, h.TUIModel.client)
	mcpDone := make(chan error, 1)
	go func() {
		mcpDone <- h.TUIModel.client.StartDynamicMCP(h.ctx, mcpSrv)
	}()
	
	// Wait for TUI to be registered in registry AND history to be loaded
	for i := 0; i < 100; i++ {
		select {
		case err := <-mcpDone:
			if err != nil {
				return fmt.Errorf("dynamic MCP failed: %w", err)
			}
		default:
		}

		registered := h.Registry.Has("tui.hud_send_interactive")
		snap := h.TUISnapshot()
		historyLoaded := (len(snap.items) + len(snap.pendingItems)) >= minItems
		if registered && historyLoaded {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	snap := h.TUISnapshot()
	return fmt.Errorf("TUI did not register tools or load history in time (registered: %v, items: %d, pending: %d, want: %d)",
		h.Registry.Has("tui.hud_send_interactive"), len(snap.items), len(snap.pendingItems), minItems)
}

func (h *E2EHarness) StopTUI() {
	if h.TUIProgram != nil {
		h.TUIProgram.Quit()
		select {
		case <-h.TUIClosed:
		case <-time.After(5 * time.Second):
			h.T.Log("Warning: TUI did not close in time")
		}
		h.TUIProgram = nil
		h.TUIModel = nil
		// Give some time for socket to be released
		time.Sleep(200 * time.Millisecond)
	}
}

func (h *E2EHarness) Close() {
	h.cancel()
	h.StopTUI()
	h.Store.Close()
	os.RemoveAll(h.Config.DataDir)
	_ = os.Remove(h.Config.ControlSocketPath())
	_ = os.Remove(h.Config.DynamicMCPSocketPath())
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (h *E2EHarness) SendKey(k string) {
	if h.TUIProgram != nil {
		h.TUIProgram.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)})
	}
}

func (h *E2EHarness) CLIToolCall(name string, args map[string]any) (*ipc.Response, error) {
	client := NewIPCClient(h.Config)
	return client.Do(ipc.Request{
		Method: ipc.MethodToolCall,
		Params: map[string]any{
			"name": name,
			"args": args,
		},
	})
}

func (h *E2EHarness) CallTool(ctx context.Context, name string, args map[string]any) (*mcp.CallToolResult, error) {
	res, err := h.Registry.Call(ctx, name, args)
	if err != nil {
		return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{mcp.TextContent{Type: "text", Text: err.Error()}}}, nil
	}
	
	var content []mcp.Content
	if s, ok := res.(string); ok {
		content = append(content, mcp.TextContent{Type: "text", Text: s})
	} else {
		data, _ := json.Marshal(res)
		content = append(content, mcp.TextContent{Type: "text", Text: string(data)})
	}

	return &mcp.CallToolResult{Content: content}, nil
}
