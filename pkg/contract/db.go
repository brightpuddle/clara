package contract

import (
	"net/rpc"

	"github.com/hashicorp/go-plugin"
)

// DBIntegration is the interface for database integrations.
type DBIntegration interface {
	Integration
	Query(sql string, params []any) ([]map[string]any, error)
	Exec(sql string, params []any) (int64, error)
	VecSearch(table string, vector []float32, limit int, minScore float64) ([]map[string]any, error)
	StageRows(table string, rows []any, replace bool) (int, error)
}

// --- RPC Wrappers ---

type DBIntegrationRPC struct {
	IntegrationRPC
}

func (g *DBIntegrationRPC) Query(sql string, params []any) ([]map[string]any, error) {
	var resp []map[string]any
	err := g.Client.Call("Plugin.Query", QueryArgs{SQL: sql, Params: params}, &resp)
	return resp, err
}

func (g *DBIntegrationRPC) Exec(sql string, params []any) (int64, error) {
	var resp int64
	err := g.Client.Call("Plugin.Exec", QueryArgs{SQL: sql, Params: params}, &resp)
	return resp, err
}

func (g *DBIntegrationRPC) VecSearch(table string, vector []float32, limit int, minScore float64) ([]map[string]any, error) {
	var resp []map[string]any
	err := g.Client.Call("Plugin.VecSearch", VecSearchArgs{Table: table, Vector: vector, Limit: limit, MinScore: minScore}, &resp)
	return resp, err
}

func (g *DBIntegrationRPC) StageRows(table string, rows []any, replace bool) (int, error) {
	var resp int
	err := g.Client.Call("Plugin.StageRows", StageRowsArgs{Table: table, Rows: rows, Replace: replace}, &resp)
	return resp, err
}

type QueryArgs struct {
	SQL    string
	Params []any
}

type VecSearchArgs struct {
	Table    string
	Vector   []float32
	Limit    int
	MinScore float64
}

type StageRowsArgs struct {
	Table   string
	Rows    []any
	Replace bool
}

type DBIntegrationRPCServer struct {
	IntegrationRPCServer
	Impl DBIntegration
}

func (s *DBIntegrationRPCServer) Query(args QueryArgs, resp *[]map[string]any) error {
	var err error
	*resp, err = s.Impl.Query(args.SQL, args.Params)
	return err
}

func (s *DBIntegrationRPCServer) Exec(args QueryArgs, resp *int64) error {
	var err error
	*resp, err = s.Impl.Exec(args.SQL, args.Params)
	return err
}

func (s *DBIntegrationRPCServer) VecSearch(args VecSearchArgs, resp *[]map[string]any) error {
	var err error
	*resp, err = s.Impl.VecSearch(args.Table, args.Vector, args.Limit, args.MinScore)
	return err
}

func (s *DBIntegrationRPCServer) StageRows(args StageRowsArgs, resp *int) error {
	var err error
	*resp, err = s.Impl.StageRows(args.Table, args.Rows, args.Replace)
	return err
}

func (s *DBIntegrationRPCServer) Configure(config []byte, resp *struct{}) error {
	return s.Impl.Configure(config)
}

func (s *DBIntegrationRPCServer) Description(args EmptyArgs, resp *string) error {
	var err error
	*resp, err = s.Impl.Description()
	return err
}

func (s *DBIntegrationRPCServer) Tools(args EmptyArgs, resp *[]byte) error {
	var err error
	*resp, err = s.Impl.Tools()
	return err
}

func (s *DBIntegrationRPCServer) CallTool(args CallToolArgs, resp *[]byte) error {
	var err error
	*resp, err = s.Impl.CallTool(args.Name, args.Args)
	return err
}

type DBIntegrationPlugin struct {
	Impl DBIntegration
}

func (p *DBIntegrationPlugin) Server(*plugin.MuxBroker) (interface{}, error) {
	return &DBIntegrationRPCServer{
		IntegrationRPCServer: IntegrationRPCServer{Impl: p.Impl},
		Impl:                 p.Impl,
	}, nil
}

func (p *DBIntegrationPlugin) Client(b *plugin.MuxBroker, c *rpc.Client) (interface{}, error) {
	return &DBIntegrationRPC{IntegrationRPC: IntegrationRPC{Client: c}}, nil
}
