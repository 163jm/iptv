package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"iptv/server"
	"iptv/source"

	"github.com/robfig/cron/v3"
)

const VERSION = "2.0.0"

func main() {
	_ = os.Setenv("TZ", "Asia/Shanghai")

	// ── CLI flags ────────────────────────────────────────────────
	host := flag.String("host", "0.0.0.0", "HTTP listen host")
	port := flag.Int("port", 5000, "HTTP listen port")
	workers := flag.Int("workers", 20, "concurrent speed-test workers")
	topN := flag.Int("top", 5, "number of top sources to keep")
	minSpeed := flag.Float64("min-speed", 1.5, "minimum source speed (MB/s)")
	interval := flag.String("interval", "6h", "update interval (cron: @every 6h)")
	epgURL := flag.String("epg", "https://epg.zsdc.eu.org/t.xml", "EPG URL embedded in m3u8")
	logoBase := flag.String("logo", "https://ghfast.top/https://raw.githubusercontent.com/Jarrey/iptv_logo/main/tv/", "logo base URL")
	channelFile := flag.String("channels", "channel_list.txt", "channel list file path")
	hsmdFile := flag.String("hsmd", "hsmd_address_list.txt", "hsmd address list file path")
	apiURL := flag.String("api", "https://iptvs.pes.im", "IPTV source API URL")
	subscribeFile := flag.String("subscribe", "", "subscribe source list file (one URL per line)")
	localFile := flag.String("local", "", "local m3u/txt source file path")
	cacheM3U8 := flag.String("cache-m3u8", "iptv_sources.m3u8", "m3u8 cache file path")
	cacheTxt := flag.String("cache-txt", "iptv_sources.txt", "txt cache file path")
	flag.Parse()

	fmt.Printf("IPTV Aggregator v%s\n", VERSION)

	cfg := &source.Config{
		APIURL:        *apiURL,
		Workers:       *workers,
		TopN:          *topN,
		MinSpeed:      *minSpeed,
		EPGUrl:        *epgURL,
		LogoBase:      *logoBase,
		ChannelFile:   *channelFile,
		HsmdFile:      *hsmdFile,
		SubscribeFile: *subscribeFile,
		LocalFile:     *localFile,
		CacheM3U8:     *cacheM3U8,
		CacheTxt:      *cacheTxt,
	}

	srv := server.New(cfg)

	// run once immediately
	go srv.RunTask()

	// scheduled updates
	c := cron.New()
	spec := "@every " + *interval
	if _, err := c.AddFunc(spec, func() { go srv.RunTask() }); err != nil {
		log.Fatalf("invalid interval %q: %v", *interval, err)
	}
	c.Start()
	defer c.Stop()

	// HTTP
	mux := http.NewServeMux()
	mux.HandleFunc("/", srv.HandleStatus)
	mux.HandleFunc("/iptv", srv.HandleM3U8)
	mux.HandleFunc("/txt", srv.HandleTxt)
	mux.HandleFunc("/forceRetest", srv.HandleForce)

	addr := fmt.Sprintf("%s:%d", *host, *port)
	fmt.Printf("Listening on http://%s\n", addr)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\nShutting down...")
		os.Exit(0)
	}()

	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}
