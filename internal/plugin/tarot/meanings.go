package tarot

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"strings"
)

// CardMeaning uses the shared numeric card ID so both image themes share meanings.
type CardMeaning struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Upright  string `json:"upright"`
	Reversed string `json:"reversed"`
}

//go:embed assets/meanings/*.json
var meaningsFS embed.FS

var cardMeanings = mustLoadMeanings()

func mustLoadMeanings() map[string]CardMeaning {
	meanings, err := loadMeanings(meaningsFS)
	if err != nil {
		panic(fmt.Errorf("tarot: %w", err))
	}
	return meanings
}

func loadMeanings(source fs.FS) (map[string]CardMeaning, error) {
	files, err := fs.Glob(source, "assets/meanings/*.json")
	if err != nil {
		return nil, err
	}
	meanings := make(map[string]CardMeaning, len(cardNames))
	for _, file := range files {
		data, err := fs.ReadFile(source, file)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", file, err)
		}
		var cards []CardMeaning
		if err := json.Unmarshal(data, &cards); err != nil {
			return nil, fmt.Errorf("decode %s: %w", file, err)
		}
		for _, card := range cards {
			name, exists := cardNames[card.ID]
			if !exists || name != card.Name {
				return nil, fmt.Errorf("%s: unknown or mismatched card %q (%s)", file, card.ID, card.Name)
			}
			if _, exists := meanings[card.ID]; exists {
				return nil, fmt.Errorf("%s: duplicate card %q", file, card.ID)
			}
			if strings.TrimSpace(card.Upright) == "" || strings.TrimSpace(card.Reversed) == "" {
				return nil, fmt.Errorf("%s: incomplete meanings for %q", file, card.ID)
			}
			meanings[card.ID] = card
		}
	}
	for id := range cardNames {
		if _, exists := meanings[id]; !exists {
			return nil, fmt.Errorf("missing meanings for card %q", id)
		}
	}
	return meanings, nil
}

func meaningText(id string, reversed bool) string {
	card, ok := cardMeanings[id]
	if !ok {
		return "暂无对应牌义。"
	}
	if reversed {
		return card.Reversed
	}
	return card.Upright
}
