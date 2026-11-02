package tarot

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"io/fs"
	"path"
	"reflect"
	"strings"
	"testing"
)

func TestParseDrawCount(t *testing.T) {
	for _, tc := range []struct {
		input string
		count int
		valid bool
	}{
		{"", 1, true}, {"  ", 1, true}, {" 3\n", 3, true}, {"8", 8, true},
		{"0", 0, false}, {"-1", 0, false}, {"9", 0, false}, {"三", 0, false},
		{"1.5", 0, false}, {"999999999999999999999999", 0, false},
	} {
		count, err := parseDrawCount(tc.input)
		if count != tc.count || (err == nil) != tc.valid {
			t.Errorf("parseDrawCount(%q) = %d, %v", tc.input, count, err)
		}
	}
}

func TestRandomDraw(t *testing.T) {
	source := []string{"a", "b", "c", "d"}
	original := append([]string(nil), source...)
	for _, count := range []int{-1, 0, 1, 4, 8} {
		result := randomDraw(source, count)
		want := max(0, min(count, len(source)))
		if len(result) != want {
			t.Fatalf("count %d: got %d cards", count, len(result))
		}
		seen := map[string]bool{}
		for _, card := range result {
			if seen[card] || !strings.Contains(strings.Join(source, ""), card) {
				t.Fatalf("invalid draw: %v", result)
			}
			seen[card] = true
		}
		if !reflect.DeepEqual(source, original) {
			t.Fatal("draw mutated source deck")
		}
	}
}

func TestDeckAssets(t *testing.T) {
	if len(cardNames) != 78 {
		t.Fatalf("got %d card names", len(cardNames))
	}
	for _, theme := range []string{"classic", "bilibili"} {
		entries, err := fs.ReadDir(deckEmbedFS, path.Join(DeckImgPath, theme))
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 78 {
			t.Fatalf("%s: got %d cards", theme, len(entries))
		}
		for _, entry := range entries {
			id := strings.SplitN(entry.Name(), "_", 2)[0]
			if cardNames[id] == "" {
				t.Errorf("missing name: %s", entry.Name())
			}
			data, err := deckEmbedFS.ReadFile(path.Join(DeckImgPath, theme, entry.Name()))
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := image.DecodeConfig(bytes.NewReader(data)); err != nil {
				t.Errorf("%s: %v", entry.Name(), err)
			}
		}
	}
}

func TestRotateImage(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 2, 3))
	img.Set(0, 0, color.NRGBA{R: 255, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	data, err := rotateImage(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	rotated, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if rotated.Bounds() != img.Bounds() || rotated.At(1, 2) != img.At(0, 0) {
		t.Fatal("image was not rotated 180 degrees")
	}
	if _, err := rotateImage([]byte("invalid")); err == nil {
		t.Fatal("expected invalid image error")
	}
}
