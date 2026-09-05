package services

import (
	"RadioPump/internal/media"
	"RadioPump/internal/models"
	"RadioPump/internal/repository"
	"errors"
	"fmt"
	"io"
	"log"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

var ErrMissingSavedFile = errors.New("сохраненный файл трека отсутствует")
var ErrFileDeletionPending = errors.New("трек удалён из библиотеки, очистка файла ожидает повторной попытки")

// Хранит только редактируемые пользователем поля трека.
// Путь к файлу генерируется сервером и не должен приходить из upload-запроса.
type TrackMetadata struct {
	CoverData []byte
	Title     string
	Artist    string
	Album     string
	Duration  uint
	TagIDs    []uint
}

// StationInvalidator скрывает реализацию scheduler от библиотечных сервисов.
// Plugin может заменить уведомление, сохранив этот маленький контракт.
type StationInvalidator interface {
	MarkAllDirty()
}

// TrackCatalog — контракт сценариев библиотеки, от которого зависит HTTP handler.
type TrackCatalog interface {
	GetAllTracks() ([]models.Track, error)
	GetByID(id uint) (*models.Track, error)
	Create(track *models.Track) error
	MaxUploadBytes() int64
	StoreUploadedFile(src io.Reader, originalName string) (*media.SavedTrackFile, error)
	DiscardUploadedFile(saved *media.SavedTrackFile)
	CreateUploadedTrack(meta TrackMetadata, saved *media.SavedTrackFile) (*models.Track, error)
	Update(track *models.Track) error
	Delete(id uint) error
}

type TrackService struct {
	repo      repository.TrackRepository
	fileStore media.TrackFileStore
	stations  StationInvalidator
	cleanupMu sync.Mutex
}

var _ TrackCatalog = (*TrackService)(nil)

// Принимает интерфейсы, а не конкретные реализации хранилищ.
// Так форки проекта смогут заменить repository или файловое хранилище без переписывания HTTP handlers.
func NewTrackService(repo repository.TrackRepository, fileStore media.TrackFileStore, stations StationInvalidator) *TrackService {
	return &TrackService{repo: repo, fileStore: fileStore, stations: stations}
}

func (s *TrackService) GetAllTracks() ([]models.Track, error) {
	return s.repo.GetAll()
}

func (s *TrackService) GetByID(id uint) (*models.Track, error) {
	return s.repo.GetByID(id)
}

func (s *TrackService) Create(track *models.Track) error {
	if s.fileStore == nil {
		return ErrMissingSavedFile
	}
	saved, err := s.fileStore.Import(track.Path)
	if err != nil {
		return err
	}
	track.Path = saved.StoredPath
	if err := s.repo.Create(track); err != nil {
		return errors.Join(err, s.discard(saved))
	}
	s.markStationsDirty()
	// Повторное чтение заполняет имена тегов для JSON-ответа, а не только их ID.
	if created, err := s.repo.GetByID(track.ID); err == nil {
		*track = *created
	}
	return nil
}

// Нужен HTTP-слою для раннего ограничения multipart body.
// Финальная проверка размера все равно остается внутри файлового хранилища.
func (s *TrackService) MaxUploadBytes() int64 {
	if s.fileStore == nil {
		return 0
	}
	return s.fileStore.MaxBytes()
}

// Сохраняет и проверяет аудиофайл до появления записи в БД.
// Такое разделение позволяет handler-у читать multipart fields в любом порядке.
func (s *TrackService) StoreUploadedFile(src io.Reader, originalName string) (*media.SavedTrackFile, error) {
	return s.fileStore.Save(src, originalName)
}

// Откатывает уже сохраненный файл, если запрос позже завершился ошибкой.
// Handler просит service выполнить rollback и не трогает файловое хранилище напрямую.
func (s *TrackService) DiscardUploadedFile(saved *media.SavedTrackFile) {
	if s.fileStore == nil {
		return
	}
	if err := s.discard(saved); err != nil {
		log.Printf("откат upload: %v", err)
	}
}

// Создает запись в БД для уже сохраненного файла.
// Если repository отклонит запись, файл удаляется, чтобы не оставлять мусор.
func (s *TrackService) CreateUploadedTrack(meta TrackMetadata, saved *media.SavedTrackFile) (*models.Track, error) {
	if saved == nil {
		return nil, ErrMissingSavedFile
	}

	title := meta.Title
	if title == "" {
		title = saved.BaseName
	}

	track := &models.Track{
		CoverData: meta.CoverData,
		Title:     title,
		Artist:    meta.Artist,
		Album:     meta.Album,
		Path:      saved.StoredPath,
		Duration:  uint(meta.Duration),
		Tags:      tagsFromIDs(meta.TagIDs),
	}

	if err := s.repo.Create(track); err != nil {
		return nil, errors.Join(err, s.discard(saved))
	}
	s.markStationsDirty()
	if created, err := s.repo.GetByID(track.ID); err == nil {
		track = created
	}

	return track, nil
}

func (s *TrackService) Update(track *models.Track) error {
	if err := s.repo.Update(track); err != nil {
		return err
	}
	s.markStationsDirty()
	return nil
}

// Delete сначала проверяет границу хранилища, затем атомарно удаляет запись и
// ставит очистку файла в durable очередь. Ошибка диска не теряет задание.
func (s *TrackService) Delete(id uint) error {
	track, err := s.repo.GetByID(id)
	if err != nil {
		return err
	}
	if s.fileStore == nil {
		return ErrMissingSavedFile
	}
	if err := s.fileStore.ValidatePath(track.Path); err != nil {
		return err
	}
	if err := s.repo.Delete(id); err != nil {
		return err
	}
	s.markStationsDirty()
	if err := s.cleanupFiles(track.Path); err != nil {
		return errors.Join(ErrFileDeletionPending, err)
	}
	return nil
}

// CleanupFiles повторяет отложенные удаления. Проверка ссылок учитывает старые
// эквивалентные пути (./music/a и music/a); импорт теперь всегда создаёт копию.
func (s *TrackService) CleanupFiles() error {
	return s.cleanupFiles("")
}

// cleanupFiles обслуживает всю очередь либо только файл текущего DELETE, чтобы
// ошибка старого задания не меняла успешный ответ несвязанного удаления.
func (s *TrackService) cleanupFiles(onlyPath string) error {
	s.cleanupMu.Lock()
	defer s.cleanupMu.Unlock()
	paths, err := s.repo.PendingFileDeletions()
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		return nil
	}
	tracks, err := s.repo.GetAll()
	if err != nil {
		return err
	}
	used := make(map[string]bool)
	for _, track := range tracks {
		used[fileKey(track.Path)] = true
	}
	var failures []error
	for _, path := range paths {
		if onlyPath != "" && path != onlyPath {
			continue
		}
		if !used[fileKey(path)] {
			if err := s.fileStore.Remove(&media.SavedTrackFile{AbsolutePath: path}); err != nil {
				failures = append(failures, fmt.Errorf("очистка %q: %w", path, err))
				continue
			}
		}
		if err := s.repo.CompleteFileDeletion(path); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

// discard пытается завершить rollback сразу и сохраняет retry при отказе диска.
// Если недоступны и диск, и БД, готовый orphan будет сохранён recovery при старте.
func (s *TrackService) discard(saved *media.SavedTrackFile) error {
	if saved == nil {
		return nil
	}
	if err := s.fileStore.Remove(saved); err != nil {
		path := saved.StoredPath
		if path == "" {
			path = saved.AbsolutePath
		}
		return errors.Join(err, s.repo.ScheduleFileDeletion(path))
	}
	return nil
}

// fileKey нормализует представление локального пути для защиты старых ссылок.
func fileKey(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	if runtime.GOOS == "windows" {
		return strings.ToLower(abs)
	}
	return abs
}

// tagsFromIDs создаёт repository-модель без доверия к клиентским именам тегов.
func tagsFromIDs(ids []uint) []models.Tag {
	tags := make([]models.Tag, 0, len(ids))
	for _, id := range ids {
		tags = append(tags, models.Tag{ID: id})
	}
	return tags
}

// markStationsDirty не заставляет service знать ID и внутреннее устройство волн.
func (s *TrackService) markStationsDirty() {
	if s.stations != nil {
		s.stations.MarkAllDirty()
	}
}
