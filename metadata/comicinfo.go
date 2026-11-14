package metadata

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/xml"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/image/draw"

	"kansho/config"
	"kansho/mangadex"
	"kansho/parser"
)

type ComicInfo struct {
	XMLName xml.Name `xml:"ComicInfo"`

	Title           string `xml:"Title,omitempty"`
	Series          string `xml:"Series,omitempty"`
	SeriesSort      string `xml:"SeriesSort,omitempty"`
	LocalizedSeries string `xml:"LocalizedSeries,omitempty"`
	Number          string `xml:"Number,omitempty"`
	Count           int    `xml:"Count,omitempty"`
	Volume          string `xml:"Volume,omitempty"`
	AlternateSeries string `xml:"AlternateSeries,omitempty"`
	AlternateNumber string `xml:"AlternateNumber,omitempty"`
	AlternateCount  int    `xml:"AlternateCount,omitempty"`

	Summary string `xml:"Summary,omitempty"`
	Notes   string `xml:"Notes,omitempty"`

	Writer      string `xml:"Writer,omitempty"`
	Penciller   string `xml:"Penciller,omitempty"`
	Inker       string `xml:"Inker,omitempty"`
	Colorist    string `xml:"Colorist,omitempty"`
	Letterer    string `xml:"Letterer,omitempty"`
	CoverArtist string `xml:"CoverArtist,omitempty"`
	Editor      string `xml:"Editor,omitempty"`
	Publisher   string `xml:"Publisher,omitempty"`

	Year  int `xml:"Year,omitempty"`
	Month int `xml:"Month,omitempty"`
	Day   int `xml:"Day,omitempty"`

	Genre         string `xml:"Genre,omitempty"`
	Tags          string `xml:"Tags,omitempty"`
	Web           string `xml:"Web,omitempty"`
	LanguageISO   string `xml:"LanguageISO,omitempty"`
	Format        string `xml:"Format,omitempty"`
	AgeRating     string `xml:"AgeRating,omitempty"`
	Manga         string `xml:"Manga,omitempty"`
	BlackAndWhite string `xml:"BlackAndWhite,omitempty"`

	PageCount int            `xml:"PageCount,omitempty"`
	Pages     *ComicPageList `xml:"Pages,omitempty"`

	SeriesGroup     string `xml:"SeriesGroup,omitempty"`
	StoryArc        string `xml:"StoryArc,omitempty"`
	StoryArcNumber  string `xml:"StoryArcNumber,omitempty"`
	ScanInformation string `xml:"ScanInformation,omitempty"`
	GTIN            string `xml:"GTIN,omitempty"`
	Imprint         string `xml:"Imprint,omitempty"`
}

type ComicPageInfo struct {
	Image int    `xml:"Image,attr"`
	Type  string `xml:"Type,attr,omitempty"`
}

type ComicPageList struct {
	Pages []ComicPageInfo `xml:"Page"`
}

func (c *ComicInfo) ToXML() (string, error) {
	output, err := xml.MarshalIndent(c, "", "  ")
	if err != nil {
		return "", err
	}
	return xml.Header + string(output), nil
}

func WriteComicInfo(comic *ComicInfo, filename string) (string, error) {
	if comic == nil {
		return "", fmt.Errorf("nil ComicInfo provided")
	}
	if filename == "" {
		return "", fmt.Errorf("filename must be specified")
	}
	absPath, err := filepath.Abs(filename)
	if err != nil {
		return "", fmt.Errorf("failed to resolve file path: %w", err)
	}
	xmlData, err := comic.ToXML()
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(absPath, []byte(xmlData), 0644); err != nil {
		return "", fmt.Errorf("failed to write ComicInfo.xml: %w", err)
	}
	return absPath, nil
}

