package transcoder

import (
	"RadioPump/internal/repository"
	"RadioPump/internal/scheduler"
	"errors"
	"sync"
)

type Station struct {
	id    string
	input chan []byte
	mu    sync.RWMutex
	subs  map[string]chan []byte
}

type PlaybackEngine struct {
	Stations  map[string]*Station
	repo      repository.TrackRepository
	scheduler scheduler.Scheduler
	streamer  TrackStreamer
}

var ErrStationNotFound = errors.New("станция не найдена")

// type ShortStation struct {
// 	Id   string
// 	tags []uint
// }

func NewPlaybackEngine(
	trackRepo repository.TrackRepository,
	scheduler scheduler.Scheduler,
	streamer TrackStreamer,
) *PlaybackEngine {
	return &PlaybackEngine{
		Stations:  make(map[string]*Station),
		repo:      trackRepo,
		scheduler: scheduler,
		streamer:  streamer,
	}
}

func (e *PlaybackEngine) NewStation(id string, tagsName []string) (*Station, error) {
	s := &Station{
		id:    id,
		input: make(chan []byte, 64),
		subs:  make(map[string]chan []byte),
	}

	e.Stations[id] = s
	err := e.scheduler.RegisterStation(id, tagsName)
	if err != nil {
		return nil, err
	}
	go s.run()
	go e.runStationWorker(id)
	return s, nil
}

func (s *Station) run() {
	for data := range s.input {
		s.broadcast(data)
	}

	s.mu.Lock()
	for id, ch := range s.subs {
		close(ch)
		delete(s.subs, id)
	}
	s.mu.Unlock()
}

func (s *Station) broadcast(data []byte) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, ch := range s.subs {
		msg := make([]byte, len(data))
		copy(msg, data)
		select {
		case ch <- msg:
		default:
		}
	}
}

func (s *Station) Close() {
	close(s.input)
}

func (e *PlaybackEngine) Subscribe(stationID, listenerID string) (<-chan []byte, error) {
	if stationID == "" || listenerID == "" {
		return nil, ErrStationNotFound
	}
	station := e.Stations[stationID]
	if station == nil {
		return nil, ErrStationNotFound
	}
	return station.Subscribe(listenerID), nil
}

func (e *PlaybackEngine) Unsubscribe(stationID, listenerID string) {
	station := e.Stations[stationID]
	if station == nil {
		return
	}
	station.Unsubscribe(listenerID)
}

func (s *Station) Subscribe(listenerId string) <-chan []byte {
	ch := make(chan []byte, 16)
	s.mu.Lock()
	s.subs[listenerId] = ch
	s.mu.Unlock()
	return ch
}

func (s *Station) Unsubscribe(listenerId string) {
	s.mu.Lock()
	if ch, ok := s.subs[listenerId]; ok {
		close(ch)
		delete(s.subs, listenerId)
	}
	s.mu.Unlock()
}

// func (e *PlaybackEngine) GetStationsShort() []ShortStation {
// 	e.Stations = make(map[string]*Station)
// 	stations := make([]ShortStation, 0)
// 	for _, station := range e.Stations {
// 		stations = append(stations, ShortStation{
// 			Id:   (station.id),
// 			tags: station.Tags,
// 		})
// 	}
// 	return stations
// }
