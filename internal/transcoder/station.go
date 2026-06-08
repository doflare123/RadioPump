package transcoder

import "sync"

type Station struct {
	id    string
	tags  []StationTag
	input chan []byte
	mu    sync.RWMutex
	subs  map[string]chan []byte
}

type StationTag struct {
	id   uint
	name string
}

type StationRuntime struct {
	StationId uint
	Queue     []uint
	CurrentId uint
	Dirty     bool // надо перечитывать кандидатов из базы
}

func NewStation(id string) *Station {
	s := &Station{
		id:    id,
		input: make(chan []byte, 64),
		subs:  make(map[string]chan []byte),
	}
	go s.run()
	return s
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
