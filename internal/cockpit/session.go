package cockpit

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
)

// SessionSummary summarizes an agent session for the cockpit views.
type SessionSummary struct {
	ID        string `json:"id"`
	Host      string `json:"host"`
	Agent     string `json:"agent"`
	Dir       string `json:"dir"`
	Title     string `json:"title"`
	State     string `json:"state"`
	Model     string `json:"model"`
	QueueLen  int    `json:"queue_len"`
	ActiveTurn string `json:"active_turn"`
}

// GitInfo holds branch and dirty working tree status for a directory.
type GitInfo struct {
	IsRepo    bool
	Branch    string
	IsDirty   bool
	Ahead     int
	Behind    int
}

// Format returns a concise git status indicator like "main*" or "feat/x ↑1↓2".
func (g GitInfo) Format() string {
	if !g.IsRepo {
		return ""
	}
	var b strings.Builder
	b.WriteString(g.Branch)
	if g.IsDirty {
		b.WriteString("*")
	}
	if g.Ahead > 0 || g.Behind > 0 {
		b.WriteString(fmt.Sprintf(" ↑%d↓%d", g.Ahead, g.Behind))
	}
	return b.String()
}

// ResolveSession finds a matching session by exact ID, prefix, suffix, or title query.
func ResolveSession(sessions []SessionSummary, query string) (SessionSummary, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return SessionSummary{}, errors.New("empty session query")
	}

	qLower := strings.ToLower(q)

	// 1. Exact ID match (case-insensitive)
	for _, s := range sessions {
		if strings.EqualFold(s.ID, q) {
			return s, nil
		}
	}

	// 2. ID prefix match
	var prefixMatches []SessionSummary
	for _, s := range sessions {
		if strings.HasPrefix(strings.ToLower(s.ID), qLower) {
			prefixMatches = append(prefixMatches, s)
		}
	}
	if len(prefixMatches) == 1 {
		return prefixMatches[0], nil
	}
	if len(prefixMatches) > 1 {
		return SessionSummary{}, errors.Newf("ambiguous session prefix %q matched %d sessions", q, len(prefixMatches))
	}

	// 3. ID suffix match (convenient for typing last 4-6 chars)
	var suffixMatches []SessionSummary
	for _, s := range sessions {
		if strings.HasSuffix(strings.ToLower(s.ID), qLower) {
			suffixMatches = append(suffixMatches, s)
		}
	}
	if len(suffixMatches) == 1 {
		return suffixMatches[0], nil
	}

	// 4. Title match
	var titleMatches []SessionSummary
	for _, s := range sessions {
		if strings.Contains(strings.ToLower(s.Title), qLower) {
			titleMatches = append(titleMatches, s)
		}
	}
	if len(titleMatches) == 1 {
		return titleMatches[0], nil
	}
	if len(titleMatches) > 1 {
		return SessionSummary{}, errors.Newf("ambiguous query %q matched multiple session titles", q)
	}

	return SessionSummary{}, errors.Newf("no session matching %q found", q)
}

// DetectGitStatus inspects the git status for a local directory.
func DetectGitStatus(dir string) GitInfo {
	if dir == "" {
		return GitInfo{}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "--abbrev-ref", "HEAD")
	branchOut, err := cmd.Output()
	if err != nil {
		return GitInfo{IsRepo: false}
	}

	branch := strings.TrimSpace(string(branchOut))
	if branch == "" {
		branch = "HEAD"
	}

	info := GitInfo{
		IsRepo: true,
		Branch: branch,
	}

	// Check dirty status
	statusCmd := exec.CommandContext(ctx, "git", "-C", dir, "status", "--porcelain")
	statusOut, err := statusCmd.Output()
	if err == nil && len(strings.TrimSpace(string(statusOut))) > 0 {
		info.IsDirty = true
	}

	// Check ahead/behind
	revCmd := exec.CommandContext(ctx, "git", "-C", dir, "rev-list", "--left-right", "--count", "@{upstream}...HEAD")
	revOut, err := revCmd.Output()
	if err == nil {
		fields := strings.Fields(string(revOut))
		if len(fields) == 2 {
			var behind, ahead int
			fmt.Sscanf(fields[0], "%d", &behind)
			fmt.Sscanf(fields[1], "%d", &ahead)
			info.Behind = behind
			info.Ahead = ahead
		}
	}

	return info
}
