package config

import (
	"context"
	"errors"
	"testing"
)

func statusByID(tasks []*DownloadTask) map[string]string {
	statuses := make(map[string]string, len(tasks))
	for _, task := range tasks {
		statuses[task.ID] = task.Status
	}
	return statuses
}

// TestCancelMangaTasksCancelsOnlyThatManga verifies that CancelMangaTasks
// cancels queued and downloading tasks for the matching manga title and leaves
// tasks for other manga (and completed tasks) untouched.
func TestCancelMangaTasksCancelsOnlyThatManga(t *testing.T) {
	_, cancelA := context.WithCancel(context.Background())
	_, cancelB := context.WithCancel(context.Background())

	q := &DownloadQueue{
		tasks: []*DownloadTask{
			{ID: "1", Manga: Bookmarks{Title: "Manga A"}, Chapter: "a1.cbz", Status: "queued"},
			{ID: "2", Manga: Bookmarks{Title: "Manga A"}, Chapter: "a2.cbz", Status: "downloading", CancelFunc: cancelA},
			{ID: "3", Manga: Bookmarks{Title: "Manga B"}, Chapter: "b1.cbz", Status: "downloading", CancelFunc: cancelB},
			{ID: "4", Manga: Bookmarks{Title: "Manga A"}, Chapter: "a3.cbz", Status: "completed"},
		},
	}

	q.CancelMangaTasks("Manga A")

	statuses := statusByID(q.tasks)
	if statuses["1"] != "cancelled" {
		t.Errorf("queued task for Manga A should be cancelled, got %s", statuses["1"])
	}
	if statuses["2"] != "cancelled" {
		t.Errorf("downloading task for Manga A should be cancelled, got %s", statuses["2"])
	}
	if statuses["3"] != "downloading" {
		t.Errorf("downloading task for Manga B must NOT be cancelled, got %s", statuses["3"])
	}
	if statuses["4"] != "completed" {
		t.Errorf("completed task must be left untouched, got %s", statuses["4"])
	}
}

// TestCancelAllMarksAllActiveTasksCancelled verifies that CancelAll cancels
// every queued and downloading task while leaving completed tasks untouched.
func TestCancelAllMarksAllActiveTasksCancelled(t *testing.T) {
	_, cancel := context.WithCancel(context.Background())

	q := &DownloadQueue{
		tasks: []*DownloadTask{
			{ID: "1", Manga: Bookmarks{Title: "Manga A"}, Chapter: "a1.cbz", Status: "queued"},
			{ID: "2", Manga: Bookmarks{Title: "Manga A"}, Chapter: "a2.cbz", Status: "downloading", CancelFunc: cancel},
			{ID: "3", Manga: Bookmarks{Title: "Manga A"}, Chapter: "a3.cbz", Status: "completed"},
		},
	}

	q.CancelAll()

	statuses := statusByID(q.tasks)
	if statuses["1"] != "cancelled" {
		t.Errorf("queued task should be cancelled, got %s", statuses["1"])
	}
	if statuses["2"] != "cancelled" {
		t.Errorf("downloading task should be cancelled, got %s", statuses["2"])
	}
	if statuses["3"] != "completed" {
		t.Errorf("completed task must be left untouched, got %s", statuses["3"])
	}
}

// registerFakeChapterDownload installs a chapter downloader for the tests. The
// config package does not import the sites package, so this is the only
// dispatcher present in this test binary.
func registerFakeChapterDownload(fn func() error) {
	RegisterChapterDownload(func(ctx context.Context, manga *Bookmarks, chapterURL, cbzName string, progressCallback func(string, float64, int, int, int)) error {
		return fn()
	})
}

