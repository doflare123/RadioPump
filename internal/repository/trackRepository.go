package repository

import (
	"RadioPump/internal/models"
	"database/sql"
)

// TrackRepository описывает, какие операции нужны сервисному слою от хранилища.
// Любой тип с таким набором методов автоматически удовлетворяет интерфейсу.
type TrackRepository interface {
	GetAll() ([]models.Track, error)
	GetByID(id uint) (*models.Track, error)
	Create(track *models.Track) error
	Update(track *models.Track) error
	Delete(id uint) error
}

type SQLiteTrackRepository struct {
	db *sql.DB
}

// Гарантия на этапе компиляции: SQLiteTrackRepository реализует TrackRepository.
var _ TrackRepository = (*SQLiteTrackRepository)(nil)

func NewTrackRepository(db *sql.DB) TrackRepository {
	return &SQLiteTrackRepository{db: db}
}

func (r *SQLiteTrackRepository) GetAll() ([]models.Track, error) {
	rows, err := r.db.Query(`
		SELECT id, title, artist, album, path, duration, created_at
		FROM tracks
		ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tracks := make([]models.Track, 0)
	for rows.Next() {
		var t models.Track
		if err := rows.Scan(&t.ID, &t.Title, &t.Artist, &t.Album, &t.Path, &t.Duration, &t.CreatedAt); err != nil {
			return nil, err
		}
		tracks = append(tracks, t)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return tracks, nil
}

func (r *SQLiteTrackRepository) GetByID(id uint) (*models.Track, error) {
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

func (r *SQLiteTrackRepository) Create(track *models.Track) error {
	res, err := r.db.Exec(`
		INSERT INTO tracks (title, artist, album, path, duration)
		VALUES (?, ?, ?, ?, ?)`,
		track.Title, track.Artist, track.Album, track.Path, track.Duration)
	if err != nil {
		return err
	}

	id, err := res.LastInsertId()
	if err == nil {
		track.ID = uint(id)
	}

	return nil
}

func (r *SQLiteTrackRepository) Update(track *models.Track) error {
	res, err := r.db.Exec(`
		UPDATE tracks
		SET title = ?, artist = ?, album = ?, duration = ?
		WHERE id = ?`,
		track.Title, track.Artist, track.Album, track.Duration, track.ID)
	if err != nil {
		return err
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}

	return nil
}

func (r *SQLiteTrackRepository) Delete(id uint) error {
	res, err := r.db.Exec(`DELETE FROM tracks WHERE id = ?`, id)
	if err != nil {
		return err
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}

	return nil
}
