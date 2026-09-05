package transcoder

import (
	"RadioPump/internal/models"
	"RadioPump/internal/scheduler"
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// Новые подписчики не запускают encoder и не изменяют время начала текущего трека.
func TestRadioStateStartsWithAudioAndSurvivesJoining(t *testing.T) {
	repo := &memoryLibrary{}
	first := make(chan struct{})
	var calls atomic.Int32
	streamer := streamFunc(func(ctx context.Context, _ string, out chan<- []byte) error {
		calls.Add(1)
		select {
		case <-first:
		case <-ctx.Done():
			return ctx.Err()
		}
		select {
		case out <- []byte("audio"):
		case <-ctx.Done():
			return ctx.Err()
		}
		<-ctx.Done()
		return ctx.Err()
	})
	engine := NewPlaybackEngine(repo, scheduler.NewScheduler(repo), streamer)
	defer engine.Close()
	_, err := engine.NewStation("main", nil)
	if err != nil {
		t.Fatal(err)
	}
	before, err := engine.Snapshot("main")
	if err != nil || before.Current != nil {
		t.Fatalf("premature current: %+v %v", before, err)
	}
	ch, err := engine.Subscribe("main", "first")
	if err != nil {
		t.Fatal(err)
	}
	close(first)
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("no audio")
	}
	initial, _ := engine.Snapshot("main")
	if initial.Current == nil || initial.Current.StartedMS == 0 || len(initial.Queue) != 5 {
		t.Fatalf("snapshot: %+v", initial)
	}
	for i := 0; i < 20; i++ {
		_, err := engine.Subscribe("main", "new")
		if err != nil {
			t.Fatal(err)
		}
		engine.Unsubscribe("main", "new")
		current, _ := engine.Snapshot("main")
		if current.StartedAt != initial.StartedAt || calls.Load() != 1 {
			t.Fatal("joining restarted playback")
		}
	}
}

// История ограничена памятью станции и не включает ещё не звучавшие ID.
func TestRadioHistoryBoundedAndCopied(t *testing.T) {
	s := &Station{}
	for id := uint(1); id <= 8; id++ {
		s.beginTrack(&models.Track{ID: id})
		s.finishTrack()
	}
	if len(s.history) != 5 || s.history[0].ID != 8 || s.history[4].ID != 4 || s.current != nil {
		t.Fatalf("history: %+v", s.history)
	}
	if s.history[0].StartedMS == 0 || s.history[0].EndedMS == 0 {
		t.Fatal("missing timeline")
	}
	fresh := &Station{}
	if len(fresh.history) != 0 {
		t.Fatal("history leaked into new station")
	}
}
