package tarot

import (
	"encoding/json"
	"strings"
	"testing"
	"testing/fstest"
)

func TestMeaningsMatchEveryCardAndOrientation(t *testing.T) {
	if len(cardMeanings) != 78 {
		t.Fatalf("got %d meanings", len(cardMeanings))
	}
	for id, name := range cardNames {
		card := cardMeanings[id]
		if card.Name != name {
			t.Errorf("%s: got name %q", id, card.Name)
		}
		if meaningText(id, false) != card.Upright || meaningText(id, true) != card.Reversed {
			t.Errorf("%s: wrong orientation mapping", id)
		}
		if card.Upright == card.Reversed {
			t.Errorf("%s: orientations have identical meanings", id)
		}
	}
	if meaningText("unknown", false) != "暂无对应牌义。" {
		t.Fatal("missing fallback")
	}
}

func TestLoadMeaningsRejectsInvalidData(t *testing.T) {
	cards := make([]CardMeaning, 0, len(cardMeanings))
	for _, card := range cardMeanings {
		cards = append(cards, card)
	}
	for _, tc := range []struct {
		name, want string
		mutate     func([]CardMeaning) []CardMeaning
	}{
		{"missing", "missing meanings", func(c []CardMeaning) []CardMeaning { return c[1:] }},
		{"duplicate", "duplicate card", func(c []CardMeaning) []CardMeaning { return append(c, c[0]) }},
		{"unknown", "unknown or mismatched", func(c []CardMeaning) []CardMeaning { c[0].ID = "99"; return c }},
		{"name", "unknown or mismatched", func(c []CardMeaning) []CardMeaning { c[0].Name = "错误牌名"; return c }},
		{"upright", "incomplete meanings", func(c []CardMeaning) []CardMeaning { c[0].Upright = " "; return c }},
		{"reversed", "incomplete meanings", func(c []CardMeaning) []CardMeaning { c[0].Reversed = ""; return c }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data, err := json.Marshal(tc.mutate(append([]CardMeaning(nil), cards...)))
			if err != nil {
				t.Fatal(err)
			}
			_, err = loadMeanings(fstest.MapFS{"assets/meanings/test.json": &fstest.MapFile{Data: data}})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want %s", err, tc.want)
			}
		})
	}
	if _, err := loadMeanings(fstest.MapFS{"assets/meanings/test.json": &fstest.MapFile{Data: []byte("{")}}); err == nil {
		t.Fatal("expected malformed JSON error")
	}
}
