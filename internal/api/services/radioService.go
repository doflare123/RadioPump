package services

import (
	"RadioPump/internal/transcoder"
	"sync"
	"time"
)

type RadioStateReader interface {
	Snapshots([]string) ([]transcoder.RadioSnapshot, error)
}

// RadioDiagnosticsReader отделяет защищённые причины ошибок от публичного кеша.
type RadioDiagnosticsReader interface {
	Diagnostics([]string) []transcoder.StationDiagnostics
}

// Diagnostics читает оперативную диагностику без SQL и без публикации в State.
func (s *RadioService) Diagnostics() []transcoder.StationDiagnostics {
	if reader, ok := s.reader.(RadioDiagnosticsReader); ok {
		return reader.Diagnostics(s.ids)
	}
	return []transcoder.StationDiagnostics{}
}

type RadioService struct {
	reader    RadioStateReader
	ids       []string
	mu        sync.Mutex
	cached    []transcoder.RadioSnapshot
	cachedErr error
	expires   time.Time
	now       func() time.Time
}
type RadioState struct {
	ServerTime time.Time                  `json:"server_time"`
	Stations   []transcoder.RadioSnapshot `json:"stations"`
}

// Порядок конфигурации сохраняется; HTTP получает единый снимок для всех карточек.
func NewRadioService(reader RadioStateReader, ids []string) *RadioService {
	return &RadioService{reader: reader, ids: append([]string{}, ids...), now: time.Now}
}

// State обновляет общий кеш не чаще раза в две секунды при наличии запросов.
// Mutex объединяет параллельные промахи, включая ошибки; блокировки эфира
// этим сервисом не удерживаются. Время сервера остаётся актуальным для часов UI.
func (s *RadioService) State() (RadioState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.now().Before(s.expires) {
		s.cached, s.cachedErr = s.reader.Snapshots(s.ids)
		s.expires = s.now().Add(2 * time.Second)
	}
	state := RadioState{ServerTime: s.now().UTC(), Stations: []transcoder.RadioSnapshot{}}
	if s.cachedErr != nil {
		return state, s.cachedErr
	}
	// Копии защищают кеш от изменения slices и Current потребителем ответа.
	for _, station := range s.cached {
		station.Tags = append([]string{}, station.Tags...)
		station.History = append([]transcoder.RadioTrack{}, station.History...)
		station.Queue = append([]transcoder.RadioTrack{}, station.Queue...)
		if station.Current != nil {
			current := *station.Current
			station.Current = &current
		}
		state.Stations = append(state.Stations, station)
	}
	return state, nil
}
