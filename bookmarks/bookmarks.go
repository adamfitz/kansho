package bookmarks

import (
	"encoding/json"
	"io"
	"log"
	"os"
)

type Manga struct {
	Manga []Bookmarks `json:"manga"`
}

type Bookmarks struct {
	Title     string `json:"title"`
	Url       string `json:"url"`
	Chapters  string `json:"chapters"`
	Location  string `json:"location"`
	Site      string `json:"site"`
	Shortname string `json:"shortname"`
}

// load bookmarks return custom struct
func LoadBookmarks() Manga {
	bookmarksLocation := "./bookmarks/bookmarks.json"

	file, err := os.Open(bookmarksLocation)
	if err != nil {
		log.Printf("error loading bookmarks file: %v", err)
		return Manga{}
	}
	defer file.Close()

	byteValues, _ := io.ReadAll(file)

	var mangaStruct Manga
	if err := json.Unmarshal(byteValues, &mangaStruct); err != nil {
		log.Printf("error unmarshalling bookmarks: %v", err)
	}

	return mangaStruct
}