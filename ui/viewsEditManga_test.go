package ui

import "testing"

func TestSelectedBookmarkTypeRequiresExplicitChoice(t *testing.T) {
	if _, err := selectedBookmarkType(""); err == nil {
		t.Fatal("empty type selection should be rejected")
	}
	for selection, want := range map[string]string{"Manga": "manga", "Webtoon": "webtoon"} {
		got, err := selectedBookmarkType(selection)
		if err != nil || got != want {
			t.Errorf("selectedBookmarkType(%q) = %q, %v; want %q", selection, got, err, want)
		}
	}
}
