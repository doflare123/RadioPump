package main

import (
	"RadioPump/internal/api/services"
	"RadioPump/internal/config"
	store "RadioPump/internal/db"
	"RadioPump/internal/media"
	"RadioPump/internal/repository"
	schedulerpkg "RadioPump/internal/scheduler"
	"RadioPump/internal/transcoder"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
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
	catalog     *services.TrackService
	libraryLock *libraryLock
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
	ready := false
	defer func() {
		if !ready {
			_ = db.Close()
		}
	}()
	// Один connection делает SQLite writes предсказуемыми и сохраняет PRAGMA
	// для всех запросов; короткое ожидание блокировки ограничено пятью секундами.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec("PRAGMA busy_timeout = 5000; PRAGMA foreign_keys = ON;"); err != nil {
		return nil, err
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
	lock, err := acquireLibraryLock(fileStorage.Directory())
	if err != nil {
		return nil, err
	}
	defer func() {
		if !ready {
			_ = lock.Close()
		}
	}()

	repo := repository.NewRepository(db)
	sched := schedulerpkg.NewScheduler(repo)
	streamer := transcoder.NewEncoder("ffmpeg", cfg.Stream.Bitrate, cfg.Stream.SampleRate)
	if _, err := exec.LookPath(streamer.Path); err != nil {
		return nil, fmt.Errorf("ffmpeg недоступен: %w", err)
	}
	streamer.ValidatePath = fileStorage.ValidatePath
	playback := transcoder.NewPlaybackEngine(repo, sched, streamer)
	defer func() {
		if !ready {
			playback.Close()
		}
	}()
	if err := playback.ConfigureBuffer(streamer.Bitrate, cfg.Stream.BufferSeconds); err != nil {
		return nil, err
	}
	catalog := services.NewTrackService(repo, fileStorage, sched)
	if err := catalog.CleanupFiles(); err != nil {
		log.Printf("отложенное удаление файлов: %v", err)
	}
	tracks, err := repo.GetAll()
	if err != nil {
		return nil, err
	}
	paths, err := repo.PendingFileDeletions()
	if err != nil {
		return nil, err
	}
	for _, track := range tracks {
		paths = append(paths, track.Path)
	}
	if err := fileStorage.RecoverUploads(paths); err != nil {
		return nil, fmt.Errorf("восстановление upload: %w", err)
	}

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
		catalog:     catalog,
		libraryLock: lock,
		scheduler:   sched,
		playback:    playback,
	}
	s.router = s.setupRouter()
	ready = true

	return s, nil
}

func (s *Server) Run(addr string) error {
	return s.RunContext(context.Background(), addr)
}

// RunContext завершает HTTP, эфир и обслуживание файлов до закрытия SQLite.
// SIGINT/SIGTERM приходят из main; отмена также доступна интеграционным тестам.
func (s *Server) RunContext(ctx context.Context, addr string) error {
	if s.router == nil {
		return fmt.Errorf("роутер не инициализирован")
	}
	if s.libraryLock != nil {
		defer s.libraryLock.Close()
	}
	defer s.storage.DB.Close()
	defer s.playback.Close()

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
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      0,
		IdleTimeout:       60 * time.Second,
	}

	log.Printf("RadioPump слушает %s", listenAddr)
	maintenanceCtx, cancel := context.WithCancel(ctx)
	maintenanceDone := make(chan struct{})
	go func() {
		defer close(maintenanceDone)
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-maintenanceCtx.Done():
				return
			case <-ticker.C:
				if err := s.catalog.CleanupFiles(); err != nil {
					log.Printf("повтор удаления файлов: %v", err)
				}
			}
		}
	}()
	defer func() { cancel(); <-maintenanceDone }()
	result := make(chan error, 1)
	go func() { result <- srv.ListenAndServe() }()
	select {
	case err := <-result:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		s.playback.Close()
		shutdownCtx, stop := context.WithTimeout(context.Background(), 15*time.Second)
		defer stop()
		err := srv.Shutdown(shutdownCtx)
		if err != nil {
			_ = srv.Close()
		}
		<-result
		return err
	}
}
