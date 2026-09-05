package media

import (
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// RecoverUploads вызывается только до запуска HTTP/worker-ов. .radiopump —
// зарезервированный namespace сервера: незавершённые .upload удаляются,
// готовое аудио без записи БД переносится в .radiopump-recovery для восстановления.
// Оно не уничтожается: причиной отсутствия записи может быть откат/утрата БД.
// Каталог с хотя бы одной ссылкой БД сохраняется целиком; рекурсивного удаления нет.
func (s *TrackFileStorage) RecoverUploads(referencedPaths []string) error {
	root, err := os.OpenRoot(s.absDir)
	if err != nil {
		return err
	}
	defer root.Close()
	keep := make(map[string]bool)
	for _, path := range referencedPaths {
		rel, err := s.relativePath(path)
		if err != nil {
			continue
		}
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if len(parts) >= 3 && strings.EqualFold(parts[0], ".radiopump") {
			keep[strings.ToLower(parts[1])] = true
		}
	}
	if info, err := root.Lstat(".radiopump"); err == nil && !info.IsDir() {
		return ErrUnsafePath
	}
	dirs, err := fs.ReadDir(root.FS(), ".radiopump")
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var failures []error
	for _, dir := range dirs {
		id, err := hex.DecodeString(dir.Name())
		if err != nil || len(id) != 16 || !dir.IsDir() || keep[strings.ToLower(dir.Name())] {
			continue
		}
		directory := ".radiopump/" + dir.Name()
		files, err := fs.ReadDir(root.FS(), directory)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		published := false
		for _, file := range files {
			if !file.Type().IsRegular() {
				failures = append(failures, fmt.Errorf("необычный файл в upload-каталоге %s", directory))
				continue
			}
			if file.Name() != ".upload" {
				published = true
				continue
			}
			if err := removeIfExists(root, directory+"/"+file.Name()); err != nil {
				failures = append(failures, err)
			}
		}
		if published {
			if err := root.MkdirAll(".radiopump-recovery", 0o755); err != nil {
				failures = append(failures, err)
				continue
			}
			info, err := root.Lstat(".radiopump-recovery")
			if err != nil || !info.IsDir() {
				failures = append(failures, ErrUnsafePath)
				continue
			}
			if err := root.Rename(directory, ".radiopump-recovery/"+dir.Name()); err != nil {
				failures = append(failures, err)
			}
			continue
		}
		if err := removeIfExists(root, directory); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}
