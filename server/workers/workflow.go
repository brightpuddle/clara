package workers

import (
	"time"

	"github.com/brightpuddle/clara/server/db"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

var defaultActivityOptions = workflow.ActivityOptions{
	StartToCloseTimeout: 2 * time.Minute,
	RetryPolicy: &temporal.RetryPolicy{
		MaximumAttempts: 3,
	},
}

// LinkAnalysisWorkflow finds semantically similar documents and creates
// backlink suggestions. It is triggered after a note is ingested.
func LinkAnalysisWorkflow(ctx workflow.Context, documentID string) error {
	ctx = workflow.WithActivityOptions(ctx, defaultActivityOptions)

	// Temporal uses the registered Activities struct; nil is fine for method ref.
	var acts *Activities

	// 1. Find similar documents via pgvector
	var candidates []db.SimilarDoc
	if err := workflow.ExecuteActivity(ctx, acts.FindSimilarDocs, documentID).Get(ctx, &candidates); err != nil {
		return err
	}

	if len(candidates) == 0 {
		return nil
	}

	// 2. Create suggestions for pairs that don't already have a wikilink
	return workflow.ExecuteActivity(ctx, acts.CreateSuggestions, documentID, candidates).Get(ctx, nil)
}
