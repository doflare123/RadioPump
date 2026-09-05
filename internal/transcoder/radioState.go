package transcoder

import (
	"RadioPump/internal/models"
	"time"
)

// RadioTrack — публичные сведения без локального пути и бинарных данных обложки.
type RadioTrack struct {
	ID        uint   `json:"id"`
	Title     string `json:"title"`
	Artist    string `json:"artist"`
	Album     string `json:"album"`
	Duration  uint   `json:"duration"`
	CoverURL  string `json:"cover_url"`
	StartedMS int64  `json:"started_ms"`
	EndedMS   int64  `json:"ended_ms"`
}

type RadioSnapshot struct {
	ID        string       `json:"id"`
	Tags      []string     `json:"tags"`
	Current   *RadioTrack  `json:"current"`
	StartedAt time.Time    `json:"started_at"`
	History   []RadioTrack `json:"history"`
	Queue     []RadioTrack `json:"queue"`
}

func radioTrack(t *models.Track) RadioTrack {
	return RadioTrack{ID: t.ID, Title: t.Title, Artist: t.Artist, Album: t.Album, Duration: t.Duration, CoverURL: t.CoverURL}
}

// beginTrack получает только уже начавший выдавать аудио трек. Состояние и fan-out
// используют короткие блокировки; SQL и сетевые записи под ними не выполняются.
func (s *Station) beginTrack(t *models.Track) {
	s.mu.Lock()
	defer s.mu.Unlock()
	track := radioTrack(t)
	s.current = &track
	s.startedAt = time.Now().UTC()
	s.current.StartedMS = s.startedAt.UnixMilli()
}

// finishTrack хранит последние пять фактически звучавших треков, новые первыми.
func (s *Station) finishTrack() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.current != nil {
		s.current.EndedMS = time.Now().UnixMilli()
		s.history = append([]RadioTrack{*s.current}, s.history...)
		s.history = s.history[:min(5, len(s.history))]
	}
	s.current = nil
	s.startedAt = time.Time{}
}

// Snapshot копирует live-состояние. Очередь — реальные запланированные ID;
// исчезнувшие из библиотеки записи не раскрываются и будут пропущены worker-ом.
func (e *PlaybackEngine) Snapshot(id string) (RadioSnapshot, error) {
	if e == nil {
		return RadioSnapshot{}, ErrStationNotFound
	}
	e.mu.RLock()
	s := e.stations[id]
	e.mu.RUnlock()
	if s == nil {
		return RadioSnapshot{}, ErrStationNotFound
	}
	s.mu.Lock()
	result := RadioSnapshot{ID: id, Tags: append([]string{}, s.tags...), StartedAt: s.startedAt, History: append([]RadioTrack{}, s.history...), Queue: []RadioTrack{}}
	if s.current != nil {
		t := *s.current
		result.Current = &t
	}
	queue, err := e.scheduler.QueueSnapshot(id)
	s.mu.Unlock()
	if err != nil {
		return result, err
	}
	for _, trackID := range queue.Queue[:min(5, len(queue.Queue))] {
		t, err := e.repo.GetByID(trackID)
		if err == nil {
			result.Queue = append(result.Queue, radioTrack(t))
		}
	}
	return result, nil
}
