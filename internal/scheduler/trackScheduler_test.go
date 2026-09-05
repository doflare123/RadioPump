package scheduler

import (
	"RadioPump/internal/models"
	"RadioPump/internal/repository"
	"errors"
	"sync"
	"testing"
	"time"
)

// controlledLibrary возвращает зафиксированный снимок и позволяет изменить
// библиотеку в точности между SQL-read и применением результата scheduler-ом.
type controlledLibrary struct {
	repository.SchedulerRepository
	mu      sync.Mutex
	tracks  []models.Track
	entered chan struct{}
	release chan struct{}
	block   bool
}

func (r *controlledLibrary) GetMusic(_ []string) ([]models.Track, error) {
	r.mu.Lock()
	tracks := append([]models.Track(nil), r.tracks...)
	block := r.block
	r.block = false
	r.mu.Unlock()
	if block {
		close(r.entered)
		<-r.release
	}
	return tracks, nil
}

// Уведомление, пришедшее во время чтения, не может исчезнуть вместе со старым snapshot.
func TestInvalidationDuringRefillDiscardsStaleResult(t *testing.T) {
	repo := &controlledLibrary{tracks: []models.Track{{ID: 1}}, entered: make(chan struct{}), release: make(chan struct{}), block: true}
	s := NewScheduler(repo)
	if err := s.RegisterStation("main", nil); err != nil {
		t.Fatal(err)
	}
	result := make(chan uint, 1)
	go func() {
		id, err := s.NextTrackID("main")
		if err != nil {
			t.Error(err)
		}
		result <- id
	}()
	select {
	case <-repo.entered:
	case <-time.After(time.Second):
		t.Fatal("refill did not start")
	}
	repo.mu.Lock()
	repo.tracks = []models.Track{{ID: 2}}
	repo.mu.Unlock()
	s.MarkAllDirty()
	close(repo.release)
	select {
	case id := <-result:
		if id != 2 {
			t.Fatalf("stale track selected: %d", id)
		}
	case <-time.After(time.Second):
		t.Fatal("refill hung")
	}
}

// Пустая выборка очищает старую очередь, а новый трек становится доступен после mark.
func TestEmptyQueueAndRepopulation(t *testing.T) {
	repo := &controlledLibrary{tracks: []models.Track{{ID: 1}, {ID: 2}}}
	s := NewScheduler(repo)
	_ = s.RegisterStation("main", nil)
	if _, err := s.NextTrackID("main"); err != nil {
		t.Fatal(err)
	}
	repo.tracks = nil
	s.MarkAllDirty()
	if _, err := s.NextTrackID("main"); !errors.Is(err, ErrNoTracks) {
		t.Fatalf("empty = %v", err)
	}
	snapshot, _ := s.QueueSnapshot("main")
	if snapshot.CurrentID != 0 || len(snapshot.Queue) != 0 {
		t.Fatalf("stale state: %+v", snapshot)
	}
	repo.tracks = []models.Track{{ID: 3}}
	s.MarkAllDirty()
	if id, err := s.NextTrackID("main"); err != nil || id != 3 {
		t.Fatalf("repopulated = %d, %v", id, err)
	}
}

// Одновременный выбор не выдаёт один и тот же элемент очереди дважды за цикл.
func TestConcurrentSelectionConsumesQueueOnce(t *testing.T) {
	repo := &controlledLibrary{}
	const count = 30
	for id := 1; id <= count; id++ {
		repo.tracks = append(repo.tracks, models.Track{ID: uint(id)})
	}
	s := NewScheduler(repo)
	_ = s.RegisterStation("main", nil)
	ids := make(chan uint, count)
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id, err := s.NextTrackID("main")
			if err != nil {
				t.Error(err)
			}
			ids <- id
		}()
	}
	wg.Wait()
	close(ids)
	seen := make(map[uint]bool)
	for id := range ids {
		if seen[id] {
			t.Fatalf("repeated %d", id)
		}
		seen[id] = true
	}
}

// Опубликованные позиции должны реально прозвучать, в том числе на границе цикла.
func TestFiveUpcomingRemainStableAcrossCycles(t *testing.T) {
	for _, count := range []int{1, 2, 9} {
		repo := &controlledLibrary{}
		for i := 1; i <= count; i++ {
			repo.tracks = append(repo.tracks, models.Track{ID: uint(i)})
		}
		s := NewScheduler(repo)
		_ = s.RegisterStation("main", nil)
		_, _ = s.NextTrackID("main")
		for cycle := 0; cycle < 4; cycle++ {
			snap, _ := s.QueueSnapshot("main")
			if len(snap.Queue) < 5 {
				t.Fatalf("short preview: %v", snap.Queue)
			}
			for _, expected := range snap.Queue[:5] {
				actual, err := s.NextTrackID("main")
				if err != nil || actual != expected {
					t.Fatalf("expected %d got %d: %v", expected, actual, err)
				}
			}
		}
	}
}
