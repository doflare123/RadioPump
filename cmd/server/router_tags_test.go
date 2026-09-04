package main

import (
	"RadioPump/internal/api/services"
	"RadioPump/internal/config"
	store "RadioPump/internal/db"
	"RadioPump/internal/media"
	"RadioPump/internal/models"
	"RadioPump/internal/repository"
	schedulerpkg "RadioPump/internal/scheduler"
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	_ "modernc.org/sqlite"
)

// TestTagAndTrackHTTPFlow проходит реальный router: авторизация, список seed-тегов,
// назначение существующего ID треку, каскадное удаление и защиту config-тега.
func TestTagAndTrackHTTPFlow(t *testing.T) {
	database, err := sql.Open("sqlite", "file:router-tags?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := store.EnsureSchema(database); err != nil {
		t.Fatal(err)
	}
	repo := repository.NewRepository(database)
	scheduler := schedulerpkg.NewScheduler(repo)
	files, err := media.NewTrackFileStorage(t.TempDir(), 1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		Server: config.ServerConfig{AdminName: "admin", AdminPassword: "password", JWTSecret: "test-secret"},
		Music:  config.MusicConfig{Dir: t.TempDir(), MaxFileSizeMB: 1},
		Waves:  []config.WaveConfig{{Name: "rock-wave", Tags: []string{"rock"}}},
	}
	server := &Server{cfg: cfg, fileStorage: files, trackRepo: repo, tagRepo: repo, scheduler: scheduler}
	router := server.setupRouter()
	token, _, err := services.NewAuthService("admin", "password", "test-secret").Login("admin", "password")
	if err != nil {
		t.Fatal(err)
	}

	tagsResponse := performAdminJSON(t, router, token, http.MethodGet, "/api/admin/tags", nil)
	if tagsResponse.Code != http.StatusOK {
		t.Fatalf("GET tags status = %d, body = %s", tagsResponse.Code, tagsResponse.Body.String())
	}
	var tags []models.Tag
	if err := json.Unmarshal(tagsResponse.Body.Bytes(), &tags); err != nil {
		t.Fatal(err)
	}
	rock := tagIDByName(t, tags, "rock")
	pop := tagIDByName(t, tags, "pop")
	createdTagResponse := performAdminJSON(t, router, token, http.MethodPost, "/api/admin/tags", map[string]any{"name": "Dream Pop"})
	if createdTagResponse.Code != http.StatusCreated {
		t.Fatalf("POST tag status = %d, body = %s", createdTagResponse.Code, createdTagResponse.Body.String())
	}
	var createdTag models.Tag
	if err := json.Unmarshal(createdTagResponse.Body.Bytes(), &createdTag); err != nil {
		t.Fatal(err)
	}
	renamedTagResponse := performAdminJSON(t, router, token, http.MethodPut, "/api/admin/tags/"+uintString(uint(createdTag.ID)), map[string]any{"name": "Shoegaze"})
	if renamedTagResponse.Code != http.StatusOK {
		t.Fatalf("PUT tag status = %d, body = %s", renamedTagResponse.Code, renamedTagResponse.Body.String())
	}

	trackResponse := performAdminJSON(t, router, token, http.MethodPost, "/api/admin/tracks", map[string]any{
		"title": "Tagged track", "path": "music/tagged.flac", "tag_ids": []uint{pop},
	})
	if trackResponse.Code != http.StatusCreated {
		t.Fatalf("POST track status = %d, body = %s", trackResponse.Code, trackResponse.Body.String())
	}
	var track models.Track
	if err := json.Unmarshal(trackResponse.Body.Bytes(), &track); err != nil {
		t.Fatal(err)
	}
	if len(track.Tags) != 1 || track.Tags[0].Name != "pop" {
		t.Fatalf("created track tags = %#v", track.Tags)
	}
	unknownTag := performAdminJSON(t, router, token, http.MethodPost, "/api/admin/tracks", map[string]any{
		"title": "Invalid tags", "path": "music/invalid.flac", "tag_ids": []uint{999999},
	})
	if unknownTag.Code != http.StatusBadRequest {
		t.Fatalf("POST unknown tag status = %d, body = %s", unknownTag.Code, unknownTag.Body.String())
	}

	protected := performAdminJSON(t, router, token, http.MethodDelete, "/api/admin/tags/"+uintString(rock), nil)
	if protected.Code != http.StatusConflict {
		t.Fatalf("DELETE configured tag status = %d", protected.Code)
	}
	removed := performAdminJSON(t, router, token, http.MethodDelete, "/api/admin/tags/"+uintString(pop), nil)
	if removed.Code != http.StatusNoContent {
		t.Fatalf("DELETE ordinary tag status = %d, body = %s", removed.Code, removed.Body.String())
	}
	loaded, err := repo.GetByID(track.ID)
	if err != nil || len(loaded.Tags) != 0 {
		t.Fatalf("track after DELETE tag = %#v, err = %v", loaded, err)
	}
}

// performAdminJSON создаёт запрос с реальным Bearer token и JSON body.
func performAdminJSON(t *testing.T, handler http.Handler, token, method, path string, payload any) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	if payload != nil {
		if err := json.NewEncoder(&body).Encode(payload); err != nil {
			t.Fatal(err)
		}
	}
	request := httptest.NewRequest(method, path, &body)
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

// tagIDByName извлекает ID из фактического ответа seed-миграции.
func tagIDByName(t *testing.T, tags []models.Tag, name string) uint {
	t.Helper()
	for _, tag := range tags {
		if tag.Name == name {
			return uint(tag.ID)
		}
	}
	t.Fatalf("tag %q not found", name)
	return 0
}

// uintString форматирует положительный ID без дополнительной зависимости API-теста.
func uintString(value uint) string {
	return fmt.Sprintf("%d", value)
}
