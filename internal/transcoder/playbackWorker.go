package transcoder

import (
	"RadioPump/internal/models"
	"context"
	"log"
	"time"
)

// runStationWorker продолжает эфир после повреждённого/удалённого файла.
// Серия ошибок увеличивает паузу до 30 секунд, исключая цикл запуска FFmpeg
// на пустой или полностью неисправной библиотеке. Ожидания отменяемы.
func (e *PlaybackEngine) runStationWorker(station *Station) {
	delay := time.Second
	defer e.scheduler.ClearCurrent(station.id)
	defer station.finishTrack()
	for station.ctx.Err() == nil {
		id, err := e.scheduler.NextTrackID(station.id)
		if err == nil {
			track, lookupErr := e.repo.GetByID(id)
			err = lookupErr
			if err == nil {
				err = e.streamLiveTrack(station, track)
			} else {
				_ = e.scheduler.MarkDirty(station.id)
			}
		}
		if station.ctx.Err() != nil {
			return
		}
		if err != nil {
			e.scheduler.ClearCurrent(station.id)
			log.Printf("станция %s: %v; повтор через %s", station.id, err, delay)
			if !waitRetry(station.ctx, delay) {
				return
			}
			delay = min(delay*2, 30*time.Second)
		} else {
			delay = time.Second
		}
	}
}

// streamLiveTrack отмечает начало на первом аудиочанке, а не при выборе SQL ID.
// Отдельный ограниченный канал позволяет дождаться encoder при shutdown и не
// приписывает предыдущему треку данные следующего. Подписки не запускают encoder.
func (e *PlaybackEngine) streamLiveTrack(station *Station, track *models.Track) error {
	chunks := make(chan []byte, 1)
	done := make(chan error, 1)
	go func() {
		done <- e.streamer.StreamTrack(station.ctx, track.Path, chunks)
		close(chunks)
	}()
	started := false
	for chunk := range chunks {
		if len(chunk) == 0 || station.ctx.Err() != nil {
			continue
		}
		if !started {
			station.beginTrack(track)
			started = true
		}
		station.broadcast(chunk)
	}
	err := <-done
	if started {
		station.finishTrack()
	}
	return err
}

// waitRetry заменяет Sleep, чтобы остановка пустой станции не ждала backoff.
func waitRetry(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
