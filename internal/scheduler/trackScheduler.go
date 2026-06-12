package scheduler

import (
	"RadioPump/internal/repository"
	"sync"
)

type Scheduler interface {
	RegisterStation(id string, tags []string) error
	NextTrackID(stationID string) (uint, error)
	MarkDirty(stationID string)
}

type scheduler struct {
	repo     repository.SchedulerRepository
	mu       sync.RWMutex
	Stations map[string]*StationScheldule
}

type StationScheldule struct {
	StationID string
	Tags      []uint
	Queue     []uint
	CurrentId uint
	Dirty     bool // надо перечитывать кандидатов из базы
}

func NewScannerService(repo repository.SchedulerRepository) Scheduler {
	return &scheduler{repo: repo}
}

func (s *scheduler) RegisterStation(id string, tags []string) error {
	tagIDs, err := s.repo.GetTagId(tags)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.Stations[id] = &StationScheldule{
		StationID: id,
		Tags:      tagIDs,
	}
	s.mu.Unlock()
	return nil
}

func (s *scheduler) NextTrackID(stationID string) (uint, error) {
	return 0, nil
}

func (s *scheduler) MarkDirty(stationID string) {

}
