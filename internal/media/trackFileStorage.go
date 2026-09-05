package media

import (
	"bytes"
	"crypto/rand"
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
	ErrUnsafePath         = errors.New("путь должен указывать на обычный файл внутри музыкальной библиотеки")
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
	Import(path string) (*SavedTrackFile, error)
	ValidatePath(path string) error
	Remove(saved *SavedTrackFile) error
}

// Стандартная реализация TrackFileStore для локального диска.
// Она отвечает только за проверку файла и выбор пути сохранения.
type TrackFileStorage struct {
	dir      string
	absDir   string
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
	absDir, err := filepath.Abs(cleanDir)
	if err != nil {
		return nil, err
	}
	return &TrackFileStorage{dir: cleanDir, absDir: absDir, maxBytes: maxBytes}, nil
}

// Возвращает максимальный размер полезного аудиофайла без учета
// служебных байтов multipart-запроса.
func (s *TrackFileStorage) MaxBytes() int64 {
	return s.maxBytes
}

// Directory возвращает фактический корень для межпроцессного lock-файла сервера.
func (s *TrackFileStorage) Directory() string { return s.absDir }

// Save резервирует отдельный каталог через Mkdir, пишет временный файл и
// публикует его Rename только после проверки и Sync. Одинаковые имена upload
// никогда не разделяют путь. Все операции ограничены os.Root, включая симлинки.
func (s *TrackFileStorage) Save(src io.Reader, originalName string) (saved *SavedTrackFile, resultErr error) {
	baseName, ext := sanitizeAudioFileName(originalName)
	if !isAllowedExtension(ext) {
		return nil, ErrUnsupportedFormat
	}

	root, err := os.OpenRoot(s.absDir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	if err := root.MkdirAll(".radiopump", 0o755); err != nil {
		return nil, err
	}
	if info, err := root.Lstat(".radiopump"); err != nil || !info.IsDir() {
		return nil, ErrUnsafePath
	}
	directory := filepath.Join(".radiopump", fmt.Sprintf("%x", randomID()))
	if err := root.Mkdir(directory, 0o755); err != nil {
		return nil, err
	}
	tmpPath := filepath.Join(directory, ".upload")
	target := filepath.Join(directory, baseName+ext)
	published := false
	defer func() {
		if !published {
			resultErr = errors.Join(resultErr, removeIfExists(root, tmpPath), removeIfExists(root, directory))
		}
	}()
	tmp, err := root.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return nil, err
	}

	header := &limitedHeaderWriter{limit: defaultHeaderLimit}
	writer := io.MultiWriter(tmp, header)
	limitedReader := io.LimitReader(src, s.maxBytes+1)
	buf := make([]byte, copyBufferSize)

	written, copyErr := io.CopyBuffer(writer, limitedReader, buf)
	syncErr := tmp.Sync()
	closeErr := tmp.Close()
	if copyErr != nil {
		return nil, copyErr
	}
	if syncErr != nil {
		return nil, syncErr
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

	if err := root.Rename(tmpPath, target); err != nil {
		return nil, err
	}
	published = true

	return &SavedTrackFile{
		StoredPath:   filepath.ToSlash(filepath.Join(s.dir, target)),
		AbsolutePath: filepath.Join(s.absDir, target),
		OriginalName: originalName,
		BaseName:     baseName,
		Extension:    ext,
		Format:       format,
		Size:         written,
	}, nil
}

// Удаляет сохраненный файл при откате операции. Например, если SQLite
// отказалась создать запись после успешной записи файла на диск.
func (s *TrackFileStorage) Remove(saved *SavedTrackFile) error {
	if saved == nil || saved.AbsolutePath == "" {
		return nil
	}
	if err := s.ValidatePath(saved.AbsolutePath); err != nil {
		return err
	}
	rel, err := s.relativePath(saved.AbsolutePath)
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(s.absDir)
	if err != nil {
		return err
	}
	defer root.Close()
	info, err := root.Lstat(rel)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return ErrUnsafePath
	}
	return removeIfExists(root, rel)
}

// randomID использует системный источник случайности; Mkdir всё равно
// обеспечивает эксклюзивное резервирование даже при совпадении идентификатора.
func randomID() []byte {
	id := make([]byte, 16)
	_, _ = rand.Read(id)
	return id
}

// relativePath сохраняет совместимость со старыми CWD-relative путями БД.
// URL, выход наверх, корень каталога и Windows device paths не являются файлами.
func (s *TrackFileStorage) relativePath(path string) (string, error) {
	if strings.TrimSpace(path) == "" || strings.Contains(path, "://") {
		return "", ErrUnsafePath
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", ErrUnsafePath
	}
	rel, err := filepath.Rel(s.absDir, abs)
	if err != nil || rel == "." || !filepath.IsLocal(rel) {
		return "", ErrUnsafePath
	}
	return rel, nil
}

// ValidatePath допускает отсутствующий файл для повторяемого удаления, но
// отвергает ссылки и специальные файлы. Root блокирует выход через родительские ссылки.
func (s *TrackFileStorage) ValidatePath(path string) error {
	rel, err := s.relativePath(path)
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(s.absDir)
	if err != nil {
		return err
	}
	defer root.Close()
	parts := strings.Split(rel, string(filepath.Separator))
	for index := range parts {
		info, err := root.Lstat(filepath.Join(parts[:index+1]...))
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("%w: %v", ErrUnsafePath, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return ErrUnsafePath
		}
		if index == len(parts)-1 {
			if !info.Mode().IsRegular() {
				return ErrUnsafePath
			}
		} else if !info.IsDir() {
			return ErrUnsafePath
		}
	}
	return nil
}

// Import копирует только существующее локальное аудио из корня библиотеки.
// Исходник не становится собственностью записи и не удаляется вместе с ней.
func (s *TrackFileStorage) Import(path string) (*SavedTrackFile, error) {
	if err := s.ValidatePath(path); err != nil {
		return nil, err
	}
	rel, err := s.relativePath(path)
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(s.absDir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	file, err := root.Open(rel)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnsafePath, err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, ErrUnsafePath
	}
	return s.Save(file, filepath.Base(rel))
}

// removeIfExists делает очистку повторяемой, но не скрывает ошибки диска/доступа.
func removeIfExists(root *os.Root, path string) error {
	err := root.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
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
