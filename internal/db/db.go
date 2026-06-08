package db

import "database/sql"

type Storage struct {
	DB *sql.DB
}

func NewStorage(db *sql.DB) *Storage {
	return &Storage{DB: db}
}

func EnsureSchema(db *sql.DB) error {
	const query = `
	CREATE TABLE IF NOT EXISTS tracks (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		title TEXT NOT NULL,
		artist TEXT NOT NULL DEFAULT '',
		album TEXT NOT NULL DEFAULT '',
		path TEXT NOT NULL,
		duration INTEGER NOT NULL DEFAULT 0,
		created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
	);
	
	CREATE INDEX IF NOT EXISTS idx_tracks_path ON tracks(path);
	CREATE TABLE IF NOT EXISTS tags (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL UNIQUE
	);
	
	CREATE TABLE IF NOT EXISTS track_tags (
		track_id INTEGER NOT NULL,
		tag_id INTEGER NOT NULL,
		PRIMARY KEY (track_id, tag_id),
		FOREIGN KEY (track_id) REFERENCES tracks(id) ON DELETE CASCADE,
		FOREIGN KEY (tag_id) REFERENCES tags(id) ON DELETE CASCADE
	);`

	_, err := db.Exec(query)
	return err
}
