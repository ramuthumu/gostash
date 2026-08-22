package db

import (
	"path/filepath"
	"testing"
)

func TestStoreOperations(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	// 1. Save articles
	id1, err := store.Save(Article{
		URL:         "https://example.com/art1",
		Title:       "Go Concurrency Patterns",
		Author:      "Rob Pike",
		Excerpt:     "Learn how channels and goroutines work together.",
		ContentHTML: "<p>Learn how channels and goroutines work together in Go.</p>",
		TextContent: "Learn how channels and goroutines work together in Go. Concurrency is not parallelism.",
	})
	if err != nil {
		t.Fatalf("Save art1 failed: %v", err)
	}

	id2, err := store.Save(Article{
		URL:         "https://example.com/art2",
		Title:       "SQLite Full Text Search Guide",
		Author:      "D. Richard Hipp",
		Excerpt:     "FTS5 virtual tables provide blazing fast text queries.",
		ContentHTML: "<p>FTS5 virtual tables provide blazing fast text queries in SQLite databases.</p>",
		TextContent: "FTS5 virtual tables provide blazing fast text queries in SQLite databases. It supports trigram and unicode tokenizers.",
	})
	if err != nil {
		t.Fatalf("Save art2 failed: %v", err)
	}

	// 2. Check counts (both unread by default)
	counts, err := store.Counts()
	if err != nil {
		t.Fatalf("Counts failed: %v", err)
	}
	if counts.Unread != 2 || counts.Archive != 0 || counts.Total != 2 {
		t.Errorf("expected 2 unread, 0 archive, 2 total; got %+v", counts)
	}

	// 3. Mark art1 as read
	if err := store.SetRead(id1, true); err != nil {
		t.Fatalf("SetRead failed: %v", err)
	}

	counts, err = store.Counts()
	if err != nil {
		t.Fatalf("Counts after read failed: %v", err)
	}
	if counts.Unread != 1 || counts.Archive != 1 || counts.Total != 2 {
		t.Errorf("expected 1 unread, 1 archive, 2 total; got %+v", counts)
	}

	// 4. Test List filtering
	unreadList, err := store.List(FilterUnread, "")
	if err != nil {
		t.Fatalf("List unread failed: %v", err)
	}
	if len(unreadList) != 1 || unreadList[0].ID != id2 {
		t.Errorf("expected unreadList to contain id2, got %+v", unreadList)
	}

	archiveList, err := store.List(FilterArchive, "")
	if err != nil {
		t.Fatalf("List archive failed: %v", err)
	}
	if len(archiveList) != 1 || archiveList[0].ID != id1 {
		t.Errorf("expected archiveList to contain id1, got %+v", archiveList)
	}

	allList, err := store.List(FilterAll, "")
	if err != nil {
		t.Fatalf("List all failed: %v", err)
	}
	if len(allList) != 2 {
		t.Errorf("expected allList to contain 2 articles, got %d", len(allList))
	}

	// 5. Test FTS5 Search
	searchRes, err := store.List(FilterAll, "goroutines")
	if err != nil {
		t.Fatalf("Search 'goroutines' failed: %v", err)
	}
	if len(searchRes) != 1 || searchRes[0].ID != id1 {
		t.Errorf("expected search for 'goroutines' to find id1, got %+v", searchRes)
	}

	searchRes, err = store.List(FilterAll, "FTS5")
	if err != nil {
		t.Fatalf("Search 'FTS5' failed: %v", err)
	}
	if len(searchRes) != 1 || searchRes[0].ID != id2 {
		t.Errorf("expected search for 'FTS5' to find id2, got %+v", searchRes)
	}

	// 6. Test WordCount & ReadingTime stats
	art2, err := store.Get(id2)
	if err != nil {
		t.Fatalf("Get art2 failed: %v", err)
	}
	if art2.WordCount <= 0 || art2.ReadingTimeMinutes < 1 {
		t.Errorf("expected valid stats on art2, got WordCount=%d, ReadingTimeMinutes=%d", art2.WordCount, art2.ReadingTimeMinutes)
	}

	// 7. Delete article and verify FTS trigger deletion
	if err := store.Delete(id1); err != nil {
		t.Fatalf("Delete art1 failed: %v", err)
	}
	searchRes, err = store.List(FilterAll, "goroutines")
	if err != nil {
		t.Fatalf("Search after delete failed: %v", err)
	}
	if len(searchRes) != 0 {
		t.Errorf("expected 0 results for deleted article, got %d", len(searchRes))
	}
}

func TestAsyncLifecycle(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	id, err := store.Enqueue("https://example.com/pending")
	if err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}
	pending, err := store.PendingArticles()
	if err != nil || len(pending) != 1 || pending[0].ID != id {
		t.Fatalf("expected one pending article id=%d, got %+v err=%v", id, pending, err)
	}
	// A pending row must not appear as a readable list entry with content.
	got, err := store.Get(id)
	if err != nil || got.Status != StatusPending {
		t.Fatalf("expected pending status, got %+v err=%v", got, err)
	}

	if err := store.Complete(id, Article{
		URL:         "https://example.com/pending",
		Title:       "Done",
		TextContent: "one two three four five",
	}); err != nil {
		t.Fatalf("Complete failed: %v", err)
	}
	got, err = store.Get(id)
	if err != nil || got.Status != StatusReady || got.Title != "Done" {
		t.Fatalf("expected ready article, got %+v err=%v", got, err)
	}
	if got.WordCount != 5 || got.ReadingTimeMinutes < 1 {
		t.Errorf("stats not persisted: WordCount=%d ReadingTime=%d", got.WordCount, got.ReadingTimeMinutes)
	}

	// Fail path stores the reason and the status survives round trips.
	id2, err := store.Enqueue("https://example.com/doomed")
	if err != nil {
		t.Fatalf("Enqueue doomed failed: %v", err)
	}
	if err := store.Fail(id2, "HTTP 404"); err != nil {
		t.Fatalf("Fail failed: %v", err)
	}
	got, err = store.Get(id2)
	if err != nil || got.Status != StatusFailed || got.FetchError != "HTTP 404" {
		t.Fatalf("expected failed status with error, got %+v err=%v", got, err)
	}

	// Re-enqueueing an existing URL flips it back to pending.
	if _, err := store.Enqueue("https://example.com/pending"); err != nil {
		t.Fatalf("re-Enqueue failed: %v", err)
	}
	got, _ = store.Get(id)
	if got.Status != StatusPending {
		t.Errorf("expected re-enqueued article to be pending, got %s", got.Status)
	}
}

func TestMediaRefs(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()
	a1 := Article{
		URL:         "https://example.com/1",
		Title:       "One",
		TextContent: "text",
		ContentHTML: `<p><img src="/media/aaaa.bbb"><img src="https://x/y.png"></p>`,
	}
	a2 := Article{
		URL:         "https://example.com/2",
		Title:       "Two",
		TextContent: "text",
		ContentHTML: `<img src="/media/cccc.png">`,
	}
	for _, a := range []Article{a1, a2} {
		if _, err := store.Save(a); err != nil {
			t.Fatalf("Save failed: %v", err)
		}
	}
	refs, err := store.MediaRefs()
	if err != nil {
		t.Fatalf("MediaRefs failed: %v", err)
	}
	if !refs["aaaa.bbb"] || !refs["cccc.png"] {
		t.Errorf("missing expected refs: %v", refs)
	}
	for name := range refs {
		if name == "y.png" {
			t.Errorf("remote image wrongly captured as media ref: %v", refs)
		}
	}
}
