package registry

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/rs/zerolog"

	"github.com/brightpuddle/clara/internal/store"
)

func TestAuditPolicy_SkipsHighFrequencyPollers(t *testing.T) {
	for _, name := range []string{"agent.hub.read", "agent.events.read", "agent.attention.list"} {
		if p := auditPolicyFor(name); !p.skip {
			t.Errorf("%s should not be audited", name)
		}
	}
	if auditPolicyFor("shell.run").skip {
		t.Error("shell.run must be audited")
	}
}

func TestAuditPolicy_ReadOnlyToolsGetSmallCap(t *testing.T) {
	for _, name := range []string{
		"webex.rooms.list", "discord.channels.list", "agent.session.list",
		"discord.channel.get", "macos.mail_get_message", "zk.note_search",
	} {
		p := auditPolicyFor(name)
		if p.skip || p.maxOutput != readOnlyOutputCap {
			t.Errorf("%s: got %+v, want cap %d", name, p, readOnlyOutputCap)
		}
	}
	if p := auditPolicyFor("shell.run"); p.maxOutput != defaultOutputCap {
		t.Errorf("shell.run cap = %d, want %d", p.maxOutput, defaultOutputCap)
	}
}

func TestCapAuditJSON_SmallValuesPassThrough(t *testing.T) {
	in := `{"ok":true}`
	if got := capAuditJSON(in, 100); got != in {
		t.Errorf("got %q, want unchanged", got)
	}
}

func TestCapAuditJSON_LargeValuesBecomeValidJSONPreview(t *testing.T) {
	in := `"` + strings.Repeat("é", 5000) + `"`
	got := capAuditJSON(in, 100)
	var v struct {
		Truncated bool   `json:"truncated"`
		Bytes     int    `json:"bytes"`
		Preview   string `json:"preview"`
	}
	if err := json.Unmarshal([]byte(got), &v); err != nil {
		t.Fatalf("not valid JSON: %v (%q)", err, got)
	}
	if !v.Truncated || v.Bytes != len(in) {
		t.Errorf("got %+v, want truncated with original size %d", v, len(in))
	}
	if len(v.Preview) > 100 {
		t.Errorf("preview is %d bytes, want <= 100", len(v.Preview))
	}
}

type recordingAudit struct{ calls []store.ToolCallRecord }

func (r *recordingAudit) RecordToolCall(_ context.Context, c store.ToolCallRecord) error {
	r.calls = append(r.calls, c)
	return nil
}

func TestCall_AppliesAuditPolicy(t *testing.T) {
	reg := New(zerolog.Nop())
	audit := &recordingAudit{}
	reg.SetAuditLogger(audit)
	big := strings.Repeat("x", 50_000)
	reg.Register("agent.hub.read", func(context.Context, map[string]any) (any, error) { return big, nil })
	reg.Register("webex.rooms.list", func(context.Context, map[string]any) (any, error) { return big, nil })
	reg.Register("shell.run", func(context.Context, map[string]any) (any, error) { return "ok", nil })

	for _, name := range []string{"agent.hub.read", "webex.rooms.list", "shell.run"} {
		if _, err := reg.Call(context.Background(), name, nil); err != nil {
			t.Fatal(err)
		}
	}

	if len(audit.calls) != 2 {
		t.Fatalf("recorded %d calls, want 2 (poller skipped)", len(audit.calls))
	}
	if got := len(audit.calls[0].OutputJSON); got > readOnlyOutputCap+128 {
		t.Errorf("list output stored at %d bytes, want <= ~%d", got, readOnlyOutputCap)
	}
	if audit.calls[1].OutputJSON != `"ok"` {
		t.Errorf("small output altered: %q", audit.calls[1].OutputJSON)
	}
}
