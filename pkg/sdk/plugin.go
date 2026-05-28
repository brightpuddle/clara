package sdk

import (
	"context"
	"encoding/json"
	"net/rpc"

	"github.com/hashicorp/go-plugin"
)

// HandshakeConfig must match between the daemon (client) and the actuator binary (server).
var HandshakeConfig = plugin.HandshakeConfig{
	ProtocolVersion:  1,
	MagicCookieKey:   "CLARA_ACTUATOR_MAGIC_COOKIE",
	MagicCookieValue: "clara_actuator_v1",
}

// PluginMap is the plugin map used by both the daemon loader and Serve().
var PluginMap = map[string]plugin.Plugin{
	"actuator": &ActuatorPlugin{},
}

// --------------------------------------------------------------------------
// Wire types (net/rpc requires concrete, JSON-serialisable structs)
// --------------------------------------------------------------------------

// ManifestArgs is the RPC argument for Manifest (no fields needed).
type ManifestArgs struct{}

// ExecuteArgs carries the JSON-encoded Event over RPC.
type ExecuteArgs struct {
	EventJSON []byte
}

// ExecuteReply carries the JSON-encoded Result (or an error string) over RPC.
type ExecuteReply struct {
	ResultJSON []byte
	ErrString  string
}

// --------------------------------------------------------------------------
// RPC client — used by the daemon
// --------------------------------------------------------------------------

// ActuatorRPC implements Actuator over net/rpc.
type ActuatorRPC struct {
	client *rpc.Client
}

func (a *ActuatorRPC) Manifest() ActuatorManifest {
	var manifest ActuatorManifest
	_ = a.client.Call("Plugin.Manifest", ManifestArgs{}, &manifest)
	return manifest
}

func (a *ActuatorRPC) Execute(_ context.Context, event Event) (Result, error) {
	b, err := json.Marshal(event)
	if err != nil {
		return Result{}, err
	}

	var reply ExecuteReply
	if err := a.client.Call("Plugin.Execute", ExecuteArgs{EventJSON: b}, &reply); err != nil {
		return Result{}, err
	}
	if reply.ErrString != "" {
		return Result{}, &rpcError{reply.ErrString}
	}

	var result Result
	if err := json.Unmarshal(reply.ResultJSON, &result); err != nil {
		return Result{}, err
	}
	return result, nil
}

// --------------------------------------------------------------------------
// RPC server — used by the actuator binary
// --------------------------------------------------------------------------

// ActuatorRPCServer wraps the user's Actuator implementation for net/rpc.
type ActuatorRPCServer struct {
	Impl Actuator
}

func (s *ActuatorRPCServer) Manifest(_ ManifestArgs, resp *ActuatorManifest) error {
	*resp = s.Impl.Manifest()
	return nil
}

func (s *ActuatorRPCServer) Execute(args ExecuteArgs, resp *ExecuteReply) error {
	var event Event
	if err := json.Unmarshal(args.EventJSON, &event); err != nil {
		resp.ErrString = err.Error()
		return nil
	}

	result, err := s.Impl.Execute(context.Background(), event)
	if err != nil {
		resp.ErrString = err.Error()
		return nil
	}

	b, err := json.Marshal(result)
	if err != nil {
		resp.ErrString = err.Error()
		return nil
	}
	resp.ResultJSON = b
	return nil
}

// --------------------------------------------------------------------------
// Plugin wrapper
// --------------------------------------------------------------------------

// ActuatorPlugin is the go-plugin plugin.Plugin implementation.
type ActuatorPlugin struct {
	// Impl is set only on the server side (inside the actuator binary).
	Impl Actuator
}

func (p *ActuatorPlugin) Server(_ *plugin.MuxBroker) (interface{}, error) {
	return &ActuatorRPCServer{Impl: p.Impl}, nil
}

func (p *ActuatorPlugin) Client(_ *plugin.MuxBroker, c *rpc.Client) (interface{}, error) {
	return &ActuatorRPC{client: c}, nil
}

// --------------------------------------------------------------------------
// helpers
// --------------------------------------------------------------------------

type rpcError struct{ msg string }

func (e *rpcError) Error() string { return e.msg }
