package models

type Track struct {
	ID        int
	Title     string
	Artist    string
	Album     string
	Path      string
	Duration  int
	CreatedAt string
	Tags      []Tag
}

type TrackTag struct {
	TrackID uint
	TagID   uint
}
