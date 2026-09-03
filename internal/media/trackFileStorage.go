package media

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

const (
	// Хранит в памяти только маленький префикс файла.
	// Этого хватает для дешевой проверки сигнатур аудио-контейнеров.
	defaultHeaderLimit = 512

	// Больше стандартного буфера io.Copy, но остается небольшим,
	// чтобы параллельные загрузки не создавали лишнее давление на память.
	copyBufferSize = 64 * 1024
)

var (
	ErrEmptyFile          = errors.New("аудиофайл пустой")
	ErrTooLarge           = errors.New("аудиофайл слишком большой")
	ErrUnsupportedFormat  = errors.New("формат аудио не поддерживается")
	ErrCorruptAudioHeader = errors.New("заголовок аудиофайла некорректный")
)

// Описывает файл, который уже прошел проверку и был записан
// в хранилище. StoredPath сохраняется в БД как путь к треку.
type SavedTrackFile struct {
	StoredPath   string
	AbsolutePath string
	OriginalName string
	BaseName     string
	Extension    string
	Format       string
	Size         int64
}

// Задает контракт файлового хранилища для сервисного слоя.
// Форки проекта могут подставить S3, сетевой диск, CDN-backed хранилище или
// более строгую проверку файлов без изменения HTTP handler и repository.
type TrackFileStore interface {
	MaxBytes() int64
	Save(src io.Reader, originalName string) (*SavedTrackFile, error)
	Remove(saved *SavedTrackFile)
}

// Стандартная реализация TrackFileStore для локального диска.
// Она отвечает только за проверку файла и выбор пути сохранения.
type TrackFileStorage struct {
	dir      string
	maxBytes int64
}

var _ TrackFileStore = (*TrackFileStorage)(nil)

// Готовит папку музыки при старте сервера, чтобы ошибки
// прав доступа были видны сразу, а не в середине пользовательской загрузки.
func NewTrackFileStorage(dir string, maxBytes int64) (*TrackFileStorage, error) {
	cleanDir := strings.TrimSpace(dir)
	if cleanDir == "" {
		cleanDir = "./music"
	}
	if maxBytes <= 0 {
		return nil, fmt.Errorf("лимит размера музыкального файла должен быть положительным")
	}
	if err := os.MkdirAll(cleanDir, 0o755); err != nil {
		return nil, err
	}
	return &TrackFileStorage{dir: cleanDir, maxBytes: maxBytes}, nil
}

// Возвращает максимальный размер полезного аудиофайла без учета
// служебных байтов multipart-запроса.
func (s *TrackFileStorage) MaxBytes() int64 {
	return s.maxBytes
}

