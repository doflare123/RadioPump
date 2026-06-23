package handlers

import (
	"RadioPump/internal/transcoder"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
)

type ListenerHandler struct {
	playback *transcoder.PlaybackEngine
}

func NewListenerHandler(playback *transcoder.PlaybackEngine) *ListenerHandler {
	return &ListenerHandler{playback: playback}
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
		writeError(w, http.StatusInternalServerError, "не удалось подключиться к станции")
		return
	}
	defer h.playback.Unsubscribe(stationID, listenerID)

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming не поддерживается")
		return
	}

	w.Header().Set("Content-Type", "audio/mpeg")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	for {
		select {
		case chunk, ok := <-ch:
			if !ok {
				return
			}
			if _, err := w.Write(chunk); err != nil {
				return
			}
			flusher.Flush()
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