func FromMangaInfo(info *mangadex.MangaInfo) *ComicInfo {
	if info == nil {
		return nil
	}
	ci := &ComicInfo{
		Manga: "Yes",
	}

	series := info.PrimaryEnglishTitle()
	if series == "" && len(info.EnglishTitleCandidates()) == 0 {
		series = info.EnglishTitle()
	}
	if series != "" {
		ci.Series = series
		ci.Title = series
		ci.SeriesSort = series
	}

	if d := info.DescriptionIn("en"); d != "" {
		lines := strings.Split(d, "\n---\n")
		ci.Summary = strings.TrimSpace(lines[0])
	}

	if info.Author != "" {
		ci.Writer = info.Author
		ci.Penciller = info.Author
	}
	if info.Artist != "" {
		if ci.Penciller == "" || ci.Penciller == info.Author {
			if info.Artist != info.Author {
				if ci.Penciller == "" {
					ci.Penciller = info.Artist
				} else {
					ci.Penciller = ci.Penciller + ", " + info.Artist
				}
			}
		}
	}

	var tags []string
	for _, tag := range info.Tags {
		name := tag.Name["en"]
		if name == "" {
			name = tag.Name[info.OriginalLanguage]
		}
		if name == "" {
			for _, n := range tag.Name {
				name = n
				break
			}
		}
		if name != "" {
			tags = append(tags, name)
		}
	}
	if len(tags) > 0 {
		ci.Tags = strings.Join(tags, ", ")
	}

	if info.PublicationDemographic != "" {
		if ci.Tags != "" {
			ci.Tags += ", " + info.PublicationDemographic
		} else {
			ci.Tags = info.PublicationDemographic
		}
	}
	if info.Year != nil {
		ci.Year = *info.Year
	}
	if info.Status != "" {
		ci.Notes = "Status: " + info.Status
	}
	switch info.ContentRating {
	case "safe":
		ci.AgeRating = "Everyone"
	case "suggestive":
		ci.AgeRating = "Teen"
	case "erotica":
		ci.AgeRating = "Mature 17+"
	case "pornographic":
		ci.AgeRating = "Adults Only 18+"
	}
	if info.OriginalLanguage != "" {
		ci.LanguageISO = info.OriginalLanguage
	}
	ci.BlackAndWhite = "Yes"
	for _, tag := range tags {
		if strings.Contains(strings.ToLower(tag), "colored") {
			ci.BlackAndWhite = "No"
			break
		}
	}
	if info.URL != "" {
		ci.Web = info.URL
	} else if info.ID != "" {
		ci.Web = fmt.Sprintf("https://mangadex.org/title/%s", info.ID)
	}
	return ci
}

var (
	chRe1  = regexp.MustCompile(`(?i)^ch[-_\s]*([0-9]+(?:\.[0-9]+)?)`)
	chRe2  = regexp.MustCompile(`(?i)^chapter[-_\s]*([0-9]+(?:\.[0-9]+)?)`)
	chRe3  = regexp.MustCompile(`(?i)^c[-_\s]*([0-9]+(?:\.[0-9]+)?)`)
	chRe4  = regexp.MustCompile(`(?i)[-_\s]ch[-_\s]*([0-9]+(?:\.[0-9]+)?)`)
	chRe5  = regexp.MustCompile(`(?i)[-_\s]chapter[-_\s]*([0-9]+(?:\.[0-9]+)?)`)
	chRe6  = regexp.MustCompile(`(?i)[-_\s]c[-_\s]*([0-9]+(?:\.[0-9]+)?)`)
	volRe1 = regexp.MustCompile(`(?i)(?:^|[-_\s.])vol(?:ume)?\.?[-_\s]*([0-9]+)`)
	volRe2 = regexp.MustCompile(`(?i)(?:^|[-_\s.])v\.?[-_\s]*([0-9]+)`)
)

func ExtractChapterNumber(filename string) string {
	name := strings.TrimSuffix(filename, filepath.Ext(filename))
	nameLower := strings.ToLower(name)
	for _, re := range []*regexp.Regexp{chRe1, chRe2, chRe3, chRe4, chRe5, chRe6} {
		if m := re.FindStringSubmatch(nameLower); len(m) > 1 {
			return normalizeNumber(m[1])
		}
	}
	// parser helpers not available; rely on our own regexes
	return ""
}

// normalizeNumber strips leading zeroes without turning a zero-only number such
// as "000" into an empty string.
func normalizeNumber(raw string) string {
	trimmed := strings.TrimLeft(raw, "0")
	if trimmed == "" {
		return "0"
	}
	if strings.HasPrefix(trimmed, ".") {
		return "0" + trimmed
	}
	return trimmed
}

