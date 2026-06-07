package main

import (
	"RadioPump/internal/api/handlers"
	adminmiddleware "RadioPump/internal/api/middleware"
	"RadioPump/internal/api/services"
	"RadioPump/internal/repository"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func (s *Server) setupRouter() http.Handler {
	r := chi.NewRouter()

	// Базовые middleware работают для всего приложения: API, сайта и статики.
	r.Use(middleware.RequestID)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))

	// Связываем зависимости по слоям: repository -> service -> HTTP handlers.
	trackRepo := repository.NewTrackRepository(s.storage.DB)
	trackService := services.NewTrackService(trackRepo, s.fileStorage)
	trackHandler := handlers.NewTrackHandler(trackService)

	authService := services.NewAuthService(
		s.cfg.Server.AdminName,
		s.cfg.Server.AdminPassword,
		s.cfg.Server.JWTSecret,
	)
	authHandler := handlers.NewAuthHandler(authService)
	adminOnly := adminmiddleware.NewMiddlewareAdmin(s.cfg.Server.JWTSecret)

	r.Get("/api/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	// Авторизация нужна только для админских операций с файлами и метаданными.
	r.Post("/api/auth/login", authHandler.Login)
	r.With(adminOnly.AdminOnly).Get("/api/auth/me", authHandler.Me)

	// Публичное API для слушателей: список треков можно читать без токена.
	r.Get("/api/tracks", trackHandler.ListTracks)
	r.Get("/api/tracks/{id}", trackHandler.GetTrackByID)

	// Админское API: запись файлов, изменение и удаление доступны только админу.
	r.Route("/api/admin", func(r chi.Router) {
		r.Use(adminOnly.AdminOnly)
		r.Post("/tracks", trackHandler.CreateTrack)
		r.Put("/tracks/{id}", trackHandler.UpdateTrack)
		r.Delete("/tracks/{id}", trackHandler.DeleteTrack)
	})

	// Загруженная музыка публично читается плеером, но запись в эту папку
	// остается только через защищенные админские endpoints.
	r.Handle("/music/*", http.StripPrefix("/music/", http.FileServer(http.Dir(s.cfg.Music.Dir))))

	// Статический сайт на чистом HTML/CSS/JS. Все страницы лежат в web/.
	r.Handle("/*", http.FileServer(http.Dir("./web")))

	return adminmiddleware.NewCORS(s.cfg.CORS.AllowedOrigins).Handler(r)
}
