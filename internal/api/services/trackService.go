package services

import (
	"RadioPump/internal/models"
	"RadioPump/internal/repository"
)

type TrackService struct {
	repo repository.TrackRepository // только интерфейс
}

func NewTrackService(repo repository.TrackRepository) *TrackService {
	return &TrackService{repo: repo}
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

func (s *TrackService) Update(track *models.Track) error {
	return s.repo.Update(track)
}

func (s *TrackService) Delete(id int) error {
	return s.repo.Delete(id)
}
