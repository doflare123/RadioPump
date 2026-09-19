package scheduler

import (
	"RadioPump/internal/models"
	"errors"
	"testing"
	"time"
)

// Карантин убирает уже опубликованные повторы, переживает CRUD, истекает
// на непустой очереди и увеличивается только после новой неудачной попытки.
func TestTrackExclusionLifecycle(t *testing.T) {
	repo := &controlledLibrary{tracks: []models.Track{{ID: 1}, {ID: 2}}}
	s := NewScheduler(repo).(*scheduler)
	now := time.Now()
	s.now = func() time.Time { return now }
	_ = s.RegisterStation("wave", nil)
	_, _ = s.NextTrackID("wave")
	s.TrackFailed("wave", 1, "broken")
	s.MarkAllDirty()
	for i := 0; i < 12; i++ {
		if id, err := s.NextTrackID("wave"); id != 2 || err != nil {
			t.Fatalf("excluded selected: %d %v", id, err)
		}
	}
	queue, _ := s.QueueSnapshot("wave")
	for _, id := range queue.Queue {
		if id == 1 {
			t.Fatal("excluded in snapshot")
		}
	}
	now = now.Add(time.Minute)
	found := false
	for i := 0; i < 12; i++ {
		id, _ := s.NextTrackID("wave")
		found = found || id == 1
	}
	if !found {
		t.Fatal("expired track did not return")
	}
	s.TrackFailed("wave", 1, "broken again")
	f := s.Failures("wave")[0]
	if f.Attempts != 2 || f.RetryAt.Sub(now) != 2*time.Minute {
		t.Fatalf("backoff: %+v", f)
	}
	for i := 0; i < 8; i++ {
		s.TrackFailed("wave", 1, "still broken")
	}
	if s.Failures("wave")[0].RetryAt.Sub(now) != 30*time.Minute {
		t.Fatal("backoff not capped")
	}
	s.TrackSucceeded("wave", 1)
	if len(s.Failures("wave")) != 0 {
		t.Fatal("success retained failure")
	}
}

// Полностью неисправная выборка отличается от пустой; удалённые записи
// больше не удерживаются диагностикой после обновления кандидатов.
func TestExcludedVersusEmptyLibrary(t *testing.T) {
	repo := &controlledLibrary{tracks: []models.Track{{ID: 1}}}
	s := NewScheduler(repo)
	_ = s.RegisterStation("wave", nil)
	s.TrackFailed("wave", 1, "broken")
	if _, err := s.NextTrackID("wave"); !errors.Is(err, ErrTracksExcluded) {
		t.Fatal(err)
	}
	repo.tracks = nil
	s.MarkAllDirty()
	if _, err := s.NextTrackID("wave"); !errors.Is(err, ErrNoTracks) {
		t.Fatal(err)
	}
	if len(s.Failures("wave")) != 0 {
		t.Fatal("deleted failure retained")
	}
}
