package mangadex

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// baseURL is the MangaDex REST API root.
const baseURL = "https://api.mangadex.org"

// searchPageSize is the number of search results returned per page. The UI
// loads this many titles initially and then pages through with offset while
// the user scrolls.
const searchPageSize = 50

// clientUserAgent identifies this application to the MangaDex API. It is a
// non-spoofed, unique string as required by the MangaDex API terms of service
// (see openspec/specs/mangadex/spec.md).
const clientUserAgent = "kansho/1.0"

// Client queries the MangaDex REST API for manga title information.
type Client struct {
	http      *http.Client
	minDelay  time.Duration
	lastReqAt time.Time
}

// NewClient creates a MangaDex API client with a polite inter-request delay.
func NewClient() *Client {
	return &Client{
		http: &http.Client{
			Timeout: 15 * time.Second,
		},
		minDelay: 250 * time.Millisecond,
	}
}

// rateLimit waits long enough to keep at least minDelay between requests.
func (c *Client) rateLimit() {
	if c.lastReqAt.IsZero() {
		return
	}
	wait := c.minDelay - time.Since(c.lastReqAt)
	if wait > 0 {
		time.Sleep(wait)
	}
}

// do performs a GET request, applies the identifiable User-Agent, and returns
// the decoded JSON body. A 429 response is retried using the MangaDex rate
// limit retry-after header when present.
func (c *Client) do(ctx context.Context, rawURL string, out interface{}) error {
	c.rateLimit()
	c.lastReqAt = time.Now()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", clientUserAgent)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("mangadex request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		retryAfter := 1
		if h := resp.Header.Get("X-RateLimit-Retry-After"); h != "" {
			if v, err := strconv.Atoi(h); err == nil && v >= 1 {
				retryAfter = v
			}
		}
		select {
		case <-time.After(time.Duration(retryAfter) * time.Second):
		case <-ctx.Done():
			return ctx.Err()
		}
		return c.do(ctx, rawURL, out)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("mangadex API %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode mangadex response: %w", err)
	}
	return nil
}

// --- API response structures ---

type mangaCollectionResponse struct {
	Result string       `json:"result"`
	Data   []mangaEntry `json:"data"`
	Limit  int          `json:"limit"`
	Offset int          `json:"offset"`
	Total  int          `json:"total"`
}

type mangaEntityResponse struct {
	Result string     `json:"result"`
	Data   mangaEntry `json:"data"`
}

type mangaEntry struct {
	ID            string              `json:"id"`
	Type          string              `json:"type"`
	Attributes    mangaAttributes     `json:"attributes"`
	Relationships []mangaRelationship `json:"relationships"`
}

type mangaAttributes struct {
	Title                  map[string]string   `json:"title"`
	AltTitles              []map[string]string `json:"altTitles"`
	Description            map[string]string   `json:"description"`
	OriginalLanguage       string              `json:"originalLanguage"`
	PublicationDemographic string              `json:"publicationDemographic"`
	Year                   *int                `json:"year"`
	ContentRating          string              `json:"contentRating"`
	Status                 string              `json:"status"`
	Tags                   []tagEntry          `json:"tags"`
}

type tagEntry struct {
	ID         string        `json:"id"`
	Type       string        `json:"type"`
	Attributes tagAttributes `json:"attributes"`
}

type tagAttributes struct {
	Name map[string]string `json:"name"`
}

type mangaRelationship struct {
	ID         string                 `json:"id"`
	Type       string                 `json:"type"`
	Attributes map[string]interface{} `json:"attributes"`
}

// SearchManga searches MangaDex for manga matching the given title and returns
// one page of results (up to 50). offset pages through additional results.
func (c *Client) SearchManga(ctx context.Context, title string, offset int) ([]SearchResult, int, error) {
	u, err := url.Parse(baseURL + "/manga")
	if err != nil {
		return nil, 0, err
	}
	q := u.Query()
	q.Set("title", title)
	q.Set("limit", strconv.Itoa(searchPageSize))
	q.Set("offset", strconv.Itoa(offset))
	q.Set("order[relevance]", "desc")
	q.Set("contentRating[]", "safe")
	q.Add("contentRating[]", "suggestive")
	q.Add("contentRating[]", "erotica")
	q.Add("contentRating[]", "pornographic")
	q.Set("includes[]", "cover_art")
	q.Set("includes[]", "author")
	q.Set("includes[]", "artist")
	u.RawQuery = q.Encode()

	var resp mangaCollectionResponse
	if err := c.do(ctx, u.String(), &resp); err != nil {
		return nil, 0, fmt.Errorf("search mangadex for %q: %w", title, err)
	}

	results := make([]SearchResult, 0, len(resp.Data))
	for _, e := range resp.Data {
		results = append(results, SearchResult{
			ID:               e.ID,
			Title:            e.Attributes.Title,
			AltTitles:        e.Attributes.AltTitles,
			OriginalLanguage: e.Attributes.OriginalLanguage,
		})
	}
	return results, resp.Total, nil
}

