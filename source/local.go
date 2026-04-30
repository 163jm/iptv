package source

import (
	"fmt"
	"os"
	"strings"
)

// FetchLocal reads a local m3u or txt file and returns channels.
func FetchLocal(filePath string) ([]Channel, error) {
	if filePath == "" {
		return nil, nil
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("local: read %s: %w", filePath, err)
	}
	content := string(data)
	var channels []Channel
	if strings.HasPrefix(strings.TrimSpace(content), "#EXTM3U") {
		channels = parseM3U(content)
	} else {
		channels = parseTxt(content)
	}
	fmt.Printf("[local] loaded %d channels from %s\n", len(channels), filePath)
	return channels, nil
}
