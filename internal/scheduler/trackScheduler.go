package scheduler

import (
	"RadioPump/internal/repository"
	"errors"
	"math/rand/v2"
	"sync"
)

var (
	ErrStationNotFound = errors.New("станция не найдена")
	ErrNoTracks        = errors.New("для станции нет подходящих треков")
)

type Scheduler interface {
	RegisterStation(id string, tags []string) error
	NextTrackID(stationID string) (uint, error)
	MarkDirty(stationID string) error
	MarkAllDirty()
	QueueSnapshot(stationID string) (StationSnapshot, error)
	CurrentTrackID(stationID string) (uint, error)
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
	// Refilling bool // сейчас идет процесс перечитывания кандидатов из базы
}

type StationSnapshot struct {
	StationID string
	CurrentID uint
	Queue     []uint
}

func NewScheduler(repo repository.SchedulerRepository) Scheduler {
	return &scheduler{
		repo:     repo,
		Stations: make(map[string]*StationSchedule),
	}
}

func (s *scheduler) RegisterStation(id string, tags []string) error {
	s.mu.Lock()

	s.Stations[id] = &StationSchedule{
		StationID: id,
		Tags:      append([]string(nil), tags...),
		Queue:     make([]uint, 0),
		Dirty:     false,
	}

	s.mu.Unlock()

	err := s.refillStation(id, tags, false)

	if errors.Is(err, ErrNoTracks) {
		return nil
	}

	return err
}

func (s *scheduler) NextTrackID(stationID string) (uint, error) {
	s.mu.Lock()
	st, ok := s.Stations[stationID]
	if !ok {
		s.mu.Unlock()
		return 0, ErrStationNotFound
	}
	needRefill := len(st.Queue) == 0
	isDirty := st.Dirty

	tags := append([]string(nil), st.Tags...)
	s.mu.Unlock()

	preserveQueue := isDirty && !needRefill
	if needRefill || isDirty {
		if err := s.refillStation(stationID, tags, preserveQueue); err != nil {
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

	return nextID, nil
}

func (s *scheduler) MarkDirty(stationID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if st, ok := s.Stations[stationID]; ok {
		st.Dirty = true
	} else {
		return ErrStationNotFound
	}
	return nil
}

// MarkAllDirty помечает все очереди после изменения библиотеки или справочника тегов.
// Пересборка выполняется лениво при следующем запросе трека каждой станции.
func (s *scheduler) MarkAllDirty() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, station := range s.Stations {
		station.Dirty = true
	}
}

func (s *scheduler) unMarkDirty(stationID string) {
	s.mu.Lock()
	if st, ok := s.Stations[stationID]; ok {
		st.Dirty = false
	}
	s.mu.Unlock()
}

func (s *scheduler) refillStation(stationID string, tags []string, preserveQueue bool) error {
	tracks, err := s.repo.GetMusic(tags)
	if err != nil {
		return err
	}
	if len(tracks) == 0 {
		return ErrNoTracks
	}

	freshSet := make(map[uint]struct{}, len(tracks))
	freshIDs := make([]uint, 0, len(tracks))
	for _, track := range tracks {
		id := uint(track.ID)
		freshSet[id] = struct{}{}
		freshIDs = append(freshIDs, id)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	st, ok := s.Stations[stationID]
	if !ok {
		return ErrStationNotFound
	}

	var nextQueue []uint

	if preserveQueue {
		queued := make(map[uint]struct{}, len(st.Queue)+1)

		for _, id := range st.Queue {
			if _, ok := freshSet[id]; ok {
				nextQueue = append(nextQueue, id)
				queued[id] = struct{}{}
			}
		}

		if st.CurrentID != 0 {
			queued[st.CurrentID] = struct{}{}
		}

		var added []uint
		for _, id := range freshIDs {
			if _, exists := queued[id]; !exists {
				added = append(added, id)
			}
		}

		rand.Shuffle(len(added), func(i, j int) {
			added[i], added[j] = added[j], added[i]
		})

		nextQueue = append(nextQueue, added...)
	} else {
		nextQueue = freshIDs
		rand.Shuffle(len(nextQueue), func(i, j int) {
			nextQueue[i], nextQueue[j] = nextQueue[j], nextQueue[i]
		})
	}

	st.Queue = nextQueue
	st.Dirty = false

	return nil
}

func (s *scheduler) QueueSnapshot(stationID string) (StationSnapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st, ok := s.Stations[stationID]
	if !ok {
		return StationSnapshot{}, ErrStationNotFound
	}
	return StationSnapshot{
		StationID: st.StationID,
		CurrentID: st.CurrentID,
		Queue:     append([]uint(nil), st.Queue...),
	}, nil
}

func (s *scheduler) CurrentTrackID(stationID string) (uint, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st, ok := s.Stations[stationID]
	if !ok {
		return 0, ErrStationNotFound
	}
	return st.CurrentID, nil
}