// ChapterNumberValue returns the numeric chapter number encoded in a CBZ
// filename. OK is false when no chapter number can be parsed. Volume-only
// archives use VolumeNumberValue instead; their volume number is not a chapter
// number.
func ChapterNumberValue(filename string) (value float64, ok bool) {
	raw := ExtractChapterNumber(filename)
	if raw == "" {
		return 0, false
	}
	parsed, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, false
	}
	return parsed, true
}

// VolumeNumberValue returns the numeric volume number encoded in a CBZ
// filename. OK is false when no volume number can be parsed.
func VolumeNumberValue(filename string) (value float64, ok bool) {
	raw := ExtractVolumeNumber(filename)
	if raw == "" {
		return 0, false
	}
	parsed, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, false
	}
	return parsed, true
}

// ChapterFile is a local CBZ file together with its parsed chapter and volume
// numbers. A volume-only archive is a digital volume that bundles several
// chapters; it has HasVolume set and HasNumber unset.
type ChapterFile struct {
	Name         string
	Path         string
	Number       float64
	HasNumber    bool
	VolumeNumber float64
	HasVolume    bool
	IsVolume     bool
}

// ListChapterFiles lists the CBZ files in dir sorted by chapter number, then
// volume-only archives by volume number, falling back to filename order for
// files without either number. Paths are absolute inputs to the metadata
// helpers.
func ListChapterFiles(dir string) ([]ChapterFile, error) {
	expandedDir, err := parser.ExpandPath(dir)
	if err != nil {
		return nil, err
	}
	names, err := parser.LocalChapterList(expandedDir)
	if err != nil {
		names = nil
	}
	if len(names) == 0 {
		entries, readErr := os.ReadDir(expandedDir)
		if readErr == nil {
			for _, e := range entries {
				if !e.IsDir() && strings.EqualFold(filepath.Ext(e.Name()), ".cbz") {
					names = append(names, e.Name())
				}
			}
		}
	}
	files := make([]ChapterFile, 0, len(names))
	for _, name := range names {
		chapter, hasChapter := ChapterNumberValue(name)
		volume, hasVolume := VolumeNumberValue(name)
		files = append(files, ChapterFile{
			Name:         name,
			Path:         filepath.Join(expandedDir, name),
			Number:       chapter,
			HasNumber:    hasChapter,
			VolumeNumber: volume,
			HasVolume:    hasVolume,
			IsVolume:     hasVolume && !hasChapter,
		})
	}
	sort.SliceStable(files, func(i, j int) bool {
		a, b := files[i], files[j]
		if a.HasNumber != b.HasNumber {
			return a.HasNumber
		}
		if a.HasNumber {
			if a.Number != b.Number {
				return a.Number < b.Number
			}
			return a.Name < b.Name
		}
		if a.IsVolume != b.IsVolume {
			return a.IsVolume
		}
		if a.IsVolume && a.VolumeNumber != b.VolumeNumber {
			return a.VolumeNumber < b.VolumeNumber
		}
		return a.Name < b.Name
	})
	return files, nil
}

// CheckCBZ performs the quick metadata check: it opens the CBZ central
// directory and confirms a non-empty ComicInfo.xml entry exists. It does not
// decompress pages or compare the metadata against MangaDex.
func CheckCBZ(cbzPath string) error {
	r, err := zip.OpenReader(cbzPath)
	if err != nil {
		return err
	}
	defer r.Close()
	for _, f := range r.File {
		if f.Name == "ComicInfo.xml" {
			if f.UncompressedSize64 == 0 {
				return fmt.Errorf("ComicInfo.xml is empty")
			}
			return nil
		}
	}
	return fmt.Errorf("ComicInfo.xml not found")
}

