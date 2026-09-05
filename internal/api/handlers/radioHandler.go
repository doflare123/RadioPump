package handlers

import (
	"RadioPump/internal/api/services"
	"net/http"
)

type RadioCatalog interface {
	State() (services.RadioState, error)
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
