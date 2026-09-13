package mangadex

import (
	"fmt"
	"sort"
	"strings"
)

// MangaInfo is the full title information for a MangaDex manga. It is
// language-neutral: title, altTitles, description, and tags all keep their
// per-language maps so the UI can display any language MangaDex actually
// publishes for the title. The record is stored verbatim in the local SQLite
// database after it is fetched from MangaDex.
type MangaInfo struct {
	ID                     string              `json:"id"`
	Title                  map[string]string   `json:"title"`
	AltTitles              []map[string]string `json:"altTitles"`
	OriginalLanguage       string              `json:"originalLanguage"`
	Description            map[string]string   `json:"description"`
	Status                 string              `json:"status"`
	PublicationDemographic string              `json:"publicationDemographic"`
	Year                   *int                `json:"year"`
	ContentRating          string              `json:"contentRating"`
	Tags                   []TagInfo           `json:"tags"`
	Author                 string              `json:"author"`
	Artist                 string              `json:"artist"`
	CoverURL               string              `json:"coverUrl"`
	URL                    string              `json:"url"`
}

// TagInfo is a single MangaDex tag (genre/theme) with its per-language name.
type TagInfo struct {
	ID   string            `json:"id"`
	Name map[string]string `json:"name"`
}

// EnglishTitle returns the title in English, falling back to the original
// language title, then to the first available title.
func (m *MangaInfo) EnglishTitle() string {
	if t, ok := m.Title["en"]; ok && t != "" {
		return t
	}
	if t, ok := m.Title[m.OriginalLanguage]; ok && t != "" {
		return t
	}
	for _, t := range m.Title {
		if t != "" {
			return t
		}
	}
	return ""
}

// TitleIn returns the title for the given language, falling back to the
// English title, then to any available title.
func (m *MangaInfo) TitleIn(lang string) string {
	if t, ok := m.Title[lang]; ok && t != "" {
		return t
	}
	return m.EnglishTitle()
}

// DescriptionIn returns the description for the requested language. It falls
// back to English and then to any available description when the requested
// language is not published.
func (m *MangaInfo) DescriptionIn(lang string) string {
	if d, ok := m.Description[lang]; ok && d != "" {
		return d
	}
	if d, ok := m.Description["en"]; ok && d != "" {
		return d
	}
	for _, d := range m.Description {
		if d != "" {
			return d
		}
	}
	return ""
}

// AvailableLanguages returns every language code MangaDex actually publishes
// content in for this title (titles, alt titles, or descriptions). The result
// is sorted and de-duplicated.
func (m *MangaInfo) AvailableLanguages() []string {
	seen := make(map[string]bool)
	add := func(m map[string]string) {
		for lang := range m {
			seen[lang] = true
		}
	}
	add(m.Title)
	add(m.Description)
	for _, alt := range m.AltTitles {
		add(alt)
	}
	for _, tag := range m.Tags {
		add(tag.Name)
	}
	if len(seen) == 0 {
		return nil
	}
	langs := make([]string, 0, len(seen))
	for lang := range seen {
		langs = append(langs, lang)
	}
	sort.Strings(langs)
	return langs
}

// AltTitleList returns a printable, deduplicated, sorted list of alternative
// titles (excluding the main title in every language).
func (m *MangaInfo) AltTitleList() []string {
	seen := make(map[string]bool)
	for _, alt := range m.AltTitles {
		for _, t := range alt {
			if t != "" {
				seen[t] = true
			}
		}
	}
	// Never show the primary title (in any language) as an alt title.
	for _, t := range m.Title {
		if t != "" {
			seen[t] = false
		}
	}
	list := make([]string, 0, len(seen))
	for t, keep := range seen {
		if keep {
			list = append(list, t)
		}
	}
	sort.Strings(list)
	return list
}

// TagList returns the tag names for the requested language, falling back to
// English and then to any available tag name.
func (m *MangaInfo) TagList(lang string) []string {
	var out []string
	for _, tag := range m.Tags {
		name := tag.Name[lang]
		if name == "" {
			name = tag.Name["en"]
		}
		if name == "" {
			for _, n := range tag.Name {
				name = n
				break
			}
		}
		if name != "" {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// SearchResult is one entry in a MangaDex title search result list.
type SearchResult struct {
	ID               string              `json:"id"`
	Title            map[string]string   `json:"title"`
	AltTitles        []map[string]string `json:"altTitles"`
	OriginalLanguage string              `json:"originalLanguage"`
}

// DisplayTitle returns a human-readable title for the search result, showing
// the English or first-available title plus the primary translated title and
// the language it is available in.
func (s *SearchResult) DisplayTitle() string {
	en := ""
	first := ""
	for lang, t := range s.Title {
		if lang == "en" {
			en = t
		}
		if first == "" {
			first = t
		}
	}
	title := en
	if title == "" {
		title = first
	}
	if title == "" {
		title = s.ID
	}
	// If there is a non-English primary title, annotate the result so the user
	// can tell titles apart in the lookup dialog.
	extra := ""
	for _, alt := range s.AltTitles {
		for _, t := range alt {
			if strings.EqualFold(t, title) {
				continue
			}
			extra = t
			break
		}
		if extra != "" {
			break
		}
	}
	if extra != "" {
		return fmt.Sprintf("%s (%s)", title, extra)
	}
	return title
}
