package repository

import (
	"RadioPump/internal/models"
	"database/sql"
	"strings"
)

type StationRepository interface {
	GetTagById(id uint) (string, error)
	GetTagId(name []string) ([]uint, error)
	GetByID(id uint) (*models.Track, error)
}

type SQLiteStationRepository struct {
	db *sql.DB
}

var _ StationRepository = (*SQLiteStationRepository)(nil)

func NewStationRepository(db *sql.DB) StationRepository {
	return &SQLiteStationRepository{db: db}
}

func (r *SQLiteStationRepository) GetTagById(id uint) (string, error) {
	var name string
	err := r.db.QueryRow(`SELECT name FROM tags WHERE id = ?`, id).Scan(&name)
	return name, err
}

func (r *SQLiteStationRepository) GetTagId(names []string) ([]uint, error) {
	if len(names) == 0 {
		return []uint{}, nil
	}

	placeholders := make([]string, len(names))
	args := make([]any, len(names))

	for i, name := range names {
		placeholders[i] = "?"
		args[i] = name
	}

	query := `SELECT id FROM tags WHERE name IN (` + strings.Join(placeholders, ",") + `)`

	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ids := make([]uint, 0, len(names))
	for rows.Next() {
		var id uint
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return ids, nil
}

func (r *SQLiteStationRepository) GetByID(id uint) (*models.Track, error) {
	var t models.Track
	err := r.db.QueryRow(`
		SELECT id, title, artist, album, path, duration, created_at
		FROM tracks
		WHERE id = ?`, id).
		Scan(&t.ID, &t.Title, &t.Artist, &t.Album, &t.Path, &t.Duration, &t.CreatedAt)
	if err != nil {
		return nil, err
	}

	return &t, nil
}
