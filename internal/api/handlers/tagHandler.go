package handlers

import (
	"RadioPump/internal/api/services"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
)

// TagHandler переводит HTTP-запросы в небольшой интерфейс TagCatalog.
type TagHandler struct {
	tags services.TagCatalog
}

// NewTagHandler зависит от service-интерфейса, поэтому transport можно оставить
// неизменным при замене справочника локальным plugin или другим backend.
func NewTagHandler(tags services.TagCatalog) *TagHandler {
	return &TagHandler{tags: tags}
}

// tagPayload одинаков для создания и переименования справочной записи.
type tagPayload struct {
	Name string `json:"name"`
}

// ListTags возвращает полный допустимый справочник для checkbox-списков админки.
func (h *TagHandler) ListTags(w http.ResponseWriter, _ *http.Request) {
	tags, err := h.tags.GetAll()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "не удалось загрузить теги")
		return
	}
	writeJSON(w, http.StatusOK, tags)
}

// CreateTag принимает только имя; ID назначает repository.
func (h *TagHandler) CreateTag(w http.ResponseWriter, r *http.Request) {
	var payload tagPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "некорректный json")
		return
	}
	tag, err := h.tags.Create(payload.Name)
	if writeTagError(w, err) {
		return
	}
	writeJSON(w, http.StatusCreated, tag)
}

// UpdateTag сохраняет ID, чтобы связи треков не приходилось переписывать.
func (h *TagHandler) UpdateTag(w http.ResponseWriter, r *http.Request) {
	id, err := parseUintParam(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "некорректный id тега")
		return
	}
	var payload tagPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "некорректный json")
		return
	}
	tag, err := h.tags.Update(id, payload.Name)
	if writeTagError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, tag)
}

// DeleteTag удаляет связи треков транзакционно; ограничения волн проверяет service.
func (h *TagHandler) DeleteTag(w http.ResponseWriter, r *http.Request) {
	id, err := parseUintParam(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "некорректный id тега")
		return
	}
	if writeTagError(w, h.tags.Delete(id)) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// writeTagError задаёт единый HTTP-контракт ошибок CRUD справочника.
func writeTagError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, services.ErrInvalidTagName):
		writeError(w, http.StatusBadRequest, "имя тега должно содержать от 1 до 64 символов")
	case errors.Is(err, services.ErrTagExists):
		writeError(w, http.StatusConflict, "тег с таким именем уже существует")
	case errors.Is(err, services.ErrTagInUse):
		writeError(w, http.StatusConflict, "тег используется в config.yaml одной из волн")
	case errors.Is(err, sql.ErrNoRows):
		writeError(w, http.StatusNotFound, "тег не найден")
	default:
		writeError(w, http.StatusInternalServerError, "не удалось изменить тег")
	}
	return true
}
