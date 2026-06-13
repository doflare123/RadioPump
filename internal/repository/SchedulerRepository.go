package repository

import (
	"RadioPump/internal/models"
	"database/sql"
	"strings"
)

type SchedulerRepository interface {
	GetMusic(tags []string) ([]models.Track, error)
	GetTagId(name []string) ([]uint, error)
}

type SQLiteSchedulerRepository struct {
	db *sql.DB
}

var _ SchedulerRepository = (*SQLiteSchedulerRepository)(nil)

func NewSchedulerRepository(db *sql.DB) SchedulerRepository {
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

func (r *SQLiteSchedulerRepository) GetTagId(names []string) ([]uint, error) {
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
