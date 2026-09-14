package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"

	"SAO/internal/pixelworld"
)

func main() {
	approveKey := flag.String("approve", "", "asset key, for example object/chest-amber")
	kind := flag.String("kind", "object", "asset kind")
	file := flag.String("file", "", "PNG path relative to data/pixel_assets")
	list := flag.Bool("list", false, "print the local asset manifest")
	flag.Parse()

	if *list {
		records, err := pixelworld.ListAssetRecords()
		if err != nil {
			log.Fatal(err)
		}
		data, _ := json.MarshalIndent(records, "", "  ")
		fmt.Println(string(data))
		return
	}
	if *approveKey == "" || *file == "" {
		log.Fatal("use -list or provide -approve and -file")
	}
	record, err := pixelworld.ApproveAsset(*approveKey, *kind, *file)
	if err != nil {
		log.Fatal(err)
	}
	data, _ := json.MarshalIndent(record, "", "  ")
	fmt.Println(string(data))
}
