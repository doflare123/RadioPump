package transcoder

import (
	"context"
	"log"
	"time"
)

func (e *PlaybackEngine) runStationWorker(stationID string) {
	for {
		trackID, err := e.scheduler.NextTrackID(stationID)
		if err != nil {
			log.Printf("станция %s: не удалось получить следующий трек: %v", stationID, err)
			time.Sleep(time.Second)
			continue
		}

		track, err := e.repo.GetByID(trackID)
		if err != nil {
			log.Printf("станция %s: трек %d не найден: %v", stationID, trackID, err)
			continue
		}

		station := e.Stations[stationID]
		if station == nil {
			log.Printf("станция %s: станция не найдена в playback engine", stationID)
			return
		}
		if e.streamer == nil {
			log.Printf("станция %s: компонент потоковой обработки треков не настроен", stationID)
			time.Sleep(time.Second)
			continue
		}

		if err := e.streamer.StreamTrack(context.Background(), track.Path, station.input); err != nil {
			log.Printf("станция %s: ffmpeg не смог обработать трек %d: %v", stationID, trackID, err)
			time.Sleep(time.Second)
			continue
		}
	}
}
