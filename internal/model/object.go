package model

import "sync"

type GameObject struct {
	ID      string         `json:"id"`
	Name    string         `json:"name"`
	Tags    []string       `json:"tags"`
	State   map[string]any `json:"state"`
	Version int64          `json:"version"`
	Lock    *sync.Mutex    `json:"-"`
}
