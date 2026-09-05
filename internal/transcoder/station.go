package transcoder

import (
	"RadioPump/internal/repository"
	"RadioPump/internal/scheduler"
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

const maxListenersPerStation = 256

var (
	ErrStationNotFound = errors.New("станция не найдена")
	ErrEngineClosed    = errors.New("эфир остановлен")
	ErrStationExists   = errors.New("станция с таким именем уже существует")
	ErrListenerLimit   = errors.New("достигнут лимит слушателей станции")
)

// Station владеет контекстом producer-а и broadcaster-а. input не закрывается
// потребителем: отмена и ожидание обоих worker-ов исключают send/close race.
type Station struct {
	id           string
	input        chan []byte
	ctx          context.Context
	cancel       context.CancelFunc
	wg           sync.WaitGroup
	mu           sync.Mutex
	subs         map[string]chan []byte
	bufferChunks int
	tags         []string
	current      *RadioTrack
	history      []RadioTrack
	startedAt    time.Time
}

// PlaybackEngine публикует станции после регистрации scheduler-а.
// Workers получают указатель на свою станцию, а не читают изменяемую map.
type PlaybackEngine struct {
	mu           sync.RWMutex
	stations     map[string]*Station
	closed       bool
	closeOnce    sync.Once
	repo         repository.TrackRepository
	scheduler    scheduler.Scheduler
	streamer     TrackStreamer
	bufferChunks int
}

// NewPlaybackEngine создаёт остановимый движок с ограниченным буфером.
func NewPlaybackEngine(repo repository.TrackRepository, sched scheduler.Scheduler, streamer TrackStreamer) *PlaybackEngine {
	return &PlaybackEngine{stations: make(map[string]*Station), repo: repo, scheduler: sched, streamer: streamer, bufferChunks: 20}
}

// ConfigureBuffer переводит секунды CBR MP3 в число чанков фиксированного размера.
// Это очередь будущих данных клиента, не история эфира. Настройка допустима до старта.
func (e *PlaybackEngine) ConfigureBuffer(bitrate string, seconds int) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.stations) != 0 || e.closed {
		return errors.New("буфер настраивается до запуска станций")
	}
	if seconds == 0 {
		seconds = 5
	}
	if seconds < 1 || seconds > 30 {
		return errors.New("buffer_seconds должен быть от 1 до 30")
	}
	value := strings.ToLower(strings.TrimSpace(bitrate))
	multiplier := 1
	if strings.HasSuffix(value, "k") {
		multiplier = 1000
		value = strings.TrimSuffix(value, "k")
	}
	rate, err := strconv.Atoi(value)
	if err != nil || rate < 1 || rate > 320000/multiplier {
		return fmt.Errorf("некорректный MP3 bitrate %q", bitrate)
	}
	rate *= multiplier
	if rate < 8000 {
		return errors.New("MP3 bitrate должен быть от 8k до 320k")
	}
	e.bufferChunks = (rate/8*seconds + defaultChunkSize - 1) / defaultChunkSize
	return nil
}

// NewStation отвергает повторы ID, не перезаписывая уже работающий эфир.
func (e *PlaybackEngine) NewStation(id string, tags []string) (*Station, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return nil, ErrEngineClosed
	}
	if strings.TrimSpace(id) == "" {
		return nil, ErrStationNotFound
	}
	if _, exists := e.stations[id]; exists {
		return nil, ErrStationExists
	}
	if e.streamer == nil {
		return nil, errors.New("не настроен encoder")
	}
	if err := e.scheduler.RegisterStation(id, tags); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Station{id: id, ctx: ctx, cancel: cancel, input: make(chan []byte, 1), subs: make(map[string]chan []byte), bufferChunks: e.bufferChunks}
	s.tags = append([]string{}, tags...)
	e.stations[id] = s
	s.wg.Add(2)
	go func() { defer s.wg.Done(); s.run() }()
	go func() { defer s.wg.Done(); e.runStationWorker(s) }()
	return s, nil
}

// run закрывает подписки на отмене, освобождая HTTP handlers даже при пустой станции.
func (s *Station) run() {
	defer func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		for id, ch := range s.subs {
			closeSubscriber(ch)
			delete(s.subs, id)
		}
	}()
	for {
		select {
		case <-s.ctx.Done():
			return
		case data := <-s.input:
			s.broadcast(data)
		}
	}
}

// broadcast передаёт неизменяемые чанки ограниченного размера. Переполнение
// завершает подписку целиком: продолжать MP3 с пропущенными байтами нельзя.
func (s *Station) broadcast(data []byte) {
	for len(data) > 0 {
		n := min(len(data), defaultChunkSize)
		chunk := append([]byte(nil), data[:n]...)
		data = data[n:]
		s.mu.Lock()
		for id, ch := range s.subs {
			select {
			case ch <- chunk:
			default:
				closeSubscriber(ch)
				delete(s.subs, id)
			}
		}
		s.mu.Unlock()
	}
}

// Close повторяем и возвращается только после освобождения FFmpeg и подписок.
func (s *Station) Close() { s.cancel(); s.wg.Wait() }

// Close сначала отменяет все станции, затем ожидает их параллельную остановку.
func (e *PlaybackEngine) Close() {
	e.closeOnce.Do(func() {
		e.mu.Lock()
		e.closed = true
		stations := make([]*Station, 0, len(e.stations))
		for _, s := range e.stations {
			s.cancel()
			stations = append(stations, s)
		}
		e.mu.Unlock()
		for _, s := range stations {
			s.wg.Wait()
		}
	})
}

// Subscribe проверяет lifecycle и лимит под блокировками владельцев состояния.
func (e *PlaybackEngine) Subscribe(stationID, listenerID string) (<-chan []byte, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.closed {
		return nil, ErrEngineClosed
	}
	s := e.stations[stationID]
	if s == nil || listenerID == "" {
		return nil, ErrStationNotFound
	}
	return s.Subscribe(listenerID)
}

// Unsubscribe безопасен после остановки и автоматического отключения клиента.
func (e *PlaybackEngine) Unsubscribe(stationID, listenerID string) {
	e.mu.RLock()
	s := e.stations[stationID]
	e.mu.RUnlock()
	if s != nil {
		s.Unsubscribe(listenerID)
	}
}

// Subscribe не заменяет канал существующего listenerID: это иначе оставило бы
// первый HTTP handler ждать бесконечно. Идентификаторы выдаёт сервер случайно.
func (s *Station) Subscribe(id string) (<-chan []byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctx.Err() != nil {
		return nil, ErrEngineClosed
	}
	if _, exists := s.subs[id]; exists {
		return nil, errors.New("слушатель уже подключён")
	}
	if len(s.subs) >= maxListenersPerStation {
		return nil, ErrListenerLimit
	}
	ch := make(chan []byte, s.bufferChunks)
	s.subs[id] = ch
	return ch, nil
}

// Unsubscribe закрывает канал ровно один раз под тем же mutex, что и broadcast.
func (s *Station) Unsubscribe(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ch, ok := s.subs[id]; ok {
		closeSubscriber(ch)
		delete(s.subs, id)
	}
}

// closeSubscriber отбрасывает только хвост уже завершённой подписки, затем
// посылает EOF. Иначе handler мог бы ещё минуты дренировать очередь в медленный
// сокет. Вызывается под s.mu; единственная текущая Write ограничена deadline.
func closeSubscriber(ch chan []byte) {
	for {
		select {
		case <-ch:
		default:
			close(ch)
			return
		}
	}
}
