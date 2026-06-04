package media

import (
	"bytes"
	"errors"
	"os"
	"testing"
)

func TestTrackFileStorageSaveValidWAV(t *testing.T) {
	storage, err := NewTrackFileStorage(t.TempDir(), 1024)
	if err != nil {
		t.Fatalf("NewTrackFileStorage() error = %v", err)
	}

	saved, err := storage.Save(bytes.NewReader(minimalWAV()), "../My Song.wav")
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	if saved.BaseName != "My-Song" {
		t.Fatalf("BaseName = %q, want %q", saved.BaseName, "My-Song")
	}
	if saved.Format != "wav" {
		t.Fatalf("Format = %q, want %q", saved.Format, "wav")
	}
	if _, err := os.Stat(saved.AbsolutePath); err != nil {
		t.Fatalf("saved file is not readable: %v", err)
	}
}

func TestTrackFileStorageRejectsTooLargeFile(t *testing.T) {
	storage, err := NewTrackFileStorage(t.TempDir(), 4)
	if err != nil {
		t.Fatalf("NewTrackFileStorage() error = %v", err)
	}

	_, err = storage.Save(bytes.NewReader(minimalWAV()), "track.wav")
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("Save() error = %v, want ErrTooLarge", err)
	}
}

func TestTrackFileStorageRejectsWrongHeader(t *testing.T) {
	storage, err := NewTrackFileStorage(t.TempDir(), 1024)
	if err != nil {
		t.Fatalf("NewTrackFileStorage() error = %v", err)
	}

	_, err = storage.Save(bytes.NewReader([]byte("not audio")), "track.mp3")
	if !errors.Is(err, ErrUnsupportedFormat) {
		t.Fatalf("Save() error = %v, want ErrUnsupportedFormat", err)
	}
}

func minimalWAV() []byte {
	return []byte{
		'R', 'I', 'F', 'F',
		36, 0, 0, 0,
		'W', 'A', 'V', 'E',
		'f', 'm', 't', ' ',
		16, 0, 0, 0,
		1, 0,
		1, 0,
		0x44, 0xAC, 0, 0,
		0x88, 0x58, 1, 0,
		2, 0,
		16, 0,
		'd', 'a', 't', 'a',
		0, 0, 0, 0,
	}
}