// Потоково пишет upload во временный файл, проверяет размер и формат,
// затем переносит файл в стабильный путь music/<safe-name>.<ext>.
func (s *TrackFileStorage) Save(src io.Reader, originalName string) (*SavedTrackFile, error) {
	baseName, ext := sanitizeAudioFileName(originalName)
	if !isAllowedExtension(ext) {
		return nil, ErrUnsupportedFormat
	}

	tmp, err := os.CreateTemp(s.dir, ".upload-*")
	if err != nil {
		return nil, err
	}
	tmpPath := tmp.Name()
	removeTemp := true
	defer func() {
		if removeTemp {
			_ = os.Remove(tmpPath)
		}
	}()

	header := &limitedHeaderWriter{limit: defaultHeaderLimit}
	writer := io.MultiWriter(tmp, header)
	limitedReader := io.LimitReader(src, s.maxBytes+1)
	buf := make([]byte, copyBufferSize)

	written, copyErr := io.CopyBuffer(writer, limitedReader, buf)
	closeErr := tmp.Close()
	if copyErr != nil {
		return nil, copyErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if written == 0 {
		return nil, ErrEmptyFile
	}
	if written > s.maxBytes {
		return nil, ErrTooLarge
	}

	format, err := validateAudioHeader(header.Bytes(), written, ext)
	if err != nil {
		return nil, err
	}

	targetPath, storedPath, err := s.nextAvailablePath(baseName, ext)
	if err != nil {
		return nil, err
	}
	if err := os.Rename(tmpPath, targetPath); err != nil {
		return nil, err
	}
	removeTemp = false

	return &SavedTrackFile{
		StoredPath:   filepath.ToSlash(storedPath),
		AbsolutePath: targetPath,
		OriginalName: originalName,
		BaseName:     baseName,
		Extension:    ext,
		Format:       format,
		Size:         written,
	}, nil
}

// Удаляет сохраненный файл при откате операции. Например, если SQLite
// отказалась создать запись после успешной записи файла на диск.
func (s *TrackFileStorage) Remove(saved *SavedTrackFile) {
	if saved == nil || saved.AbsolutePath == "" {
		return
	}
	_ = os.Remove(saved.AbsolutePath)
}

// Запоминает только первые байты upload и не растет вместе
// с полным размером файла.
type limitedHeaderWriter struct {
	buf   bytes.Buffer
	limit int
}

func (w *limitedHeaderWriter) Write(p []byte) (int, error) {
	remaining := w.limit - w.buf.Len()
	if remaining > 0 {
		if len(p) < remaining {
			remaining = len(p)
		}
		_, _ = w.buf.Write(p[:remaining])
	}
	return len(p), nil
}

func (w *limitedHeaderWriter) Bytes() []byte {
	return w.buf.Bytes()
}

// Сохраняет читаемое имя файла, но не затирает существующий
// трек: при конфликте добавляет числовой суффикс.
func (s *TrackFileStorage) nextAvailablePath(baseName, ext string) (string, string, error) {
	for i := 0; i < 10_000; i++ {
		name := baseName + ext
		if i > 0 {
			name = fmt.Sprintf("%s-%d%s", baseName, i+1, ext)
		}

		targetPath := filepath.Join(s.dir, name)
		_, err := os.Stat(targetPath)
		if errors.Is(err, os.ErrNotExist) {
			return targetPath, filepath.Join(s.dir, name), nil
		}
		if err != nil {
			return "", "", err
		}
	}
	return "", "", fmt.Errorf("слишком много файлов с именем %q", baseName)
}

// Удаляет сегменты пути, управляющие символы и опасную
// пунктуацию, но оставляет читаемое имя файла.
func sanitizeAudioFileName(originalName string) (string, string) {
	name := filepath.Base(strings.TrimSpace(originalName))
	ext := strings.ToLower(filepath.Ext(name))
	base := strings.TrimSuffix(name, filepath.Ext(name))

	var out []rune
	lastDash := false
	for _, r := range base {
		allowed := unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-'
		if allowed {
			out = append(out, r)
			lastDash = false
			continue
		}
		if unicode.IsSpace(r) || r == '.' {
			if !lastDash {
				out = append(out, '-')
				lastDash = true
			}
		}
	}

	cleanBase := strings.Trim(string(out), "-_")
	if cleanBase == "" {
		cleanBase = "track"
	}
	if len([]rune(cleanBase)) > 80 {
		cleanBase = string([]rune(cleanBase)[:80])
		cleanBase = strings.Trim(cleanBase, "-_")
	}

	return cleanBase, ext
}

// isAllowedExtension перечисляет имена контейнеров, которые понимает браузерная
// форма и может транскодировать FFmpeg. Содержимое дополнительно проверяется ниже.
func isAllowedExtension(ext string) bool {
	switch ext {
	case ".mp3", ".mp2", ".mpa", ".wav", ".wave", ".flac", ".ogg", ".oga", ".opus", ".spx", ".m4a", ".m4b", ".mp4", ".alac", ".aac", ".aif", ".aiff", ".aifc", ".wma", ".ape", ".wv", ".mka", ".webm", ".mpc", ".dsf", ".dff":
		return true
	default:
		return false
	}
}

// validateAudioHeader делает дешевую проверку контейнера по magic bytes.
// Полный decode аудио здесь не выполняется: это дорого и потребовало бы
// дополнительных codec-зависимостей.
func validateAudioHeader(header []byte, size int64, ext string) (string, error) {
	if len(header) < 4 {
		return "", ErrCorruptAudioHeader
	}

	switch ext {
	case ".mp3", ".mp2", ".mpa":
		if hasID3Header(header) || hasMPEGFrameSync(header) {
			return "mp3", nil
		}
	case ".wav", ".wave":
		if len(header) >= 12 && string(header[:4]) == "RF64" && string(header[8:12]) == "WAVE" {
			return "wav", nil
		}
		if len(header) >= 12 && string(header[:4]) == "RIFF" && string(header[8:12]) == "WAVE" {
			if len(header) >= 8 {
				riffSize := int64(binary.LittleEndian.Uint32(header[4:8])) + 8
				if riffSize > size {
					return "", ErrCorruptAudioHeader
				}
			}
			return "wav", nil
		}
	case ".flac":
		if len(header) >= 4 && string(header[:4]) == "fLaC" {
			return "flac", nil
		}
	case ".ogg", ".oga", ".opus", ".spx":
		if len(header) >= 4 && string(header[:4]) == "OggS" {
			return "ogg", nil
		}
	case ".m4a", ".m4b", ".mp4", ".alac":
		if hasMP4FileType(header) {
			return "m4a", nil
		}
	case ".aac":
		if hasADTSHeader(header) || hasID3Header(header) {
			return "aac", nil
		}
	case ".aif", ".aiff", ".aifc":
		if len(header) >= 12 && string(header[:4]) == "FORM" && (string(header[8:12]) == "AIFF" || string(header[8:12]) == "AIFC") {
			return "aiff", nil
		}
	case ".wma":
		if bytes.HasPrefix(header, []byte{0x30, 0x26, 0xb2, 0x75, 0x8e, 0x66, 0xcf, 0x11, 0xa6, 0xd9, 0, 0xaa, 0, 0x62, 0xce, 0x6c}) {
			return "asf", nil
		}
	case ".ape":
		if string(header[:4]) == "MAC " {
			return "ape", nil
		}
	case ".wv":
		if string(header[:4]) == "wvpk" {
			return "wv", nil
		}
	case ".mka", ".webm":
		if bytes.Equal(header[:4], []byte{0x1a, 0x45, 0xdf, 0xa3}) {
			return "matroska", nil
		}
	case ".mpc":
		if string(header[:4]) == "MPCK" || string(header[:3]) == "MP+" {
			return "musepack", nil
		}
	case ".dsf":
		if string(header[:4]) == "DSD " {
			return "dsf", nil
		}
	case ".dff":
		if len(header) >= 16 && string(header[:4]) == "FRM8" && string(header[12:16]) == "DSD " {
			return "dsdiff", nil
		}
	}

	return "", ErrUnsupportedFormat
}

func hasID3Header(header []byte) bool {
	return len(header) >= 3 && string(header[:3]) == "ID3"
}

func hasMPEGFrameSync(header []byte) bool {
	return len(header) >= 2 && header[0] == 0xFF && header[1]&0xE0 == 0xE0
}

func hasADTSHeader(header []byte) bool {
	return len(header) >= 2 && header[0] == 0xFF && header[1]&0xF0 == 0xF0
}

func hasMP4FileType(header []byte) bool {
	// Browser-анализатор проверяет codec внутри; список совместимых MP4 brands открыт.
	return len(header) >= 12 && string(header[4:8]) == "ftyp"
}
