package source

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// FetchSubscribe reads a file of URLs (one per line) and parses each as m3u or txt.
// Returns a flat list of channels.
func FetchSubscribe(filePath string) ([]Channel, error) {
	if filePath == "" {
		return nil, nil
	}
	f, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("subscribe: open %s: %w", filePath, err)
	}
	defer f.Close()

	var channels []Channel
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fmt.Printf("[subscribe] fetching: %s\n", line)
		chs, err := fetchURL(line)
		if err != nil {
			fmt.Printf("[subscribe] skip %s: %v\n", line, err)
			continue
		}
		channels = append(channels, chs...)
	}
	return channels, scanner.Err()
}

func fetchURL(rawURL string) ([]Channel, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(rawURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	content := string(body)
	if strings.HasPrefix(strings.TrimSpace(content), "#EXTM3U") {
		return parseM3U(content), nil
	}
	return parseTxt(content), nil
}

// parseM3U parses #EXTINF lines followed by a URL.
func parseM3U(content string) []Channel {
	var channels []Channel
	var pendingName string
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#EXTINF") {
			// extract name after last comma
			if idx := strings.LastIndex(line, ","); idx >= 0 {
				pendingName = strings.TrimSpace(line[idx+1:])
			}
		} else if line != "" && !strings.HasPrefix(line, "#") && pendingName != "" {
			channels = append(channels, Channel{Name: pendingName, URL: line})
			pendingName = ""
		}
	}
	return channels
}

// parseTxt parses "name,url" lines.
func parseTxt(content string) []Channel {
	var channels []Channel
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, ",", 2)
		if len(parts) == 2 {
			name := strings.TrimSpace(parts[0])
			u := strings.TrimSpace(parts[1])
			if name != "" && u != "" {
				channels = append(channels, Channel{Name: name, URL: u})
			}
		}
	}
	return channels
}
