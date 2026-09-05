package repository

// CoverRepository читает бинарные данные отдельно: списки треков содержат лишь URL.
type CoverRepository interface{ GetCover(id uint) ([]byte, error) }

func (r *SQLiteRepository) GetCover(id uint) ([]byte, error) {
	var data []byte
	err := r.db.QueryRow(`SELECT data FROM track_covers WHERE track_id = ?`, id).Scan(&data)
	return data, err
}
