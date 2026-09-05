package transcoder

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const defaultChunkSize = 4096

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
	Path         string             // путь к бинарнику ffmpeg; обычно достаточно "ffmpeg" из PATH
	Bitrate      string             // целевой битрейт MP3, например "128k" или "256k"
	SampleRate   int                // целевая частота дискретизации, например 44100 или 48000
	Channels     int                // количество каналов; для радио обычно stereo = 2
	ChunkSize    int                // размер чанка, который читаем из stdout ffmpeg
	StallTimeout time.Duration      // отсутствие выходного аудио ограничивает зависший decode
	ValidatePath func(string) error // корень библиотеки задаёт storage при сборке сервера
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
		Path:         path,
		Bitrate:      bitrate,
		SampleRate:   sampleRate,
		Channels:     2,
		ChunkSize:    defaultChunkSize,
		StallTimeout: 15 * time.Second,
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
	// CBR позволяет ограничить и начальный burst FFmpeg, и последний короткий
	// чанк. Один -re допускает опережение на каждом новом процессе/треке.
	rateText := strings.ToLower(strings.TrimSpace(e.Bitrate))
	multiplier := 1
	if strings.HasSuffix(rateText, "k") {
		multiplier = 1000
		rateText = strings.TrimSuffix(rateText, "k")
	}
	rate, rateErr := strconv.Atoi(rateText)
	if rateErr != nil || rate < 1 || rate > 320000/multiplier || rate*multiplier < 8000 {
		return errors.New("некорректный CBR MP3 bitrate")
	}
	rate *= multiplier
	if inputPath == "" {
		return errors.New("путь к входному аудиофайлу пустой")
	}
	if e.ValidatePath != nil {
		if err := e.ValidatePath(inputPath); err != nil {
			return err
		}
	}
	// Никаких URL, устройств и директорий даже для старых записей библиотеки.
	info, err := os.Stat(inputPath)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("источник эфира должен быть обычным локальным файлом")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stall := e.StallTimeout
	if stall <= 0 {
		stall = 15 * time.Second
	}
	watchdog := time.AfterFunc(stall, cancel)
	defer watchdog.Stop()

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
		"-nostdin",
		"-re",
		"-protocol_whitelist", "file,pipe",
		"-i", inputPath,
		"-map", "0:a:0",
		"-map_metadata", "-1",
		"-vn",
		"-ac", strconv.Itoa(channels),
		"-ar", strconv.Itoa(e.SampleRate),
		"-b:a", e.Bitrate,
		"-f", "mp3",
		"-write_xing", "0",
		"-id3v2_version", "0",
		"pipe:1",
	)
	cmd.WaitDelay = 2 * time.Second

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("не удалось открыть stdout ffmpeg: %w", err)
	}

	// stderr нужен только для диагностики: ffmpeg пишет туда причины ошибок
	// декодирования, отсутствующих кодеков и проблем с файлом.
	stderr := boundedDiagnostics{limit: 16 * 1024}
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("не удалось запустить ffmpeg: %w", err)
	}

	buf := make([]byte, chunkSize)
	wroteAudio := false
	var audioStart time.Time
	var emitted int64
	for {
		n, readErr := io.ReadFull(stdout, buf)
		if n > 0 {
			if !wroteAudio {
				audioStart = time.Now()
				// FFmpeg округляет нестандартный -b:a до допустимого MP3 bitrate.
				// При наличии frame header используем фактическое значение.
				if actual := mp3FrameBitrate(buf[:n]); actual != 0 {
					rate = actual
				}
			}
			wroteAudio = true
			watchdog.Reset(stall)
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
			emitted += int64(n)
			deadline := audioStart.Add(time.Duration(float64(emitted) * 8 / float64(rate) * float64(time.Second)))
			if delay := time.Until(deadline); delay > 0 {
				timer := time.NewTimer(delay)
				select {
				case <-timer.C:
				case <-ctx.Done():
					timer.Stop()
					_ = cmd.Process.Kill()
					_ = cmd.Wait()
					return ctx.Err()
				}
			}
		}

		if errors.Is(readErr, io.EOF) || errors.Is(readErr, io.ErrUnexpectedEOF) {
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
	if ctx.Err() != nil {
		return fmt.Errorf("трансляция отменена или отсутствует аудио дольше %s: %w", stall, ctx.Err())
	}
	if !wroteAudio {
		return errors.New("ffmpeg не выдал аудиоданных")
	}

	return nil
}

// mp3FrameBitrate читает bitrate первого Layer III frame (ID3/Xing отключены).
// MPEG-1 и MPEG-2/2.5 используют разные таблицы; зарезервированные header дают 0.
func mp3FrameBitrate(header []byte) int {
	if len(header) < 4 || header[0] != 0xff || header[1]&0xe0 != 0xe0 || header[1]&6 != 2 {
		return 0
	}
	version := (header[1] >> 3) & 3
	if version == 1 {
		return 0
	}
	index := header[2] >> 4
	if version == 3 {
		return [16]int{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 0}[index] * 1000
	}
	return [16]int{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160, 0}[index] * 1000
}

// boundedDiagnostics принимает весь stderr без блокировки, сохраняя лишь
// ограниченный префикс. Повреждённый файл не может бесконечно наращивать RAM.
type boundedDiagnostics struct {
	buffer bytes.Buffer
	limit  int
}

// Len и String читаются после cmd.Wait, когда writer stderr уже завершён.
func (b *boundedDiagnostics) Len() int       { return b.buffer.Len() }
func (b *boundedDiagnostics) String() string { return b.buffer.String() }

func (b *boundedDiagnostics) Write(p []byte) (int, error) {
	n := len(p)
	if remaining := b.limit - b.Len(); remaining > 0 {
		_, _ = b.buffer.Write(p[:min(remaining, n)])
	}
	return n, nil
}
