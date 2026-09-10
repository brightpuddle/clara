package templ

import (
	"github.com/brightpuddle/clara/internal/supervisor"
	"github.com/brightpuddle/clara/internal/webui/manifest"
)

// IndexVM holds base HTML shell configuration.
type IndexVM struct {
	Manifest manifest.Manifest
	Title    string
	IsDev    bool
	DevHost  string
}

// NavbarVM holds top app bar configuration.
type NavbarVM struct {
	Title string
}

// DrawerVM holds sidebar drawer navigation configuration.
type DrawerVM struct {
	Location string
}

// LayoutVM holds master layout properties.
type LayoutVM struct {
	Navbar *NavbarVM
	Drawer *DrawerVM
}

// BaseVM holds shared properties for pages.
type BaseVM struct {
	Index  *IndexVM
	Layout *LayoutVM
}

// NewBaseVM constructs a BaseVM.
func NewBaseVM(m manifest.Manifest, isDev bool, devHost, pageTitle, location string) BaseVM {
	return BaseVM{
		Index: &IndexVM{
			Manifest: m,
			Title:    pageTitle + " — Clara",
			IsDev:    isDev,
			DevHost:  devHost,
		},
		Layout: &LayoutVM{
			Navbar: &NavbarVM{
				Title: pageTitle,
			},
			Drawer: &DrawerVM{
				Location: location,
			},
		},
	}
}

// DashboardVM holds data for the dashboard page.
type DashboardVM struct {
	Base              BaseVM
	ActuatorsCount    int
	ToolsCount        int
	IntegrationsCount int
	PendingApprovals  int
	Automations       []supervisor.AutomationSummary
	Integrations      []map[string]any
	RecentLogs        []string
}

// ActuatorsVM holds data for the actuators/automations list page.
type ActuatorsVM struct {
	Base        BaseVM
	Automations []supervisor.AutomationSummary
}

// ActuatorDetailVM holds data for an actuator detail page.
type ActuatorDetailVM struct {
	Base       BaseVM
	Summary    supervisor.AutomationSummary
	RecentLogs []string
	RunResult  string
	RunSuccess bool
}

// ApprovalsVM holds data for the HITL approvals page.
type ApprovalsVM struct {
	Base      BaseVM
	Approvals []supervisor.ApprovalRequest
	Flash     string
	FlashKind string
}

// IntegrationsVM holds data for the integrations and MCP servers page.
type IntegrationsVM struct {
	Base       BaseVM
	Plugins    []map[string]any
	MCPServers []map[string]any
}

// LogsVM holds data for the logs streaming page.
type LogsVM struct {
	Base        BaseVM
	Lines       []string
	LevelFilter string
}

// ConfigVM holds data for the configuration YAML and structured editor.
type ConfigVM struct {
	Base       BaseVM
	ConfigJSON string
	YAML       string
	Flash      string
	FlashKind  string
	ReadOnly   bool
}
