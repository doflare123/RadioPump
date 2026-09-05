package handlers

import (
	"RadioPump/internal/models"
	"RadioPump/internal/repository"
	"RadioPump/internal/scheduler"
	"RadioPump/internal/transcoder"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

type listenerLibrary struct {
	repository.TrackRepository
	repository.SchedulerRepository
}

func (*listenerLibrary) GetMusic([]string) ([]models.Track, error) {
	return []models.Track{{ID: 1}}, nil
}
func (*listenerLibrary) GetByID(uint) (*models.Track, error) { return &models.Track{ID: 1}, nil }

// controlledStream ждёт, пока handler действительно начнёт ответ слушателю.
type controlledStream struct{ ready <-chan struct{} }

func (s controlledStream) StreamTrack(ctx context.Context, _ string, out chan<- []byte) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-s.ready:
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case out <- []byte("audio"):
	}
	<-ctx.Done()
	return ctx.Err()
}

// blockedWriter моделирует сокет, который блокирует Write до установленного deadline.
type blockedWriter struct {
	header   http.Header
	deadline time.Time
	ready    chan struct{}
	calls    int
}

func (w *blockedWriter) Header() http.Header { return w.header }
func (w *blockedWriter) WriteHeader(int)     { close(w.ready) }
func (w *blockedWriter) SetWriteDeadline(deadline time.Time) error {
	w.deadline = deadline
	w.calls++
	return nil
}
func (*blockedWriter) FlushError() error { return nil }
func (w *blockedWriter) Write([]byte) (int, error) {
	timer := time.NewTimer(time.Until(w.deadline))
	defer timer.Stop()
	<-timer.C
	return 0, os.ErrDeadlineExceeded
}

// Deadline ограничивает блокирующую запись, а defer отписывает клиента после ошибки.
func TestListenerWriteDeadlineEndsBlockedResponse(t *testing.T) {
	repo := &listenerLibrary{}
	ready := make(chan struct{})
	engine := transcoder.NewPlaybackEngine(repo, scheduler.NewScheduler(repo), controlledStream{ready})
	defer engine.Close()
	if _, err := engine.NewStation("main", nil); err != nil {
		t.Fatal(err)
	}
	handler := NewListenerHandler(engine)
	handler.writeTimeout = 20 * time.Millisecond
	router := chi.NewRouter()
	router.Get("/stream/{name}", handler.StreamStation)
	w := &blockedWriter{header: make(http.Header), ready: ready}
	done := make(chan struct{})
	go func() { router.ServeHTTP(w, httptest.NewRequest("GET", "/stream/main", nil)); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("blocked response did not stop")
	}
	if w.calls < 3 || !w.deadline.IsZero() {
		t.Fatalf("deadlines not refreshed/cleared: %+v", w)
	}
}
