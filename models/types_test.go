package models

import "testing"

func TestIsLocalSite(t *testing.T) {
	for _, site := range []string{"", "local", " Local ", "LOCAL"} {
		if !IsLocalSite(site) {
			t.Errorf("IsLocalSite(%q) = false, want true", site)
		}
	}
	if IsLocalSite("mangadex") {
		t.Error("IsLocalSite(mangadex) = true, want false")
	}
}
