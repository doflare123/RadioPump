package repository

import (
	"RadioPump/internal/models"
	"fmt"
	"testing"
)

// Реальная SQLite проверяет несколько пакетов на одном соединении, повторы,
// удалённые ID, метаданные обложки и отсутствие приватных полей в проекции.
func TestGetRadioTracksBatches(t *testing.T) {
	db := testDatabase(t, "radio-batches")
	db.SetMaxOpenConns(1)
	r := NewRepository(db)
	ids := []uint{}
	for i := 0; i < 501; i++ {
		track := &models.Track{Title: fmt.Sprint(i), Artist: "artist", Album: "album", Duration: 123, Path: fmt.Sprintf("private/%d", i)}
		if i == 0 {
			track.CoverData = []byte("cover")
		}
		if err := r.Create(track); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, track.ID)
	}
	tracks, err := r.GetRadioTracks(append(ids, ids[0], 999999))
	if err != nil || len(tracks) != 501 {
		t.Fatalf("tracks=%d err=%v", len(tracks), err)
	}
	first := tracks[ids[0]]
	if first.Title != "0" || first.Artist != "artist" || first.Album != "album" || first.Duration != 123 || first.CoverURL != fmt.Sprintf("/api/covers/%d", ids[0]) || first.Path != "" || len(first.CoverData) != 0 {
		t.Fatalf("track: %+v", first)
	}
	if tracks[ids[500]].CoverURL != "" {
		t.Fatal("invented cover")
	}
	if err := r.Delete(ids[0]); err != nil {
		t.Fatal(err)
	}
	tracks, err = r.GetRadioTracks(ids[:2])
	if err != nil || len(tracks) != 1 {
		t.Fatalf("deleted track: %v %v", tracks, err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := r.GetRadioTracks(ids[:1]); err == nil {
		t.Fatal("database failure hidden")
	}
	if tracks, err := r.GetRadioTracks(nil); err != nil || len(tracks) != 0 {
		t.Fatal("empty request touched database")
	}
}
