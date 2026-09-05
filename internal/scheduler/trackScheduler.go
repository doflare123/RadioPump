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
	ErrQueueChanged    = errors.New("библиотека изменилась во время обновления очереди")
)

type Scheduler interface {
	RegisterStation(id string, tags []string) error
	NextTrackID(stationID string) (uint, error)
	MarkDirty(stationID string) error
	MarkAllDirty()
	QueueSnapshot(stationID string) (StationSnapshot, error)
	CurrentTrackID(stationID string) (uint, error)
	ClearCurrent(stationID string)
}

// scheduler защищает быстрые операции общим mutex. Выбор следующего трека
// сериализован отдельно для каждой волны, а SQL выполняется без общего mutex.
type scheduler struct {
	repo     repository.SchedulerRepository
	mu       sync.RWMutex
	stations map[string]*stationSchedule
}

type stationSchedule struct {
	nextMu    sync.Mutex
	tags      []string
	queue     []uint
	available []uint
	current   uint
	version   uint64
	loaded    uint64
}

// StationSnapshot содержит выбранный трек и очередь, но не позицию аудиоплеера.
type StationSnapshot struct {
	StationID string
	CurrentID uint
	Queue     []uint
}

// NewScheduler создаёт пустой реестр без фоновых задач.
func NewScheduler(repo repository.SchedulerRepository) Scheduler {
	return &scheduler{repo: repo, stations: make(map[string]*stationSchedule)}
}

// RegisterStation не требует музыки при старте; первый worker загрузит кандидатов.
func (s *scheduler) RegisterStation(id string, tags []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.stations[id]; exists {
		return errors.New("станция уже зарегистрирована")
	}
	s.stations[id] = &stationSchedule{tags: append([]string(nil), tags...), version: 1}
	return nil
}

// NextTrackID отбрасывает результат SQL, если во время запроса пришла
// инвалидизация. Ограничение повторов предотвращает зависание при непрерывных edits.
func (s *scheduler) NextTrackID(id string) (uint, error) {
	s.mu.RLock()
	st := s.stations[id]
	s.mu.RUnlock()
	if st == nil {
		return 0, ErrStationNotFound
	}
	st.nextMu.Lock()
	defer st.nextMu.Unlock()
	for attempt := 0; attempt < 3; attempt++ {
		s.mu.Lock()
		version := st.version
		refill := len(st.queue) == 0 || st.loaded != version
		preserve := len(st.queue) > 0
		if !refill {
			next := takeNext(st)
			s.mu.Unlock()
			return next, nil
		}
		s.mu.Unlock()
		tracks, err := s.repo.GetMusic(st.tags)
		if err != nil {
			return 0, err
		}
		ids := make([]uint, 0, len(tracks))
		for _, track := range tracks {
			ids = append(ids, track.ID)
		}
		s.mu.Lock()
		if st.version != version {
			s.mu.Unlock()
			continue
		}
		st.queue = refreshedQueue(st.queue, ids, st.current, preserve)
		st.available = ids
		st.loaded = version
		if len(st.queue) == 0 {
			st.current = 0
			s.mu.Unlock()
			return 0, ErrNoTracks
		}
		next := takeNext(st)
		s.mu.Unlock()
		return next, nil
	}
	return 0, ErrQueueChanged
}

// takeNext вызывается под mutex и отделяет выбор ID от подготовки кандидатов.
func takeNext(st *stationSchedule) uint {
	id := st.queue[0]
	st.queue = st.queue[1:]
	st.current = id
	// Планируем следующий цикл заранее, сохраняя опубликованные пять позиций.
	// При маленькой библиотеке будущие повторы честно видны слушателю.
	for len(st.queue) < 5 && len(st.available) > 0 {
		last := id
		if len(st.queue) > 0 {
			last = st.queue[len(st.queue)-1]
		}
		st.queue = append(st.queue, refreshedQueue(nil, st.available, last, false)...)
	}
	return id
}

// refreshedQueue сохраняет подходящие старые позиции, добавляя новые случайно.
// При полном цикле избегает немедленного повтора, если доступен другой трек.
func refreshedQueue(old, fresh []uint, current uint, preserve bool) []uint {
	available := make(map[uint]bool, len(fresh))
	for _, id := range fresh {
		available[id] = true
	}
	var queue []uint
	if preserve {
		for _, id := range old {
			if available[id] {
				queue = append(queue, id)
				delete(available, id)
			}
		}
		// Текущий трек уже сыгран в этом цикле; не добавляем его повторно.
		delete(available, current)
	}
	var added []uint
	for _, id := range fresh {
		if available[id] {
			added = append(added, id)
			delete(available, id)
		}
	}
	rand.Shuffle(len(added), func(i, j int) { added[i], added[j] = added[j], added[i] })
	queue = append(queue, added...)
	// Если после фильтрации остался лишь текущий трек, его можно играть снова.
	if len(queue) == 0 && len(fresh) > 0 {
		queue = append(queue, fresh...)
	}
	if len(queue) > 1 && queue[0] == current {
		queue[0], queue[1] = queue[1], queue[0]
	}
	return queue
}

// MarkDirty увеличивает поколение; старый SQL-результат не может его сбросить.
func (s *scheduler) MarkDirty(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if st := s.stations[id]; st != nil {
		st.version++
		return nil
	}
	return ErrStationNotFound
}

// MarkAllDirty вызывается после фиксации CRUD библиотеки в БД.
func (s *scheduler) MarkAllDirty() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, st := range s.stations {
		st.version++
	}
}

// ClearCurrent убирает устаревший ID при ошибке, пустой очереди и остановке эфира.
func (s *scheduler) ClearCurrent(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if st := s.stations[id]; st != nil {
		st.current = 0
	}
}

// QueueSnapshot возвращает независимую копию, не раскрывая внутренние slices.
func (s *scheduler) QueueSnapshot(id string) (StationSnapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st := s.stations[id]
	if st == nil {
		return StationSnapshot{}, ErrStationNotFound
	}
	return StationSnapshot{StationID: id, CurrentID: st.current, Queue: append([]uint(nil), st.queue...)}, nil
}

// CurrentTrackID сохраняет небольшой контракт чтения выбранного scheduler-ом ID.
func (s *scheduler) CurrentTrackID(id string) (uint, error) {
	snapshot, err := s.QueueSnapshot(id)
	return snapshot.CurrentID, err
}
