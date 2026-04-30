package source

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// FetchAPI pulls the host list from the remote API (iptvs.pes.im format).
// Returns a slice of raw map items, each containing "host", "matchType", "source".
func FetchAPI(apiURL string) ([]map[string]interface{}, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		fmt.Printf("[api] fetch attempt %d: %s\n", attempt, apiURL)
		resp, err := client.Get(apiURL)
		if err != nil {
			lastErr = err
			time.Sleep(5 * time.Second)
			continue
		}
		if resp.StatusCode != 200 {
			resp.Body.Close()
			lastErr = fmt.Errorf("HTTP %d", resp.StatusCode)
			time.Sleep(5 * time.Second)
			continue
		}
		var data map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
			resp.Body.Close()
			lastErr = err
			time.Sleep(5 * time.Second)
			continue
		}
		resp.Body.Close()

		results, ok := data["results"].([]interface{})
		if !ok {
			return nil, fmt.Errorf("API response missing 'results' key")
		}
		out := make([]map[string]interface{}, 0, len(results))
		for _, r := range results {
			if m, ok := r.(map[string]interface{}); ok {
				out = append(out, m)
			}
		}
		fmt.Printf("[api] received %d hosts\n", len(out))
		return out, nil
	}
	return nil, fmt.Errorf("API fetch failed after 3 retries: %v", lastErr)
}
