package transcoder

import (
	"RadioPump/internal/models"
	"RadioPump/internal/repository"
	"RadioPump/internal/scheduler"
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// memoryLibrary — фиксированная тестовая библиотека без диска и личной музыки.
type memoryLibrary struct {
	repository.TrackRepository
	repository.SchedulerRepository
	empty bool
}

func (r *memoryLibrary) GetMusic([]string) ([]models.Track, error) {
	if r.empty {
		return nil, nil
	}
	return []models.Track{{ID: 1, Path: "test.wav"}}, nil
}
func (r *memoryLibrary) GetByID(uint) (*models.Track, error) {
	return &models.Track{ID: 1, Path: "test.wav"}, nil
}

type streamFunc func(context.Context, string, chan<- []byte) error

func (f streamFunc) StreamTrack(ctx context.Context, path string, out chan<- []byte) error {
	return f(ctx, path, out)
}

// Завершение producer-а обязательно предшествует возврату Close; параллельные
// создание волн, подписки и закрытия проверяются также под race detector.
func TestEngineConcurrentLifecycle(t *testing.T) {
	repo := &memoryLibrary{}
	streamer := streamFunc(func(ctx context.Context, _ string, out chan<- []byte) error {
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case out <- []byte("audio"):
			}
		}
	})
	engine := NewPlaybackEngine(repo, scheduler.NewScheduler(repo), streamer)
	defer engine.Close()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprint(i)
			station, err := engine.NewStation(id, nil)
			if err != nil {
				t.Error(err)
				return
			}
			if _, err := engine.Subscribe(id, "listener"); err != nil {
				t.Error(err)
			}
			engine.Unsubscribe(id, "listener")
			station.Close()
			station.Close()
		}(i)
	}
	done := make(chan struct{})
	go func() { wg.Wait(); engine.Close(); engine.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("engine failed to stop")
	}
	if _, err := engine.NewStation("late", nil); !errors.Is(err, ErrEngineClosed) {
		t.Fatalf("late station: %v", err)
	}
}

// Отстающий канал получает только непрерывный префикс и EOF; здоровый получает всё.
func TestSlowListenerDisconnectsWithoutDroppingMiddleBytes(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := &Station{ctx: ctx, cancel: cancel, subs: make(map[string]chan []byte), bufferChunks: 1}
	slow, _ := s.Subscribe("slow")
	fast, _ := s.Subscribe("fast")
	for i := byte(1); i <= 3; i++ {
		s.broadcast([]byte{i})
		if i == 1 {
			if chunk := <-slow; len(chunk) != 1 || chunk[0] != 1 {
				t.Fatalf("slow prefix %v", chunk)
			}
		}
		if chunk := <-fast; len(chunk) != 1 || chunk[0] != i {
			t.Fatalf("fast received %v", chunk)
		}
	}
	if _, open := <-slow; open {
		t.Fatal("slow subscriber not disconnected")
	}
	s.Unsubscribe("slow")
	s.Unsubscribe("fast")
}

// Пустая станция и повторный ID не должны оставлять неостанавливаемые worker-ы.
func TestEmptyStationStopsDuringRetry(t *testing.T) {
	repo := &memoryLibrary{empty: true}
	engine := NewPlaybackEngine(repo, scheduler.NewScheduler(repo), streamFunc(func(context.Context, string, chan<- []byte) error {
		t.Error("empty station started encoder")
		return nil
	}))
	defer engine.Close()
	_, err := engine.NewStation("empty", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.NewStation("empty", nil); !errors.Is(err, ErrStationExists) {
		t.Fatalf("duplicate: %v", err)
	}
	ch, err := engine.Subscribe("empty", "listener")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { engine.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Close waited for retry")
	}
	if _, open := <-ch; open {
		t.Fatal("subscriber not closed")
	}
}

// Одна ошибка decode не завершает worker: следующий выбранный трек запускается.
func TestWorkerContinuesAfterDecodeFailure(t *testing.T) {
	repo := &memoryLibrary{}
	var attempts atomic.Int32
	playing := make(chan struct{})
	streamer := streamFunc(func(ctx context.Context, _ string, _ chan<- []byte) error {
		if attempts.Add(1) == 1 {
			return errors.New("corrupt audio")
		}
		close(playing)
		<-ctx.Done()
		return ctx.Err()
	})
	engine := NewPlaybackEngine(repo, scheduler.NewScheduler(repo), streamer)
	defer engine.Close()
	if _, err := engine.NewStation("main", nil); err != nil {
		t.Fatal(err)
	}
	select {
	case <-playing:
	case <-time.After(3 * time.Second):
		t.Fatal("worker stopped after decode error")
	}
}

// buffer_seconds влияет на ограничение памяти, а не увеличивает очередь без предела.
func TestBufferConfigurationAndListenerLimit(t *testing.T) {
	repo := &memoryLibrary{empty: true}
	engine := NewPlaybackEngine(repo, scheduler.NewScheduler(repo), streamFunc(func(context.Context, string, chan<- []byte) error { return nil }))
	defer engine.Close()
	if err := engine.ConfigureBuffer("128k", 5); err != nil {
		t.Fatal(err)
	}
	s, err := engine.NewStation("main", nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < maxListenersPerStation; i++ {
		ch, err := s.Subscribe(fmt.Sprint(i))
		if err != nil {
			t.Fatal(err)
		}
		if cap(ch)*defaultChunkSize < 80000 || cap(ch)*defaultChunkSize >= 80000+defaultChunkSize {
			t.Fatal("wrong CBR buffer bound")
		}
	}
	if _, err := s.Subscribe("overflow"); !errors.Is(err, ErrListenerLimit) {
		t.Fatalf("limit: %v", err)
	}
}
