package db

import (
	"database/sql"
	"fmt"
	"regexp"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const (
	FilterUnread  = "unread"
	FilterArchive = "archive"
	FilterAll     = "all"
)

// Article lifecycle statuses. Articles are created in StatusPending by the
// async archiving pipeline, move to StatusReady once content is extracted,
// or StatusFailed if the fetch/extract failed (with FetchError set).
const (
	StatusPending = "pending"
	StatusReady   = "ready"
	StatusFailed  = "failed"
)

type Article struct {
	ID                 int64
	URL                string
	Title              string
	Author             string
	Excerpt            string
	ContentHTML        string
	TextContent        string
	ArchivedAt         time.Time
	Read               bool
	WordCount          int
	ReadingTimeMinutes int
	Status             string
	FetchError         string
}

// CalculateStats computes WordCount and ReadingTimeMinutes from TextContent.
// Stored stats win: rows saved since the word_count column exists already
// carry them, so computation only happens as a fallback for legacy rows and
// freshly extracted articles.
func (a *Article) CalculateStats() {
	if a.WordCount > 0 {
		return
	}
	text := strings.TrimSpace(a.TextContent)
	if text == "" {
		text = strings.TrimSpace(a.Excerpt)
	}
	if text == "" {
		text = strings.TrimSpace(a.Title)
	}
	words := strings.Fields(text)
	a.WordCount = len(words)
	if a.WordCount > 0 {
		// Standard adult reading speed: ~220 words per minute. Minimum 1 minute.
		mins := (a.WordCount + 110) / 220
		if mins < 1 {
			mins = 1
		}
		a.ReadingTimeMinutes = mins
	}
}

type Counts struct {
	Unread  int
	Archive int
	Total   int
}

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	d, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	// Better concurrency for SQLite
	_, _ = d.Exec("PRAGMA journal_mode=WAL;")
	_, _ = d.Exec("PRAGMA busy_timeout=5000;")
	if err := d.Ping(); err != nil {
		return nil, err
	}
	schema := `
	CREATE TABLE IF NOT EXISTS articles (
		id            INTEGER PRIMARY KEY AUTOINCREMENT,
		url           TEXT NOT NULL UNIQUE,
		title         TEXT,
		author        TEXT,
		excerpt       TEXT,
		content_html  TEXT,
		text_content  TEXT,
		archived_at   DATETIME DEFAULT CURRENT_TIMESTAMP,
		read          INTEGER DEFAULT 0,
		status        TEXT NOT NULL DEFAULT 'ready',
		fetch_error   TEXT NOT NULL DEFAULT '',
		word_count    INTEGER NOT NULL DEFAULT 0,
		reading_time  INTEGER NOT NULL DEFAULT 0
	);

	CREATE VIRTUAL TABLE IF NOT EXISTS articles_fts USING fts5(
		title,
		author,
		excerpt,
		text_content,
		content='articles',
		content_rowid='id'
	);

	CREATE TRIGGER IF NOT EXISTS articles_ai AFTER INSERT ON articles BEGIN
		INSERT INTO articles_fts(rowid, title, author, excerpt, text_content)
		VALUES (new.id, new.title, new.author, new.excerpt, new.text_content);
	END;

	CREATE TRIGGER IF NOT EXISTS articles_ad AFTER DELETE ON articles BEGIN
		INSERT INTO articles_fts(articles_fts, rowid, title, author, excerpt, text_content)
		VALUES ('delete', old.id, old.title, old.author, old.excerpt, old.text_content);
	END;

	CREATE TRIGGER IF NOT EXISTS articles_au AFTER UPDATE ON articles BEGIN
		INSERT INTO articles_fts(articles_fts, rowid, title, author, excerpt, text_content)
		VALUES ('delete', old.id, old.title, old.author, old.excerpt, old.text_content);
		INSERT INTO articles_fts(rowid, title, author, excerpt, text_content)
		VALUES (new.id, new.title, new.author, new.excerpt, new.text_content);
	END;
	`
	if _, err := d.Exec(schema); err != nil {
		return nil, err
	}
	// Migrate databases created before these columns existed.
	migrations := []struct{ col, def string }{
		{"status", "TEXT NOT NULL DEFAULT 'ready'"},
		{"fetch_error", "TEXT NOT NULL DEFAULT ''"},
		{"word_count", "INTEGER NOT NULL DEFAULT 0"},
		{"reading_time", "INTEGER NOT NULL DEFAULT 0"},
	}
	for _, m := range migrations {
		if err := ensureColumn(d, "articles", m.col, m.def); err != nil {
			return nil, fmt.Errorf("migrate %s: %w", m.col, err)
		}
	}

	// Rebuild FTS index if table exists but FTS was newly added or empty
	var countArticles, countFTS int
	_ = d.QueryRow("SELECT COUNT(*) FROM articles").Scan(&countArticles)
	_ = d.QueryRow("SELECT COUNT(*) FROM articles_fts").Scan(&countFTS)
	if countArticles > 0 && countFTS == 0 {
		_, _ = d.Exec("INSERT INTO articles_fts(articles_fts) VALUES('rebuild');")
	}

	return &Store{db: d}, nil
}

