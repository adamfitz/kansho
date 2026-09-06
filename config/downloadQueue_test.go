package config

import (
	"context"
	"errors"
	"sync/atomic"
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

// TestClearRetriesRemovesOnlyRetryableTasks verifies that ClearRetries removes
// every task that can be retried (failed, cancelled, waiting on a CF challenge,
// or CF-skipped) across all manga titles, while leaving active and still-queued
// tasks untouched.
func TestClearRetriesRemovesOnlyRetryableTasks(t *testing.T) {
	_, cancel := context.WithCancel(context.Background())
	defer cancel()

	q := &DownloadQueue{
		tasks: []*DownloadTask{
			{ID: "1", Manga: Bookmarks{Title: "Manga A"}, Chapter: "a1.cbz", Status: "queued"},
			{ID: "2", Manga: Bookmarks{Title: "Manga A"}, Chapter: "a2.cbz", Status: "downloading", CancelFunc: cancel},
			{ID: "3", Manga: Bookmarks{Title: "Manga A"}, Chapter: "a3.cbz", Status: "failed"},
			{ID: "4", Manga: Bookmarks{Title: "Manga B"}, Chapter: "b1.cbz", Status: "cancelled"},
			{ID: "5", Manga: Bookmarks{Title: "Manga B"}, Chapter: "b2.cbz", Status: "waiting_cf", CancelFunc: cancel},
			{ID: "6", Manga: Bookmarks{Title: "Manga B"}, Chapter: "b3.cbz", Status: "skipped_cf"},
			{ID: "7", Manga: Bookmarks{Title: "Manga C"}, Chapter: "c1.cbz", Status: "completed"},
		},
	}

	q.ClearRetries()

	var statuses map[string]string = make(map[string]string, len(q.tasks))
	for _, task := range q.tasks {
		statuses[task.ID] = task.Status
	}

	if len(q.tasks) != 3 {
		t.Fatalf("queue should keep only non-retryable tasks after ClearRetries, got %d tasks: %v", len(q.tasks), statuses)
	}
	if statuses["1"] != "queued" {
		t.Errorf("queued task should be kept, got %s", statuses["1"])
	}
	if statuses["2"] != "downloading" {
		t.Errorf("downloading task should be kept, got %s", statuses["2"])
	}
	if statuses["7"] != "completed" {
		t.Errorf("completed task should be kept, got %s", statuses["7"])
	}
	if _, ok := statuses["3"]; ok {
		t.Error("failed task should have been removed")
	}
	if _, ok := statuses["4"]; ok {
		t.Error("cancelled task should have been removed")
	}
	if _, ok := statuses["5"]; ok {
		t.Error("waiting_cf task should have been removed")
	}
	if _, ok := statuses["6"]; ok {
		t.Error("skipped_cf task should have been removed")
	}
}

// TestClearRetriesAbortsWaitingCF verifies that removing a waiting_cf task
// calls its cancel function so any CF-wait goroutine parked on it aborts.
func TestClearRetriesAbortsWaitingCF(t *testing.T) {
	cancelled := false
	cancelFunc := func() {
		cancelled = true
	}

	q := &DownloadQueue{
		tasks: []*DownloadTask{
			{ID: "cf", Manga: Bookmarks{Title: "Manga A"}, Chapter: "a1.cbz", Status: "waiting_cf", CancelFunc: cancelFunc},
		},
	}

	q.ClearRetries()

	if len(q.tasks) != 0 {
		t.Fatalf("queue should be empty after ClearRetries, got %d tasks", len(q.tasks))
	}
	if !cancelled {
		t.Error("cancel function of a removed waiting_cf task should have been called")
	}
}

// TestRetryTaskRejectsActiveTasks verifies that a queued task cannot be
// retried (it is not in a retryable state).
func TestRetryTaskRejectsActiveTasks(t *testing.T) {
	q := &DownloadQueue{}

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

// TestNextDispatchableSkipsBusySites verifies that nextDispatchable never hands
// out two queued tasks for the same site while that site already has a task in
// the "downloading" state, and that it returns nil only once nothing is left.
func TestNextDispatchableSkipsBusySites(t *testing.T) {
	q := &DownloadQueue{
		tasks: []*DownloadTask{
			{ID: "a1", Manga: Bookmarks{Title: "A", Site: "siteA"}, Chapter: "a1.cbz", Status: "downloading"},
			{ID: "a2", Manga: Bookmarks{Title: "A", Site: "siteA"}, Chapter: "a2.cbz", Status: "queued"},
			{ID: "b1", Manga: Bookmarks{Title: "B", Site: "siteB"}, Chapter: "b1.cbz", Status: "queued"},
		},
	}

	task := q.nextDispatchable()
	if task == nil || task.ID != "b1" {
		t.Fatalf("expected siteB task to be dispatchable while siteA is busy, got %+v", task)
	}
	task.Status = "downloading"

	if task := q.nextDispatchable(); task != nil {
		t.Fatalf("expected no dispatchable task once all sites are busy, got %+v", task)
	}

	q.mu.Lock()
	q.tasks[0].Status = "completed"
	q.tasks[1].Status = "completed"
	q.tasks[2].Status = "completed"
	q.mu.Unlock()

	if task := q.nextDispatchable(); task != nil {
		t.Fatalf("expected nil when no task is queued, got %+v", task)
	}
}

// TestNextDispatchableYieldsWithinSiteInFifoOrder verifies that, when a site
// becomes free, its queued chapters are handed out in the order they were
// enqueued (FIFO within a site).
func TestNextDispatchableYieldsWithinSiteInFifoOrder(t *testing.T) {
	q := &DownloadQueue{
		tasks: []*DownloadTask{
			{ID: "a1", Manga: Bookmarks{Title: "A", Site: "siteA"}, Chapter: "a1.cbz", Status: "queued"},
			{ID: "a2", Manga: Bookmarks{Title: "A", Site: "siteA"}, Chapter: "a2.cbz", Status: "queued"},
		},
	}

	for _, want := range []string{"a1", "a2"} {
		task := q.nextDispatchable()
		if task == nil || task.ID != want {
			t.Fatalf("expected to dispatch %s, got %+v", want, task)
		}
		// Free the site once the chapter is done so the next queued chapter of
		// the same site becomes dispatchable in FIFO order.
		task.Status = "completed"
	}
	if task := q.nextDispatchable(); task != nil {
		t.Fatalf("expected no more dispatchable tasks, got %+v", task)
	}
}

// TestRunTaskGatesAndReleasesCfWaits verifies that a worker whose task hits a
// Cloudflare challenge holds the CF gate open while handleCfWait is running, and
// releases it once the wait ends.
func TestRunTaskGatesAndReleasesCfWaits(t *testing.T) {
	withTestCfData(t, func(domain string) bool { return false })
	withTestCfWaitTiming(t, time.Minute, 5*time.Millisecond)

	registerFakeChapterDownload(func() error {
		return &cf.CfChallengeError{URL: "https://cf.example.com/chapter-1/"}
	})

	q := &DownloadQueue{}
	q.setCallbacksForPoolTest()
	q.tasks = []*DownloadTask{
		{ID: "1", Manga: Bookmarks{Title: "A", Site: "siteA"}, Chapter: "a1.cbz", ChapterURL: "http://a1", Status: "queued"},
	}
	task := q.tasks[0]

	done := make(chan struct{})
	go func() {
		q.runTask(task)
		close(done)
	}()

	start := time.Now()
	for time.Since(start) < 5*time.Second {
		q.mu.Lock()
		gate := q.cfWaits
		q.mu.Unlock()
		if gate == 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	q.mu.Lock()
	if q.cfWaits != 1 {
		t.Fatalf("expected cfWaits to be held at 1 while the CF wait runs, got %d", q.cfWaits)
	}
	q.mu.Unlock()

	// Abort the CF wait by cancelling the task, then verify the gate releases.
	q.mu.Lock()
	task.Status = "cancelled"
	q.mu.Unlock()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("runTask should return once the CF wait aborts")
	}

	waitFor(func() bool {
		q.mu.Lock()
		defer q.mu.Unlock()
		return q.cfWaits == 0
	})
}

// TestWorkerPoolRunsConcurrentDownloads verifies that the dispatcher starts up
// to maxWorkers downloads concurrently across distinct sites, never beyond that
// limit, and that a task queued later starts once a slot frees up.
func TestWorkerPoolRunsConcurrentDownloads(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{}, 10)
	var startedCounted int32
	registerFakeChapterDownload(func() error {
		atomic.AddInt32(&startedCounted, 1)
		started <- struct{}{}
		<-release
		return nil
	})

	q := &DownloadQueue{maxWorkers: 3}
	q.setCallbacksForPoolTest()
	q.tasks = []*DownloadTask{
		{ID: "1", Manga: Bookmarks{Title: "A", Site: "siteA"}, Chapter: "a1.cbz", ChapterURL: "http://a", Status: "queued"},
		{ID: "2", Manga: Bookmarks{Title: "B", Site: "siteB"}, Chapter: "b1.cbz", ChapterURL: "http://b", Status: "queued"},
		{ID: "3", Manga: Bookmarks{Title: "C", Site: "siteC"}, Chapter: "c1.cbz", ChapterURL: "http://c", Status: "queued"},
		{ID: "4", Manga: Bookmarks{Title: "D", Site: "siteD"}, Chapter: "d1.cbz", ChapterURL: "http://d", Status: "queued"},
	}
	q.startDispatcher()

	waitCount(t, started, 3, "three concurrent downloads should start")

	q.mu.RLock()
	active := make([]string, 0, 3)
	var activeCount int
	for _, tk := range q.tasks {
		if tk.Status == "downloading" {
			active = append(active, tk.ID)
			activeCount++
		}
	}
	if activeCount != 3 {
		t.Errorf("expected exactly 3 concurrent downloads, got %d (%v)", activeCount, active)
	}
	if s := q.qTaskByID("4").Status; s != "queued" {
		t.Errorf("task 4 should stay queued while all %d slots are busy, got %s", q.maxWorkers, s)
	}
	q.mu.RUnlock()

	// Free one slot; the fourth site's task must then start.
	release <- struct{}{}
	waitSignal(t, started, "fourth download should start once a slot frees up")

	// Drain the remaining workers.
	close(release)
	waitFor(func() bool {
		q.mu.RLock()
		done := q.running == 0
		q.mu.RUnlock()
		return done
	})

	if got := atomic.LoadInt32(&startedCounted); got != 4 {
		t.Errorf("expected 4 downloads overall, got %d", got)
	}
}

// TestWorkerPoolNeverRunsSecondTaskOnSameSite verifies that a queued chapter is
// not dispatched while another chapter of the same site is still downloading,
// even if other sites have already filled the remaining worker slots.
func TestWorkerPoolNeverRunsSecondTaskOnSameSite(t *testing.T) {
	started := make(chan struct{}, 10)
	release := make(chan struct{})
	registerFakeChapterDownload(func() error {
		started <- struct{}{}
		<-release
		return nil
	})

	q := &DownloadQueue{maxWorkers: 3}
	q.setCallbacksForPoolTest()
	q.tasks = []*DownloadTask{
		{ID: "a1", Manga: Bookmarks{Title: "A", Site: "siteA"}, Chapter: "a1.cbz", ChapterURL: "http://a1", Status: "queued"},
		{ID: "a2", Manga: Bookmarks{Title: "A", Site: "siteA"}, Chapter: "a2.cbz", ChapterURL: "http://a2", Status: "queued"},
		{ID: "b1", Manga: Bookmarks{Title: "B", Site: "siteB"}, Chapter: "b1.cbz", ChapterURL: "http://b1", Status: "queued"},
	}
	q.startDispatcher()

	// Exactly two downloads start (one per distinct site); a2 must stay put.
	waitCount(t, started, 2, "one download per distinct site should start")

	q.mu.RLock()
	activeCount := 0
	for _, tk := range q.tasks {
		if tk.Status == "downloading" {
			activeCount++
		}
	}
	if activeCount != 2 {
		t.Errorf("expected exactly 2 concurrent downloads (siteA + siteB), got %d", activeCount)
	}
	if s := q.qTaskByID("a2").Status; s != "queued" {
		t.Errorf("second chapter of siteA must stay queued while siteA is busy, got %s", s)
	}
	q.mu.RUnlock()

	// Release the running workers; only then may a2 run, and it runs alone.
	close(release)
	waitFor(func() bool {
		q.mu.RLock()
		done := q.running == 0
		q.mu.RUnlock()
		return done
	})
}

// qTaskByID returns the queue task with the given ID (callers must hold the
// queue mutex).
func (q *DownloadQueue) qTaskByID(id string) *DownloadTask {
	for _, t := range q.tasks {
		if t.ID == id {
			return t
		}
	}
	return nil
}

// setCallbacksForPoolTest installs no-op UI callbacks on a bare queue so pooling
// tests that go through startDispatcher do not nil-pointer panic.
func (q *DownloadQueue) setCallbacksForPoolTest() {
	q.SetCallbacks(
		func(*DownloadTask) {},
		func(*DownloadTask) {},
		func(string) {},
		func() {},
	)
}

// waitCount blocks until at least want signals arrive on ch, or the test times
// out.
func waitCount(t *testing.T, ch chan struct{}, want int, msg string) {
	t.Helper()
	for got := 0; got < want; got++ {
		select {
		case <-ch:
		case <-time.After(5 * time.Second):
			t.Fatalf("%s (got %d of %d within timeout)", msg, got, want)
		}
	}
}

// waitSignal blocks until a single signal arrives on ch, or the test times out.
func waitSignal(t *testing.T, ch chan struct{}, msg string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatalf("%s (no signal within timeout)", msg)
	}
}

// waitFor polls cond until it returns true or a 5s timeout elapses.
func waitFor(cond func() bool) {
	deadline := time.Now().Add(5 * time.Second)
	for {
		if cond() {
			return
		}
		if time.Now().After(deadline) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}
