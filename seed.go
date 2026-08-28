package main

import (
	_ "embed"
	"encoding/json"
	"fmt"

	"github.com/DecoyOtter/buyforbub/internal/store"
)

// seedJSON is the default checklist, loaded on first run into an empty
// database. Edit seed.json to change what a fresh install starts with.
//
//go:embed seed.json
var seedJSON []byte

func loadSeed() ([]store.ItemInput, error) {
	var items []store.ItemInput
	if err := json.Unmarshal(seedJSON, &items); err != nil {
		return nil, fmt.Errorf("parse seed.json: %w", err)
	}
	return items, nil
}
