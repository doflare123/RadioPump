package handlers

import (
	"RadioPump/internal/api/services"
	"RadioPump/internal/models"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
)

type TrackHandler struct {
	trackService *services.TrackService
}

func NewTrackHandler(trackService *services.TrackService) *TrackHandler {
	return &TrackHandler{trackService: trackService}
}

// trackPayload - DTO транспортного слоя. Он описывает, какие поля мы ожидаем в JSON при создании или обновлении трека. 
// Это позволяет нам отделить внутреннюю модель данных от внешнего API и гибко управлять форматом входящих данных.
type trackPayload struct {
	Title    string `json:"title"`
	Artist   string `json:"artist"`
	Album    string `json:"album"`
	Path     string `json:"path"`
	Duration int    `json:"duration"`
}

func (h *TrackHandler) ListTracks(w http.ResponseWriter, r *http.Request) {
	tracks, err := h.trackService.GetAllTracks()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "не удалось загрузить треки")
		return
	}

	writeJSON(w, http.StatusOK, tracks)
}

func (h *TrackHandler) GetTrackByID(w http.ResponseWriter, r *http.Request) {
	id, err := parseIntParam(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "некорректный id трека")
		return
	}

	track, err := h.trackService.GetByID(id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "трек не найден")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "не удалось загрузить трек")
		return
	}

	writeJSON(w, http.StatusOK, track)
}

func (h *TrackHandler) CreateTrack(w http.ResponseWriter, r *http.Request) {
	var payload trackPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "некорректный json")
		return
	}

	if payload.Title == "" || payload.Path == "" {
		writeError(w, http.StatusBadRequest, "поля title и path обязательны")
		return
	}

	track := &models.Track{
		Title:    payload.Title,
		Artist:   payload.Artist,
		Album:    payload.Album,
		Path:     payload.Path,
		Duration: payload.Duration,
	}

	if err := h.trackService.Create(track); err != nil {
		writeError(w, http.StatusInternalServerError, "не удалось создать трек")
		return
	}

	writeJSON(w, http.StatusCreated, track)
}

func (h *TrackHandler) UpdateTrack(w http.ResponseWriter, r *http.Request) {
	id, err := parseIntParam(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "некорректный id трека")
		return
	}

	var payload trackPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "некорректный json")
		return
	}

	track := &models.Track{
		ID:       id,
		Title:    payload.Title,
		Artist:   payload.Artist,
		Album:    payload.Album,
		Path:     payload.Path,
		Duration: payload.Duration,
	}

	err = h.trackService.Update(track)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "трек не найден")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "не удалось обновить трек")
		return
	}

	writeJSON(w, http.StatusOK, track)
}

func (h *TrackHandler) DeleteTrack(w http.ResponseWriter, r *http.Request) {
	id, err := parseIntParam(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "некорректный id трека")
		return
	}

	err = h.trackService.Delete(id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "трек не найден")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "не удалось удалить трек")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func parseIntParam(r *http.Request, key string) (int, error) {
	return strconv.Atoi(chi.URLParam(r, key))
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
