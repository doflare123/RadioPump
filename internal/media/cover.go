package media

import (
	"bytes"
	"errors"
	"image"
	_ "image/jpeg"
	_ "image/png"
)

const MaxCoverBytes = 2 << 20

var ErrInvalidCover = errors.New("обложка должна быть исправным JPEG/PNG до 2 MiB и 2048×2048 пикселей")

// ValidateCover ограничивает сжатый размер и память декодера до чтения пикселей.
// SVG и произвольное активное содержимое не принимаются.
func ValidateCover(data []byte) error {
	if len(data) == 0 || len(data) > MaxCoverBytes {
		return ErrInvalidCover
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "jpeg" && format != "png") || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 2048 || cfg.Height > 2048 {
		return ErrInvalidCover
	}
	if _, _, err := image.Decode(bytes.NewReader(data)); err != nil {
		return ErrInvalidCover
	}
	return nil
}
