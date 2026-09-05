package main

import (
	"RadioPump/internal/api/services"
	"RadioPump/internal/config"
	store "RadioPump/internal/db"
	"RadioPump/internal/media"
	"RadioPump/internal/models"
	"RadioPump/internal/repository"
	"RadioPump/internal/scheduler"
	"RadioPump/internal/transcoder"
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// radioFixture изолирует БД/аудио теста от библиотеки владельца. Тот же настоящий
// router используется для HTTP-регрессий и опциональной визуальной проверки.
func radioFixture(t *testing.T) (*Server, []byte, []byte) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg unavailable")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "tone.wav")
	if out, err := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "sine=frequency=220:duration=8", "-filter:a", "volume=0.02", path).CombinedOutput(); err != nil {
		t.Fatalf("fixture: %v %s", err, out)
	}
	wave, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	picture := image.NewRGBA(image.Rect(0, 0, 128, 128))
	for y := 0; y < 128; y++ {
		for x := 0; x < 128; x++ {
			picture.Set(x, y, color.RGBA{uint8(40 + x), uint8(80 + y), 180, 255})
		}
	}
	var cover bytes.Buffer
	if err := png.Encode(&cover, picture); err != nil {
		t.Fatal(err)
	}
	database, err := sql.Open("sqlite", filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	database.SetMaxOpenConns(1)
	if err := store.EnsureSchema(database); err != nil {
		t.Fatal(err)
	}
	repo := repository.NewRepository(database)
	files, err := media.NewTrackFileStorage(dir, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	sched := scheduler.NewScheduler(repo)
	encoder := transcoder.NewEncoder("ffmpeg", "128k", 44100)
	encoder.ValidatePath = files.ValidatePath
	engine := transcoder.NewPlaybackEngine(repo, sched, encoder)
	t.Cleanup(engine.Close)
	cfg := &config.Config{Server: config.ServerConfig{AdminName: "test", AdminPassword: "test", JWTSecret: "radio-test-secret"}, Waves: []config.WaveConfig{{Name: "Main"}, {Name: "Chill", Tags: []string{"ambient"}}}}
	server := &Server{cfg: cfg, trackRepo: repo, tagRepo: repo, fileStorage: files, scheduler: sched, playback: engine}
	return server, wave, cover.Bytes()
}

// Настоящий multipart проверяет сохранение обложки, безопасный публичный URL,
// откат при неизвестном теге/неисправной картинке и удаление вместе с треком.
func TestRadioUploadCoverAndPublicSnapshot(t *testing.T) {
	server, wave, cover := radioFixture(t)
	router := server.setupRouter()
	token, _, _ := services.NewAuthService("test", "test", "radio-test-secret").Login("test", "test")
	upload := func(imageData []byte, badTag bool) *httptest.ResponseRecorder {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		file, _ := writer.CreateFormFile("file", "tone.wav")
		file.Write(wave)
		part, _ := writer.CreateFormFile("cover", "cover.png")
		part.Write(imageData)
		writer.WriteField("title", "Synthetic radio")
		writer.WriteField("album", "Test album")
		writer.WriteField("duration", "8")
		if badTag {
			writer.WriteField("tag_ids", "9999999")
		}
		writer.Close()
		req := httptest.NewRequest("POST", "/api/admin/tracks", &body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		req.Header.Set("Authorization", "Bearer "+token)
		result := httptest.NewRecorder()
		router.ServeHTTP(result, req)
		return result
	}
	for _, invalid := range [][]byte{[]byte("<svg onload='alert(1)'/>"), cover[:20], make([]byte, media.MaxCoverBytes+1)} {
		result := upload(invalid, false)
		if result.Code != 400 {
			t.Fatalf("invalid cover accepted: %d %s", result.Code, result.Body)
		}
	}
	failed := upload(cover, true)
	if failed.Code != 400 {
		t.Fatalf("bad tag: %d", failed.Code)
	}
	tracks, err := server.trackRepo.GetAll()
	if err != nil || len(tracks) != 0 {
		t.Fatalf("rollback tracks: %v %v", tracks, err)
	}
	ready := upload(cover, false)
	if ready.Code != 201 {
		t.Fatalf("upload: %d %s", ready.Code, ready.Body)
	}
	var track models.Track
	if err := json.Unmarshal(ready.Body.Bytes(), &track); err != nil {
		t.Fatal(err)
	}
	if track.CoverURL == "" {
		t.Fatal("missing cover URL")
	}
	fetch := func(path string) *httptest.ResponseRecorder {
		res := httptest.NewRecorder()
		router.ServeHTTP(res, httptest.NewRequest("GET", path, nil))
		return res
	}
	saved := fetch(track.CoverURL)
	if saved.Code != 200 || !bytes.Equal(saved.Body.Bytes(), cover) || saved.Header().Get("Content-Type") != "image/png" {
		t.Fatal("cover roundtrip failed")
	}
	for _, w := range server.cfg.Waves {
		if _, err := server.playback.NewStation(w.Name, w.Tags); err != nil {
			t.Fatal(err)
		}
	}
	deadline := time.Now().Add(3 * time.Second)
	var state services.RadioState
	for time.Now().Before(deadline) {
		res := fetch("/api/radio")
		if res.Code != 200 {
			t.Fatalf("radio %d", res.Code)
		}
		if bytes.Contains(res.Body.Bytes(), []byte("StoredPath")) || bytes.Contains(res.Body.Bytes(), []byte("\"Path\"")) {
			t.Fatal("local path disclosed")
		}
		if err := json.Unmarshal(res.Body.Bytes(), &state); err != nil {
			t.Fatal(err)
		}
		if len(state.Stations) == 2 && state.Stations[0].Current != nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(state.Stations) != 2 || state.Stations[0].Current == nil || state.Stations[0].Current.CoverURL != track.CoverURL || len(state.Stations[0].Queue) != 5 {
		t.Fatalf("state: %+v", state)
	}
	if state.Stations[1].Current != nil || len(state.Stations[1].Queue) != 0 {
		t.Fatal("empty station has fabricated tracks")
	}
	server.playback.Close()
	deleted := performAdminJSON(t, router, token, "DELETE", fmt.Sprintf("/api/admin/tracks/%d", track.ID), nil)
	if deleted.Code != 204 {
		t.Fatalf("delete: %d %s", deleted.Code, deleted.Body)
	}
	if res := fetch(track.CoverURL); res.Code != 404 {
		t.Fatalf("cover after deletion: %d", res.Code)
	}
}

// RADIOPUMP_PREVIEW=1 запускает временное радио на localhost для ручного/browser QA.
// Оно не читает config.yaml или личную БД и само завершается через 15 минут.
func TestRadioBrowserPreview(t *testing.T) {
	if os.Getenv("RADIOPUMP_PREVIEW") != "1" {
		t.Skip("opt-in browser preview")
	}
	server, _, cover := radioFixture(t)
	dir := server.fileStorage.Directory()
	for i := 1; i <= 7; i++ {
		track := &models.Track{Title: fmt.Sprintf("Синтетический трек %d", i), Artist: "RadioPump", Album: "Тестовый эфир", Duration: 8, Path: filepath.Join(dir, "tone.wav")}
		if i%2 == 0 {
			track.CoverData = cover
		}
		if err := server.trackRepo.Create(track); err != nil {
			t.Fatal(err)
		}
	}
	for _, w := range server.cfg.Waves {
		if _, err := server.playback.NewStation(w.Name, w.Tags); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(filepath.Join("..", ".."))
	listener, err := net.Listen("tcp", "127.0.0.1:18080")
	if err != nil {
		t.Fatal(err)
	}
	httpServer := &http.Server{Handler: server.setupRouter(), ReadHeaderTimeout: 5 * time.Second}
	defer httpServer.Close()
	go httpServer.Serve(listener)
	fmt.Println("Radio preview: http://127.0.0.1:18080")
	<-time.After(15 * time.Minute)
}
