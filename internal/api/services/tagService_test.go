package services

import (
	store "RadioPump/internal/db"
	"RadioPump/internal/models"
	"RadioPump/internal/repository"
	"database/sql"
	"errors"
	"testing"

	_ "modernc.org/sqlite"
)

type invalidatorSpy struct {
	calls int
}

// MarkAllDirty фиксирует уведомления service без зависимости от реального scheduler.
func (s *invalidatorSpy) MarkAllDirty() { s.calls++ }

// TestTagServiceRules проверяет нормализацию, уникальность, обычное удаление seed-тега
// и защиту имени, на которое ссылается текущая конфигурация волны.
func TestTagServiceRules(t *testing.T) {
	database, err := sql.Open("sqlite", "file:tag-service?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := store.EnsureSchema(database); err != nil {
		t.Fatal(err)
	}

	repo := repository.NewRepository(database)
	spy := &invalidatorSpy{}
	service := NewTagService(repo, spy, []string{"rock"})

	created, err := service.Create("  Dream   Pop  ")
	if err != nil {
		t.Fatal(err)
	}
	if created.Name != "dream pop" {
		t.Fatalf("normalized name = %q", created.Name)
	}
	if _, err := service.Create("DREAM POP"); !errors.Is(err, ErrTagExists) {
		t.Fatalf("duplicate error = %v, want ErrTagExists", err)
	}

	rock := findServiceTag(t, service, "rock")
	if _, err := service.Update(uint(rock.ID), "guitar"); !errors.Is(err, ErrTagInUse) {
		t.Fatalf("configured rename error = %v, want ErrTagInUse", err)
	}
	if err := service.Delete(uint(rock.ID)); !errors.Is(err, ErrTagInUse) {
		t.Fatalf("configured delete error = %v, want ErrTagInUse", err)
	}

	pop := findServiceTag(t, service, "pop")
	if err := service.Delete(uint(pop.ID)); err != nil {
		t.Fatal(err)
	}
	if spy.calls != 1 {
		t.Fatalf("invalidation calls = %d, want 1", spy.calls)
	}
}

// findServiceTag находит тестовый tag через публичный контракт TagCatalog.
func findServiceTag(t *testing.T, service TagCatalog, name string) models.Tag {
	t.Helper()
	tags, err := service.GetAll()
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
