package main

import (
	"RadioPump/internal/api/handlers"
	adminmiddleware "RadioPump/internal/api/middleware"
	"RadioPump/internal/api/services"
	"net/http"
	"strings"
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
	r.Use(timeoutExceptStream(60 * time.Second))

	// Связываем зависимости по слоям: repository -> service -> HTTP handlers.
	trackService := services.NewTrackService(s.trackRepo, s.fileStorage)
	trackHandler := handlers.NewTrackHandler(trackService)
	listenerHandler := handlers.NewListenerHandler(s.playback)

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

	// Список треков.
	r.With(adminOnly.AdminOnly).Get("/api/tracks", trackHandler.ListTracks)
	r.With(adminOnly.AdminOnly).Get("/api/tracks/{id}", trackHandler.GetTrackByID)

	r.Get("/stream/{name}", listenerHandler.StreamStation)

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

func timeoutExceptStream(timeout time.Duration) func(http.Handler) http.Handler {
	timeoutMiddleware := middleware.Timeout(timeout)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/stream/") {
				next.ServeHTTP(w, r)
				return
			}
			timeoutMiddleware(next).ServeHTTP(w, r)
		})
	}
}
