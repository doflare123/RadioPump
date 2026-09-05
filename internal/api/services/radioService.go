package services

import (
	"RadioPump/internal/transcoder"
	"time"
)

type RadioStateReader interface {
	Snapshot(string) (transcoder.RadioSnapshot, error)
}
type RadioService struct {
	reader RadioStateReader
	ids    []string
}
type RadioState struct {
	ServerTime time.Time                  `json:"server_time"`
	Stations   []transcoder.RadioSnapshot `json:"stations"`
}

// Порядок конфигурации сохраняется; HTTP получает единый снимок для всех карточек.
func NewRadioService(reader RadioStateReader, ids []string) *RadioService {
	return &RadioService{reader, append([]string{}, ids...)}
}
func (s *RadioService) State() (RadioState, error) {
	state := RadioState{Stations: []transcoder.RadioSnapshot{}}
	for _, id := range s.ids {
		station, err := s.reader.Snapshot(id)
		if err != nil {
			return state, err
		}
		state.Stations = append(state.Stations, station)
	}
	state.ServerTime = time.Now().UTC()
	return state, nil
}
