package repository

import (
	"RadioPump/internal/models"
	"database/sql"
)

type TrackRepository interface {
	GetAll() ([]models.Track, error)
	GetByID(id int) (*models.Track, error)
	Create(track *models.Track) error
	Update(track *models.Track) error
	Delete(id int) error
}

type SQLiteTrackRepository struct {
	db *sql.DB
}

func NewTrackRepository(db *sql.DB) *SQLiteTrackRepository {
	return &SQLiteTrackRepository{db: db}
}

func (s *SQLiteTrackRepository) GetAll() ([]models.Track, error) {
	return nil, nil
}

func (s *SQLiteTrackRepository) GetByID(id int) (*models.Track, error) {
	return nil, nil
}

func (s *SQLiteTrackRepository) Create(track *models.Track) error {
	return nil
}

func (s *SQLiteTrackRepository) Update(track *models.Track) error {
	return nil
}

func (s *SQLiteTrackRepository) Delete(id int) error {
	return nil
}
