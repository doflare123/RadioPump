package repository

import (
	store "RadioPump/internal/db"
	"RadioPump/internal/models"
	"database/sql"
	"errors"
	"testing"

	_ "modernc.org/sqlite"
)

// TestTrackTagsAreTransactional проверяет загрузку связей, очистку и rollback
// всей записи трека при передаче несуществующего tag ID.
func TestTrackTagsAreTransactional(t *testing.T) {
	database := testDatabase(t, "track-tags-transaction")
	repo := NewRepository(database)
	rock := findTag(t, repo, "rock")

	track := &models.Track{Title: "Tagged", Path: "music/tagged.flac", Tags: []models.Tag{{ID: rock.ID}}}
	if err := repo.Create(track); err != nil {
		t.Fatal(err)
	}
	loaded, err := repo.GetByID(track.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Tags) != 1 || loaded.Tags[0].Name != "rock" {
		t.Fatalf("loaded tags = %#v", loaded.Tags)
	}

	loaded.Tags = []models.Tag{}
	if err := repo.Update(loaded); err != nil {
		t.Fatal(err)
	}
	cleared, err := repo.GetByID(track.ID)
	if err != nil || len(cleared.Tags) != 0 {
		t.Fatalf("cleared track = %#v, err = %v", cleared, err)
	}

	bad := &models.Track{Title: "Bad", Path: "music/bad.flac", Tags: []models.Tag{{ID: 999999}}}
	if err := repo.Create(bad); !errors.Is(err, ErrUnknownTag) {
		t.Fatalf("Create error = %v, want ErrUnknownTag", err)
	}
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM tracks WHERE path = ?`, bad.Path).Scan(&count); err != nil || count != 0 {
		t.Fatalf("rolled back rows = %d, err = %v", count, err)
	}
}

// TestGetMusicKeepsOrSemantics проверяет выбор по любому тегу и возврат полного
// списка тегов каждого найденного трека, а не только совпавшего.
func TestGetMusicKeepsOrSemantics(t *testing.T) {
	database := testDatabase(t, "track-tags-or")
	repo := NewRepository(database)
	rock := findTag(t, repo, "rock")
	electronic := findTag(t, repo, "electronic")
	pop := findTag(t, repo, "pop")

	tracks := []*models.Track{
		{Title: "Rock", Path: "rock", Tags: []models.Tag{{ID: rock.ID}, {ID: pop.ID}}},
		{Title: "Electronic", Path: "electronic", Tags: []models.Tag{{ID: electronic.ID}}},
		{Title: "Untagged", Path: "untagged", Tags: []models.Tag{}},
	}
	for _, track := range tracks {
		if err := repo.Create(track); err != nil {
			t.Fatal(err)
		}
	}

	selected, err := repo.GetMusic([]string{"rock", "electronic"})
	if err != nil {
		t.Fatal(err)
	}
	if len(selected) != 2 {
		t.Fatalf("selected tracks = %d, want 2", len(selected))
	}
	for _, track := range selected {
		if track.Title == "Rock" && len(track.Tags) != 2 {
			t.Fatalf("Rock tags = %#v, want both tags", track.Tags)
		}
	}
}

// TestDeleteTagRemovesTrackLinks подтверждает явную очистку связей даже без FK pragma.
func TestDeleteTagRemovesTrackLinks(t *testing.T) {
	database := testDatabase(t, "delete-tag-links")
	repo := NewRepository(database)
	custom := &models.Tag{Name: "custom"}
	if err := repo.CreateTag(custom); err != nil {
		t.Fatal(err)
	}
	track := &models.Track{Title: "Track", Path: "track", Tags: []models.Tag{{ID: custom.ID}}}
	if err := repo.Create(track); err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteTag(uint(custom.ID)); err != nil {
		t.Fatal(err)
	}
	loaded, err := repo.GetByID(track.ID)
	if err != nil || len(loaded.Tags) != 0 {
		t.Fatalf("track after tag delete = %#v, err = %v", loaded, err)
	}
}

// testDatabase создаёт отдельную shared-memory SQLite для каждого сценария.
func testDatabase(t *testing.T, name string) *sql.DB {
	t.Helper()
	database, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := store.EnsureSchema(database); err != nil {
		t.Fatal(err)
	}
	return database
}

// findTag возвращает seed-тег по имени и завершает тест понятной ошибкой.
func findTag(t *testing.T, repo TagRepository, name string) models.Tag {
	t.Helper()
	tags, err := repo.GetAllTags()
	if err != nil {
		t.Fatal(err)
	}
	for _, tag := range tags {
		if tag.Name == name {
			return tag
		}
	}
	t.Fatalf("tag %q not found", name)
	return models.Tag{}
}
