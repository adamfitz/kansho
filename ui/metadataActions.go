package ui

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"

	"kansho/config"
	"kansho/mangadex"
	"kansho/metadata"
	"kansho/metadataqueue"
)

const (
	metadataActionCheck  = "Check"
	metadataActionRepair = "Repair"
)

type metadataKindCounts struct {
	OK     int
	Fixed  int
	Failed int
}

type metadataRunSummary struct {
	Action   string
	Chapters metadataKindCounts
	Volumes  metadataKindCounts
	Files    metadataKindCounts
	Skipped  int
}

func (s metadataRunSummary) message() string {
	repair := s.Action == metadataActionRepair
	parts := make([]string, 0, 3)
	parts = appendKindSummary(parts, s.Chapters, "chapter", "chapters", repair)
	parts = appendKindSummary(parts, s.Volumes, "volume", "volumes", repair)
	parts = appendKindSummary(parts, s.Files, "file", "files", repair)
	if len(parts) == 0 {
		parts = append(parts, "0 files OK")
	}
	msg := fmt.Sprintf("%s: %s", s.Action, strings.Join(parts, ", "))
	if s.Skipped > 0 {
		msg += fmt.Sprintf(", %d already queued", s.Skipped)
	}
	return msg
}

func appendKindSummary(parts []string, c metadataKindCounts, singular, plural string, repair bool) []string {
	total := c.OK + c.Failed
	if total == 0 {
		return parts
	}
	noun := plural
	if total == 1 {
		noun = singular
	}
	part := fmt.Sprintf("%d %s OK", c.OK, noun)
	if repair && c.Fixed > 0 {
		part += fmt.Sprintf(" (%d fixed)", c.Fixed)
	}
	if c.Failed > 0 {
		failedNoun := plural
		if c.Failed == 1 {
			failedNoun = singular
		}
		part += fmt.Sprintf(", %d %s failed", c.Failed, failedNoun)
	}
	return append(parts, part)
}

type metadataRun struct {
	mu           sync.Mutex
	store        *mangadex.Store
	info         *mangadex.MangaInfo
	key          string
	title        string
	action       string
	remaining    int
	chapters     metadataKindCounts
	volumes      metadataKindCounts
	files        metadataKindCounts
	skipped      int
	allowAdvance bool
	done         func(metadataRunSummary)
}

// submitMetadataRun queues one task per file so only one CBZ is opened,
// checked or rewritten at a time. onDone runs after every accepted or skipped
// file has finished.
func submitMetadataRun(action string, store *mangadex.Store, info *mangadex.MangaInfo, manga *config.Bookmarks, files []metadata.ChapterFile, onDone func(metadataRunSummary)) int {
	if len(files) == 0 {
		return 0
	}
	run := &metadataRun{
		store:        store,
		info:         info,
		key:          metadata.MangaSourceKey(*manga),
		title:        manga.Title,
		action:       action,
		remaining:    len(files),
		allowAdvance: true,
		done:         onDone,
	}

	submitted := 0
	for _, file := range files {
		file := file
		var fixed bool
		task := &metadataqueue.Task{
			Action:    action,
			Title:     manga.Title,
			Chapter:   metadataChapterLabel(file),
			DedupeKey: fmt.Sprintf("metadata:%s:%s:%s", run.key, action, file.Name),
			Run: func(ctx context.Context) error {
				return run.runFile(file, &fixed)
			},
			OnSuccess: func() {
				run.complete(true, fixed, file)
			},
			OnError: func(err error) {
				run.complete(false, false, file)
			},
		}
		if metadataqueue.Get().Submit(task) {
			submitted++
			continue
		}
		run.skip()
	}
	return submitted
}

func (r *metadataRun) runFile(file metadata.ChapterFile, fixed *bool) error {
	var err error
	switch r.action {
	case metadataActionRepair:
		*fixed, err = metadata.RepairCBZ(r.store, r.info, file.Path)
	default:
		err = metadata.CheckCBZ(file.Path)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if err != nil {
		r.allowAdvance = false
		return err
	}
	if !r.allowAdvance {
		return nil
	}
	switch {
	case file.IsVolume:
		if advErr := r.store.AdvanceVolumeCheck(r.key, file.VolumeNumber); advErr != nil {
			log.Printf("[Metadata] Could not save checked volume %.4g for %s: %v",
				file.VolumeNumber, file.Name, advErr)
		}
	case file.HasNumber:
		if advErr := r.store.AdvanceChapterCheck(r.key, file.Number); advErr != nil {
			log.Printf("[Metadata] Could not save checked chapter %.4g for %s: %v",
				file.Number, file.Name, advErr)
		}
	}
	return nil
}

func (r *metadataRun) complete(success, fixed bool, file metadata.ChapterFile) {
	r.mu.Lock()
	counts := r.countsForLocked(file)
	if success {
		counts.OK++
		if fixed {
			counts.Fixed++
			log.Printf("[Metadata] Repaired %s %s", r.title, metadataChapterLabel(file))
		}
	} else {
		counts.Failed++
	}
	r.remaining--
	done, summary := r.finishLocked()
	r.mu.Unlock()
	if done {
		r.done(summary)
	}
}

func (r *metadataRun) countsForLocked(file metadata.ChapterFile) *metadataKindCounts {
	switch {
	case file.IsVolume:
		return &r.volumes
	case file.HasNumber:
		return &r.chapters
	default:
		return &r.files
	}
}

func (r *metadataRun) skip() {
	r.mu.Lock()
	r.skipped++
	r.remaining--
	done, summary := r.finishLocked()
	r.mu.Unlock()
	if done {
		r.done(summary)
	}
}

func (r *metadataRun) finishLocked() (bool, metadataRunSummary) {
	summary := metadataRunSummary{
		Action:   r.action,
		Chapters: r.chapters,
		Volumes:  r.volumes,
		Files:    r.files,
		Skipped:  r.skipped,
	}
	return r.remaining == 0, summary
}

func metadataChapterLabel(file metadata.ChapterFile) string {
	if file.HasNumber {
		return "Chapter " + metadata.ExtractChapterNumber(file.Name)
	}
	if file.IsVolume {
		return "Volume " + metadata.ExtractVolumeNumber(file.Name)
	}
	return file.Name
}

// uncheckedChapterFiles lists the local CBZ files not covered by the stored
// cursors. Numbered chapters compare against the chapter cursor, volume-only
// archives compare against the volume cursor, and files without either number
// are always included.
func uncheckedChapterFiles(dir string, chapterCursor, volumeCursor float64) ([]metadata.ChapterFile, error) {
	files, err := metadata.ListChapterFiles(dir)
	if err != nil {
		return nil, err
	}
	out := make([]metadata.ChapterFile, 0, len(files))
	for _, file := range files {
		switch {
		case file.HasNumber:
			if file.Number > chapterCursor || (file.Number == 0 && chapterCursor <= 0) {
				out = append(out, file)
			}
		case file.IsVolume:
			if file.VolumeNumber > volumeCursor {
				out = append(out, file)
			}
		default:
			out = append(out, file)
		}
	}
	return out, nil
}
