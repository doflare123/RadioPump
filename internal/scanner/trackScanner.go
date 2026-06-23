package scanner

import "RadioPump/internal/repository"

type ScannerFile struct {
	Path       string
	Ext        string
	SizeBytes  int64
	ModifiedAt int64
}

type Scanner interface {
	Scan() error
	GetTrack() (ScannerFile, error)
}

type scanner struct {
	repo repository.ScannerRepository
}

func NewScanner(repo repository.ScannerRepository) Scanner {
	return &scanner{repo: repo}
}

func scanMusic(root string) (ScannerFile, error) {
	allow := map[string]bool{
		".mp3":  true,
		".flac": true,
		".wav":  true,
		".aac":  true,
		".m4a":  true,
		".ogg":  true,
		".alac": true,
		".aiff": true,
	}
	_ = allow
	var tracks ScannerFile
	// tracks =
	return tracks, nil
}

func (s *scanner) Scan() error {
	return nil
}

func (s *scanner) GetTrack() (ScannerFile, error) {
	return ScannerFile{}, nil
}
