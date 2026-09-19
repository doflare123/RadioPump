package scheduler

import (
	"sort"
	"time"
)

// TrackFailure хранится в RAM отдельно для каждой волны. Причина предназначена
// только администратору: сообщения ОС и FFmpeg могут содержать локальные пути.
type TrackFailure struct {
	TrackID  uint      `json:"track_id"`
	Attempts int       `json:"attempts"`
	Reason   string    `json:"reason"`
	FailedAt time.Time `json:"failed_at"`
	RetryAt  time.Time `json:"retry_at"`
}

// TrackFailed исключает все повторы ID из очереди и следующих циклов. CRUD
// не снимает карантин; только успешное полное воспроизведение сбрасывает счётчик.
func (s *scheduler) TrackFailed(stationID string, trackID uint, reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.stations[stationID]
	if st == nil {
		return
	}
	f := st.failures[trackID]
	f.TrackID = trackID
	f.Attempts = min(f.Attempts+1, 31)
	f.Reason = string([]rune(reason)[:min(len([]rune(reason)), 4096)])
	f.FailedAt = s.now().UTC()
	f.RetryAt = f.FailedAt.Add(time.Minute * time.Duration(min(1<<min(f.Attempts-1, 5), 30)))
	st.failures[trackID] = f
	st.queue = withoutTrack(st.queue, trackID)
	st.available = withoutTrack(st.available, trackID)
	st.version++
}

// withoutTrack удаляет также повторы будущих циклов, сохраняя порядок остальных.
func withoutTrack(ids []uint, excluded uint) []uint {
	result := ids[:0]
	for _, id := range ids {
		if id != excluded {
			result = append(result, id)
		}
	}
	return result
}

// TrackSucceeded вызывается после полного трека, а не первого аудиочанка.
func (s *scheduler) TrackSucceeded(stationID string, trackID uint) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if st := s.stations[stationID]; st != nil {
		delete(st.failures, trackID)
	}
}

// Failures возвращает независимую, упорядоченную диагностику для админского API.
func (s *scheduler) Failures(stationID string) []TrackFailure {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := []TrackFailure{}
	if st := s.stations[stationID]; st != nil {
		for _, f := range st.failures {
			result = append(result, f)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].TrackID < result[j].TrackID })
	return result
}
