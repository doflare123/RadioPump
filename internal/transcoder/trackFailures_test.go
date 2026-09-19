package transcoder

import (
	"RadioPump/internal/models"
	"RadioPump/internal/scheduler"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// mixedLibrary чередует повреждённый и исправный синтетические источники.
type mixedLibrary struct{ memoryLibrary }

func (*mixedLibrary) GetMusic([]string) ([]models.Track, error) {
	return []models.Track{{ID: 1}, {ID: 2}}, nil
}
func (*mixedLibrary) GetByID(id uint) (*models.Track, error) {
	path := "good"
	if id == 1 {
		path = "bad"
	}
	return &models.Track{ID: id, Path: path}, nil
}

// Ошибка даже после аудиочанка исключает трек; исправный продолжает звучать
// без секундного backoff. Публичный JSON не содержит приватную причину.
func TestWorkerSkipsBrokenTrackAndKeepsHealthy(t *testing.T) {
	repo := &mixedLibrary{}
	var bad, good atomic.Int32
	sched := scheduler.NewScheduler(repo)
	engine := NewPlaybackEngine(repo, sched, streamFunc(func(ctx context.Context, path string, out chan<- []byte) error {
		out <- []byte("audio")
		if path == "bad" {
			bad.Add(1)
			return &TrackError{Err: errors.New("private/path corrupt")}
		}
		good.Add(1)
		if !waitRetry(ctx, 10*time.Millisecond) {
			return ctx.Err()
		}
		return nil
	}))
	defer engine.Close()
	_, _ = engine.NewStation("wave", nil)
	deadline := time.Now().Add(900 * time.Millisecond)
	for time.Now().Before(deadline) && (bad.Load() == 0 || good.Load() < 8) {
		time.Sleep(time.Millisecond)
	}
	if bad.Load() != 1 || good.Load() < 8 {
		t.Fatalf("bad=%d good=%d", bad.Load(), good.Load())
	}
	diagnostics := engine.Diagnostics([]string{"wave"})
	if len(diagnostics[0].Failures) != 1 {
		t.Fatalf("diagnostics: %+v", diagnostics)
	}
	state, err := engine.Snapshot("wave")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(state)
	if strings.Contains(string(data), "private/path") {
		t.Fatal("private diagnostic leaked")
	}
}

// Настоящий FFmpeg отвергает повреждённый WAV как ошибку трека, а отсутствие
// исполняемого файла остаётся системной ошибкой и не ведёт к карантину музыки.
func TestEncoderClassifiesSourceAndStartupErrors(t *testing.T) {
	path := audioFixture(t, "wav", "0.1")
	if err := os.WriteFile(path, []byte("RIFFbroken WAVE data"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, binary := range []string{"ffmpeg", filepath.Join(t.TempDir(), "missing-ffmpeg")} {
		encoder := NewEncoder(binary, "128k", 44100)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := encoder.StreamTrack(ctx, path, make(chan []byte, 32))
		cancel()
		var source *TrackError
		if err == nil || errors.As(err, &source) != (binary == "ffmpeg") {
			t.Fatalf("%s: %v", binary, err)
		}
	}
}

// Состояния пустой станции, полного карантина и системного сбоя различимы;
// остановка отменяет ожидание, не создавая новую ошибку трека.
func TestWorkerFailureStates(t *testing.T) {
	for _, tc := range []struct {
		name, status string
		empty        bool
		err          error
		failures     int
	}{
		{"empty", "empty", true, nil, 0},
		{"broken", "decode_error", false, &TrackError{Err: errors.New("broken")}, 1},
		{"system", "error", false, errors.New("encoder unavailable"), 0},
		{"silent", "decode_error", false, nil, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &memoryLibrary{empty: tc.empty}
			var calls atomic.Int32
			sched := scheduler.NewScheduler(repo)
			engine := NewPlaybackEngine(repo, sched, streamFunc(func(context.Context, string, chan<- []byte) error { calls.Add(1); return tc.err }))
			defer engine.Close()
			_, _ = engine.NewStation("wave", nil)
			deadline := time.Now().Add(time.Second)
			for {
				d := engine.Diagnostics([]string{"wave"})[0]
				if d.Status == tc.status && len(d.Failures) == tc.failures {
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("unexpected state: %+v", d)
				}
				time.Sleep(time.Millisecond)
			}
			if tc.failures > 0 {
				// Повторный проход worker-а попадает в ожидание, не запускает encoder.
				time.Sleep(30 * time.Millisecond)
				if calls.Load() != 1 {
					t.Fatal("broken file retried immediately")
				}
			}
			engine.Close()
			if engine.Diagnostics([]string{"wave"})[0].Status != "stopped" {
				t.Fatal("station did not stop")
			}
		})
	}
}
