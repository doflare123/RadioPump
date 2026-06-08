package repository

import (
	"RadioPump/internal/models"
	"database/sql"
	"strings"
)

type SchedulerRepository interface {
	GetMusic(tags []string) ([]models.Track, error)
}

type SQLiteSchedulerRepository struct {
	db *sql.DB
}

var _ SchedulerRepository = (*SQLiteSchedulerRepository)(nil)

func NewScheldulerRepository(db *sql.DB) SchedulerRepository {
	return &SQLiteSchedulerRepository{db: db}
}

func (r *SQLiteSchedulerRepository) GetMusic(tags []string) ([]models.Track, error) {
	tracks := make([]models.Track, 0)

	if len(tags) == 0 {
		rows, err := r.db.Query(`
			SELECT id, title, artist, album, path, duration, created_at
			FROM tracks
			ORDER BY id DESC`)

		if err != nil {
			return nil, err
		}

		defer rows.Close()

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
	} else {
		placeholders := strings.TrimRight(strings.Repeat("?,", len(tags)), ",")

		query := `
				SELECT DISTINCT t.id, t.title, t.artist, t.album, t.path, t.duration, t.created_at
				FROM tracks t
				JOIN track_tags tt ON t.id = tt.track_id
				JOIN tags g ON tt.tag_id = g.id
				WHERE g.name IN (` + placeholders + `)`

		args := make([]any, 0, len(tags))

		for _, tag := range tags {
			args = append(args, tag)
		}

		rows, err := r.db.Query(query, args...)

		if err != nil {
			return nil, err
		}

		defer rows.Close()

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
}
