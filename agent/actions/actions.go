package actions

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/brightpuddle/clara/pb"
)

// Executor applies server-approved actions to local files.
type Executor struct{}

func NewExecutor() *Executor { return &Executor{} }

// Execute dispatches to the appropriate handler based on action type.
func (e *Executor) Execute(ctx context.Context, action *pb.Action) error {
	switch action.Type {
	case pb.ActionType_ACTION_ADD_BACKLINK:
		return addBacklink(action.DocumentPath, action.LinkTarget)
	default:
		return fmt.Errorf("unknown action type: %v", action.Type)
	}
}

var seeAlsoRe = regexp.MustCompile(`(?im)^#{1,6}\s*(see also|related|links|references)\s*$`)

// addBacklink inserts a [[wikilink]] into the target markdown file.
// It appends to an existing "## See Also" section, or creates one at the end.
func addBacklink(path, linkTarget string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}

	content := string(data)

	// Idempotency check: skip if link already present
	if strings.Contains(content, linkTarget) {
		return nil
	}

	var updated string
	if loc := seeAlsoRe.FindStringIndex(content); loc != nil {
		// Insert after the heading line
		insertAt := loc[1]
		// Skip to end of that line
		if nl := strings.Index(content[insertAt:], "\n"); nl != -1 {
			insertAt += nl + 1
		}
		updated = content[:insertAt] + "- " + linkTarget + "\n" + content[insertAt:]
	} else {
		// Append a new ## See Also section
		suffix := "\n## See Also\n\n- " + linkTarget + "\n"
		if strings.HasSuffix(content, "\n") {
			updated = content + suffix[1:] // avoid double newline
		} else {
			updated = content + "\n" + suffix[1:]
		}
	}

	return os.WriteFile(path, []byte(updated), 0644)
}
