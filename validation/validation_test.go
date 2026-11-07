package validation

import (
	"strings"
	"testing"

	"kansho/models"
)

func TestValidateAddMangaAllowsLocalTitles(t *testing.T) {
	config := models.SitesConfig{}

	if err := ValidateAddManga("", "Local Title", "", "", "/manga/local", &config); err != nil {
		t.Fatalf("local title rejected: %v", err)
	}
	if err := ValidateAddManga("", "Local Title", "", "https://example.test/local", "/manga/local", &config); err != nil {
		t.Fatalf("local title with URL rejected: %v", err)
	}
	if err := ValidateAddManga(models.LocalSiteName, "Local Title", "", "", "/manga/local", &config); err != nil {
		t.Fatalf("explicit local site rejected: %v", err)
	}
	if err := ValidateAddManga(" Local ", "Local Title", "", "", "/manga/local", &config); err != nil {
		t.Fatalf("case/space-insensitive local site rejected: %v", err)
	}
	if err := ValidateAddManga("", "", "", "", "/manga/local", &config); err == nil || !strings.Contains(err.Error(), "title is required") {
		t.Fatalf("missing local title error = %v", err)
	}
	if err := ValidateAddManga("", "Local Title", "", "", "", &config); err == nil || !strings.Contains(err.Error(), "location is required") {
		t.Fatalf("missing local location error = %v", err)
	}
}

func TestValidateAddMangaStillRequiresConfiguredSiteFields(t *testing.T) {
	config := models.SitesConfig{Sites: []models.Site{{
		Name:        "mangadex",
		DisplayName: "mangadex",
		RequiredFields: models.RequiredFields{
			URL:      true,
			Title:    true,
			Location: true,
		},
	}}}

	if err := ValidateAddManga("mangadex", "Title", "", "", "/manga/title", &config); err == nil || !strings.Contains(err.Error(), "URL is required") {
		t.Fatalf("missing URL error = %v", err)
	}
	if err := ValidateAddManga("mangadex", "Title", "", "https://mangadex.org/title/1", "/manga/title", &config); err != nil {
		t.Fatalf("valid mangadex title rejected: %v", err)
	}
}