// GetManga fetches the full title information for a single MangaDex manga ID
// and returns it populated as MangaInfo.
func (c *Client) GetManga(ctx context.Context, id string) (*MangaInfo, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, fmt.Errorf("empty MangaDex ID")
	}

	u, err := url.Parse(fmt.Sprintf("%s/manga/%s", baseURL, url.PathEscape(id)))
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("includes[]", "cover_art")
	q.Set("includes[]", "author")
	q.Set("includes[]", "artist")
	u.RawQuery = q.Encode()

	var resp mangaEntityResponse
	if err := c.do(ctx, u.String(), &resp); err != nil {
		return nil, fmt.Errorf("fetch mangadex title %s: %w", id, err)
	}

	return entryToMangaInfo(resp.Data), nil
}

// ExtractID parses a MangaDex title page URL or a plain title ID into a
// MangaDex manga ID.
//
//	https://mangadex.org/title/<id>/manga-slug  -> <id>
//	https://mangadex.org/title/<id>             -> <id>
//	<id>                                        -> <id>
func ExtractID(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("empty MangaDex URL/ID")
	}

	if !strings.Contains(raw, "/") {
		return raw, nil
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid MangaDex URL: %w", err)
	}
	if parsed.Host != "" && !strings.EqualFold(parsed.Host, "mangadex.org") &&
		!strings.HasSuffix(strings.ToLower(parsed.Host), ".mangadex.org") {
		return "", fmt.Errorf("not a MangaDex URL: %s", raw)
	}

	segments := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	for i, segment := range segments {
		if segment == "title" && i+1 < len(segments) {
			id := strings.TrimSpace(segments[i+1])
			if id == "" {
				return "", fmt.Errorf("could not extract MangaDex ID from URL: %s", raw)
			}
			return id, nil
		}
	}
	return "", fmt.Errorf("could not extract MangaDex ID from URL: %s", raw)
}

// entryToMangaInfo converts a raw API entry into a MangaInfo.
func entryToMangaInfo(e mangaEntry) *MangaInfo {
	info := &MangaInfo{
		ID:                     e.ID,
		Title:                  e.Attributes.Title,
		AltTitles:              e.Attributes.AltTitles,
		OriginalLanguage:       e.Attributes.OriginalLanguage,
		Description:            e.Attributes.Description,
		Status:                 e.Attributes.Status,
		PublicationDemographic: e.Attributes.PublicationDemographic,
		Year:                   e.Attributes.Year,
		ContentRating:          e.Attributes.ContentRating,
		URL:                    fmt.Sprintf("https://mangadex.org/title/%s", e.ID),
		Tags:                   make([]TagInfo, 0, len(e.Attributes.Tags)),
	}
	for _, t := range e.Attributes.Tags {
		info.Tags = append(info.Tags, TagInfo{ID: t.ID, Name: t.Attributes.Name})
	}
	for _, rel := range e.Relationships {
		name, _ := rel.Attributes["name"].(string)
		switch rel.Type {
		case "author":
			if info.Author == "" {
				info.Author = name
			} else {
				info.Author += ", " + name
			}
		case "artist":
			if info.Artist == "" {
				info.Artist = name
			} else {
				info.Artist += ", " + name
			}
		case "cover_art":
			if fileName, ok := rel.Attributes["fileName"].(string); ok && fileName != "" {
				info.CoverURL = fmt.Sprintf("https://uploads.mangadex.org/covers/%s/%s.512.jpg", e.ID, fileName)
			}
		}
	}
	if info.Title == nil {
		info.Title = map[string]string{}
	}
	if info.Description == nil {
		info.Description = map[string]string{}
	}
	return info
}
