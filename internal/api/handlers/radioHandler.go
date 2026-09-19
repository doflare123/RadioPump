package handlers

import (
	"RadioPump/internal/api/services"
	"RadioPump/internal/transcoder"
	"net/http"
)

type RadioCatalog interface {
	State() (services.RadioState, error)
}

// RadioDiagnosticsHandler подключается только за JWT middleware администратора.
func RadioDiagnosticsHandler(catalog interface {
	Diagnostics() []transcoder.StationDiagnostics
}) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, catalog.Diagnostics())
	}
}

// Короткий snapshot-запрос не владеет аудиоподпиской. Его ошибка или повтор
// не закрывают эфир; клиент обновляет сведения без переназначения audio.src.
func RadioHandler(catalog RadioCatalog) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		state, err := catalog.State()
		w.Header().Set("Cache-Control", "no-store")
		if err != nil {
			writeError(w, 503, "состояние эфира временно недоступно")
			return
		}
		writeJSON(w, 200, state)
	}
}
