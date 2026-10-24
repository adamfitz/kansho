package metadata

import (
	"testing"

	"kansho/mangadex"
)

func TestFromMangaInfoUsesSelectedEnglishTitleForSeries(t *testing.T) {
	info := &mangadex.MangaInfo{
		Title:                map[string]string{"ko": "Jeoldae Gunrim"},
		AltTitles:            []map[string]string{{"en": "Absolute Dominion"}, {"en": "Absolute Rule"}},
		SelectedEnglishTitle: "Absolute Rule",
		Author:               "Author Name",
	}

	comicInfo := FromMangaInfo(info)
	if comicInfo.Series != "Absolute Rule" {
		t.Fatalf("Series = %q, want %q", comicInfo.Series, "Absolute Rule")
	}
	if comicInfo.Series == info.Author {
		t.Fatalf("Series incorrectly contains author name %q", info.Author)
	}
}

func TestFromMangaInfoDoesNotUseNonEnglishSeries(t *testing.T) {
	info := &mangadex.MangaInfo{
		Title: map[string]string{"ko": "Jeoldae Gunrim"},
	}

	comicInfo := FromMangaInfo(info)
	if comicInfo.Series != "" {
		t.Fatalf("Series = %q, want empty when no English title exists", comicInfo.Series)
	}
}
