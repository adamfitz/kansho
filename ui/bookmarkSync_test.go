package ui

import (
	"testing"

	"kansho/models"
)

func TestBuildPlatformBookmarkGroups(t *testing.T) {
	linux := []models.Bookmarks{
		{Title: "Linux name", MangaDexID: "shared-id"},
		{Title: "Unlinked", Site: "Example"},
		{Title: "Other ID", MangaDexID: "linux-id"},
	}
	windows := []models.Bookmarks{
		{Title: "Windows name", MangaDexID: "shared-id"},
		{Title: "Unlinked", Site: "Example"},
		{Title: "Other ID", MangaDexID: "windows-id"},
	}

	groups := buildPlatformBookmarkGroups(linux, windows)
	if len(groups) != 4 {
		t.Fatalf("got %d groups, want 4: %+v", len(groups), groups)
	}

	var shared *platformBookmarkGroup
	var unlinked *platformBookmarkGroup
	for i := range groups {
		group := &groups[i]
		if group.id == "shared-id" {
			shared = group
		}
		if group.id == "" && group.title == "Unlinked" {
			unlinked = group
		}
	}
	if shared == nil || len(shared.linux) != 1 || len(shared.windows) != 1 {
		t.Fatalf("same MangaDex ID was not grouped across OS tables: %+v", shared)
	}
	if unlinked == nil || len(unlinked.linux) != 1 || len(unlinked.windows) != 1 {
		t.Fatalf("unlinked shared title was not grouped: %+v", unlinked)
	}
}
