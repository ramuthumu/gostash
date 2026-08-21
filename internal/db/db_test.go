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
