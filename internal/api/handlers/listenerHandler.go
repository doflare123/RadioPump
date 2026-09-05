package handlers

import (
	"RadioPump/internal/transcoder"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

type ListenerHandler struct {
	playback     *transcoder.PlaybackEngine
	writeTimeout time.Duration
}

func NewListenerHandler(playback *transcoder.PlaybackEngine) *ListenerHandler {
	return &ListenerHandler{playback: playback, writeTimeout: 10 * time.Second}
}

func (h *ListenerHandler) StreamStation(w http.ResponseWriter, r *http.Request) {
	stationID := chi.URLParam(r, "name")
	listenerID, err := newListenerID()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "не удалось создать идентификатор слушателя")
		return
	}

	ch, err := h.playback.Subscribe(stationID, listenerID)
	if errors.Is(err, transcoder.ErrStationNotFound) {
		writeError(w, http.StatusNotFound, "станция не найдена")
		return
	}
	if err != nil {
		if errors.Is(err, transcoder.ErrEngineClosed) || errors.Is(err, transcoder.ErrListenerLimit) {
			w.Header().Set("Retry-After", "5")
			writeError(w, http.StatusServiceUnavailable, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "не удалось подключиться к станции")
		return
	}
	defer h.playback.Unsubscribe(stationID, listenerID)

	// ResponseController проходит Unwrap middleware и ограничивает каждую
	// Write/Flush отдельно. Общий WriteTimeout не должен обрывать здоровый live-поток.
	controller := http.NewResponseController(w)
	if err := controller.SetWriteDeadline(time.Now().Add(h.writeTimeout)); err != nil {
		writeError(w, http.StatusInternalServerError, "streaming не поддерживается")
		return
	}
	defer controller.SetWriteDeadline(time.Time{})

	w.Header().Set("Content-Type", "audio/mpeg")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	if err := controller.Flush(); err != nil {
		return
	}

	for {
		select {
		case chunk, ok := <-ch:
			if !ok {
				return
			}
			if r.Context().Err() != nil {
				return
			}
			if err := controller.SetWriteDeadline(time.Now().Add(h.writeTimeout)); err != nil {
				return
			}
			if _, err := w.Write(chunk); err != nil {
				return
			}
			if err := controller.Flush(); err != nil {
				return
			}
		case <-r.Context().Done():
			return
		}
	}
}

func newListenerID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
