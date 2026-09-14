//go:build !pgvector

package semantic

import (
	"context"
	"fmt"
)

func MatchCandidatesWithDB(_ context.Context, _ string, _ string, _ map[string]string) (MatchResult, error) {
	return MatchResult{}, fmt.Errorf("pgvector build tag required for db matching")
}
