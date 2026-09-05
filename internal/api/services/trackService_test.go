package services

import (
	store "RadioPump/internal/db"
	"RadioPump/internal/media"
	"RadioPump/internal/models"
	"RadioPump/internal/repository"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// failingRemoval моделирует занятый файл/отказ диска без платформенных ACL.
type failingRemoval struct {
	media.TrackFileStore
	fail bool
}

func (f *failingRemoval) Remove(saved *media.SavedTrackFile) error {
	if f.fail {
		return errors.New("disk unavailable")
	}
	return f.TrackFileStore.Remove(saved)
}

// serviceFixture использует настоящую SQLite на временном диске и локальное storage.
func serviceFixture(t *testing.T) (*TrackService, repository.TrackRepository, *failingRemoval, string) {
	t.Helper()
	dir := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(dir, "radio.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	if err := store.EnsureSchema(db); err != nil {
		t.Fatal(err)
	}
	files, err := media.NewTrackFileStorage(filepath.Join(dir, "music"), 1024)
	if err != nil {
		t.Fatal(err)
	}
	failing := &failingRemoval{TrackFileStore: files}
	repo := repository.NewTrackRepository(db)
	return NewTrackService(repo, failing, nil), repo, failing, filepath.Join(dir, "music")
}

// Неудачное удаление остаётся в БД, а новый экземпляр сервиса завершает его повторно.
func TestDeletionSurvivesFailureAndServiceRestart(t *testing.T) {
	service, repo, files, music := serviceFixture(t)
	source := filepath.Join(music, "source.flac")
	if err := os.WriteFile(source, []byte("fLaCsynthetic"), 0o600); err != nil {
		t.Fatal(err)
	}
	track := &models.Track{Title: "Import", Path: source}
	if err := service.Create(track); err != nil {
		t.Fatal(err)
	}
	if track.Path == source {
		t.Fatal("import took ownership of original")
	}
	files.fail = true
	if err := service.Delete(track.ID); !errors.Is(err, ErrFileDeletionPending) {
		t.Fatalf("Delete = %v", err)
	}
	if _, err := repo.GetByID(track.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("track still indexed: %v", err)
	}
	pending, err := repo.PendingFileDeletions()
	if err != nil || len(pending) != 1 {
		t.Fatalf("pending = %v, %v", pending, err)
	}
	files.fail = false
	restarted := NewTrackService(repo, files, nil)
	if err := restarted.CleanupFiles(); err != nil {
		t.Fatal(err)
	}
	if err := restarted.CleanupFiles(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(track.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("copy still exists: %v", err)
	}
	if _, err := os.Stat(source); err != nil {
		t.Fatalf("original removed: %v", err)
	}
	pending, err = repo.PendingFileDeletions()
	if err != nil || len(pending) != 0 {
		t.Fatalf("pending = %v, %v", pending, err)
	}
}

// Старые несколько записей одного файла не дают удалить аудио оставшегося трека.
func TestDeletionPreservesSharedLegacyFile(t *testing.T) {
	service, repo, _, music := serviceFixture(t)
	path := filepath.Join(music, "shared.flac")
	if err := os.WriteFile(path, []byte("fLaCsynthetic"), 0o600); err != nil {
		t.Fatal(err)
	}
	a := &models.Track{Title: "A", Path: path}
	b := &models.Track{Title: "B", Path: filepath.ToSlash(music) + "/./shared.flac"}
	for _, track := range []*models.Track{a, b} {
		if err := repo.Create(track); err != nil {
			t.Fatal(err)
		}
	}
	if err := service.Delete(a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("shared file lost: %v", err)
	}
	if err := service.Delete(b.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("last file not removed: %v", err)
	}
}

// Даже опасная запись, созданная старой версией, не разрешает удалить внешний файл.
func TestDeletionRejectsLegacyOutsidePath(t *testing.T) {
	service, repo, _, _ := serviceFixture(t)
	path := filepath.Join(t.TempDir(), "outside.flac")
	if err := os.WriteFile(path, []byte("fLaCoutside"), 0o600); err != nil {
		t.Fatal(err)
	}
	track := &models.Track{Title: "Legacy", Path: path}
	if err := repo.Create(track); err != nil {
		t.Fatal(err)
	}
	if err := service.Delete(track.ID); !errors.Is(err, media.ErrUnsafePath) {
		t.Fatalf("Delete = %v", err)
	}
	if _, err := repo.GetByID(track.ID); err != nil {
		t.Fatal("unsafe deletion removed DB row")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("outside file removed")
	}
}

// Отказ rollback тоже получает durable retry, не оставляя файл навсегда бесхозным.
func TestFailedUploadRollbackSchedulesCleanup(t *testing.T) {
	service, repo, files, music := serviceFixture(t)
	path := filepath.Join(music, "source.flac")
	if err := os.WriteFile(path, []byte("fLaCsynthetic"), 0o600); err != nil {
		t.Fatal(err)
	}
	files.fail = true
	track := &models.Track{Title: "Bad tags", Path: path, Tags: []models.Tag{{ID: 999999}}}
	if err := service.Create(track); !errors.Is(err, repository.ErrUnknownTag) {
		t.Fatalf("Create: %v", err)
	}
	pending, err := repo.PendingFileDeletions()
	if err != nil || len(pending) != 1 {
		t.Fatalf("rollback pending: %v, %v", pending, err)
	}
	files.fail = false
	if err := service.CleanupFiles(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(track.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed import copy remains: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("original lost: %v", err)
	}
}
