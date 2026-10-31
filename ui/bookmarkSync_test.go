package ui

import (
	"testing"

	"kansho/models"
)

func TestBuildPlatformBookmarkGroups(t *testing.T) {
	linux := []models.Bookmarks{
		{Title: "Linux name", MangaDexID: "shared-id"},
		{Title: "Unlinked", Site: "Linux site"},
		{Title: "Other ID", MangaDexID: "linux-id"},
	}
	windows := []models.Bookmarks{
		{Title: "Windows name", MangaDexID: "shared-id"},
		{Title: "Unlinked", Site: "Windows site"},
		{Title: "Other ID", MangaDexID: "windows-id"},
	}

	groups := buildPlatformBookmarkGroups(linux, windows)
	if len(groups) != 5 {
		t.Fatalf("got %d groups, want 5: %+v", len(groups), groups)
	}

	var shared *platformBookmarkGroup
	var unlinkedLinux, unlinkedWindows int
	for i := range groups {
		group := &groups[i]
		if group.id == "shared-id" {
			shared = group
		}
		if group.id == "" && group.title == "Unlinked" {
			if len(group.linux) == 1 {
				unlinkedLinux++
			}
			if len(group.windows) == 1 {
				unlinkedWindows++
			}
		}
	}
	if shared == nil || len(shared.linux) != 1 || len(shared.windows) != 1 {
		t.Fatalf("same MangaDex ID was not grouped across OS tables: %+v", shared)
	}
	if unlinkedLinux != 1 || unlinkedWindows != 1 {
		t.Fatalf("unlinked same-title rows were conflated: Linux=%d Windows=%d", unlinkedLinux, unlinkedWindows)
	}
}
