//go:build !pgvector

package world

import "fmt"

func loadWorldFromStructuredDB(_ string, _ string) (persistedWorld, bool, error) {
	return persistedWorld{}, false, fmt.Errorf("pgvector build tag required for db persistence")
}

func persistWorldToStructuredDB(_ string, _ string, _ persistedWorld) error {
	return fmt.Errorf("pgvector build tag required for db persistence")
}
