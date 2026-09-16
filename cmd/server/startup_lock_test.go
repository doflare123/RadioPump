package main

import (
	"bytes"
	"database/sql"
	"os"
	"strings"
	"testing"
)

// Готовит изолированную библиотеку без пользовательских файлов и рабочих секретов.
func prepareStartupLockTest(t *testing.T) {
	t.Helper()
	t.Chdir(t.TempDir())
	const cfg = `server:
  admin_name: test
  admin_password: startup-test-password
  jwt_secret: startup-test-secret-0123456789abcdef
music:
  dir: ./music
`
	if err := os.WriteFile("config.yaml", []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{"music", "data"} {
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

// Занятая библиотека не допускает ни создания БД, ни миграции прежней схемы.
func TestNewServerRejectsLockedLibraryBeforeDatabaseChanges(t *testing.T) {
	for _, existing := range []bool{false, true} {
		name := "missing database"
		if existing {
			name = "old schema"
		}
		t.Run(name, func(t *testing.T) {
			prepareStartupLockTest(t)
			var before []byte
			if existing {
				db, err := sql.Open("sqlite", "data/radio.db")
				if err != nil {
					t.Fatal(err)
				}
				_, err = db.Exec("CREATE TABLE legacy_marker (value TEXT); INSERT INTO legacy_marker VALUES ('preserve')")
				closeErr := db.Close()
				if err != nil || closeErr != nil {
					t.Fatalf("prepare database: %v, close: %v", err, closeErr)
				}
				before, err = os.ReadFile("data/radio.db")
				if err != nil {
					t.Fatal(err)
				}
			}
			lock, err := acquireLibraryLock("music")
			if err != nil {
				t.Fatal(err)
			}
			defer lock.Close()
			if _, err := NewServer(); err == nil || !strings.Contains(err.Error(), "библиотека уже занята") {
				t.Fatalf("expected library lock error, got %v", err)
			}
			after, err := os.ReadFile("data/radio.db")
			if existing {
				if err != nil || !bytes.Equal(before, after) {
					t.Fatalf("database changed before lock acquisition: %v", err)
				}
			} else if !os.IsNotExist(err) {
				t.Fatalf("database created before lock acquisition: %v", err)
			}
		})
	}
}

// Ошибка схемы после получения блокировки не мешает следующей попытке запуска.
func TestNewServerReleasesLibraryLockOnSchemaFailure(t *testing.T) {
	prepareStartupLockTest(t)
	db, err := sql.Open("sqlite", "data/radio.db")
	if err != nil {
		t.Fatal(err)
	}
	// Старая несовместимая таблица вызывает ошибку создания индекса по path.
	_, err = db.Exec("CREATE TABLE tracks (id INTEGER PRIMARY KEY)")
	closeErr := db.Close()
	if err != nil || closeErr != nil {
		t.Fatalf("prepare database: %v, close: %v", err, closeErr)
	}
	if _, err := NewServer(); err == nil || !strings.Contains(err.Error(), "подготовить схему БД") {
		t.Fatalf("expected schema error, got %v", err)
	}
	lock, err := acquireLibraryLock("music")
	if err != nil {
		t.Fatalf("startup failure retained library lock: %v", err)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
}
