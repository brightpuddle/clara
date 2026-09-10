package templ

import (
	"github.com/brightpuddle/clara/internal/store"
	"github.com/brightpuddle/clara/internal/trigger"
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
	Base                 BaseVM
	TriggersCount        int
	EventTriggersCount   int
	ScheduleTriggersCount int
	WorkerTriggersCount  int
	ToolsCount           int
	IntegrationsCount    int
	Triggers             []trigger.Definition
	RecentRuns           []store.TriggerRunRecord
	Integrations         []map[string]any
	RecentLogs           []string
}

// TriggersVM holds data for the triggers list page.
type TriggersVM struct {
	Base       BaseVM
	Triggers   []trigger.Definition
	RecentRuns []store.TriggerRunRecord
}

// TriggerDetailVM holds data for a trigger detail page.
type TriggerDetailVM struct {
	Base       BaseVM
	Trigger    trigger.Definition
	RecentRuns []store.TriggerRunRecord
	RunResult  *trigger.RunRecord
	MatchResult *bool
}

// RunsVM holds data for the execution audit log page.
type RunsVM struct {
	Base          BaseVM
	Runs          []store.TriggerRunRecord
	SelectedRun   *store.TriggerRunRecord
	ToolCalls     []store.ToolCallRecord
	TriggerFilter string
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
