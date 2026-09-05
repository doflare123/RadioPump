package services

import (
	"RadioPump/internal/repository"
	"database/sql"
)

// GetCover оставляет HTTP независимым от способа хранения обложки.
func (s *TrackService) GetCover(id uint) ([]byte, error) {
	repo, ok := s.repo.(repository.CoverRepository)
	if !ok {
		return nil, sql.ErrNoRows
	}
	return repo.GetCover(id)
}
