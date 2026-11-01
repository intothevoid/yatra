package adapters

import (
	"context"
	"encoding/json"
	"os/exec"
)

// ExifToolAdapter calls exiftool CLI to extract metadata.
type ExifToolAdapter struct {
	ExifToolPath string
}

// ExtractEXIF runs exiftool -json and returns a flat map of key-value pairs.
func (a *ExifToolAdapter) ExtractEXIF(ctx context.Context, srcPath string) (map[string]string, error) {
	path := a.ExifToolPath
	if path == "" {
		path = "exiftool"
	}

	cmd := exec.CommandContext(ctx, path, "-json", srcPath)
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	// exiftool -json returns an array of objects
	var results []map[string]any
	if err := json.Unmarshal(output, &results); err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, nil
	}

	// Flatten to string map
	result := make(map[string]string)
	for k, v := range results[0] {
		result[k] = formatValue(v)
	}
	return result, nil
}

func formatValue(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case float64:
		return string(rune(int(val)))
	case []any:
		out, _ := json.Marshal(val)
		return string(out)
	default:
		out, _ := json.Marshal(val)
		return string(out)
	}
}
