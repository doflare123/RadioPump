package models

type Track struct {
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

type TrackTag struct {
	TrackID uint
	TagID   uint
}
