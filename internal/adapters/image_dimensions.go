package adapters

import (
	"image"
	_ "image/jpeg"
	_ "image/png"
	"os"
)

// ImageDimensions reads an image's pixel dimensions without decoding all pixels.
func ImageDimensions(path string) (int, int, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, 0, err
	}
	defer file.Close()
	dimensions, _, err := image.DecodeConfig(file)
	if err != nil {
		return 0, 0, err
	}
	return dimensions.Width, dimensions.Height, nil
}