// DirectoryFullyChecked reports whether every CBZ in dir is covered by the
// stored cursors. Numbered chapters compare against lastCheckedChapter and
// volume-only archives compare against lastCheckedVolume.
func DirectoryFullyChecked(dir string, lastCheckedChapter, lastCheckedVolume float64) bool {
	files, err := ListChapterFiles(dir)
	if err != nil || len(files) == 0 {
		return false
	}
	for _, file := range files {
		switch {
		case file.HasNumber:
			if file.Number > lastCheckedChapter {
				return false
			}
		case file.IsVolume:
			if file.VolumeNumber > lastCheckedVolume {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func ExtractVolumeNumber(filename string) string {
	name := strings.TrimSuffix(filename, filepath.Ext(filename))
	for _, re := range []*regexp.Regexp{volRe1, volRe2} {
		if m := re.FindStringSubmatch(name); len(m) > 1 {
			return normalizeNumber(m[1])
		}
	}
	return ""
}

func ensureCoverJPEG(src []byte) ([]byte, error) {
	if len(src) == 0 {
		return nil, nil
	}
	if len(src) > 2 && src[0] == 0xFF && src[1] == 0xD8 {
		return src, nil
	}
	img, _, err := image.Decode(bytes.NewReader(src))
	if err != nil {
		return nil, err
	}
	dst := image.NewRGBA(img.Bounds())
	draw.Draw(dst, dst.Bounds(), img, img.Bounds().Min, draw.Src)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 90}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func downloadCoverJPEG(coverURL string) ([]byte, error) {
	if coverURL == "" {
		return nil, nil
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(coverURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("cover download failed: %d", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return ensureCoverJPEG(data)
}

func getCoverBytes(store *mangadex.Store, info *mangadex.MangaInfo) ([]byte, error) {
	if store == nil || info == nil {
		return nil, nil
	}
	if b, err := store.GetCoverCache(info.ID); err == nil && len(b) > 0 {
		return b, nil
	}
	if b, err := downloadCoverJPEG(info.CoverURL); err == nil && len(b) > 0 {
		_ = store.SetCoverCache(info.ID, b, info.CoverURL)
		return b, nil
	}
	return nil, nil
}

// GetCoverBytes returns a cached cover JPEG or downloads and caches it.
func GetCoverBytes(store *mangadex.Store, info *mangadex.MangaInfo) ([]byte, error) {
	return getCoverBytes(store, info)
}

func extractCBZ(cbzPath, destDir string) error {
	r, err := zip.OpenReader(cbzPath)
	if err != nil {
		return err
	}
	defer r.Close()
	for _, f := range r.File {
		if f.Name == "ComicInfo.xml" || f.Name == "cover.jpg" || strings.HasPrefix(f.Name, "000_cover.") {
			continue
		}
		fpath := filepath.Join(destDir, f.Name)
		if !strings.HasPrefix(filepath.Clean(fpath), filepath.Clean(destDir)+string(os.PathSeparator)) {
			return fmt.Errorf("illegal file path: %s", fpath)
		}
		if f.FileInfo().IsDir() {
			os.MkdirAll(fpath, os.ModePerm)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(fpath), os.ModePerm); err != nil {
			return err
		}
		outFile, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			outFile.Close()
			return err
		}
		_, err = io.Copy(outFile, rc)
		outFile.Close()
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func createCBZ(sourceDir, cbzPath string) error {
	file, err := os.Create(cbzPath)
	if err != nil {
		return err
	}
	defer file.Close()
	w := zip.NewWriter(file)
	defer w.Close()
	return filepath.Walk(sourceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		relPath, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return err
		}
		relPath = filepath.ToSlash(relPath)
		zipFile, err := w.Create(relPath)
		if err != nil {
			return err
		}
		fsFile, err := os.Open(path)
		if err != nil {
			return err
		}
		defer fsFile.Close()
		_, err = io.Copy(zipFile, fsFile)
		return err
	})
}

func validateCBZIntegrity(cbzPath string) error {
	r, err := zip.OpenReader(cbzPath)
	if err != nil {
		return err
	}
	defer r.Close()
	for _, f := range r.File {
		rc, err := f.Open()
		if err != nil {
			return err
		}
		if _, err := io.Copy(io.Discard, rc); err != nil {
			rc.Close()
			return err
		}
		rc.Close()
	}
	return nil
}

func verifyCBZChecksum(newCBZ, sourceDir string) error {
	r, err := zip.OpenReader(newCBZ)
	if err != nil {
		return err
	}
	defer r.Close()
	for _, f := range r.File {
		rc, err := f.Open()
		if err != nil {
			return err
		}
		cbzHash := sha256.New()
		if _, err := io.Copy(cbzHash, rc); err != nil {
			rc.Close()
			return err
		}
		rc.Close()
		sourcePath := filepath.Join(sourceDir, f.Name)
		sourceFile, err := os.Open(sourcePath)
		if err != nil {
			return err
		}
		srcHash := sha256.New()
		if _, err := io.Copy(srcHash, sourceFile); err != nil {
			sourceFile.Close()
			return err
		}
		sourceFile.Close()
		if !bytes.Equal(cbzHash.Sum(nil), srcHash.Sum(nil)) {
			return fmt.Errorf("checksum mismatch for %s", f.Name)
		}
	}
	return nil
}

const maxIntegrityRetries = 3

func applyComicInfo(cbzPath string, comicInfo *ComicInfo, coverBytes []byte) error {
	fileName := filepath.Base(cbzPath)
	chapterName := strings.TrimSuffix(fileName, filepath.Ext(fileName))
	absCBZPath, err := filepath.Abs(cbzPath)
	if err != nil {
		return err
	}
	tempDir := filepath.Join(os.TempDir(), fmt.Sprintf("kansho_md_%s_%d", chapterName, time.Now().UnixNano()))
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return err
	}
	defer os.RemoveAll(tempDir)
	if err := extractCBZ(absCBZPath, tempDir); err != nil {
		return err
	}
	if len(coverBytes) > 0 {
		if err := os.WriteFile(filepath.Join(tempDir, "cover.jpg"), coverBytes, 0644); err == nil {
			comicInfo.Pages = &ComicPageList{Pages: []ComicPageInfo{{Image: 0, Type: "FrontCover"}}}
		}
	}
	comicInfoPath := filepath.Join(tempDir, "ComicInfo.xml")
	if _, err := WriteComicInfo(comicInfo, comicInfoPath); err != nil {
		return err
	}
	var lastErr error
	for attempt := 1; attempt <= maxIntegrityRetries; attempt++ {
		newCBZPath := absCBZPath + ".new"
		if err := createCBZ(tempDir, newCBZPath); err != nil {
			lastErr = err
			os.Remove(newCBZPath)
			continue
		}
		if err := validateCBZIntegrity(newCBZPath); err != nil {
			lastErr = err
			os.Remove(newCBZPath)
			continue
		}
		if err := verifyCBZChecksum(newCBZPath, tempDir); err != nil {
			lastErr = err
			os.Remove(newCBZPath)
			continue
		}
		if err := os.Remove(absCBZPath); err != nil {
			os.Remove(newCBZPath)
			return err
		}
		if err := os.Rename(newCBZPath, absCBZPath); err != nil {
			return err
		}
		return nil
	}
	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("failed to update cbz")
}

func chapterTitle(ci *ComicInfo, fileName, chapterNum string) string {
	origChapterNum := ExtractChapterNumber(fileName)
	volNum := ExtractVolumeNumber(fileName)
	if origChapterNum == "" && volNum != "" {
		if ci.Series != "" {
			return fmt.Sprintf("%s - Volume %s", ci.Series, volNum)
		}
		return strings.TrimSuffix(fileName, filepath.Ext(fileName))
	}
	if origChapterNum == "" {
		if ci.Series != "" {
			return ci.Series
		}
		return strings.TrimSuffix(fileName, filepath.Ext(fileName))
	}
	return fmt.Sprintf("Chapter %s", chapterNum)
}

type Result struct {
	Name   string
	Status string
	Error  string
}

// comicInfoForFile returns a copy of base with the chapter number, volume and
// title fields filled in for one CBZ filename.
func comicInfoForFile(base *ComicInfo, fileName string) ComicInfo {
	chapterNum := ExtractChapterNumber(fileName)
	if chapterNum == "" {
		if v := ExtractVolumeNumber(fileName); v != "" {
			chapterNum = v
		} else {
			chapterNum = "1"
		}
	}
	ci := *base
	ci.Number = chapterNum
	if v := ExtractVolumeNumber(fileName); v != "" {
		ci.Volume = v
	}
	ci.Title = chapterTitle(base, fileName, chapterNum)
	return ci
}

// RepairCBZ regenerates the metadata of one CBZ when it is missing, corrupt,
// or does not match the MangaDex title. Matching files are left untouched so
// Repair does not recompress every archive. It reports whether the file was
// changed.
func RepairCBZ(store *mangadex.Store, info *mangadex.MangaInfo, cbzPath string) (bool, error) {
	if info == nil {
		return false, nil
	}
	base := FromMangaInfo(info)
	fileName := filepath.Base(cbzPath)
	ci := comicInfoForFile(base, fileName)
	coverBytes, _ := getCoverBytes(store, info)
	if len(coverBytes) > 0 {
		ci.Pages = &ComicPageList{Pages: []ComicPageInfo{{Image: 0, Type: "FrontCover"}}}
	}
	if matchesComicInfo(cbzPath, &ci) {
		return false, nil
	}
	if err := applyComicInfo(cbzPath, &ci, coverBytes); err != nil {
		return false, err
	}
	return true, nil
}

func VerifyForDirectory(store *mangadex.Store, info *mangadex.MangaInfo, dir string) []Result {
	if info == nil {
		return nil
	}
	expandedDir, err := parser.ExpandPath(dir)
	if err != nil {
		return []Result{{Name: dir, Status: "Failed", Error: err.Error()}}
	}
	files, err := parser.LocalChapterList(expandedDir)
	if err != nil {
		files = nil
	}
	if len(files) == 0 {
		entries, err := os.ReadDir(expandedDir)
		if err == nil {
			for _, e := range entries {
				if !e.IsDir() && strings.EqualFold(filepath.Ext(e.Name()), ".cbz") {
					files = append(files, e.Name())
				}
			}
		}
	}
	for i, file := range files {
		files[i] = filepath.Join(expandedDir, file)
	}
	sort.Strings(files)
	base := FromMangaInfo(info)
	coverBytes, _ := getCoverBytes(store, info)
	var results []Result
	for _, cbzPath := range files {
		fileName := filepath.Base(cbzPath)
		needsFix := false
		if !hasComicInfo(cbzPath) {
			needsFix = true
		} else if !matchesBase(cbzPath, base) {
			needsFix = true
		}
		chapterNum := ExtractChapterNumber(fileName)
		if chapterNum == "" {
			if v := ExtractVolumeNumber(fileName); v != "" {
				chapterNum = v
			} else {
				chapterNum = "1"
			}
		}
		if !needsFix {
			results = append(results, Result{Name: fileName, Status: "OK"})
			continue
		}
		ci := *base
		ci.Number = chapterNum
		if v := ExtractVolumeNumber(fileName); v != "" {
			ci.Volume = v
		}
		ci.Title = chapterTitle(base, fileName, chapterNum)
		if err := applyComicInfo(cbzPath, &ci, coverBytes); err != nil {
			results = append(results, Result{Name: fileName, Status: "Failed", Error: err.Error()})
			continue
		}
		results = append(results, Result{Name: fileName, Status: "Fixed"})
	}
	return results
}

func hasComicInfo(cbzPath string) bool {
	r, err := zip.OpenReader(cbzPath)
	if err != nil {
		return false
	}
	defer r.Close()
	for _, f := range r.File {
		if f.Name == "ComicInfo.xml" {
			return true
		}
	}
	return false
}

func matchesComicInfo(cbzPath string, expected *ComicInfo) bool {
	if expected == nil {
		return true
	}
	r, err := zip.OpenReader(cbzPath)
	if err != nil {
		return false
	}
	defer r.Close()
	var actual *ComicInfo
	for _, f := range r.File {
		if f.Name != "ComicInfo.xml" {
			continue
		}
		if f.UncompressedSize64 == 0 {
			return false
		}
		rc, err := f.Open()
		if err != nil {
			return false
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return false
		}
		var parsed ComicInfo
		if err := xml.Unmarshal(data, &parsed); err != nil {
			return false
		}
		actual = &parsed
		break
	}
	if actual == nil {
		return false
	}

	want := *expected
	actual.XMLName = xml.Name{}
	want.XMLName = xml.Name{}
	// When there is no cover to write, ignore any existing Pages entry so an
	// unavailable cover download cannot force repeated rewrites.
	if want.Pages == nil {
		actual.Pages = nil
	}
	return reflect.DeepEqual(*actual, want)
}

func matchesBase(cbzPath string, base *ComicInfo) bool {
	r, err := zip.OpenReader(cbzPath)
	if err != nil {
		return false
	}
	defer r.Close()
	var ci *ComicInfo
	for _, f := range r.File {
		if f.Name == "ComicInfo.xml" {
			rc, err := f.Open()
			if err != nil {
				return false
			}
			data, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				return false
			}
			var parsed ComicInfo
			if err := xml.Unmarshal(data, &parsed); err != nil {
				return false
			}
			ci = &parsed
			break
		}
	}
	if ci == nil {
		return false
	}
	if base == nil {
		return true
	}
	if ci.Series != base.Series {
		return false
	}
	if ci.Summary != base.Summary {
		return false
	}
	if ci.Writer != base.Writer {
		return false
	}
	if ci.Penciller != base.Penciller {
		return false
	}
	if ci.Web != base.Web {
		return false
	}
	if ci.LanguageISO != base.LanguageISO {
		return false
	}
	if ci.Tags != base.Tags {
		return false
	}
	if ci.AgeRating != base.AgeRating {
		return false
	}
	if ci.Manga != base.Manga {
		return false
	}
	if ci.BlackAndWhite != base.BlackAndWhite {
		return false
	}
	if ci.Notes != base.Notes {
		return false
	}
	if base.Year != 0 && ci.Year != base.Year {
		return false
	}
	return true
}

func ApplyToCBZ(store *mangadex.Store, info *mangadex.MangaInfo, cbzPath string) error {
	if info == nil {
		return nil
	}
	base := FromMangaInfo(info)
	fileName := filepath.Base(cbzPath)
	chapterNum := ExtractChapterNumber(fileName)
	if chapterNum == "" {
		if v := ExtractVolumeNumber(fileName); v != "" {
			chapterNum = v
		} else {
			chapterNum = "1"
		}
	}
	ci := *base
	ci.Number = chapterNum
	if v := ExtractVolumeNumber(fileName); v != "" {
		ci.Volume = v
	}
	ci.Title = chapterTitle(base, fileName, chapterNum)
	coverBytes, _ := getCoverBytes(store, info)
	return applyComicInfo(cbzPath, &ci, coverBytes)
}

func GetMangaInfoForBookmark(manga config.Bookmarks) (*mangadex.MangaInfo, error) {
	store, err := mangadex.GetStore()
	if err != nil {
		return nil, err
	}

	ids := make([]string, 0, 2)
	addID := func(id string) {
		id = strings.TrimSpace(id)
		if id == "" {
			return
		}
		for _, existing := range ids {
			if existing == id {
				return
			}
		}
		ids = append(ids, id)
	}
	addID(manga.MangaDexID)
	if id, parseErr := mangadex.ExtractID(manga.Url); parseErr == nil {
		addID(id)
	}

	for _, id := range ids {
		local, localErr := store.LookupByID(id)
		if localErr != nil {
			return nil, localErr
		}
		if local != nil {
			return local, nil
		}
		fetched, fetchErr := mangadex.NewClient().GetManga(context.Background(), id)
		if fetchErr != nil || fetched == nil {
			continue
		}
		_ = store.Upsert(fetched)
		return fetched, nil
	}

	local, err := store.LookupByTitle(manga.Title)
	if err != nil {
		return nil, err
	}
	return local, nil
}

func MangaSourceKey(manga config.Bookmarks) string {
	site := strings.ToLower(strings.TrimSpace(manga.Site))
	target := strings.TrimSpace(manga.Url)
	if target == "" {
		target = strings.ToLower(strings.TrimSpace(manga.Title))
	}
	sum := sha256.Sum256([]byte(site + "\x00" + target))
	return fmt.Sprintf("%x", sum)
}
