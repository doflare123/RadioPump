package media

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// Параллельные upload одного имени должны сохранять собственные байты и пути.
func TestConcurrentUploadsDoNotOverwrite(t *testing.T) {
	storage, err := NewTrackFileStorage(t.TempDir(), 1024)
	if err != nil {
		t.Fatal(err)
	}
	const count = 24
	results := make(chan *SavedTrackFile, count)
	var wg sync.WaitGroup
	for index := 0; index < count; index++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			data := append([]byte("fLaC"), byte(index))
			saved, err := storage.Save(bytes.NewReader(data), "same.flac")
			if err != nil {
				t.Error(err)
				return
			}
			got, err := os.ReadFile(saved.AbsolutePath)
			if err != nil || !bytes.Equal(got, data) {
				t.Errorf("upload %d: %x, %v", index, got, err)
			}
			results <- saved
		}(index)
	}
	wg.Wait()
	close(results)
	seen := make(map[string]bool)
	for file := range results {
		if seen[file.StoredPath] {
			t.Fatal("shared upload path")
		}
		seen[file.StoredPath] = true
	}
	if len(seen) != count {
		t.Fatalf("saved %d files", len(seen))
	}
}

// Граница проверяется независимо для импорта и удаления, включая родительские ссылки.
func TestStorageRejectsOutsidePathsAndLinks(t *testing.T) {
	base := t.TempDir()
	music := filepath.Join(base, "music")
	storage, err := NewTrackFileStorage(music, 1024)
	if err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(base, "outside.flac")
	if err := os.WriteFile(outside, []byte("fLaCsafe"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{outside, filepath.Join(music, "..", "outside.flac"), music, "https://example.invalid/audio.flac"} {
		if _, err := storage.Import(path); !errors.Is(err, ErrUnsafePath) {
			t.Errorf("Import(%q) = %v", path, err)
		}
		if err := storage.Remove(&SavedTrackFile{AbsolutePath: path}); !errors.Is(err, ErrUnsafePath) {
			t.Errorf("Remove(%q) = %v", path, err)
		}
	}
	t.Run("symlinks", func(t *testing.T) {
		link := filepath.Join(music, "link.flac")
		if err := os.Symlink(outside, link); err != nil {
			t.Skipf("OS does not permit symlinks: %v", err)
		}
		parent := filepath.Join(music, "alias")
		if err := os.Symlink(base, parent); err != nil {
			t.Fatal(err)
		}
		for _, path := range []string{link, filepath.Join(parent, "outside.flac")} {
			if _, err := storage.Import(path); !errors.Is(err, ErrUnsafePath) {
				t.Errorf("Import link = %v", err)
			}
			if err := storage.Remove(&SavedTrackFile{AbsolutePath: path}); !errors.Is(err, ErrUnsafePath) {
				t.Errorf("Remove link = %v", err)
			}
		}
	})
	if got, err := os.ReadFile(outside); err != nil || string(got) != "fLaCsafe" {
		t.Fatalf("outside file changed: %q, %v", got, err)
	}
}

// Startup recovery удаляет только управляемые orphan upload и сохраняет все
// каталоги со ссылками БД, а также ручную библиотеку за пределами namespace.
func TestRecoverUploadsAfterInterruptedCommit(t *testing.T) {
	storage, err := NewTrackFileStorage(t.TempDir(), 1024)
	if err != nil {
		t.Fatal(err)
	}
	orphan, err := storage.Save(bytes.NewReader(minimalWAV()), "orphan.wav")
	if err != nil {
		t.Fatal(err)
	}
	kept, err := storage.Save(bytes.NewReader(minimalWAV()), "kept.wav")
	if err != nil {
		t.Fatal(err)
	}
	manual := filepath.Join(storage.absDir, "manual.wav")
	if err := os.WriteFile(manual, minimalWAV(), 0o600); err != nil {
		t.Fatal(err)
	}
	partialDir := filepath.Join(storage.absDir, ".radiopump", "11111111111111111111111111111111")
	if err := os.Mkdir(partialDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(partialDir, ".upload"), []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := storage.RecoverUploads([]string{kept.StoredPath}); err != nil {
			t.Fatal(err)
		}
	}
	quarantined := filepath.Join(storage.absDir, ".radiopump-recovery", filepath.Base(filepath.Dir(orphan.AbsolutePath)), filepath.Base(orphan.AbsolutePath))
	if got, err := os.ReadFile(quarantined); err != nil || !bytes.Equal(got, minimalWAV()) {
		t.Fatalf("orphan audio not preserved: %v", err)
	}
	for _, path := range []string{orphan.AbsolutePath, partialDir} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("orphan still exists: %v", err)
		}
	}
	for _, path := range []string{kept.AbsolutePath, manual} {
		if _, err := os.Stat(path); err != nil {
			t.Fatal(err)
		}
	}
}

// Готовое аудио поддерживает Range; временные файлы и каталоги не публикуются.
func TestMusicHTTPServesReadyAudioOnly(t *testing.T) {
	storage, err := NewTrackFileStorage(t.TempDir(), 1024)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := storage.Save(bytes.NewReader(minimalWAV()), "song.wav")
	if err != nil {
		t.Fatal(err)
	}
	rel, _ := filepath.Rel(storage.absDir, saved.AbsolutePath)
	handler := http.StripPrefix("/music/", storage)
	req := httptest.NewRequest("GET", "/music/"+filepath.ToSlash(rel), nil)
	req.Header.Set("Range", "bytes=0-3")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusPartialContent || w.Body.String() != "RIFF" {
		t.Fatalf("Range: %d %q", w.Code, w.Body.String())
	}
	for _, path := range []string{"/music/", "/music/.radiopump/", "/music/../outside.wav", "/music/.radiopump/x/.upload"} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != http.StatusNotFound {
			t.Fatalf("%s returned %d", path, w.Code)
		}
	}
}
