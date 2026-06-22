package models

type Track struct {
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
