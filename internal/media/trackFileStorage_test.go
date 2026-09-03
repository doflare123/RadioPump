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

func TestTrackFileStorageAcceptsSupportedContainerHeaders(t *testing.T) {
	tests := []struct {
		name   string
		header []byte
		format string
	}{
		{"track.flac", []byte("fLaCmetadata"), "flac"},
		{"track.mp2", []byte{0xff, 0xfd, 0x90, 0}, "mp3"},
		{"track.ogg", []byte("OggSmetadata"), "ogg"},
		{"track.opus", []byte("OggSmetadata"), "ogg"},
		{"track.m4a", append([]byte{0, 0, 0, 24}, []byte("ftypM4A metadata")...), "m4a"},
		{"track.aac", []byte{0xff, 0xf1, 0x50, 0x80}, "aac"},
		{"track.aiff", []byte("FORMxxxxAIFFmetadata"), "aiff"},
		{"track.wma", []byte{0x30, 0x26, 0xb2, 0x75, 0x8e, 0x66, 0xcf, 0x11, 0xa6, 0xd9, 0, 0xaa, 0, 0x62, 0xce, 0x6c}, "asf"},
		{"track.ape", []byte("MAC metadata"), "ape"},
		{"track.wv", []byte("wvpkmetadata"), "wv"},
		{"track.mka", []byte{0x1a, 0x45, 0xdf, 0xa3}, "matroska"},
		{"track.mpc", []byte("MPCKmetadata"), "musepack"},
		{"track.dsf", []byte("DSD metadata"), "dsf"},
		{"track.dff", []byte("FRM8xxxx....DSD metadata"), "dsdiff"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			storage, err := NewTrackFileStorage(t.TempDir(), 1024)
			if err != nil {
				t.Fatal(err)
			}
			saved, err := storage.Save(bytes.NewReader(tt.header), tt.name)
			if err != nil {
				t.Fatalf("Save() error = %v", err)
			}
			if saved.Format != tt.format {
				t.Fatalf("Format = %q, want %q", saved.Format, tt.format)
			}
		})
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
