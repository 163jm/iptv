package server

import (
	"fmt"
	"net/http"
	"sort"
	"sync"
	"time"

	"iptv/channel"
	"iptv/output"
	"iptv/source"
	"iptv/speedtest"
)

// Server holds runtime state and wires all subsystems together.
type Server struct {
	cfg    *source.Config
	writer *output.Writer

	mu         sync.RWMutex
	m3u8       string
	txt        string
	updateTime time.Time
	running    bool
}

func New(cfg *source.Config) *Server {
	w := output.New(cfg.EPGUrl, cfg.CacheM3U8, cfg.CacheTxt)
	srv := &Server{cfg: cfg, writer: w}
	// restore cache so HTTP is available before first task finishes
	srv.m3u8, srv.txt = w.ReadCache()
	return srv
}

// ── HTTP handlers ─────────────────────────────────────────────────

func (s *Server) HandleStatus(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	t := s.updateTime
	running := s.running
	s.mu.RUnlock()

	status := "idle"
	if running {
		status = "scanning..."
	}
	fmt.Fprintf(w, "IPTV Aggregator\nStatus: %s\nLast update: %s\n\nEndpoints:\n  /iptv  → M3U8\n  /txt   → TXT\n  /forceRetest → force rescan\n",
		status, t.Format("2006-01-02 15:04:05"))
}

func (s *Server) HandleM3U8(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	body := s.m3u8
	s.mu.RUnlock()
	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	fmt.Fprint(w, body)
}

func (s *Server) HandleTxt(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	body := s.txt
	s.mu.RUnlock()
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprint(w, body)
}

func (s *Server) HandleForce(w http.ResponseWriter, r *http.Request) {
	go s.RunTask()
	fmt.Fprint(w, "Rescan triggered\n")
}

// ── Task orchestration ────────────────────────────────────────────

func (s *Server) RunTask() {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		fmt.Println("[task] already running, skip")
		return
	}
	s.running = true
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.running = false
		s.mu.Unlock()
	}()

	fmt.Println("[task] ── start ──────────────────────────────────────────")
	start := time.Now()

	cfg := s.cfg
	stdMap := channel.LoadStdMap(cfg.ChannelFile)
	var allEntries []channel.Entry
	sourceIdx := 0

	// ── 1. API sources ─────────────────────────────────────────
	apiItems, err := source.FetchAPI(cfg.APIURL)
	if err != nil {
		fmt.Printf("[task] API fetch error: %v\n", err)
	} else {
		fmt.Printf("[task] speed-testing %d API hosts...\n", len(apiItems))
		results := speedtest.RunSpeedTests(apiItems, cfg.Workers)

		// filter and keep top N per matchType
		results = filterAndRank(results, cfg.MinSpeed, cfg.TopN)

		fmt.Printf("[task] valid sources after filter: %d — fetching channels...\n", len(results))
		var wg sync.WaitGroup
		var mu sync.Mutex
		sem := make(chan struct{}, cfg.Workers)
		for i := range results {
			i := i
			wg.Add(1)
			sem <- struct{}{}
			go func() {
				defer wg.Done()
				defer func() { <-sem }()
				speedtest.FetchChannels(&results[i], cfg.HsmdFile)
				mu.Lock()
				idx := sourceIdx
				sourceIdx++
				for _, ch := range results[i].Channels {
					entries := channel.Process(
						[]source.Channel{ch}, idx, stdMap, cfg.LogoBase, results[i].Speed,
					)
					allEntries = append(allEntries, entries...)
				}
				mu.Unlock()
			}()
		}
		wg.Wait()
	}

	// ── 2. Subscribe sources ───────────────────────────────────
	subChannels, err := source.FetchSubscribe(cfg.SubscribeFile)
	if err != nil {
		fmt.Printf("[task] subscribe error: %v\n", err)
	} else if len(subChannels) > 0 {
		fmt.Printf("[task] subscribe: %d channels\n", len(subChannels))
		entries := channel.Process(subChannels, sourceIdx, stdMap, cfg.LogoBase)
		allEntries = append(allEntries, entries...)
		sourceIdx++
	}

	// ── 3. Local sources ───────────────────────────────────────
	localChannels, err := source.FetchLocal(cfg.LocalFile)
	if err != nil {
		fmt.Printf("[task] local error: %v\n", err)
	} else if len(localChannels) > 0 {
		fmt.Printf("[task] local: %d channels\n", len(localChannels))
		entries := channel.Process(localChannels, sourceIdx, stdMap, cfg.LogoBase)
		allEntries = append(allEntries, entries...)
	}

	if len(allEntries) == 0 {
		fmt.Println("[task] no entries collected, keeping previous cache")
		return
	}

	// ── 4. Build & write output ────────────────────────────────
	names, grouped := channel.Build(allEntries, cfg.ChannelFile)
	updateTime := time.Now()
	m3u8, txt := s.writer.Write(names, grouped, updateTime)

	s.mu.Lock()
	s.m3u8 = m3u8
	s.txt = txt
	s.updateTime = updateTime
	s.mu.Unlock()

	total := 0
	for _, entries := range grouped {
		total += len(entries)
	}
	fmt.Printf("[task] done: %d channels, %d unique names, elapsed %s\n",
		total, len(names), time.Since(start).Round(time.Second))
}

// ── helpers ───────────────────────────────────────────────────────

// filterAndRank removes sources below minSpeed, then keeps top N per matchType.
func filterAndRank(results []source.SourceResult, minSpeed float64, topN int) []source.SourceResult {
	// filter by speed
	var valid []source.SourceResult
	for _, r := range results {
		if r.Speed >= minSpeed {
			valid = append(valid, r)
		}
	}

	// sort by speed desc per matchType
	byType := map[string][]source.SourceResult{}
	for _, r := range valid {
		byType[r.MatchType] = append(byType[r.MatchType], r)
	}
	var out []source.SourceResult
	for _, group := range byType {
		sort.Slice(group, func(i, j int) bool {
			return group[i].Speed > group[j].Speed
		})
		if len(group) > topN {
			group = group[:topN]
		}
		out = append(out, group...)
	}
	return out
}
