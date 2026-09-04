package db

import (
	"database/sql"
	"fmt"
)

// Базовые теги создаются первой версионированной миграцией. После применения
// они становятся обычными записями: администратор может их изменить или удалить.
var defaultTags = []string{
	"rock", "punk", "pop", "metal", "electronic", "hip-hop", "jazz", "blues", "classical", "folk",
	"indie", "alternative", "ambient", "lo-fi", "dance", "disco", "funk", "reggae", "soul", "soundtrack",
}

// migration отделяет номер версии от функции изменения, чтобы следующие
// изменения схемы добавлялись новой записью без переписывания runner-а.
type migration struct {
	version int
	apply   func(*sql.Tx) error
}

var migrations = []migration{
	{version: 1, apply: seedDefaultTags},
}

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

	if _, err := db.Exec(query); err != nil {
		return err
	}

	return applyMigrations(db)
}

// applyMigrations выполняет каждую миграцию ровно один раз и фиксирует версию
// в той же транзакции, что и изменение данных. Удалённые базовые теги не появятся снова.
func applyMigrations(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		applied_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`); err != nil {
		return fmt.Errorf("создание таблицы миграций: %w", err)
	}

	for _, item := range migrations {
		applied, err := migrationApplied(db, item.version)
		if err != nil {
			return err
		}
		if applied {
			continue
		}
		if err := applyMigration(db, item); err != nil {
			return err
		}
	}
	return nil
}

// applyMigration связывает изменение данных и запись версии одной транзакцией.
func applyMigration(db *sql.DB, item migration) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("начало миграции %d: %w", item.version, err)
	}
	defer tx.Rollback()

	if err := item.apply(tx); err != nil {
		return fmt.Errorf("применение миграции %d: %w", item.version, err)
	}
	if _, err := tx.Exec(`INSERT INTO schema_migrations (version) VALUES (?)`, item.version); err != nil {
		return fmt.Errorf("фиксация миграции %d: %w", item.version, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("завершение миграции %d: %w", item.version, err)
	}
	return nil
}

// seedDefaultTags — содержимое миграции №1. INSERT OR IGNORE сохраняет теги,
// которые администратор успел создать до появления версионированных миграций.
func seedDefaultTags(tx *sql.Tx) error {
	for _, name := range defaultTags {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO tags (name) VALUES (?)`, name); err != nil {
			return fmt.Errorf("добавление базового тега %q: %w", name, err)
		}
	}
	return nil
}

// migrationApplied отделяет проверку версии от содержимого конкретной миграции.
func migrationApplied(db *sql.DB, version int) (bool, error) {
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, version).Scan(&count); err != nil {
		return false, fmt.Errorf("проверка миграции %d: %w", version, err)
	}
	return count != 0, nil
}
