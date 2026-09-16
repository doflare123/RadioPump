package repository

import (
	"RadioPump/internal/models"
	"fmt"
)

// GetRadioTracks получает только поля карточек, без путей, тегов и данных обложек.
// Повторы ID объединяются; удалённые записи отсутствуют в результате. Пакеты
// до 500 параметров ограничивают размер SQL даже при большом числе волн.
func (r *SQLiteRepository) GetRadioTracks(ids []uint) (map[uint]models.Track, error) {
	result := make(map[uint]models.Track)
	seen := make(map[uint]bool)
	args := make([]any, 0, len(ids))
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			args = append(args, id)
		}
	}
	for len(args) > 0 {
		n := min(500, len(args))
		if err := r.readRadioTracks(args[:n], result); err != nil {
			return nil, err
		}
		args = args[n:]
	}
	return result, nil
}

// readRadioTracks закрывает rows до следующего пакета: SQLite использует одно
// соединение. EXISTS проверяет обложку по ключу, не читая её бинарное содержимое.
func (r *SQLiteRepository) readRadioTracks(args []any, result map[uint]models.Track) error {
	rows, err := r.db.Query(`SELECT t.id, t.title, t.artist, t.album, t.duration,
		EXISTS(SELECT 1 FROM track_covers c WHERE c.track_id = t.id)
		FROM tracks t WHERE t.id IN (`+makePlaceholders(len(args))+`)`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var track models.Track
		var cover bool
		if err := rows.Scan(&track.ID, &track.Title, &track.Artist, &track.Album, &track.Duration, &cover); err != nil {
			return err
		}
		if cover {
			track.CoverURL = fmt.Sprintf("/api/covers/%d", track.ID)
		}
		result[track.ID] = track
	}
	return rows.Err()
}
