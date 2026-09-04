package db

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

// TestMigrationOneSeedsTagsOnce проверяет и начальный набор, и главное свойство
// миграции: удалённый администратором базовый тег не возвращается при новом запуске.
func TestMigrationOneSeedsTagsOnce(t *testing.T) {
	database, err := sql.Open("sqlite", "file:migration-tags?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	if err := EnsureSchema(database); err != nil {
		t.Fatal(err)
	}
	assertTagCount(t, database, 20)

	if _, err := database.Exec(`DELETE FROM tags WHERE name = 'rock'`); err != nil {
		t.Fatal(err)
	}
	if err := EnsureSchema(database); err != nil {
		t.Fatal(err)
	}
	assertTagCount(t, database, 19)

	var migrations int
	if err := database.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version = 1`).Scan(&migrations); err != nil {
		t.Fatal(err)
	}
	if migrations != 1 {
		t.Fatalf("migration rows = %d, want 1", migrations)
	}
}

// assertTagCount оставляет сообщения проверки понятными при изменении seed-набора.
func assertTagCount(t *testing.T, database *sql.DB, want int) {
	t.Helper()
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM tags`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != want {
		t.Fatalf("tag count = %d, want %d", count, want)
	}
}
