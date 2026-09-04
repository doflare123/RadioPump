package main

import (
	"RadioPump/internal/config"
	store "RadioPump/internal/db"
	"RadioPump/internal/media"
	"RadioPump/internal/repository"
	schedulerpkg "RadioPump/internal/scheduler"
	"RadioPump/internal/transcoder"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Server struct {
	cfg         *config.Config
	storage     *store.Storage
	fileStorage *media.TrackFileStorage
	trackRepo   repository.TrackRepository
	tagRepo     repository.TagRepository
	scheduler   schedulerpkg.Scheduler
	playback    *transcoder.PlaybackEngine
	router      http.Handler
}

// Собирает все инфраструктурные зависимости приложения:
// конфиг, SQLite, файловое хранилище музыки и HTTP router.
func NewServer() (*Server, error) {
	cfg, err := config.NewConfig()
	if err != nil {
		return nil, fmt.Errorf("не удалось загрузить конфиг: %w", err)
	}

	if err := os.MkdirAll("./data", 0o755); err != nil {
		return nil, fmt.Errorf("не удалось создать папку data: %w", err)
	}

	db, err := sql.Open("sqlite", "./data/radio.db")
	if err != nil {
		return nil, fmt.Errorf("не удалось открыть sqlite: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("не удалось проверить соединение с sqlite: %w", err)
	}

	if err := store.EnsureSchema(db); err != nil {
		return nil, fmt.Errorf("не удалось подготовить схему БД: %w", err)
	}

	fileStorage, err := media.NewTrackFileStorage(cfg.Music.Dir, cfg.Music.MaxFileSizeBytes())
	if err != nil {
		return nil, fmt.Errorf("не удалось подготовить папку музыки: %w", err)
	}

	storage := store.NewStorage(db)

	repo := repository.NewRepository(db)
	sched := schedulerpkg.NewScheduler(repo)
	streamer := transcoder.NewEncoder("ffmpeg", cfg.Stream.Bitrate, cfg.Stream.SampleRate)
	playback := transcoder.NewPlaybackEngine(repo, sched, streamer)

	for _, wave := range cfg.Waves {
		if _, err := playback.NewStation(wave.Name, wave.Tags); err != nil {
			return nil, fmt.Errorf("не удалось создать волну %q: %w", wave.Name, err)
		}
	}

	s := &Server{
		cfg:         cfg,
		storage:     storage,
		fileStorage: fileStorage,
		trackRepo:   repo,
		tagRepo:     repo,
		scheduler:   sched,
		playback:    playback,
	}
	s.router = s.setupRouter()

	return s, nil
}

func (s *Server) Run(addr string) error {
	if s.router == nil {
		return fmt.Errorf("роутер не инициализирован")
	}

	listenAddr := strings.TrimSpace(addr)
	if listenAddr == "" {
		port := s.cfg.Server.Port
		if port == 0 {
			port = 8080
		}
		listenAddr = fmt.Sprintf(":%d", port)
	}

	srv := &http.Server{
		Addr:              listenAddr,
		Handler:           s.router,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      0,
		IdleTimeout:       60 * time.Second,
	}

	log.Printf("RadioPump слушает %s", listenAddr)
	return srv.ListenAndServe()
}
