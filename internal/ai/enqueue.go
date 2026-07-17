package ai

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/TuotHash/mindful-social/internal/db"
)

// Enqueue creates a pending generation job for the user. urls are the
// (already-trimmed) source links the user supplied; useSearch asks the worker
// to also search the web. The background Worker picks the row up.
func Enqueue(ctx context.Context, queries *db.Queries, userID uuid.UUID, prompt string, urls []string, useSearch bool) (db.NodeGenerationJob, error) {
	urlsJSON, err := json.Marshal(urls)
	if err != nil {
		return db.NodeGenerationJob{}, err
	}
	return queries.EnqueueGenerationJob(ctx, db.EnqueueGenerationJobParams{
		UserID:    userID,
		Prompt:    prompt,
		InputUrls: urlsJSON,
		UseSearch: useSearch,
	})
}
