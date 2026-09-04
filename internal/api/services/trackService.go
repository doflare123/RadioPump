package services

import (
	"RadioPump/internal/media"
	"RadioPump/internal/models"
	"RadioPump/internal/repository"
	"errors"
	"io"
)

var ErrMissingSavedFile = errors.New("сохраненный файл трека отсутствует")

// Хранит только редактируемые пользователем поля трека.
// Путь к файлу генерируется сервером и не должен приходить из upload-запроса.
type TrackMetadata struct {
	Title    string
	Artist   string
	Album    string
	Duration uint
	TagIDs   []uint
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
	if err := s.repo.Create(track); err != nil {
		return err
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
	s.fileStore.Remove(saved)
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
		Title:    title,
		Artist:   meta.Artist,
		Album:    meta.Album,
		Path:     saved.StoredPath,
		Duration: uint(meta.Duration),
		Tags:     tagsFromIDs(meta.TagIDs),
	}

	if err := s.repo.Create(track); err != nil {
		s.fileStore.Remove(saved)
		return nil, err
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

// Delete удаляет запись трека и связанный с ней файл. Сначала удаляется БД,
// потому что именно она является источником правды для существования трека.
func (s *TrackService) Delete(id uint) error {
	track, err := s.repo.GetByID(id)
	if err != nil {
		return err
	}
	if err := s.repo.Delete(id); err != nil {
		return err
	}
	if s.fileStore != nil {
		s.fileStore.Remove(&media.SavedTrackFile{AbsolutePath: track.Path})
	}
	s.markStationsDirty()
	return nil
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
