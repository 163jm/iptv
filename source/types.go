package source

// Channel is a single IPTV channel with a name and stream URL.
type Channel struct {
	Name string
	URL  string
}

// SourceResult is the result of scanning one IPTV host.
type SourceResult struct {
	Host      string
	MatchType string
	Origin    string // "api" | "subscribe" | "local"
	Source    string
	Speed     float64
	Channels  []Channel
}
