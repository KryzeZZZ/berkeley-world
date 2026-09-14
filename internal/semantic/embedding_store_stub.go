//go:build !pgvector

package semantic

import (
	"context"
	"fmt"
)

type pgEmbeddingStore struct{}

func newPGEmbeddingStore(_ context.Context, _ string, _ int) (*pgEmbeddingStore, error) {
	return nil, fmt.Errorf("pgvector support requires build tag 'pgvector'")
}

func (s *pgEmbeddingStore) GetEmbeddings(_ context.Context, _ []string) (map[string][]float64, error) {
	return nil, fmt.Errorf("pgvector support requires build tag 'pgvector'")
}

func (s *pgEmbeddingStore) UpsertEmbeddings(_ context.Context, _ map[string][]float64) error {
	return fmt.Errorf("pgvector support requires build tag 'pgvector'")
}

func (s *pgEmbeddingStore) Close() {}
