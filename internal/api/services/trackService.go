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
	Duration int
}

type TrackService struct {
	repo      repository.TrackRepository
	fileStore media.TrackFileStore
}

// Принимает интерфейсы, а не конкретные реализации хранилищ.
// Так форки проекта смогут заменить repository или файловое хранилище без переписывания HTTP handlers.
func NewTrackService(repo repository.TrackRepository, fileStore media.TrackFileStore) *TrackService {
	return &TrackService{repo: repo, fileStore: fileStore}
}

func (s *TrackService) GetAllTracks() ([]models.Track, error) {
	return s.repo.GetAll()
}

func (s *TrackService) GetByID(id int) (*models.Track, error) {
	return s.repo.GetByID(id)
}

func (s *TrackService) Create(track *models.Track) error {
	return s.repo.Create(track)
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
		Duration: meta.Duration,
	}

	if err := s.repo.Create(track); err != nil {
		s.fileStore.Remove(saved)
		return nil, err
	}

	return track, nil
}

func (s *TrackService) Update(track *models.Track) error {
	return s.repo.Update(track)
}

func (s *TrackService) Delete(id int) error {
	return s.repo.Delete(id)
}
