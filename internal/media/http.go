package media

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// ServeHTTP раздаёт только готовые аудиофайлы через защищённый Root.
// Индекс каталогов и временные .upload не публикуются, ссылки наружу не читаются.
// Router предварительно убирает /music/ из URL.
func (s *TrackFileStorage) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	name := filepath.ToSlash(filepath.Clean(r.URL.Path))
	if strings.HasPrefix(strings.ToLower(name), ".radiopump-recovery/") {
		http.NotFound(w, r)
		return
	}
	if !isAllowedExtension(strings.ToLower(filepath.Ext(name))) {
		http.NotFound(w, r)
		return
	}
	root, err := os.OpenRoot(s.absDir)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer root.Close()
	file, err := root.Open(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	http.ServeContent(w, r, info.Name(), info.ModTime(), file)
}
