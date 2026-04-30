package channel

import (
	"fmt"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"iptv/source"
)

// Entry is a single resolved playlist entry.
type Entry struct {
	Name    string
	URL     string
	Content string  // full #EXTINF + URL line
	Index   int     // source priority index
	Speed   float64 // MB/s from speed test (0 for subscribe/local)
}

// ── Name cleaning ─────────────────────────────────────────────────

var (
	reCCTVNum  = regexp.MustCompile(`CCTV(\d+)台`)
	reLeadNum  = regexp.MustCompile(`^\s*\d+\s+`)
)

var nameMap = map[string]string{
	"CCTV1综合": "CCTV1", "CCTV2财经": "CCTV2", "CCTV3综艺": "CCTV3",
	"CCTV4国际": "CCTV4", "CCTV4中文国际": "CCTV4", "CCTV4欧洲": "CCTV4",
	"CCTV5体育": "CCTV5", "CCTV6电影": "CCTV6",
	"CCTV7军事": "CCTV7", "CCTV7军农": "CCTV7", "CCTV7农业": "CCTV7", "CCTV7国防军事": "CCTV7",
	"CCTV8电视剧": "CCTV8", "CCTV9记录": "CCTV9", "CCTV9纪录": "CCTV9",
	"CCTV10科教": "CCTV10", "CCTV11戏曲": "CCTV11", "CCTV12社会与法": "CCTV12",
	"CCTV13新闻": "CCTV13", "CCTV新闻": "CCTV13",
	"CCTV14少儿": "CCTV14", "CCTV15音乐": "CCTV15", "CCTV16奥林匹克": "CCTV16",
	"CCTV17农业农村": "CCTV17", "CCTV17农业": "CCTV17",
	"CCTV5+体育赛视": "CCTV5+", "CCTV5+体育赛事": "CCTV5+", "CCTV5+体育": "CCTV5+",
	"CCTV01": "CCTV1", "CCTV02": "CCTV2", "CCTV03": "CCTV3",
	"CCTV04": "CCTV4", "CCTV05": "CCTV5", "CCTV06": "CCTV6",
	"CCTV07": "CCTV7", "CCTV08": "CCTV8", "CCTV09": "CCTV9",
}

// Clean normalises a raw channel name.
func Clean(name string) string {
	name = strings.ReplaceAll(name, "cctv", "CCTV")
	name = strings.ReplaceAll(name, "中央", "CCTV")
	name = strings.ReplaceAll(name, "央视", "CCTV")

	for _, rep := range []string{"高清", "超高", "HD", "标清", "频道", "-", " ", "(", ")"} {
		name = strings.ReplaceAll(name, rep, "")
	}
	name = strings.ReplaceAll(name, "PLUS", "+")
	name = strings.ReplaceAll(name, "＋", "+")
	name = reCCTVNum.ReplaceAllString(name, "CCTV$1")

	if mapped, ok := nameMap[name]; ok {
		return mapped
	}
	return name
}

// ── Standard channel map ──────────────────────────────────────────

// LoadStdMap reads channel_list.txt and builds a normalised→standard map.
func LoadStdMap(filePath string) map[string]string {
	m := map[string]string{}
	if filePath == "" {
		return m
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		return m
	}
	for _, line := range strings.Split(string(data), "\n") {
		std := strings.TrimSpace(line)
		if std == "" {
			continue
		}
		key := normalKey(std)
		m[key] = std
	}
	return m
}

func normalKey(name string) string {
	return strings.ToUpper(strings.ReplaceAll(strings.ReplaceAll(name, "-", ""), " ", ""))
}

// MapStd maps a cleaned name to its standard form if available.
func MapStd(name string, stdMap map[string]string) string {
	if std, ok := stdMap[normalKey(name)]; ok {
		return std
	}
	return name
}

// ── Group title ───────────────────────────────────────────────────

// GroupTitle returns the playlist group for a channel name.
func GroupTitle(name string) string {
	upper := strings.ToUpper(name)
	if strings.Contains(upper, "CCTV") {
		return "央视"
	}
	if strings.Contains(name, "卫视") {
		return "卫视"
	}
	return "其他"
}

// ── Sort key ──────────────────────────────────────────────────────

func SortKey(name string) (int, float64, string) {
	upper := strings.ToUpper(name)
	if strings.Contains(upper, "CCTV") {
		re := regexp.MustCompile(`CCTV(\d+)`)
		if m := re.FindStringSubmatch(upper); m != nil {
			num, _ := strconv.ParseFloat(m[1], 64)
			return 0, num, ""
		}
		if strings.Contains(upper, "5+") {
			return 0, 5.5, ""
		}
		return 0, 999, ""
	}
	if strings.Contains(name, "卫视") {
		return 1, 0, name
	}
	return 2, 0, name
}

// ── M3U8 entry builder ────────────────────────────────────────────

// BuildEntry creates the #EXTINF + URL block for one channel.
func BuildEntry(name, streamURL, logoBase string) string {
	logo := logoBase + url.PathEscape(name) + ".png"
	group := GroupTitle(name)
	return fmt.Sprintf(
		"#EXTINF:-1 tvg-name=%q tvg-logo=%q group-title=%q,%s\n%s",
		name, logo, group, name, streamURL,
	)
}

// ── Process source channels into Entries ─────────────────────────

// Process converts raw source channels into sorted Entries.
func Process(channels []source.Channel, sourceIndex int, stdMap map[string]string, logoBase string, speed ...float64) []Entry {
	spd := 0.0
	if len(speed) > 0 {
		spd = speed[0]
	}
	entries := make([]Entry, 0, len(channels))
	for _, ch := range channels {
		name := Clean(ch.Name)
		name = MapStd(name, stdMap)
		entries = append(entries, Entry{
			Name:    name,
			URL:     ch.URL,
			Content: BuildEntry(name, ch.URL, logoBase),
			Index:   sourceIndex,
			Speed:   spd,
		})
	}
	return entries
}

// ── Build final grouped & sorted channel list ─────────────────────

// Build takes all entries (from API sources + subscribe + local),
// groups by name, sorts, and returns ordered names + grouped map.
func Build(allEntries []Entry, channelFile string) ([]string, map[string][]Entry) {
	grouped := map[string][]Entry{}

	// pre-populate order from channel_list.txt if present
	var order []string
	if channelFile != "" {
		if data, err := os.ReadFile(channelFile); err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				name := strings.TrimSpace(line)
				if name != "" {
					grouped[name] = []Entry{}
					order = append(order, name)
				}
			}
		}
	}

	for _, e := range allEntries {
		grouped[e.Name] = append(grouped[e.Name], e)
	}

	// collect all names and sort
	allNames := make([]string, 0, len(grouped))
	for n := range grouped {
		allNames = append(allNames, n)
	}
	sort.Slice(allNames, func(i, j int) bool {
		a0, a1, a2 := SortKey(allNames[i])
		b0, b1, b2 := SortKey(allNames[j])
		if a0 != b0 {
			return a0 < b0
		}
		if a1 != b1 {
			return a1 < b1
		}
		return a2 < b2
	})
	_ = order // channel_list.txt order is a hint; sorted order takes precedence

	return allNames, grouped
}
