package config

import (
	"context"
	"errors"
	"testing"
	"time"

	"kansho/cf"
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

// TestRetryTaskAllowsSkippedCF verifies that a task skipped because no
// Cloudflare data was provided can be retried once the data is available.
func TestRetryTaskAllowsSkippedCF(t *testing.T) {
	q := &DownloadQueue{}
	q.processingMu.Lock()
	q.processing = true
	q.processingMu.Unlock()

	q.tasks = []*DownloadTask{
		{ID: "1", Manga: Bookmarks{Title: "Manga A"}, Chapter: "a1.cbz", Status: "skipped_cf"},
	}

	if err := q.RetryTask("1"); err != nil {
		t.Fatalf("retrying a skipped_cf task should succeed, got: %v", err)
	}
	if q.tasks[0].Status != "queued" {
		t.Errorf("retried task should be queued, got %s", q.tasks[0].Status)
	}
}

// TestCancelTaskRemovesSkippedCF verifies that a skipped_cf task (which has no
// running download) is removed from the queue when cancelled, like a queued one.
func TestCancelTaskRemovesSkippedCF(t *testing.T) {
	q := &DownloadQueue{
		tasks: []*DownloadTask{
			{ID: "1", Manga: Bookmarks{Title: "Manga A"}, Chapter: "a1.cbz", Status: "skipped_cf"},
		},
	}

	if err := q.CancelTask("1"); err != nil {
		t.Fatalf("cancelling a skipped_cf task should succeed, got: %v", err)
	}
	if len(q.tasks) != 0 {
		t.Errorf("skipped_cf task should be removed from the queue, got %d tasks", len(q.tasks))
	}
}

// TestCancelAllCancelsWaitingCF verifies that CancelAll cancels a task waiting
// on a Cloudflare challenge (so the queue's CF wait is aborted) and marks
// skipped_cf tasks as cancelled too.
func TestCancelAllCancelsWaitingCF(t *testing.T) {
	_, cancel := context.WithCancel(context.Background())

	q := &DownloadQueue{
		tasks: []*DownloadTask{
			{ID: "1", Manga: Bookmarks{Title: "Manga A"}, Chapter: "a1.cbz", Status: "waiting_cf", CancelFunc: cancel},
			{ID: "2", Manga: Bookmarks{Title: "Manga A"}, Chapter: "a2.cbz", Status: "skipped_cf"},
			{ID: "3", Manga: Bookmarks{Title: "Manga A"}, Chapter: "a3.cbz", Status: "completed"},
		},
	}

	q.CancelAll()

	statuses := statusByID(q.tasks)
	if statuses["1"] != "cancelled" {
		t.Errorf("waiting_cf task should be cancelled, got %s", statuses["1"])
	}
	if statuses["2"] != "cancelled" {
		t.Errorf("skipped_cf task should be cancelled, got %s", statuses["2"])
	}
	if statuses["3"] != "completed" {
		t.Errorf("completed task must be left untouched, got %s", statuses["3"])
	}
}

// withTestCfData swaps the Cloudflare data check used by the queue for the
// duration of a test and restores the original afterwards.
func withTestCfData(t *testing.T, check func(domain string) bool) {
	t.Helper()
	orig := cfDataAvailable
	cfDataAvailable = check
	t.Cleanup(func() { cfDataAvailable = orig })
}

// withTestCfWaitTiming shortens the CF wait deadline and poll interval for the
// duration of a test and restores the original values afterwards.
func withTestCfWaitTiming(t *testing.T, timeout, poll time.Duration) {
	t.Helper()
	origTimeout, origPoll := cfWaitTimeout, cfWaitPollInterval
	cfWaitTimeout, cfWaitPollInterval = timeout, poll
	t.Cleanup(func() { cfWaitTimeout, cfWaitPollInterval = origTimeout, origPoll })
}

// TestTaskNeedsCF verifies the static classification the queue uses to decide
// whether a queued chapter would require Cloudflare bypass data.
func TestTaskNeedsCF(t *testing.T) {
	RegisterSiteCfRequirement("cf-site", true)
	RegisterSiteCfRequirement("plain-site", false)
	withTestCfData(t, func(domain string) bool {
		return domain == "ok.example.com"
	})

	cases := []struct {
		name string
		task *DownloadTask
		want bool
	}{
		{
			name: "CF site without stored data needs CF",
			task: &DownloadTask{Manga: Bookmarks{Site: "cf-site", Url: "https://cf.example.com/manga"}},
			want: true,
		},
		{
			name: "CF site with stored data does not need CF",
			task: &DownloadTask{Manga: Bookmarks{Site: "cf-site", Url: "https://ok.example.com/manga"}},
			want: false,
		},
		{
			name: "non-CF site never needs CF",
			task: &DownloadTask{Manga: Bookmarks{Site: "plain-site", Url: "https://plain.example.com/manga"}},
			want: false,
		},
		{
			name: "CF site without URL is conservatively treated as needing CF",
			task: &DownloadTask{Manga: Bookmarks{Site: "cf-site"}},
			want: true,
		},
	}

	for _, tc := range cases {
		if got := taskNeedsCF(tc.task); got != tc.want {
			t.Errorf("%s: taskNeedsCF() = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestSkipCfBlockedTasks verifies that after a CF wait timeout the queue marks
// CF-protected queued tasks as skipped while leaving non-CF tasks queued.
func TestSkipCfBlockedTasks(t *testing.T) {
	RegisterSiteCfRequirement("cf-site", true)
	RegisterSiteCfRequirement("plain-site", false)
	withTestCfData(t, func(domain string) bool { return false })

	q := &DownloadQueue{
		tasks: []*DownloadTask{
			{ID: "blocked", Manga: Bookmarks{Title: "Manga A"}, Chapter: "a1.cbz", Status: "waiting_cf"},
			{ID: "cf", Manga: Bookmarks{Site: "cf-site", Title: "Manga A"}, Chapter: "a2.cbz", Status: "queued"},
			{ID: "plain", Manga: Bookmarks{Site: "plain-site", Title: "Manga B"}, Chapter: "b1.cbz", Status: "queued"},
			{ID: "done", Manga: Bookmarks{Site: "cf-site", Title: "Manga C"}, Chapter: "c1.cbz", Status: "completed"},
		},
	}

	q.skipCfBlockedTasks("blocked")

	statuses := statusByID(q.tasks)
	if statuses["blocked"] != "waiting_cf" {
		t.Errorf("the task that hit the challenge must stay waiting_cf, got %s", statuses["blocked"])
	}
	if statuses["cf"] != "skipped_cf" {
		t.Errorf("CF-protected queued task should be skipped, got %s", statuses["cf"])
	}
	if statuses["plain"] != "queued" {
		t.Errorf("non-CF queued task must remain queued, got %s", statuses["plain"])
	}
	if statuses["done"] != "completed" {
		t.Errorf("completed task must be left untouched, got %s", statuses["done"])
	}
}

// TestHandleCfWaitTimeoutSkipsCfTasks verifies the full pause path: when no CF
// data arrives within the timeout, the blocked task stays waiting_cf and the
// remaining CF-protected queued tasks are skipped so non-CF downloads proceed.
func TestHandleCfWaitTimeoutSkipsCfTasks(t *testing.T) {
	RegisterSiteCfRequirement("cf-site", true)
	RegisterSiteCfRequirement("plain-site", false)
	withTestCfData(t, func(domain string) bool { return false })
	withTestCfWaitTiming(t, 30*time.Millisecond, 5*time.Millisecond)

	blocked := &DownloadTask{
		ID:            "blocked",
		Manga:         Bookmarks{Title: "Manga A"},
		Chapter:       "a1.cbz",
		Status:        "waiting_cf",
		StatusMessage: "Cloudflare challenge detected - browser opened",
		Error:         &cf.CfChallengeError{URL: "https://cf.example.com/chapter-1/"},
	}
	q := &DownloadQueue{
		tasks: []*DownloadTask{
			blocked,
			{ID: "cf", Manga: Bookmarks{Site: "cf-site", Title: "Manga A"}, Chapter: "a2.cbz", ChapterURL: "https://cf.example.com/chapter-2/", Status: "queued"},
			{ID: "plain", Manga: Bookmarks{Site: "plain-site", Title: "Manga B"}, Chapter: "b1.cbz", Status: "queued"},
		},
	}

	q.handleCfWait(blocked)

	statuses := statusByID(q.tasks)
	if statuses["blocked"] != "waiting_cf" {
		t.Errorf("blocked task should stay waiting_cf after timeout, got %s", statuses["blocked"])
	}
	if statuses["cf"] != "skipped_cf" {
		t.Errorf("CF-protected queued task should be skipped after timeout, got %s", statuses["cf"])
	}
	if statuses["plain"] != "queued" {
		t.Errorf("non-CF queued task must remain queued, got %s", statuses["plain"])
	}
}

// TestHandleCfWaitResumesWhenDataArrives verifies that the queue re-queues the
// blocked task as soon as Cloudflare bypass data for its domain is available.
func TestHandleCfWaitResumesWhenDataArrives(t *testing.T) {
	withTestCfData(t, func(domain string) bool { return domain == "cf.example.com" })
	withTestCfWaitTiming(t, time.Second, 5*time.Millisecond)

	blocked := &DownloadTask{
		ID:      "1",
		Manga:   Bookmarks{Title: "Manga A"},
		Chapter: "a1.cbz",
		Status:  "waiting_cf",
		Error:   &cf.CfChallengeError{URL: "https://cf.example.com/chapter-1/"},
	}
	q := &DownloadQueue{tasks: []*DownloadTask{blocked}}

	q.handleCfWait(blocked)

	if blocked.Status != "queued" {
		t.Errorf("blocked task should be re-queued when CF data arrives, got %s", blocked.Status)
	}
	if blocked.Error != nil {
		t.Errorf("blocked task error should be cleared, got %v", blocked.Error)
	}
}

// TestResumeCfTasksRequeuesBlockedTasksWithData verifies that after Cloudflare
// data is imported, blocked tasks whose domain now has data are re-queued
// automatically while tasks that still have no data stay blocked.
func TestResumeCfTasksRequeuesBlockedTasksWithData(t *testing.T) {
	RegisterSiteCfRequirement("cf-site", true)
	RegisterSiteCfRequirement("plain-site", false)
	withTestCfData(t, func(domain string) bool {
		return domain == "ready.example.com"
	})

	q := &DownloadQueue{}
	q.processingMu.Lock()
	q.processing = true
	q.processingMu.Unlock()

	q.tasks = []*DownloadTask{
		{ID: "wait", Manga: Bookmarks{Site: "cf-site", Url: "https://ready.example.com/manga"}, Chapter: "a1.cbz", Status: "waiting_cf", Error: &cf.CfChallengeError{URL: "https://ready.example.com/chapter-1/"}},
		{ID: "skip", Manga: Bookmarks{Site: "cf-site", Url: "https://ready.example.com/manga"}, Chapter: "a2.cbz", Status: "skipped_cf"},
		{ID: "still", Manga: Bookmarks{Site: "cf-site", Url: "https://else.example.com/manga"}, Chapter: "a3.cbz", Status: "skipped_cf"},
		{ID: "done", Manga: Bookmarks{Site: "plain-site", Title: "Manga B"}, Chapter: "b1.cbz", Status: "completed"},
	}

	q.ResumeCfTasks()

	statuses := statusByID(q.tasks)
	if statuses["wait"] != "queued" {
		t.Errorf("waiting_cf task with data should be re-queued, got %s", statuses["wait"])
	}
	if q.tasks[0].Error != nil {
		t.Errorf("resumed task error should be cleared, got %v", q.tasks[0].Error)
	}
	if statuses["skip"] != "queued" {
		t.Errorf("skipped_cf task with data should be re-queued, got %s", statuses["skip"])
	}
	if statuses["still"] != "skipped_cf" {
		t.Errorf("task without data must stay skipped, got %s", statuses["still"])
	}
	if statuses["done"] != "completed" {
		t.Errorf("completed task must be left untouched, got %s", statuses["done"])
	}
}

// TestHandleCfWaitAbortsOnCancel verifies that cancelling the blocked task (for
// example via Cancel All) aborts the queue's wait without waiting for the full
// timeout.
func TestHandleCfWaitAbortsOnCancel(t *testing.T) {
	withTestCfData(t, func(domain string) bool { return false })
	withTestCfWaitTiming(t, time.Minute, 5*time.Millisecond)

	blocked := &DownloadTask{
		ID:      "1",
		Manga:   Bookmarks{Title: "Manga A"},
		Chapter: "a1.cbz",
		Status:  "waiting_cf",
		Error:   &cf.CfChallengeError{URL: "https://cf.example.com/chapter-1/"},
	}
	q := &DownloadQueue{tasks: []*DownloadTask{blocked}}

	done := make(chan struct{})
	go func() {
		q.handleCfWait(blocked)
		close(done)
	}()

	time.Sleep(3 * cfWaitPollInterval)
	q.mu.Lock()
	blocked.Status = "cancelled"
	q.mu.Unlock()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handleCfWait should abort promptly when the task is cancelled")
	}
}
