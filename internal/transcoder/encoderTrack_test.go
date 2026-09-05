package transcoder

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestMain также запускает управляемый subprocess для проверки зависшего encoder-а.
// Он принимает любые ffmpeg-аргументы и намеренно не выдаёт stdout.
func TestMain(m *testing.M) {
	if os.Getenv("RADIOPUMP_TEST_ENCODER") == "stall" {
		_, _ = os.Stderr.Write(bytes.Repeat([]byte("decode error\n"), 10000))
		time.Sleep(time.Hour)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// fixture синтезирует короткое аудио, не обращаясь к музыкальной библиотеке владельца.
func audioFixture(t *testing.T, extension, duration string) string {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg unavailable")
	}
	path := filepath.Join(t.TempDir(), "tone."+extension)
	cmd := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "sine=frequency=440:duration="+duration, "-y", path)
	if data, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fixture: %v: %s", err, data)
	}
	return path
}

// WAV/FLAC/MP3 последовательно образуют декодируемый общий MP3-поток без ID3
// между треками. Проверяем реальные байты FFmpeg, а не ответы fake streamer-а.
func TestFFmpegTrackTransitionsDecode(t *testing.T) {
	encoder := NewEncoder("ffmpeg", "128k", 44100)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var stream bytes.Buffer
	for _, extension := range []string{"wav", "flac", "mp3"} {
		path := audioFixture(t, extension, "0.25")
		chunks := make(chan []byte, 32)
		started := time.Now()
		if err := encoder.StreamTrack(ctx, path, chunks); err != nil {
			t.Fatal(err)
		}
		close(chunks)
		elapsed := time.Since(started)
		bytesSent := 0
		first := true
		for chunk := range chunks {
			bytesSent += len(chunk)
			if first && bytes.HasPrefix(chunk, []byte("ID3")) {
				t.Fatal("track starts with ID3 inside live stream")
			}
			first = false
			stream.Write(chunk)
		}
		// Даже короткий трек не выдаётся burst-ом: его MP3-длительность
		// ограничивает темп, поэтому циклы не наращивают отставание клиентов.
		audioDuration := time.Duration(float64(bytesSent) * 8 / 128000 * float64(time.Second))
		if elapsed+10*time.Millisecond < audioDuration {
			t.Fatalf("%s emitted too fast: %s for %s of audio", extension, elapsed, audioDuration)
		}
	}
	cmd := exec.CommandContext(ctx, "ffmpeg", "-hide_banner", "-loglevel", "error", "-f", "mp3", "-i", "pipe:0", "-f", "s16le", "pipe:1")
	cmd.Stdin = bytes.NewReader(stream.Bytes())
	var diagnostics bytes.Buffer
	cmd.Stderr = &diagnostics
	decoded, err := cmd.Output()
	if err != nil || diagnostics.Len() != 0 {
		t.Fatalf("decode transitions: %v: %s", err, diagnostics.String())
	}
	if len(decoded) < 44100*2*2/2 {
		t.Fatalf("too little decoded audio: %d", len(decoded))
	}
}

// Отмена во время реального FFmpeg останавливает процесс, даже если out никто не читает.
func TestFFmpegCancellationWithBlockedOutput(t *testing.T) {
	path := audioFixture(t, "wav", "4")
	encoder := NewEncoder("ffmpeg", "128k", 44100)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	chunks := make(chan []byte)
	done := make(chan error, 1)
	go func() { done <- encoder.StreamTrack(ctx, path, chunks) }()
	select {
	case <-chunks:
	case <-time.After(5 * time.Second):
		t.Fatal("ffmpeg produced no audio")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("canceled encoder succeeded")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("ffmpeg did not stop")
	}
}

// Watchdog завершает subprocess без аудио и ограничивает даже длинный stderr.
func TestEncoderWatchdogAndBoundedErrors(t *testing.T) {
	t.Setenv("RADIOPUMP_TEST_ENCODER", "stall")
	path := filepath.Join(t.TempDir(), "dummy.wav")
	if err := os.WriteFile(path, []byte("dummy"), 0o600); err != nil {
		t.Fatal(err)
	}
	encoder := NewEncoder(os.Args[0], "128k", 44100)
	encoder.StallTimeout = 200 * time.Millisecond
	started := time.Now()
	err := encoder.StreamTrack(context.Background(), path, make(chan []byte))
	if err == nil {
		t.Fatal("stalled encoder succeeded")
	}
	if time.Since(started) > 3*time.Second {
		t.Fatal("watchdog failed to stop subprocess")
	}
	if len(err.Error()) > 17000 {
		t.Fatalf("unbounded diagnostics: %d", len(err.Error()))
	}
}

// io.Copy не должен обходить лимит через случайно унаследованный ReadFrom.
func TestDiagnosticsBoundedThroughIOCopy(t *testing.T) {
	writer := &boundedDiagnostics{limit: 1024}
	n, err := io.Copy(writer, io.LimitReader(strings.NewReader(strings.Repeat("x", 10000)), 10000))
	if err != nil || n != 10000 || writer.Len() != 1024 {
		t.Fatalf("copy = %d, %v; stored = %d", n, err, writer.Len())
	}
}
