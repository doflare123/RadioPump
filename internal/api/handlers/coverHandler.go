package handlers

import (
	"bytes"
	"database/sql"
	"errors"
	"net/http"
	"time"
)

type CoverCatalog interface{ GetCover(uint) ([]byte, error) }

// CoverHandler публикует только проверенные растровые данные по ID, без путей БД.
func CoverHandler(catalog CoverCatalog) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := parseUintParam(r, "id")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		data, err := catalog.GetCover(id)
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			writeError(w, 500, "не удалось прочитать обложку")
			return
		}
		w.Header().Set("Content-Type", http.DetectContentType(data))
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "public, max-age=3600")
		http.ServeContent(w, r, "cover", time.Time{}, bytes.NewReader(data))
	}
}
