package transcoder

import (
	"RadioPump/internal/models"
	"RadioPump/internal/scheduler"
	"context"
	"errors"
	"reflect"
	"sync/atomic"
	"testing"
	"time"
)

// batchRadioLibrary запрещает одиночные lookup при сборке состояния волн.
type batchRadioLibrary struct {
	memoryLibrary
	calls int
	ids   []uint
	err   error
}

func (r *batchRadioLibrary) GetByID(uint) (*models.Track, error) {
	panic("single lookup in radio snapshot")
}

func (r *batchRadioLibrary) GetRadioTracks(ids []uint) (map[uint]models.Track, error) {
	r.calls++
	r.ids = append([]uint{}, ids...)
	return map[uint]models.Track{1: {ID: 1, Title: "first"}, 2: {ID: 2, Title: "second"}}, r.err
}

// fixedRadioQueues позволяет проверить порядок и повторы, включая удалённый ID.
type fixedRadioQueues struct{ scheduler.Scheduler }

func (fixedRadioQueues) QueueSnapshot(id string) (scheduler.StationSnapshot, error) {
	return scheduler.StationSnapshot{Queue: []uint{2, 1, 2, 99, 1, 77}}, nil
}

// Три волны используют один пакет уникальных ID; удалённая запись пропускается,
// но существующие повторы сохраняют позиции. SQL-ошибка не маскируется пустотой.
func TestRadioSnapshotsBatchAcrossStations(t *testing.T) {
	r := &batchRadioLibrary{}
	e := NewPlaybackEngine(r, fixedRadioQueues{}, nil)
	for _, id := range []string{"c", "b", "a"} {
		e.stations[id] = &Station{}
	}
	states, err := e.Snapshots([]string{"c", "b", "a"})
	if err != nil || r.calls != 1 || !reflect.DeepEqual(r.ids, []uint{2, 1, 99}) {
		t.Fatalf("calls=%d ids=%v err=%v", r.calls, r.ids, err)
	}
	for i, id := range []string{"c", "b", "a"} {
		queue := []uint{}
		for _, track := range states[i].Queue {
			queue = append(queue, track.ID)
		}
		if states[i].ID != id || !reflect.DeepEqual(queue, []uint{2, 1, 2, 1}) {
			t.Fatalf("state: %+v", states[i])
		}
	}
	r.err = errors.New("database unavailable")
	if _, err := e.Snapshots([]string{"a"}); !errors.Is(err, r.err) {
		t.Fatalf("error: %v", err)
	}
	if _, err := e.Snapshots([]string{"missing"}); !errors.Is(err, ErrStationNotFound) {
		t.Fatalf("missing: %v", err)
	}
	before := r.calls
	if states, err := e.Snapshots(nil); err != nil || len(states) != 0 || r.calls != before {
		t.Fatal("empty catalog queried database")
	}
}

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