// ensureColumn adds col to table with the given DDL fragment if the column is
// missing. Only called with package-constant identifiers.
func ensureColumn(d *sql.DB, table, col, def string) error {
	rows, err := d.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var dflt sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dflt, &pk); err != nil {
			return err
		}
		if name == col {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = d.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, col, def))
	return err
}

func (s *Store) Close() error { return s.db.Close() }

// Save inserts or refreshes a fully-extracted article (StatusReady) and
// returns its id in one round trip via RETURNING.
func (s *Store) Save(a Article) (int64, error) {
	a.CalculateStats()
	var id int64
	err := s.db.QueryRow(`
		INSERT INTO articles(url, title, author, excerpt, content_html, text_content, status, fetch_error, word_count, reading_time)
		VALUES (?, ?, ?, ?, ?, ?, ?, '', ?, ?)
		ON CONFLICT(url) DO UPDATE SET
			title=excluded.title,
			author=excluded.author,
			excerpt=excluded.excerpt,
			content_html=excluded.content_html,
			text_content=excluded.text_content,
			status=excluded.status,
			fetch_error='',
			word_count=excluded.word_count,
			reading_time=excluded.reading_time
		RETURNING id`,
		a.URL, a.Title, a.Author, a.Excerpt, a.ContentHTML, a.TextContent, StatusReady, a.WordCount, a.ReadingTimeMinutes).Scan(&id)
	return id, err
}

// Enqueue marks url for archiving: it inserts a pending row, or flips an
// existing row back to pending so the worker refetches and refreshes it.
// Returns the row id; status is always StatusPending on return.
func (s *Store) Enqueue(rawURL string) (int64, error) {
	if _, err := s.db.Exec(`
		INSERT INTO articles(url, status) VALUES (?, ?)
		ON CONFLICT(url) DO UPDATE SET status=?, fetch_error=''`,
		rawURL, StatusPending, StatusPending); err != nil {
		return 0, err
	}
	var id int64
	err := s.db.QueryRow(`SELECT id FROM articles WHERE url = ?`, rawURL).Scan(&id)
	return id, err
}

// Complete stores extraction results for a pending article and marks it ready.
func (s *Store) Complete(id int64, a Article) error {
	a.CalculateStats()
	_, err := s.db.Exec(`
		UPDATE articles SET
			title=?, author=?, excerpt=?, content_html=?, text_content=?,
			status=?, fetch_error='', word_count=?, reading_time=?
		WHERE id=?`,
		a.Title, a.Author, a.Excerpt, a.ContentHTML, a.TextContent,
		StatusReady, a.WordCount, a.ReadingTimeMinutes, id)
	return err
}

// Fail marks an article failed with a human-readable reason (truncated).
func (s *Store) Fail(id int64, errMsg string) error {
	if len(errMsg) > 500 {
		errMsg = errMsg[:500]
	}
	_, err := s.db.Exec(`UPDATE articles SET status=?, fetch_error=? WHERE id=?`, StatusFailed, errMsg, id)
	return err
}

