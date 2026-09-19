package transcoder

import (
	"RadioPump/internal/models"
	"RadioPump/internal/scheduler"
	"time"
)

// StationDiagnostics доступен только через защищённый API; чтение памяти не
// зависит от работоспособности SQL и сохраняет объяснение системного сбоя.
type StationDiagnostics struct {
	ID        string                   `json:"id"`
	Status    string                   `json:"status"`
	LastError string                   `json:"last_error"`
	Failures  []scheduler.TrackFailure `json:"failures"`
}

// Diagnostics копирует причины ошибок без удержания mutex при HTTP-записи.
func (e *PlaybackEngine) Diagnostics(ids []string) []StationDiagnostics {
	result := []StationDiagnostics{}
	if e == nil {
		return result
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	for _, id := range ids {
		if s := e.stations[id]; s != nil {
			s.mu.Lock()
			d := StationDiagnostics{ID: id, Status: s.status, LastError: s.lastError}
			s.mu.Unlock()
			d.Failures = e.scheduler.Failures(id)
			result = append(result, d)
		}
	}
	return result
}

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
	Status    string       `json:"status"`
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
	s.status = "playing"
	s.lastError = ""
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
	if s.status == "playing" {
		s.status = "starting"
	}
	s.startedAt = time.Time{}
}

// Snapshot сохраняет одиночное чтение для внутренних потребителей.
func (e *PlaybackEngine) Snapshot(id string) (RadioSnapshot, error) {
	states, err := e.Snapshots([]string{id})
	if err != nil {
		return RadioSnapshot{}, err
	}
	return states[0], nil
}

// Snapshots копирует состояние всех волн и получает метаданные очередей одним
// пакетным вызовом вне блокировок эфира. Порядок и повторы позиций сохраняются;
// удалённые ID пропускаются, ошибка БД возвращается вызывающему сервису.
func (e *PlaybackEngine) Snapshots(ids []string) ([]RadioSnapshot, error) {
	states := make([]RadioSnapshot, 0, len(ids))
	queues := make([][]uint, 0, len(ids))
	trackIDs := []uint{}
	seen := make(map[uint]bool)
	for _, id := range ids {
		state, queue, err := e.snapshotIDs(id)
		if err != nil {
			return nil, err
		}
		states = append(states, state)
		queues = append(queues, queue)
		for _, trackID := range queue {
			if !seen[trackID] {
				seen[trackID] = true
				trackIDs = append(trackIDs, trackID)
			}
		}
	}
	if len(trackIDs) == 0 {
		return states, nil
	}
	tracks, err := e.repo.GetRadioTracks(trackIDs)
	if err != nil {
		return nil, err
	}
	for i, queue := range queues {
		for _, id := range queue {
			if track, ok := tracks[id]; ok {
				states[i].Queue = append(states[i].Queue, radioTrack(&track))
			}
		}
	}
	return states, nil
}

// snapshotIDs снимает только состояние памяти, не выполняя SQL под mutex станции.
func (e *PlaybackEngine) snapshotIDs(id string) (RadioSnapshot, []uint, error) {
	if e == nil {
		return RadioSnapshot{}, nil, ErrStationNotFound
	}
	e.mu.RLock()
	s := e.stations[id]
	e.mu.RUnlock()
	if s == nil {
		return RadioSnapshot{}, nil, ErrStationNotFound
	}
	s.mu.Lock()
	result := RadioSnapshot{ID: id, Status: s.status, Tags: append([]string{}, s.tags...), StartedAt: s.startedAt, History: append([]RadioTrack{}, s.history...), Queue: []RadioTrack{}}
	if s.current != nil {
		t := *s.current
		result.Current = &t
	}
	queue, err := e.scheduler.QueueSnapshot(id)
	s.mu.Unlock()
	if err != nil {
		return result, nil, err
	}
	return result, queue.Queue[:min(5, len(queue.Queue))], nil
}
