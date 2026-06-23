package repository

import (
	"RadioPump/internal/models"
	"database/sql"
	"strings"
)

type TrackRepository interface {
	GetAll() ([]models.Track, error)
	GetByID(id uint) (*models.Track, error)
	Create(track *models.Track) error
	Update(track *models.Track) error
	Delete(id uint) error
}

type SchedulerRepository interface {
	GetMusic(tags []string) ([]models.Track, error)
	GetTagId(names []string) ([]uint, error)
}

type ScannerRepository interface {
	Scan() error
	GetTrack() (string, error)
}

type SQLiteRepository struct {
	db *sql.DB
}

var _ TrackRepository = (*SQLiteRepository)(nil)
var _ SchedulerRepository = (*SQLiteRepository)(nil)
var _ ScannerRepository = (*SQLiteRepository)(nil)

func NewRepository(db *sql.DB) *SQLiteRepository {
	return &SQLiteRepository{db: db}
}

func NewTrackRepository(db *sql.DB) TrackRepository {
	return NewRepository(db)
}

func NewSchedulerRepository(db *sql.DB) SchedulerRepository {
	return NewRepository(db)
}

func NewScannerRepository(db *sql.DB) ScannerRepository {
	return NewRepository(db)
}

func (r *SQLiteRepository) GetAll() ([]models.Track, error) {
	return r.queryTracks(`
		SELECT id, title, artist, album, path, duration, created_at
		FROM tracks
		ORDER BY id DESC`)
}

func (r *SQLiteRepository) GetMusic(tags []string) ([]models.Track, error) {
	if len(tags) == 0 {
		return r.GetAll()
	}

	placeholders := makePlaceholders(len(tags))
	query := `
		SELECT DISTINCT t.id, t.title, t.artist, t.album, t.path, t.duration, t.created_at
		FROM tracks t
		JOIN track_tags tt ON t.id = tt.track_id
		JOIN tags g ON tt.tag_id = g.id
		WHERE g.name IN (` + placeholders + `)
		ORDER BY t.id DESC`

	args := make([]any, 0, len(tags))
	for _, tag := range tags {
		args = append(args, tag)
	}

	return r.queryTracks(query, args...)
}

func (r *SQLiteRepository) GetByID(id uint) (*models.Track, error) {
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

func (r *SQLiteRepository) Create(track *models.Track) error {
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

func (r *SQLiteRepository) Update(track *models.Track) error {
	res, err := r.db.Exec(`
		UPDATE tracks
		SET title = ?, artist = ?, album = ?, duration = ?
		WHERE id = ?`,
		track.Title, track.Artist, track.Album, track.Duration, track.ID)
	if err != nil {
		return err
	}

	return requireAffectedRow(res)
}

func (r *SQLiteRepository) Delete(id uint) error {
	res, err := r.db.Exec(`DELETE FROM tracks WHERE id = ?`, id)
	if err != nil {
		return err
	}

	return requireAffectedRow(res)
}

func (r *SQLiteRepository) GetTagId(names []string) ([]uint, error) {
	if len(names) == 0 {
		return []uint{}, nil
	}

	placeholders := makePlaceholders(len(names))
	args := make([]any, 0, len(names))
	for _, name := range names {
		args = append(args, name)
	}

	rows, err := r.db.Query(`SELECT id FROM tags WHERE name IN (`+placeholders+`)`, args...)
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

func (r *SQLiteRepository) Scan() error {
	return nil
}

func (r *SQLiteRepository) GetTrack() (string, error) {
	return "", nil
}

func (r *SQLiteRepository) queryTracks(query string, args ...any) ([]models.Track, error) {
	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanTracks(rows)
}

func scanTracks(rows *sql.Rows) ([]models.Track, error) {
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

func makePlaceholders(count int) string {
	return strings.TrimRight(strings.Repeat("?,", count), ",")
}

func requireAffectedRow(res sql.Result) error {
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}
