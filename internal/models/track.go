package models

type Track struct {
	// Changes задаёт частичное обновление; nil сохраняет контракт полной записи.
	Changes *TrackChanges `json:"-"`
	// CoverData передаётся только внутри upload-транзакции; публично выдаётся URL.
	CoverData []byte `json:"-"`
	CoverURL  string
	ID        uint
	Title     string
	Artist    string
	Album     string
	Path      string
	Duration  uint
	CreatedAt string
	Tags      []Tag
}

// TrackChanges отличает отсутствующее поле от явно переданного пустого значения.
// Репозиторий применяет его внутри UPDATE, без чтения устаревающего снимка.
type TrackChanges struct {
	Title    *string `json:"title"`
	Artist   *string `json:"artist"`
	Album    *string `json:"album"`
	Duration *uint   `json:"duration"`
}

type TrackTag struct {
	TrackID uint
	TagID   uint
}
