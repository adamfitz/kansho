package ui

import (
	"strings"
	"testing"

	"kansho/metadata"
)

func TestMetadataRunSummarySeparatesChaptersAndVolumes(t *testing.T) {
	summary := metadataRunSummary{
		Action:   metadataActionCheck,
		Chapters: metadataKindCounts{OK: 211},
		Volumes:  metadataKindCounts{OK: 10},
	}
	got := summary.message()
	for _, want := range []string{"211 chapters OK", "10 volumes OK"} {
		if !strings.Contains(got, want) {
			t.Fatalf("message() = %q, want substring %q", got, want)
		}
	}
}

func TestMetadataRunSummaryReportsFailedVolume(t *testing.T) {
	summary := metadataRunSummary{
		Action:   metadataActionCheck,
		Chapters: metadataKindCounts{OK: 211},
		Volumes:  metadataKindCounts{OK: 9, Failed: 1},
	}
	got := summary.message()
	for _, want := range []string{"211 chapters OK", "9 volumes OK", "1 volume failed"} {
		if !strings.Contains(got, want) {
			t.Fatalf("message() = %q, want substring %q", got, want)
		}
	}
}

func TestMetadataChapterLabelNamesTheType(t *testing.T) {
	if got := metadataChapterLabel(metadata.ChapterFile{Name: "ch001.cbz", HasNumber: true, Number: 1}); got != "Chapter 1" {
		t.Fatalf("chapter label = %q, want Chapter 1", got)
	}
	if got := metadataChapterLabel(metadata.ChapterFile{Name: "Series v02.cbz", HasVolume: true, IsVolume: true, VolumeNumber: 2}); got != "Volume 2" {
		t.Fatalf("volume label = %q, want Volume 2", got)
	}
	if got := metadataChapterLabel(metadata.ChapterFile{Name: "extra.cbz"}); got != "extra.cbz" {
		t.Fatalf("file label = %q, want extra.cbz", got)
	}
}
