package opencode

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"

	"github.com/brightpuddle/clara/internal/agents"
)

type apiModel struct {
	ID         string `json:"id"`
	ProviderID string `json:"providerID"`
}

func (m apiModel) String() string {
	if m.ProviderID == "" {
		return m.ID
	}
	return m.ProviderID + "/" + m.ID
}

type apiSession struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Model    apiModel
	Location struct {
		Directory string `json:"directory"`
	} `json:"location"`
}

type formField struct {
	Key         string `json:"key"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Custom      bool   `json:"custom"`
	Options     []struct {
		Value string `json:"value"`
		Label string `json:"label"`
	} `json:"options"`
}

type form struct {
	ID        string      `json:"id"`
	SessionID string      `json:"sessionID"`
	Title     string      `json:"title"`
	Fields    []formField `json:"fields"`
}

func (f form) attention() agents.AttentionRequested {
	a := agents.AttentionRequested{
		AttentionID: f.ID, Kind: agents.AttentionQuestion, Title: f.Title,
		Native: &agents.NativeRef{FormID: f.ID},
	}
	if len(f.Fields) != 1 {
		return a
	}
	fl := f.Fields[0]
	switch {
	case fl.Description != "":
		a.Title = fl.Description
	case fl.Title != "":
		a.Title = fl.Title
	}
	a.AllowText = fl.Custom
	for _, o := range fl.Options {
		a.Options = append(a.Options, agents.Option{ID: o.Value, Label: o.Label})
	}
	return a
}

func firstAnswer(ans map[string]any) string {
	for _, v := range ans {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

type apiMessage struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Text string `json:"text"`
	Time struct {
		Completed int64 `json:"completed"`
	} `json:"time"`
	Outcome string `json:"outcome"`
	Content []struct {
		Type  string `json:"type"`
		ID    string `json:"id"`
		Name  string `json:"name"`
		Text  string `json:"text"`
		State struct {
			Status  string `json:"status"`
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"state"`
	} `json:"content"`
}

const pageSize = 200

// catchUp ingests every message of the session not yet seen, in order. It
// stops at an assistant message that is still streaming, so nothing is
// recorded half-finished and the next trigger picks it up.
func (m *Mirror) catchUp(ctx context.Context, id string) error {
	st := m.sess[id]
	base := "/api/session/" + url.PathEscape(id) + "/message?order=asc&limit=" + strconv.Itoa(pageSize)
	path := base
	for {
		var page struct {
			Data   []json.RawMessage `json:"data"`
			Cursor struct {
				Next string `json:"next"`
			} `json:"cursor"`
		}
		if err := m.c.do(ctx, http.MethodGet, path, nil, &page); err != nil {
			return err
		}
		for _, raw := range page.Data {
			var msg apiMessage
			if err := json.Unmarshal(raw, &msg); err != nil || st.seen[msg.ID] {
				continue
			}
			if msg.Type == "assistant" && msg.Time.Completed == 0 {
				return nil
			}
			m.mapMessage(ctx, id, st, msg)
			st.seen[msg.ID] = true
			m.markSeen(ctx, id, msg.ID)
		}
		if len(page.Data) < pageSize || page.Cursor.Next == "" {
			return nil
		}
		path = "/api/session/" + url.PathEscape(id) + "/message?cursor=" + url.QueryEscape(page.Cursor.Next)
	}
}

func (m *Mirror) mapMessage(ctx context.Context, id string, st *sessState, msg apiMessage) {
	switch msg.Type {
	case "user":
		m.record(ctx, id, agents.TypeMessageUser,
			agents.MessageUser{MessageID: msg.ID, Text: msg.Text, Origin: agentName})
		st.activeTurn, st.tools = msg.ID, nil
		m.record(ctx, id, agents.TypeTurnStarted, agents.TurnStarted{TurnID: msg.ID})
		m.record(ctx, id, agents.TypeSessionStatus,
			agents.SessionStatus{State: agents.StateRunning})
	case "assistant":
		turn := st.activeTurn
		if turn == "" {
			turn = msg.ID
		}
		text := ""
		for _, p := range msg.Content {
			switch p.Type {
			case "tool":
				st.tools = append(st.tools, p.Name)
				m.record(ctx, id, agents.TypeToolCall, agents.ToolCall{
					CallID: p.ID, TurnID: turn, Name: p.Name, Status: "in_progress",
				})
				summary := ""
				if len(p.State.Content) > 0 {
					summary = truncate(p.State.Content[0].Text, 500)
				}
				m.record(ctx, id, agents.TypeToolUpdate, agents.ToolUpdate{
					CallID: p.ID, Status: p.State.Status, OutputSummary: summary,
				})
			case "text":
				text += p.Text
			}
		}
		if text != "" {
			m.record(ctx, id, agents.TypeMessageAssistant,
				agents.MessageAssistant{MessageID: msg.ID, TurnID: turn, Text: text})
		}
	case "idle":
		turn := st.activeTurn
		if turn == "" {
			turn = msg.ID
		}
		stop := stopReason(msg.Outcome)
		m.record(ctx, id, agents.TypeTurnEnded, agents.TurnEnded{
			TurnID: turn, StopReason: stop, ToolsSummary: summarize(st.tools),
		})
		status := agents.SessionStatus{State: agents.StateIdle}
		if stop == agents.StopError {
			status.Reason = msg.Outcome
		}
		m.record(ctx, id, agents.TypeSessionStatus, status)
		st.activeTurn, st.tools = "", nil
	}
}

func stopReason(outcome string) agents.StopReason {
	switch outcome {
	case "succeeded":
		return agents.StopEndTurn
	case "interrupted", "cancelled", "aborted":
		return agents.StopCancelled
	}
	return agents.StopError
}

func summarize(tools []string) agents.ToolsSummary {
	s := agents.ToolsSummary{Count: len(tools)}
	last := tools
	if len(last) > 3 {
		last = last[len(last)-3:]
	}
	s.Last = append(s.Last, last...)
	return s
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
