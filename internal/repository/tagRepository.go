package repository

import (
	"RadioPump/internal/models"
)

// GetAllTags возвращает справочник в стабильном алфавитном порядке для UI.
func (r *SQLiteRepository) GetAllTags() ([]models.Tag, error) {
	rows, err := r.db.Query(`SELECT id, name FROM tags ORDER BY name COLLATE NOCASE ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tags := make([]models.Tag, 0)
	for rows.Next() {
		var tag models.Tag
		if err := rows.Scan(&tag.ID, &tag.Name); err != nil {
			return nil, err
		}
		tags = append(tags, tag)
	}
	return tags, rows.Err()
}

// GetTagByID нужен service-слою для проверки имени перед rename/delete.
func (r *SQLiteRepository) GetTagByID(id uint) (*models.Tag, error) {
	var tag models.Tag
	if err := r.db.QueryRow(`SELECT id, name FROM tags WHERE id = ?`, id).Scan(&tag.ID, &tag.Name); err != nil {
		return nil, err
	}
	return &tag, nil
}

// CreateTag сохраняет уже нормализованное service-слоем имя.
func (r *SQLiteRepository) CreateTag(tag *models.Tag) error {
	result, err := r.db.Exec(`INSERT INTO tags (name) VALUES (?)`, tag.Name)
	if err != nil {
		return err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return err
	}
	tag.ID = uint(id)
	return nil
}

// UpdateTag меняет только имя: связи track_tags используют стабильный ID.
func (r *SQLiteRepository) UpdateTag(tag *models.Tag) error {
	result, err := r.db.Exec(`UPDATE tags SET name = ? WHERE id = ?`, tag.Name, tag.ID)
	if err != nil {
		return err
	}
	return requireAffectedRow(result)
}

// DeleteTag явно удаляет связи перед справочником в одной транзакции, поэтому
// результат не зависит от PRAGMA foreign_keys конкретного connection из пула.
func (r *SQLiteRepository) DeleteTag(id uint) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM track_tags WHERE tag_id = ?`, id); err != nil {
		return err
	}
	result, err := tx.Exec(`DELETE FROM tags WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if err := requireAffectedRow(result); err != nil {
		return err
	}
	return tx.Commit()
}
