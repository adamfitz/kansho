package downloader

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

type contextParserTestSite struct {
	method *ChapterExtractionMethod
}

func (s contextParserTestSite) GetSiteName() string { return "context-parser-test" }
func (s contextParserTestSite) GetDomain() string   { return "localhost" }
func (s contextParserTestSite) NeedsCFBypass() bool { return false }
func (s contextParserTestSite) GetChapterExtractionMethod() *ChapterExtractionMethod {
	return s.method
}
func (contextParserTestSite) GetImageExtractionMethod() *ImageExtractionMethod { return nil }
func (contextParserTestSite) NormalizeChapterURL(rawURL, _ string) string      { return rawURL }
func (contextParserTestSite) NormalizeChapterFilename(data map[string]string) string {
	return data["text"]
}

func TestExtractChaptersCustomPassesCancellationToContextParser(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("chapter list"))
	}))
	defer server.Close()

	parserStarted := make(chan struct{})
	method := &ChapterExtractionMethod{
		Type: "custom",
		ContextParser: func(ctx context.Context, _ string) (map[string]string, error) {
			close(parserStarted)
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := extractChaptersCustom(ctx, server.URL, contextParserTestSite{method: method}, method)
		result <- err
	}()

	<-parserStarted
	cancel()

	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("extractChaptersCustom error = %v, want context.Canceled", err)
	}
}
