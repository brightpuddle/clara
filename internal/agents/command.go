package agents

import (
	"encoding/json"
	"reflect"

	"github.com/cockroachdb/errors"
)

// decodeInto unmarshals raw into a new value of the same type as zero and
// returns it as a value (not a pointer).
func decodeInto(zero any, raw json.RawMessage) (any, error) {
	ptr := reflect.New(reflect.TypeOf(zero))
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, ptr.Interface()); err != nil {
			return nil, errors.Wrapf(err, "decode %T", zero)
		}
	}
	return ptr.Elem().Interface(), nil
}

func sameType(a, b any) bool { return reflect.TypeOf(a) == reflect.TypeOf(b) }

// Command is a client request. CommandID is the idempotency key.
type Command struct {
	CommandID string          `json:"command_id"`
	Device    string          `json:"device,omitempty"`
	Name      string          `json:"name"`
	Args      json.RawMessage `json:"args"`
}

// DecodeArgs returns the typed argument struct for the command name.
func (c Command) DecodeArgs() (any, error) {
	var zero any
	switch c.Name {
	case "session.create":
		zero = SessionCreate{}
	case "session.close":
		zero = SessionClose{}
	case "session.resume":
		zero = SessionResume{}
	case "prompt.send":
		zero = PromptSend{}
	case "prompt.cancel":
		zero = PromptCancel{}
	case "turn.interrupt":
		zero = TurnInterrupt{}
	case "attention.answer":
		zero = AttentionAnswer{}
	case "session.mark_read":
		zero = SessionMarkRead{}
	default:
		return nil, errors.Newf("unknown command %q", c.Name)
	}
	return decodeInto(zero, c.Args)
}

type SessionCreate struct {
	Host   string      `json:"host"`
	Agent  string      `json:"agent"`
	Dir    string      `json:"dir"`
	Model  string      `json:"model,omitempty"`
	Kind   SessionKind `json:"kind"`
	Prompt string      `json:"prompt,omitempty"`
	Title  string      `json:"title,omitempty"`
}

type SessionClose struct {
	Session string `json:"session"`
}

type SessionResume struct {
	Host     string `json:"host"`
	Agent    string `json:"agent"`
	NativeID string `json:"native_id"`
}

type PromptSend struct {
	Session     string       `json:"session"`
	Text        string       `json:"text"`
	Attachments []Attachment `json:"attachments,omitempty"`
}

type PromptCancel struct {
	Session  string `json:"session"`
	PromptID string `json:"prompt_id"`
}

type TurnInterrupt struct {
	Session string `json:"session"`
}

type AttentionAnswer struct {
	AttentionID string `json:"attention_id"`
	Answer      string `json:"answer"`
}

type SessionMarkRead struct {
	Session string `json:"session"`
	UptoSeq int64  `json:"upto_seq"`
}