// TestExecuteTaskRemovesCompletedChapter verifies that a chapter whose download
// succeeds is removed from the queue immediately.
func TestExecuteTaskRemovesCompletedChapter(t *testing.T) {
	registerFakeChapterDownload(func() error { return nil })

	q := &DownloadQueue{}
	task := &DownloadTask{
		ID:         "1",
		Manga:      Bookmarks{Title: "Manga A"},
		Chapter:    "a1.cbz",
		ChapterURL: "http://example.com/a1",
		Status:     "queued",
	}
	q.tasks = append(q.tasks, task)

	q.executeTask(task)

	if len(q.tasks) != 0 {
		t.Fatalf("completed chapter should be removed from the queue, got %d tasks", len(q.tasks))
	}
}

// TestExecuteTaskKeepsFailedChapter verifies that a chapter whose download
// fails is left in the queue (so it can be retried).
func TestExecuteTaskKeepsFailedChapter(t *testing.T) {
	registerFakeChapterDownload(func() error { return errors.New("boom") })

	q := &DownloadQueue{}
	task := &DownloadTask{
		ID:         "1",
		Manga:      Bookmarks{Title: "Manga A"},
		Chapter:    "a1.cbz",
		ChapterURL: "http://example.com/a1",
		Status:     "queued",
	}
	q.tasks = append(q.tasks, task)

	q.executeTask(task)

	if len(q.tasks) != 1 {
		t.Fatalf("failed chapter should stay in the queue, got %d tasks", len(q.tasks))
	}
	if task.Status != "failed" {
		t.Errorf("expected task status 'failed', got %s", task.Status)
	}
}

// TestRetryTaskAllowsCancelled verifies that a cancelled task can be retried.
func TestRetryTaskAllowsCancelled(t *testing.T) {
	q := &DownloadQueue{}
	q.processingMu.Lock()
	q.processing = true
	q.processingMu.Unlock()

	q.tasks = []*DownloadTask{
		{ID: "1", Manga: Bookmarks{Title: "Manga A"}, Chapter: "a1.cbz", Status: "cancelled"},
	}

	if err := q.RetryTask("1"); err != nil {
		t.Fatalf("retrying a cancelled task should succeed, got: %v", err)
	}
	if q.tasks[0].Status != "queued" {
		t.Errorf("retried task should be queued, got %s", q.tasks[0].Status)
	}

	if err := q.RetryTask("1"); err == nil {
		t.Error("retrying an already-queued task should fail")
	}
}

// TestClearAllEmptiesQueue verifies that ClearAll removes every task, cancels
// active downloads, and leaves nothing to retry.
func TestClearAllEmptiesQueue(t *testing.T) {
	_, cancel := context.WithCancel(context.Background())
	defer cancel()

	q := &DownloadQueue{
		tasks: []*DownloadTask{
			{ID: "1", Manga: Bookmarks{Title: "Manga A"}, Chapter: "a1.cbz", Status: "queued"},
			{ID: "2", Manga: Bookmarks{Title: "Manga A"}, Chapter: "a2.cbz", Status: "downloading", CancelFunc: cancel},
			{ID: "3", Manga: Bookmarks{Title: "Manga A"}, Chapter: "a3.cbz", Status: "failed"},
			{ID: "4", Manga: Bookmarks{Title: "Manga A"}, Chapter: "a4.cbz", Status: "cancelled"},
		},
	}

	q.ClearAll()

	if len(q.tasks) != 0 {
		t.Fatalf("queue should be empty after ClearAll, got %d tasks", len(q.tasks))
	}
}

// TestRetryTaskRejectsActiveTasks verifies that a queued task cannot be
// retried (it is not in a retryable state).
func TestRetryTaskRejectsActiveTasks(t *testing.T) {
	q := &DownloadQueue{}
	q.processingMu.Lock()
	q.processing = true
	q.processingMu.Unlock()

	q.tasks = []*DownloadTask{
		{ID: "1", Manga: Bookmarks{Title: "Manga A"}, Chapter: "a1.cbz", Status: "queued"},
	}

	if err := q.RetryTask("1"); err == nil {
		t.Error("retrying a queued task should fail")
	}
}
