package main

import (
	"RadioPump/internal/api/handlers"
	"RadioPump/internal/api/services"
	"RadioPump/internal/repository"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func (s *Server) setupRouter() http.Handler {
	r := chi.NewRouter()

	// Базовые middleware: id запроса, лог, recover и timeout.
	r.Use(middleware.RequestID)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))

	// Связываем зависимости: repository -> service -> HTTP handler.
	trackRepo := repository.NewTrackRepository(s.storage.DB)
	trackService := services.NewTrackService(trackRepo, s.fileStorage)
	trackHandler := handlers.NewTrackHandler(trackService)

	r.Get("/api/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	r.Route("/api/admin", func(r chi.Router) {
		r.Get("/tracks", trackHandler.ListTracks)
		r.Get("/tracks/{id}", trackHandler.GetTrackByID)
		r.Post("/tracks", trackHandler.CreateTrack)
		r.Put("/tracks/{id}", trackHandler.UpdateTrack)
		r.Delete("/tracks/{id}", trackHandler.DeleteTrack)
	})

	return r
}
