package scheduler

import (
	"RadioPump/internal/repository"
	"errors"
	"math/rand/v2"
	"sync"
)

var (
	ErrStationNotFound = errors.New("station not found")
	ErrNoTracks        = errors.New("no tracks for station")
)

type Scheduler interface {
	RegisterStation(id string, tags []string) error
	NextTrackID(stationID string) (uint, error)
	MarkDirty(stationID string)
	UnMarkDirty(stationID string)
}

type scheduler struct {
	repo     repository.SchedulerRepository
	mu       sync.RWMutex
	Stations map[string]*StationSchedule
}

type StationSchedule struct {
	StationID string
	Tags      []string
	Queue     []uint
	CurrentID uint
	Dirty     bool // надо перечитывать кандидатов из базы
}

func NewScheduler(repo repository.SchedulerRepository) Scheduler {
	return &scheduler{
		repo:     repo,
		Stations: make(map[string]*StationSchedule),
	}
}

func (s *scheduler) RegisterStation(id string, tags []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Stations[id] = &StationSchedule{
		StationID: id,
		Tags:      append([]string(nil), tags...),
		Queue:     make([]uint, 0),
		Dirty:     true,
	}

	return nil
}

func (s *scheduler) NextTrackID(stationID string) (uint, error) {
	s.mu.Lock()
	st, ok := s.Stations[stationID]
	if !ok {
		s.mu.Unlock()
		return 0, ErrStationNotFound
	}
	needRefill := st.Dirty || len(st.Queue) == 0
	tags := append([]string(nil), st.Tags...)
	s.mu.Unlock()

	if needRefill {
		if err := s.refillStation(stationID, tags); err != nil {
			return 0, err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	st, ok = s.Stations[stationID]
	if !ok {
		return 0, ErrStationNotFound
	}
	if len(st.Queue) == 0 {
		return 0, ErrNoTracks
	}
	nextID := st.Queue[0]
	st.Queue = st.Queue[1:]
	st.CurrentID = nextID
	s.mu.Unlock()
	return nextID, nil
}

func (s *scheduler) MarkDirty(stationID string) {
	s.mu.Lock()
	s.Stations[stationID].Dirty = true
	s.mu.Unlock()
}

func (s *scheduler) UnMarkDirty(stationID string) {
	s.mu.Lock()
	s.Stations[stationID].Dirty = false
	s.mu.Unlock()
}

func (s *scheduler) refillStation(stationID string, tags []string) error {
	tracks, err := s.repo.GetMusic(tags)
	if err != nil {
		return err
	}
	if len(tracks) == 0 {
		return ErrNoTracks
	}
	ids := make([]uint, len(tracks))
	for _, track := range tracks {
		ids = append(ids, uint(track.ID))
	}

	rand.Shuffle(len(ids), func(i, j int) {
		ids[i], ids[j] = ids[j], ids[i]
	})

	s.mu.Lock()
	defer s.mu.Unlock()

	st, ok := s.Stations[stationID]
	if !ok {
		return ErrStationNotFound
	}

	st.Queue = ids
	st.Dirty = false

	return nil
}
