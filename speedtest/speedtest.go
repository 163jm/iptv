package speedtest

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"iptv/source"
)

const (
	hostTimeout   = 15 * time.Second
	batchSize     = 60
	hsmdTestURI   = "/newlive/live/hls/1/live.m3u8"
	zhgxtvIface   = "/ZHGXTV/Public/json/live_interface.txt"
)

// RunSpeedTests tests all hosts from apiItems concurrently and returns
// SourceResults with Speed > 0.
func RunSpeedTests(apiItems []map[string]interface{}, workers int) []source.SourceResult {
	type workItem struct {
		item  map[string]interface{}
		speed float64
	}

	total := len(apiItems)
	completed, valid := 0, 0
	var mu sync.Mutex
	var results []source.SourceResult

	printProgress := func() {
		barWidth := 30
		ratio := float64(completed) / float64(total)
		filled := int(float64(barWidth) * ratio)
		bar := strings.Repeat("=", filled) + strings.Repeat("-", barWidth-filled)
		fmt.Printf("\r测速进度 [%s] %d/%d (%.1f%%) 有效源: %d", bar, completed, total, ratio*100, valid)
	}
	printProgress()

	for i := 0; i < len(apiItems); i += batchSize {
		end := i + batchSize
		if end > len(apiItems) {
			end = len(apiItems)
		}
		batch := apiItems[i:end]

		sem := make(chan struct{}, workers)
		resCh := make(chan workItem, len(batch))
		var wg sync.WaitGroup

		for _, item := range batch {
			item := item
			wg.Add(1)
			sem <- struct{}{}
			go func() {
				defer wg.Done()
				defer func() { <-sem }()
				spd, _ := testHost(item, false)
				resCh <- workItem{item: item, speed: spd}
			}()
		}
		go func() { wg.Wait(); close(resCh) }()

		for r := range resCh {
			mu.Lock()
			completed++
			if r.speed > 0 {
				valid++
				host, _ := r.item["host"].(string)
				matchType, _ := r.item["matchType"].(string)
				src, _ := r.item["source"].(string)
				results = append(results, source.SourceResult{
					Host:      host,
					MatchType: matchType,
					Origin:    "api",
					Source:    src,
					Speed:     r.speed,
					Channels:  []source.Channel{},
				})
			}
			printProgress()
			mu.Unlock()
		}
	}
	fmt.Println()
	return results
}

// FetchChannels populates the Channels field of a SourceResult.
func FetchChannels(sr *source.SourceResult, hsmdFile string) {
	switch sr.MatchType {
	case "txiptv", "jsmpeg", "zhgxtv":
		_, chs := testHost(map[string]interface{}{
			"host":      sr.Host,
			"matchType": sr.MatchType,
		}, true)
		sr.Channels = chs
	case "hsmdtv":
		sr.Channels = loadHsmdChannels(sr.Host, hsmdFile)
	}
}

// ── internal helpers ─────────────────────────────────────────────

func testHost(item map[string]interface{}, fetchChannels bool) (float64, []source.Channel) {
	host, _ := item["host"].(string)
	matchType, _ := item["matchType"].(string)
	if host == "" {
		return -1, nil
	}

	deadline := time.Now().Add(hostTimeout)
	timedOut := func() bool { return time.Now().After(deadline) }

	switch matchType {
	case "txiptv":
		return testTxiptv(host, deadline, timedOut, fetchChannels)
	case "hsmdtv":
		return testHsmdtv(host, deadline, timedOut)
	case "jsmpeg":
		return testJsmpeg(host, deadline, timedOut, fetchChannels)
	case "zhgxtv":
		return testZhgxtv(host, deadline, timedOut, fetchChannels)
	}
	return -1, nil
}

func remaining(deadline time.Time, fallback time.Duration) time.Duration {
	if deadline.IsZero() {
		return fallback
	}
	r := time.Until(deadline)
	if r <= 0 {
		return 0
	}
	if r < fallback {
		return r
	}
	return fallback
}

