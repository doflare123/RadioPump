package main

import (
	"RadioPump/internal/config"
	store "RadioPump/internal/db"
	"RadioPump/internal/media"
	"RadioPump/internal/models"
	"RadioPump/internal/repository"
	"RadioPump/internal/scheduler"
	"RadioPump/internal/transcoder"
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// Реальный router с middleware, FFmpeg и несколькими HTTP-подключениями:
// клиент отключается/подключается, другой продолжает слушать; Close завершает stream.
func TestLiveHTTPReconnectAndShutdown(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg unavailable")
	}
	music := t.TempDir()
	path := filepath.Join(music, "tone.wav")
	if data, err := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "sine=duration=1.2", "-y", path).CombinedOutput(); err != nil {
		t.Fatalf("fixture: %v: %s", err, data)
	}
	database, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "radio.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	database.SetMaxOpenConns(1)
	if err := store.EnsureSchema(database); err != nil {
		t.Fatal(err)
	}
	repo := repository.NewRepository(database)
	track := &models.Track{Title: "Synthetic", Path: path}
	if err := repo.Create(track); err != nil {
		t.Fatal(err)
	}
	files, err := media.NewTrackFileStorage(music, 1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	sched := scheduler.NewScheduler(repo)
	encoder := transcoder.NewEncoder("ffmpeg", "128k", 44100)
	encoder.ValidatePath = files.ValidatePath
	engine := transcoder.NewPlaybackEngine(repo, sched, encoder)
	defer engine.Close()
	if err := engine.ConfigureBuffer("128k", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.NewStation("main", nil); err != nil {
		t.Fatal(err)
	}
	server := &Server{cfg: &config.Config{Server: config.ServerConfig{JWTSecret: "test"}, Music: config.MusicConfig{Dir: music}}, trackRepo: repo, tagRepo: repo, scheduler: sched, playback: engine, fileStorage: files}
	httpServer := httptest.NewServer(server.setupRouter())
	defer httpServer.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	connect := func() *http.Response {
		t.Helper()
		req, _ := http.NewRequestWithContext(ctx, "GET", httpServer.URL+"/stream/main", nil)
		response, err := httpServer.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != 200 || response.Header.Get("Content-Type") != "audio/mpeg" {
			response.Body.Close()
			t.Fatalf("stream: %d", response.StatusCode)
		}
		return response
	}
	continuous := connect()
	defer continuous.Body.Close()
	continuousDone := make(chan error, 1)
	go func() { _, err := io.Copy(io.Discard, continuous.Body); continuousDone <- err }()
	for i := 0; i < 5; i++ {
		response := connect()
		_, err := io.CopyN(io.Discard, response.Body, 4096)
		response.Body.Close()
		if err != nil {
			t.Fatalf("reconnection %d: %v", i, err)
		}
	}
	// Всплеск новых участников получает будущие байты того же encoder-а,
	// пока исходное соединение продолжает читать без EOF.
	var joins sync.WaitGroup
	errors := make(chan error, 20)
	for i := 0; i < 20; i++ {
		joins.Add(1)
		go func() {
			defer joins.Done()
			req, _ := http.NewRequestWithContext(ctx, "GET", httpServer.URL+"/stream/main", nil)
			response, err := httpServer.Client().Do(req)
			if err != nil {
				errors <- err
				return
			}
			defer response.Body.Close()
			if response.StatusCode != 200 {
				errors <- fmt.Errorf("join status %d", response.StatusCode)
				return
			}
			_, err = io.CopyN(io.Discard, response.Body, 4096)
			if err != nil {
				errors <- err
			}
		}()
	}
	joins.Wait()
	close(errors)
	for err := range errors {
		t.Error(err)
	}
	select {
	case err := <-continuousDone:
		t.Fatalf("healthy stream ended early: %v", err)
	default:
	}
	engine.Close()
	select {
	case err := <-continuousDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("HTTP stream did not close")
	}
	response, err := httpServer.Client().Get(httpServer.URL + "/stream/main")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 503 {
		t.Fatalf("closed engine: %d", response.StatusCode)
	}
}

// Контекст запуска закрывает и пустой эфир, и SQLite без ожидания backoff.
func TestRunContextClosesEmptyServer(t *testing.T) {
	database, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "radio.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := store.EnsureSchema(database); err != nil {
		t.Fatal(err)
	}
	repo := repository.NewRepository(database)
	sched := scheduler.NewScheduler(repo)
	engine := transcoder.NewPlaybackEngine(repo, sched, transcoder.NewEncoder("ffmpeg", "128k", 44100))
	defer engine.Close()
	if _, err := engine.NewStation("empty", nil); err != nil {
		t.Fatal(err)
	}
	files, err := media.NewTrackFileStorage(t.TempDir(), 1024)
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{cfg: &config.Config{}, storage: store.NewStorage(database), trackRepo: repo, tagRepo: repo, scheduler: sched, playback: engine, fileStorage: files}
	server.router = server.setupRouter()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan error, 1)
	go func() { done <- server.RunContext(ctx, "127.0.0.1:0") }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server shutdown hung")
	}
	if err := database.Ping(); err == nil {
		t.Fatal("database left open")
	}
}
