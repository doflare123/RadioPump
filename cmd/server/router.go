// cmd/server/router.go
package main

import (
	"RadioPump/internal/api/services"
	"RadioPump/internal/repository"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func (s *Server) setupRouter() {
	r := chi.NewRouter()
	r.Use(middleware.Logger)

	// 1. создаём репозиторий — передаём *sql.DB
	trackRepo := repository.NewTrackRepository(s.storage.DB)

	// 2. создаём сервис — передаём репозиторий
	trackService := services.NewTrackService(trackRepo)

	// 3. регистрируем маршруты
	r.Get("/api/admin/tracks", trackHandler(trackService))
}
