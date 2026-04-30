package source

// Config holds all runtime parameters passed via CLI flags.
type Config struct {
	APIURL        string
	Workers       int
	TopN          int
	MinSpeed      float64
	EPGUrl        string
	LogoBase      string
	ChannelFile   string
	HsmdFile      string
	SubscribeFile string
	LocalFile     string
	CacheM3U8     string
	CacheTxt      string
}
