package transcoder

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
)

const defaultChunkSize = 32 * 1024

// TrackStreamer задает минимальный контракт для компонента, который умеет
// превратить файл трека в поток байтов и отправить его в канал станции.
//
// PlaybackEngine зависит от этого интерфейса, а не от конкретного Encoder.
// Поэтому позже ffmpeg можно заменить другой реализацией без переписывания
// worker-а и логики станций.
type TrackStreamer interface {
	StreamTrack(ctx context.Context, inputPath string, out chan<- []byte) error
}

// Encoder описывает настройки ffmpeg-процесса, который превращает любой
// поддерживаемый входной аудиофайл в единый MP3-поток для радио.
//
// Encoder не хранит состояние конкретного трека и не держит ffmpeg постоянно
// запущенным.
type Encoder struct {
	Path       string // путь к бинарнику ffmpeg; обычно достаточно "ffmpeg" из PATH
	Bitrate    string // целевой битрейт MP3, например "128k" или "256k"
	SampleRate int    // целевая частота дискретизации, например 44100 или 48000
	Channels   int    // количество каналов; для радио обычно stereo = 2
	ChunkSize  int    // размер чанка, который читаем из stdout ffmpeg
}

// NewEncoder создает encoder с безопасными дефолтами.
// Если конфиг не передал значения, поток будет MP3 48 kHz stereo 256 kbps.
func NewEncoder(path, bitrate string, sampleRate int) *Encoder {
	if path == "" {
		path = "ffmpeg"
	}
	if bitrate == "" {
		bitrate = "256k"
	}
	if sampleRate == 0 {
		sampleRate = 48000
	}

	return &Encoder{
		Path:       path,
		Bitrate:    bitrate,
		SampleRate: sampleRate,
		Channels:   2,
		ChunkSize:  defaultChunkSize,
	}
}

// StreamTrack запускает ffmpeg для одного трека и потоково отправляет MP3-байты
// в канал out. Метод блокируется до конца трека, ошибки ffmpeg или отмены ctx.
//
// Поток данных выглядит так:
//
//	файл трека -> ffmpeg stdout -> chunk []byte -> station.input -> listeners
//
// Важно: метод не возвращает весь трек как []byte. Для радио так нельзя делать:
// большие файлы съедят память, а слушателям нужен живой поток небольшими чанками.
func (e *Encoder) StreamTrack(ctx context.Context, inputPath string, out chan<- []byte) error {
	if inputPath == "" {
		return errors.New("путь к входному аудиофайлу пустой")
	}

	channels := e.Channels
	if channels == 0 {
		channels = 2
	}
	chunkSize := e.ChunkSize
	if chunkSize <= 0 {
		chunkSize = defaultChunkSize
	}

	// -re заставляет ffmpeg читать файл примерно в реальном времени.
	// Без этого ffmpeg может выдать весь трек максимально быстро, что ломает
	// модель live-радио и переполняет буферы раздачи.
	cmd := exec.CommandContext(
		ctx,
		e.Path,
		"-hide_banner",
		"-loglevel", "error",
		"-re",
		"-i", inputPath,
		"-vn",
		"-ac", strconv.Itoa(channels),
		"-ar", strconv.Itoa(e.SampleRate),
		"-b:a", e.Bitrate,
		"-f", "mp3",
		"pipe:1",
	)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("не удалось открыть stdout ffmpeg: %w", err)
	}

	// stderr нужен только для диагностики: ffmpeg пишет туда причины ошибок
	// декодирования, отсутствующих кодеков и проблем с файлом.
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("не удалось запустить ffmpeg: %w", err)
	}

	buf := make([]byte, chunkSize)
	for {
		n, readErr := stdout.Read(buf)
		if n > 0 {
			// Копируем данные в новый slice, потому что buf переиспользуется
			// на следующей итерации. Без копии слушатели могли бы получить
			// измененные байты.
			chunk := make([]byte, n)
			copy(chunk, buf[:n])

			select {
			case out <- chunk:
			case <-ctx.Done():
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
				return fmt.Errorf("трансляция трека отменена: %w", ctx.Err())
			}
		}

		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return fmt.Errorf("не удалось прочитать stdout ffmpeg: %w", readErr)
		}
	}

	if err := cmd.Wait(); err != nil {
		if stderr.Len() > 0 {
			return fmt.Errorf("ffmpeg завершился с ошибкой: %w: %s", err, stderr.String())
		}
		return fmt.Errorf("ffmpeg завершился с ошибкой: %w", err)
	}

	return nil
}
