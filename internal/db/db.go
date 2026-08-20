package db

import (
	"database/sql"
	"time"

	_ "modernc.org/sqlite"
)

type Article struct {
	ID          int64
	URL         string
	Title       string
	Author      string
	Excerpt     string
	ContentHTML string
	TextContent string
	ArchivedAt  time.Time
	Read        bool
}

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	d, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	// better concurrency for SQLite
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
	`
	if _, err := d.Exec(schema); err != nil {
		return nil, err
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

func (s *Store) List() ([]Article, error) {
	rows, err := s.db.Query(`SELECT id, url, title, author, excerpt, archived_at, read FROM articles ORDER BY archived_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Article
	for rows.Next() {
		var a Article
		var title, author, excerpt sql.NullString
		if err := rows.Scan(&a.ID, &a.URL, &title, &author, &excerpt, &a.ArchivedAt, &a.Read); err != nil {
			return nil, err
		}
		a.Title = title.String
		a.Author = author.String
		a.Excerpt = excerpt.String
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