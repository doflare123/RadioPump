package services

import (
	"RadioPump/internal/transcoder"
	"errors"
	"sync"
	"testing"
	"time"
)

// countingRadioReader считает общие обновления и имитирует смену состояния/сбой.
type countingRadioReader struct {
	calls int
	err   error
}

func (r *countingRadioReader) Snapshots(ids []string) ([]transcoder.RadioSnapshot, error) {
	r.calls++
	result := []transcoder.RadioSnapshot{}
	for _, id := range ids {
		result = append(result, transcoder.RadioSnapshot{ID: id, Tags: []string{"rock"},
			Current: &transcoder.RadioTrack{ID: uint(r.calls)},
			History: []transcoder.RadioTrack{{ID: 2}}, Queue: []transcoder.RadioTrack{{ID: 3}}})
	}
	return result, r.err
}

// Сто параллельных посетителей разделяют одно обновление трёх волн. Проверяем
// границу TTL, свежие часы, независимые копии и обновление после ошибки БД.
func TestRadioStateSharedCache(t *testing.T) {
	r := &countingRadioReader{}
	s := NewRadioService(r, []string{"a", "b", "c"})
	now := time.Unix(1000, 0)
	s.now = func() time.Time { return now }
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			state, err := s.State()
			if err != nil || len(state.Stations) != 3 {
				t.Errorf("state: %+v, %v", state, err)
				return
			}
			state.Stations[0].ID = "changed"
			state.Stations[0].Tags[0] = "changed"
			state.Stations[0].Current.ID = 99
			state.Stations[0].History[0].ID = 99
			state.Stations[0].Queue[0].ID = 99
		}()
	}
	wg.Wait()
	if r.calls != 1 {
		t.Fatalf("refreshes = %d", r.calls)
	}
	now = now.Add(time.Second)
	state, _ := s.State()
	first := state.Stations[0]
	if r.calls != 1 || !state.ServerTime.Equal(now) || first.ID != "a" || first.Tags[0] != "rock" || first.Current.ID != 1 || first.History[0].ID != 2 || first.Queue[0].ID != 3 {
		t.Fatalf("cache or clock corrupted: %+v", state)
	}
	now = now.Add(time.Second)
	state, _ = s.State()
	if r.calls != 2 || state.Stations[0].Current.ID != 2 {
		t.Fatal("expired state not refreshed")
	}
	r.err = errors.New("database unavailable")
	now = now.Add(2 * time.Second)
	for i := 0; i < 100; i++ {
		if _, err := s.State(); !errors.Is(err, r.err) {
			t.Fatalf("error = %v", err)
		}
	}
	if r.calls != 3 {
		t.Fatalf("failure caused %d refreshes", r.calls)
	}
	r.err = nil
	now = now.Add(2 * time.Second)
	if _, err := s.State(); err != nil || r.calls != 4 {
		t.Fatal("cache did not recover")
	}
}
