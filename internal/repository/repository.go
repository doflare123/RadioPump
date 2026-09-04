package repository

import (
	"RadioPump/internal/models"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

var (
	ErrUnknownTag  = errors.New("один или несколько тегов не существуют")
	ErrTooManyTags = errors.New("у трека слишком много тегов")
)

type TrackRepository interface {
	GetAll() ([]models.Track, error)
	GetByID(id uint) (*models.Track, error)
	Create(track *models.Track) error
	Update(track *models.Track) error
	Delete(id uint) error
}

// TagRepository описывает справочник допустимых тегов отдельно от HTTP и SQLite.
// Другой backend или будущий plugin может реализовать тот же контракт.
type TagRepository interface {
	GetAllTags() ([]models.Tag, error)
	GetTagByID(id uint) (*models.Tag, error)
	CreateTag(tag *models.Tag) error
	UpdateTag(tag *models.Tag) error
	DeleteTag(id uint) error
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
var _ TagRepository = (*SQLiteRepository)(nil)
var _ SchedulerRepository = (*SQLiteRepository)(nil)
var _ ScannerRepository = (*SQLiteRepository)(nil)

func NewRepository(db *sql.DB) *SQLiteRepository {
	return &SQLiteRepository{db: db}
}

func NewTrackRepository(db *sql.DB) TrackRepository {
	return NewRepository(db)
}

// NewTagRepository возвращает справочник через интерфейс, скрывая SQLite от caller-а.
func NewTagRepository(db *sql.DB) TagRepository {
	return NewRepository(db)
}

func NewSchedulerRepository(db *sql.DB) SchedulerRepository {
	return NewRepository(db)
}

func NewScannerRepository(db *sql.DB) ScannerRepository {
	return NewRepository(db)
}

// GetAll загружает треки вместе с полным набором назначенных тегов.
func (r *SQLiteRepository) GetAll() ([]models.Track, error) {
	return r.queryTracks(`
		SELECT t.id, t.title, t.artist, t.album, t.path, t.duration, t.created_at, g.id, g.name
		FROM tracks t
		LEFT JOIN track_tags tt ON tt.track_id = t.id
		LEFT JOIN tags g ON g.id = tt.tag_id
		ORDER BY t.id DESC, g.name ASC`)
}

// GetMusic сохраняет согласованную семантику ИЛИ: достаточно одного тега волны.
func (r *SQLiteRepository) GetMusic(tags []string) ([]models.Track, error) {
	if len(tags) == 0 {
		return r.GetAll()
	}

	placeholders := makePlaceholders(len(tags))
	query := `
		SELECT t.id, t.title, t.artist, t.album, t.path, t.duration, t.created_at, all_tags.id, all_tags.name
		FROM tracks t
		LEFT JOIN track_tags all_tt ON all_tt.track_id = t.id
		LEFT JOIN tags all_tags ON all_tags.id = all_tt.tag_id
		WHERE EXISTS (
			SELECT 1 FROM track_tags filter_tt
			JOIN tags filter_tags ON filter_tags.id = filter_tt.tag_id
			WHERE filter_tt.track_id = t.id AND filter_tags.name IN (` + placeholders + `)
		)
		ORDER BY t.id DESC, all_tags.name ASC`

	args := make([]any, 0, len(tags))
	for _, tag := range tags {
		args = append(args, tag)
	}

	return r.queryTracks(query, args...)
}

// GetByID использует тот же grouped scan, чтобы одиночный ответ не терял теги.
func (r *SQLiteRepository) GetByID(id uint) (*models.Track, error) {
	tracks, err := r.queryTracks(`
		SELECT t.id, t.title, t.artist, t.album, t.path, t.duration, t.created_at, g.id, g.name
		FROM tracks t
		LEFT JOIN track_tags tt ON tt.track_id = t.id
		LEFT JOIN tags g ON g.id = tt.tag_id
		WHERE t.id = ?
		ORDER BY g.name ASC`, id)
	if err != nil {
		return nil, err
	}
	if len(tracks) == 0 {
		return nil, sql.ErrNoRows
	}
	return &tracks[0], nil
}

// Create записывает трек и проверенные связи в одной SQLite-транзакции.
func (r *SQLiteRepository) Create(track *models.Track) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	res, err := tx.Exec(`
		INSERT INTO tracks (title, artist, album, path, duration)
		VALUES (?, ?, ?, ?, ?)`,
		track.Title, track.Artist, track.Album, track.Path, track.Duration)
	if err != nil {
		return err
	}

	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	track.ID = uint(id)
	if err := replaceTrackTags(tx, track.ID, track.Tags); err != nil {
		return err
	}
	return tx.Commit()
}

// Update сохраняет метаданные и при наличии Tags атомарно заменяет связи.
func (r *SQLiteRepository) Update(track *models.Track) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	res, err := tx.Exec(`
		UPDATE tracks
		SET title = ?, artist = ?, album = ?, duration = ?
		WHERE id = ?`,
		track.Title, track.Artist, track.Album, track.Duration, track.ID)
	if err != nil {
		return err
	}

	if err := requireAffectedRow(res); err != nil {
		return err
	}
	// nil означает, что старый API-клиент не передал tag_ids и связи надо сохранить.
	if track.Tags != nil {
		if err := replaceTrackTags(tx, track.ID, track.Tags); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Delete явно удаляет связи и сам трек, не полагаясь на connection-local FK pragma.
func (r *SQLiteRepository) Delete(id uint) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM track_tags WHERE track_id = ?`, id); err != nil {
		return err
	}
	res, err := tx.Exec(`DELETE FROM tracks WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if err := requireAffectedRow(res); err != nil {
		return err
	}
	return tx.Commit()
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

// scanTracks группирует строки LEFT JOIN обратно в Track и возвращает все теги,
// включая случай, когда фильтр станции совпал только с одним из них.
func scanTracks(rows *sql.Rows) ([]models.Track, error) {
	tracks := make([]models.Track, 0)
	positions := make(map[uint]int)
	for rows.Next() {
		var t models.Track
		var tagID sql.NullInt64
		var tagName sql.NullString
		if err := rows.Scan(&t.ID, &t.Title, &t.Artist, &t.Album, &t.Path, &t.Duration, &t.CreatedAt, &tagID, &tagName); err != nil {
			return nil, err
		}
		position, exists := positions[t.ID]
		if !exists {
			t.Tags = make([]models.Tag, 0)
			tracks = append(tracks, t)
			position = len(tracks) - 1
			positions[t.ID] = position
		}
		if tagID.Valid && tagName.Valid {
			tracks[position].Tags = append(tracks[position].Tags, models.Tag{ID: uint(tagID.Int64), Name: tagName.String})
		}
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return tracks, nil
}

// replaceTrackTags проверяет существование всех ID до удаления старых связей.
// Дубликаты входных ID схлопываются, а вся операция остаётся частью внешней транзакции.
func replaceTrackTags(tx *sql.Tx, trackID uint, tags []models.Tag) error {
	if len(tags) > 128 {
		return ErrTooManyTags
	}
	ids := make([]uint, 0, len(tags))
	seen := make(map[uint]struct{}, len(tags))
	for _, tag := range tags {
		id := tag.ID
		if id == 0 {
			return ErrUnknownTag
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}

	if len(ids) > 0 {
		placeholders := makePlaceholders(len(ids))
		args := make([]any, len(ids))
		for index, id := range ids {
			args[index] = id
		}
		var count int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM tags WHERE id IN (`+placeholders+`)`, args...).Scan(&count); err != nil {
			return err
		}
		if count != len(ids) {
			return ErrUnknownTag
		}
	}

	if _, err := tx.Exec(`DELETE FROM track_tags WHERE track_id = ?`, trackID); err != nil {
		return err
	}
	for _, id := range ids {
		if _, err := tx.Exec(`INSERT INTO track_tags (track_id, tag_id) VALUES (?, ?)`, trackID, id); err != nil {
			return fmt.Errorf("связь трека %d с тегом %d: %w", trackID, id, err)
		}
	}
	return nil
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
