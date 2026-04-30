package output

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"iptv/channel"
)

// Writer builds and persists playlist files.
type Writer struct {
	EPGUrl    string
	CacheM3U8 string
	CacheTxt  string
}

func New(epgURL, cacheM3U8, cacheTxt string) *Writer {
	return &Writer{EPGUrl: epgURL, CacheM3U8: cacheM3U8, CacheTxt: cacheTxt}
}

// Write generates both m3u8 and txt and saves them to disk.
// Returns the rendered m3u8 and txt strings.
func (w *Writer) Write(names []string, grouped map[string][]channel.Entry, updateTime time.Time) (m3u8 string, txt string) {
	m3u8 = w.buildM3U8(names, grouped, updateTime)
	txt = w.buildTxt(names, grouped, updateTime)

	if err := os.WriteFile(w.CacheM3U8, []byte(m3u8), 0644); err != nil {
		fmt.Printf("[output] write m3u8: %v\n", err)
	}
	if err := os.WriteFile(w.CacheTxt, []byte(txt), 0644); err != nil {
		fmt.Printf("[output] write txt: %v\n", err)
	}
	return
}

// ReadCache loads previously persisted files from disk.
func (w *Writer) ReadCache() (m3u8 string, txt string) {
	if data, err := os.ReadFile(w.CacheM3U8); err == nil {
		m3u8 = string(data)
	}
	if data, err := os.ReadFile(w.CacheTxt); err == nil {
		txt = string(data)
	}
	return
}

// ── private builders ──────────────────────────────────────────────

func (w *Writer) buildM3U8(names []string, grouped map[string][]channel.Entry, updateTime time.Time) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("#EXTM3U x-tvg-url=%q\n", w.EPGUrl))

	for _, name := range names {
		entries := grouped[name]
		if len(entries) == 0 {
			continue
		}
		entries = dedupeAndSort(entries)
		for _, e := range entries {
			sb.WriteString(e.Content)
			sb.WriteByte('\n')
		}
	}

	// Update entry last
	ts := updateTime.Format("2006-01-02 15:04:05")
	sb.WriteString(fmt.Sprintf(
		"#EXTINF:-1 group-title=\"Update\",更新时间: %s\nhttp://127.0.0.1/\n", ts,
	))
	return sb.String()
}

func (w *Writer) buildTxt(names []string, grouped map[string][]channel.Entry, updateTime time.Time) string {
	var sb strings.Builder
	currentGroup := ""

	for _, name := range names {
		entries := grouped[name]
		if len(entries) == 0 {
			continue
		}
		entries = dedupeAndSort(entries)

		grp := channel.GroupTitle(name)
		if grp != currentGroup {
			if currentGroup != "" {
				sb.WriteByte('\n')
			}
			sb.WriteString(grp + ",#genre#\n")
			currentGroup = grp
		}
		for _, e := range entries {
			sb.WriteString(fmt.Sprintf("%s,%s\n", e.Name, e.URL))
		}
	}

	// Update group last
	ts := updateTime.Format("2006-01-02 15:04:05")
	sb.WriteString("\nUpdate,#genre#\n")
	sb.WriteString(fmt.Sprintf("更新时间: %s,http://127.0.0.1/\n", ts))
	return sb.String()
}

// dedupeAndSort removes duplicate URLs and sorts by source index then speed desc.
func dedupeAndSort(entries []channel.Entry) []channel.Entry {
	seen := map[string]struct{}{}
	var out []channel.Entry
	for _, e := range entries {
		if _, ok := seen[e.URL]; ok {
			continue
		}
		seen[e.URL] = struct{}{}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Index != out[j].Index {
			return out[i].Index < out[j].Index
		}
		return out[i].Speed > out[j].Speed
	})
	return out
}