// PendingArticles lists rows stuck in pending — used to resume workers that a
// crash interrupted mid-fetch.
func (s *Store) PendingArticles() ([]Article, error) {
	rows, err := s.db.Query(`SELECT id, url FROM articles WHERE status = ? ORDER BY id`, StatusPending)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Article
	for rows.Next() {
		var a Article
		if err := rows.Scan(&a.ID, &a.URL); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// mediaRefRe matches filenames as written into content_html by the archiver's
// media downloader (/media/<sha256>.<ext>).
var mediaRefRe = regexp.MustCompile(`/media/([A-Za-z0-9][A-Za-z0-9._-]{0,199})`)

// MediaRefs returns every media filename referenced by any stored article.
// Callers use it to garbage-collect unreferenced files from the media dir.
func (s *Store) MediaRefs() (map[string]bool, error) {
	rows, err := s.db.Query(`SELECT content_html FROM articles WHERE content_html IS NOT NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	refs := make(map[string]bool)
	for rows.Next() {
		var h string
		if err := rows.Scan(&h); err != nil {
			return nil, err
		}
		for _, m := range mediaRefRe.FindAllStringSubmatch(h, -1) {
			refs[m[1]] = true
		}
	}
	return refs, rows.Err()
}

func (s *Store) Counts() (Counts, error) {
	var c Counts
	err := s.db.QueryRow(`
		SELECT
			COALESCE(SUM(CASE WHEN read = 0 THEN 1 ELSE 0 END), 0) AS unread,
			COALESCE(SUM(CASE WHEN read = 1 THEN 1 ELSE 0 END), 0) AS archive,
			COUNT(*) AS total
		FROM articles
	`).Scan(&c.Unread, &c.Archive, &c.Total)
	return c, err
}

func listQuery(filter string, withFTS bool) string {
	readCond := ""
	switch filter {
	case FilterArchive:
		readCond = " AND a.read = 1"
	case FilterUnread:
		readCond = " AND a.read = 0"
	}
	order := "ORDER BY a.archived_at DESC"
	match := ""
	if withFTS {
		match = "JOIN articles_fts ON a.id = articles_fts.rowid WHERE articles_fts MATCH ?"
		order = "ORDER BY rank, a.archived_at DESC"
	} else {
		match = "WHERE 1=1"
	}
	return `
		SELECT a.id, a.url, a.title, a.author, a.excerpt, a.archived_at, a.read,
		       a.status, a.fetch_error, a.word_count, a.reading_time
		FROM articles a
		` + match + readCond + `
		` + order
}

// List returns articles for a filter/search combination. It deliberately does
// not select text_content/content_html: persisted word counts make them
// unnecessary here, and they dominate memory at scale.
func (s *Store) List(filter string, query string) ([]Article, error) {
	query = strings.TrimSpace(query)

	var ftsArg string
	if query != "" {
		tokens := strings.Fields(query)
		var ftsQuery strings.Builder
		for i, tok := range tokens {
			clean := strings.ReplaceAll(tok, `"`, `""`)
			if i > 0 {
				ftsQuery.WriteString(" ")
			}
			ftsQuery.WriteString(`"` + clean + `"*`)
		}
		ftsArg = ftsQuery.String()
	}

	rows, err := s.db.Query(listQuery(filter, ftsArg != ""), nullArgs(ftsArg)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Article
	for rows.Next() {
		var a Article
		var title, author, excerpt, status, fetchError sql.NullString
		if err := rows.Scan(&a.ID, &a.URL, &title, &author, &excerpt, &a.ArchivedAt, &a.Read,
			&status, &fetchError, &a.WordCount, &a.ReadingTimeMinutes); err != nil {
			return nil, err
		}
		a.Title = title.String
		a.Author = author.String
		a.Excerpt = excerpt.String
		a.Status = status.String
		a.FetchError = fetchError.String
		if a.Status == "" {
			a.Status = StatusReady // pre-migration rows
		}
		a.CalculateStats() // fallback only; stored stats win
		out = append(out, a)
	}
	return out, rows.Err()
}

// nullArgs maps "" to no variadic args so the non-FTS query gets zero params.
func nullArgs(fts string) []any {
	if fts == "" {
		return nil
	}
	return []any{fts}
}

func (s *Store) Get(id int64) (Article, error) {
	var a Article
	var title, author, excerpt, contentHTML, textContent, status, fetchError sql.NullString
	err := s.db.QueryRow(`
		SELECT id, url, title, author, excerpt, content_html, text_content,
		       archived_at, read, status, fetch_error, word_count, reading_time
		FROM articles WHERE id = ?`, id).
		Scan(&a.ID, &a.URL, &title, &author, &excerpt, &contentHTML, &textContent,
			&a.ArchivedAt, &a.Read, &status, &fetchError, &a.WordCount, &a.ReadingTimeMinutes)
	a.Title = title.String
	a.Author = author.String
	a.Excerpt = excerpt.String
	a.ContentHTML = contentHTML.String
	a.TextContent = textContent.String
	a.Status = status.String
	a.FetchError = fetchError.String
	if a.Status == "" {
		a.Status = StatusReady // pre-migration rows
	}
	a.CalculateStats() // fallback only; stored stats win
	return a, err
}

func (s *Store) SetRead(id int64, read bool) error {
	v := 0
	if read {
		v = 1
	}
	_, err := s.db.Exec("UPDATE articles SET read = ? WHERE id = ?", v, id)
	return err
}

func (s *Store) Delete(id int64) error {
	_, err := s.db.Exec("DELETE FROM articles WHERE id = ?", id)
	return err
}