func getTsURL(m3u8URL string, deadline time.Time) string {
	t := remaining(deadline, 5*time.Second)
	if t <= 0 {
		return ""
	}
	resp, err := (&http.Client{Timeout: t}).Get(m3u8URL)
	if err != nil || resp.StatusCode != 200 {
		return ""
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	parsed, _ := url.Parse(m3u8URL)
	origin := parsed.Scheme + "://" + parsed.Host
	base := m3u8URL[:strings.LastIndex(m3u8URL, "/")+1]

	for _, line := range strings.Split(strings.TrimSpace(string(body)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		switch {
		case strings.HasPrefix(line, "http"):
			return line
		case strings.HasPrefix(line, "/"):
			return origin + line
		default:
			return base + line
		}
	}
	return ""
}

func downloadSpeed(streamURL string, deadline time.Time) float64 {
	t := remaining(deadline, 10*time.Second)
	if t <= 0 {
		return -1
	}
	start := time.Now()
	resp, err := (&http.Client{Timeout: t}).Get(streamURL)
	if err != nil || resp.StatusCode >= 400 {
		return -1
	}
	defer resp.Body.Close()

	var size int64
	buf := make([]byte, 8192)
	limit := int64(10 * 1024 * 1024)
	for {
		n, err := resp.Body.Read(buf)
		size += int64(n)
		if size > limit || time.Since(start) > 8*time.Second || (!deadline.IsZero() && time.Now().After(deadline)) {
			break
		}
		if err != nil {
			break
		}
	}
	dur := time.Since(start).Seconds()
	if dur == 0 {
		dur = 0.001
	}
	return float64(size) / 1024 / 1024 / dur
}

func testTxiptv(host string, deadline time.Time, timedOut func() bool, fetchCh bool) (float64, []source.Channel) {
	if timedOut() {
		return -1, nil
	}
	t := remaining(deadline, 2*time.Second)
	if t <= 0 {
		return -1, nil
	}
	jsonURL := fmt.Sprintf("http://%s/iptv/live/1000.json?key=txiptv", host)
	resp, err := (&http.Client{Timeout: t}).Get(jsonURL)
	if err != nil || resp.StatusCode != 200 {
		return -1, nil
	}
	var data map[string]interface{}
	_ = json.NewDecoder(resp.Body).Decode(&data)
	resp.Body.Close()

	var channels []source.Channel
	var firstURL string
	if arr, ok := data["data"].([]interface{}); ok {
		for _, d := range arr {
			ch, ok := d.(map[string]interface{})
			if !ok {
				continue
			}
			name, _ := ch["name"].(string)
			rawURL, _ := ch["url"].(string)
			if name == "" || rawURL == "" || strings.Contains(rawURL, ",") {
				continue
			}
			var full string
			switch {
			case strings.Contains(rawURL, "http"):
				full = rawURL
			case strings.HasPrefix(rawURL, "/"):
				full = "http://" + host + rawURL
			default:
				full = "http://" + host + "/" + rawURL
			}
			if fetchCh {
				channels = append(channels, source.Channel{Name: name, URL: full})
			}
			if firstURL == "" {
				firstURL = full
			}
		}
	}
	if firstURL == "" {
		return -1, channels
	}
	if timedOut() {
		return -1, channels
	}
	ts := getTsURL(firstURL, deadline)
	if ts == "" {
		return -1, channels
	}
	return downloadSpeed(ts, deadline), channels
}

func testHsmdtv(host string, deadline time.Time, timedOut func() bool) (float64, []source.Channel) {
	if timedOut() {
		return -1, nil
	}
	testURL := fmt.Sprintf("http://%s%s", host, hsmdTestURI)
	ts := getTsURL(testURL, deadline)
	if ts == "" {
		return -1, nil
	}
	return downloadSpeed(ts, deadline), nil
}

func testJsmpeg(host string, deadline time.Time, timedOut func() bool, fetchCh bool) (float64, []source.Channel) {
	if timedOut() {
		return -1, nil
	}
	t := remaining(deadline, 2*time.Second)
	if t <= 0 {
		return -1, nil
	}
	resp, err := (&http.Client{Timeout: t}).Get(fmt.Sprintf("http://%s/streamer/list", host))
	if err != nil || resp.StatusCode != 200 {
		return -1, nil
	}
	var list []map[string]interface{}
	_ = json.NewDecoder(resp.Body).Decode(&list)
	resp.Body.Close()

	var channels []source.Channel
	var firstURL string
	for _, d := range list {
		name, _ := d["name"].(string)
		key, _ := d["key"].(string)
		name = strings.TrimSpace(name)
		key = strings.TrimSpace(key)
		if name == "" || key == "" {
			continue
		}
		full := fmt.Sprintf("http://%s/hls/%s/index.m3u8", host, key)
		if fetchCh {
			channels = append(channels, source.Channel{Name: name, URL: full})
		}
		if firstURL == "" {
			firstURL = full
		}
	}
	if firstURL == "" {
		return -1, channels
	}
	if timedOut() {
		return -1, channels
	}
	ts := getTsURL(firstURL, deadline)
	if ts == "" {
		return -1, channels
	}
	return downloadSpeed(ts, deadline), channels
}

func testZhgxtv(host string, deadline time.Time, timedOut func() bool, fetchCh bool) (float64, []source.Channel) {
	if timedOut() {
		return -1, nil
	}
	t := remaining(deadline, 5*time.Second)
	if t <= 0 {
		return -1, nil
	}
	resp, err := (&http.Client{Timeout: t}).Get(fmt.Sprintf("http://%s%s", host, zhgxtvIface))
	if err != nil || resp.StatusCode != 200 {
		return -1, nil
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	var channels []source.Channel
	var firstURL string
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if !strings.Contains(line, ",") {
			continue
		}
		parts := strings.SplitN(line, ",", 2)
		if len(parts) < 2 {
			continue
		}
		name := strings.TrimSpace(parts[0])
		urlPart := strings.TrimSpace(parts[1])
		var full string
		if strings.HasPrefix(urlPart, "http") {
			p, err := url.Parse(urlPart)
			if err != nil {
				continue
			}
			full = p.Scheme + "://" + host + p.Path
			if p.RawQuery != "" {
				full += "?" + p.RawQuery
			}
		} else if strings.HasPrefix(urlPart, "/") {
			full = "http://" + host + urlPart
		} else {
			full = "http://" + host + "/" + urlPart
		}
		if fetchCh {
			channels = append(channels, source.Channel{Name: name, URL: full})
		}
		if firstURL == "" {
			firstURL = full
		}
	}
	if firstURL == "" {
		return -1, channels
	}
	if timedOut() {
		return -1, channels
	}
	ts := getTsURL(firstURL, deadline)
	if ts == "" {
		return -1, channels
	}
	return downloadSpeed(ts, deadline), channels
}

func loadHsmdChannels(host, hsmdFile string) []source.Channel {
	if hsmdFile == "" {
		return nil
	}
	data, err := os.ReadFile(hsmdFile)
	if err != nil {
		return nil
	}
	// reuse channel cleaner logic via raw parse; cleaning happens in channel package
	var channels []source.Channel
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// find http URL in line
		idx := strings.Index(line, "http://")
		if idx < 0 {
			idx = strings.Index(line, "https://")
		}
		if idx < 0 {
			continue
		}
		rawURL := strings.Fields(line[idx:])[0]
		namePart := strings.TrimSpace(line[:idx])
		// strip leading number
		if i := strings.IndexFunc(namePart, func(r rune) bool { return r < '0' || r > '9' }); i > 0 {
			namePart = strings.TrimSpace(namePart[i:])
		}
		namePart = strings.ReplaceAll(namePart, "（默认频道）", "")
		namePart = strings.TrimSpace(namePart)

		p, err := url.Parse(rawURL)
		if err != nil {
			continue
		}
		full := "http://" + host + p.Path
		channels = append(channels, source.Channel{Name: namePart, URL: full})
	}
	return channels
}
