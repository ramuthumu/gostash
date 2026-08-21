package db

import (
	"database/sql"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const (
	FilterUnread  = "unread"
	FilterArchive = "archive"
	FilterAll     = "all"
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
}

// CalculateStats computes WordCount and ReadingTimeMinutes from TextContent.
func (a *Article) CalculateStats() {
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
		read          INTEGER DEFAULT 0
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

	// Rebuild FTS index if table exists but FTS was newly added or empty
	var countArticles, countFTS int
	_ = d.QueryRow("SELECT COUNT(*) FROM articles").Scan(&countArticles)
	_ = d.QueryRow("SELECT COUNT(*) FROM articles_fts").Scan(&countFTS)
	if countArticles > 0 && countFTS == 0 {
		_, _ = d.Exec("INSERT INTO articles_fts(articles_fts) VALUES('rebuild');")
	}

	return &Store{db: d}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Save(a Article) (int64, error) {
	_, err := s.db.Exec(`
		INSERT INTO articles(url, title, author, excerpt, content_html, text_content)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(url) DO UPDATE SET
			title=excluded.title,
			author=excluded.author,
			excerpt=excluded.excerpt,
			content_html=excluded.content_html,
			text_content=excluded.text_content`,
		a.URL, a.Title, a.Author, a.Excerpt, a.ContentHTML, a.TextContent)
	if err != nil {
		return 0, err
	}
	var id int64
	err = s.db.QueryRow("SELECT id FROM articles WHERE url = ?", a.URL).Scan(&id)
	return id, err
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

func (s *Store) List(filter string, query string) ([]Article, error) {
	query = strings.TrimSpace(query)
	var q string
	var args []any

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
		formattedQuery := ftsQuery.String()

		switch filter {
		case FilterArchive:
			q = `
				SELECT a.id, a.url, a.title, a.author, a.excerpt, a.text_content, a.archived_at, a.read
				FROM articles a
				JOIN articles_fts ON a.id = articles_fts.rowid
				WHERE articles_fts MATCH ? AND a.read = 1
				ORDER BY rank, a.archived_at DESC
			`
			args = append(args, formattedQuery)
		case FilterAll:
			q = `
				SELECT a.id, a.url, a.title, a.author, a.excerpt, a.text_content, a.archived_at, a.read
				FROM articles a
				JOIN articles_fts ON a.id = articles_fts.rowid
				WHERE articles_fts MATCH ?
				ORDER BY rank, a.archived_at DESC
			`
			args = append(args, formattedQuery)
		default: // FilterUnread
			q = `
				SELECT a.id, a.url, a.title, a.author, a.excerpt, a.text_content, a.archived_at, a.read
				FROM articles a
				JOIN articles_fts ON a.id = articles_fts.rowid
				WHERE articles_fts MATCH ? AND a.read = 0
				ORDER BY rank, a.archived_at DESC
			`
			args = append(args, formattedQuery)
		}
	} else {
		switch filter {
		case FilterArchive:
			q = `SELECT id, url, title, author, excerpt, text_content, archived_at, read FROM articles WHERE read = 1 ORDER BY archived_at DESC`
		case FilterAll:
			q = `SELECT id, url, title, author, excerpt, text_content, archived_at, read FROM articles ORDER BY archived_at DESC`
		default: // FilterUnread
			q = `SELECT id, url, title, author, excerpt, text_content, archived_at, read FROM articles WHERE read = 0 ORDER BY archived_at DESC`
		}
	}

	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Article
	for rows.Next() {
		var a Article
		var title, author, excerpt, textContent sql.NullString
		if err := rows.Scan(&a.ID, &a.URL, &title, &author, &excerpt, &textContent, &a.ArchivedAt, &a.Read); err != nil {
			return nil, err
		}
		a.Title = title.String
		a.Author = author.String
		a.Excerpt = excerpt.String
		a.TextContent = textContent.String
		a.CalculateStats()
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) Get(id int64) (Article, error) {
	var a Article
	var author sql.NullString
	err := s.db.QueryRow(`SELECT id, url, title, author, excerpt, content_html, text_content, archived_at, read FROM articles WHERE id = ?`, id).
		Scan(&a.ID, &a.URL, &a.Title, &author, &a.Excerpt, &a.ContentHTML, &a.TextContent, &a.ArchivedAt, &a.Read)
	a.Author = author.String
	a.CalculateStats()
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