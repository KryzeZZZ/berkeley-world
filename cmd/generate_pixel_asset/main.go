package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"

	"SAO/internal/pixelworld"
)

func main() {
	objectID := flag.String("object", "", "world object ID to generate")
	flag.Parse()
	if *objectID == "" {
		log.Fatal("provide -object, for example -object chest-amber")
	}
	manager, err := pixelworld.NewManager("data/pixel_world.json")
	if err != nil {
		log.Fatal(err)
	}
	for _, entity := range manager.Snapshot().Entities {
		if entity.ID != *objectID {
			continue
		}
		candidate, err := pixelworld.GenerateComfyObjectCandidate(entity)
		if err != nil {
			log.Fatal(err)
		}
		data, _ := json.MarshalIndent(candidate, "", "  ")
		fmt.Println(string(data))
		return
	}
	log.Fatalf("object %q not found in data/pixel_world.json", *objectID)
}
