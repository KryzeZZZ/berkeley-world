package semantic

import "context"

type embeddingStore interface {
	GetEmbeddings(ctx context.Context, texts []string) (map[string][]float64, error)
	UpsertEmbeddings(ctx context.Context, vectors map[string][]float64) error
	Close()
}
