package handlers

import (
	"RadioPump/internal/api/services"
	"RadioPump/internal/media"
	"RadioPump/internal/models"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
)

const (
	// Покрывает границы multipart и маленькие текстовые поля.
	// Сам файл дополнительно ограничен внутри media.TrackFileStorage.
	multipartOverheadBytes = 1 << 20

	// Защищает handler от больших текстовых form fields.
	maxTextFieldBytes = 8 << 10
)

type TrackHandler struct {
	trackService *services.TrackService
}

func NewTrackHandler(trackService *services.TrackService) *TrackHandler {
	return &TrackHandler{trackService: trackService}
}

// trackPayload нужен для JSON-режима. Основной путь создания трека — multipart
// upload, а JSON оставлен для ручного импорта файла, который уже лежит на сервере.
type trackPayload struct {
	Title    string `json:"title"`
	Artist   string `json:"artist"`
	Album    string `json:"album"`
	Path     string `json:"path"`
	Duration int    `json:"duration"`
}

func (h *TrackHandler) ListTracks(w http.ResponseWriter, _ *http.Request) {
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
	contentType := strings.ToLower(r.Header.Get("Content-Type"))
	if strings.HasPrefix(contentType, "multipart/form-data") {
		h.createTrackFromMultipart(w, r)
		return
	}

	h.createTrackFromJSON(w, r)
}

// createTrackFromMultipart реализует основной upload-путь: файл пишется
// потоково, проходит лимит размера и дешевую проверку аудио-заголовка.
func (h *TrackHandler) createTrackFromMultipart(w http.ResponseWriter, r *http.Request) {
	maxBodyBytes := h.trackService.MaxUploadBytes() + multipartOverheadBytes
	if maxBodyBytes > multipartOverheadBytes {
		r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	}

	reader, err := r.MultipartReader()
	if err != nil {
		writeError(w, http.StatusBadRequest, "некорректный multipart-запрос")
		return
	}

	var meta services.TrackMetadata
	var saved *media.SavedTrackFile

	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			h.trackService.DiscardUploadedFile(saved)
			writeUploadError(w, err)
			return
		}

		if part.FileName() != "" {
			if part.FormName() != "file" {
				_ = part.Close()
				h.trackService.DiscardUploadedFile(saved)
				writeError(w, http.StatusBadRequest, "файл нужно передавать в поле file")
				return
			}
			if saved != nil {
				_ = part.Close()
				h.trackService.DiscardUploadedFile(saved)
				writeError(w, http.StatusBadRequest, "за один запрос можно загрузить только один трек")
				return
			}

			saved, err = h.trackService.StoreUploadedFile(part, part.FileName())
			_ = part.Close()
			if err != nil {
				writeUploadError(w, err)
				return
			}
			continue
		}

		if err := readTrackTextField(part, &meta); err != nil {
			_ = part.Close()
			h.trackService.DiscardUploadedFile(saved)
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		_ = part.Close()
	}

	if saved == nil {
		writeError(w, http.StatusBadRequest, "файл трека обязателен")
		return
	}

	track, err := h.trackService.CreateUploadedTrack(meta, saved)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "не удалось создать трек")
		return
	}

	writeJSON(w, http.StatusCreated, track)
}

// createTrackFromJSON нужен для ручного импорта записей, когда файл уже лежит
// на сервере. Обычная админская загрузка должна использовать multipart.
func (h *TrackHandler) createTrackFromJSON(w http.ResponseWriter, r *http.Request) {
	var payload trackPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "некорректный json")
		return
	}

	if payload.Title == "" {
		writeError(w, http.StatusBadRequest, "поле title обязательно")
		return
	}
	if payload.Path == "" {
		writeError(w, http.StatusBadRequest, "поле path обязательно для JSON-импорта")
		return
	}

	track := &models.Track{
		Title:    payload.Title,
		Artist:   payload.Artist,
		Album:    payload.Album,
		Path:     payload.Path,
		Duration: uint(payload.Duration),
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
		ID:       uint(id),
		Title:    payload.Title,
		Artist:   payload.Artist,
		Album:    payload.Album,
		Duration: uint(payload.Duration),
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

	updated, err := h.trackService.GetByID(id)
	if err != nil {
		writeJSON(w, http.StatusOK, track)
		return
	}
	writeJSON(w, http.StatusOK, updated)
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

func readTrackTextField(part multipartPart, meta *services.TrackMetadata) error {
	value, err := readSmallText(part)
	if err != nil {
		return err
	}

	switch part.FormName() {
	case "title":
		meta.Title = value
	case "artist":
		meta.Artist = value
	case "album":
		meta.Album = value
	case "duration":
		if value == "" {
			return nil
		}
		duration, err := strconv.Atoi(value)
		if err != nil || duration < 0 {
			return errors.New("поле duration должно быть неотрицательным числом")
		}
		meta.Duration = duration
	}

	return nil
}

// multipartPart описывает минимальный набор методов, который нужен helper-у.
type multipartPart interface {
	FormName() string
	io.Reader
}

func readSmallText(r io.Reader) (string, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxTextFieldBytes+1))
	if err != nil {
		return "", err
	}
	if len(data) > maxTextFieldBytes {
		return "", errors.New("текстовое поле слишком большое")
	}
	return strings.TrimSpace(string(data)), nil
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

func writeUploadError(w http.ResponseWriter, err error) {
	var maxBytesErr *http.MaxBytesError
	switch {
	case errors.As(err, &maxBytesErr):
		writeError(w, http.StatusRequestEntityTooLarge, "файл трека превышает лимит")
	case errors.Is(err, media.ErrTooLarge):
		writeError(w, http.StatusRequestEntityTooLarge, "файл трека превышает лимит")
	case errors.Is(err, media.ErrUnsupportedFormat):
		writeError(w, http.StatusUnsupportedMediaType, "формат аудио не поддерживается")
	case errors.Is(err, media.ErrEmptyFile), errors.Is(err, media.ErrCorruptAudioHeader):
		writeError(w, http.StatusBadRequest, "файл трека пустой или поврежден")
	default:
		writeError(w, http.StatusInternalServerError, "не удалось сохранить файл трека")
	}
}
