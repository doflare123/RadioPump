package scheduler

import (
	"RadioPump/internal/repository"
)

type SchedulerFile struct {
	Path       string
	Ext        string
	SizeBytes  int64
	ModifiedAt int64
}

type Scheduler struct {
	repo repository.SchedulerRepository
}

func NewScannerService(repo repository.SchedulerRepository) *Scheduler {
	return &Scheduler{repo: repo}
}

func scanMusic(root string) ([]SchedulerFile, error) {
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
	var tracks []SchedulerFile
	// tracks =
	return tracks, nil
}
