//go:build !pgvector

package world

import "fmt"

func loadWorldFromDB(_ string, _ string) (persistedWorld, bool, error) {
	return persistedWorld{}, false, fmt.Errorf("pgvector build tag required for db persistence")
}

func persistWorldToDB(_ string, _ string, _ persistedWorld) error {
	return fmt.Errorf("pgvector build tag required for db persistence")
}

func SaveWorldSnapshotToDB(_ string, _ string, _ any) error {
	return fmt.Errorf("pgvector build tag required for db persistence")
}
