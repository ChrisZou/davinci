package cli

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"time"
)

// prettyURL is where a person opens davinci on a machine that routes
// davinci.localhost to it (for example port 80 → a local reverse proxy →
// 127.0.0.1:7789). The CLI itself keeps talking to BaseURL directly.
const prettyURL = "http://davinci.localhost"

// WebURL is the address to hand a person for opening the editor:
// DAVINCI_WEB_URL if set, else prettyURL when it reaches this same server
// (same boot id), else BaseURL. Machines without the proxy — the mini, the dev
// server — thus keep printing 127.0.0.1 links that actually work.
func (c *Client) WebURL() string {
	if c.webURL != "" {
		return c.webURL
	}
	c.webURL = c.BaseURL
	if v := os.Getenv("DAVINCI_WEB_URL"); v != "" {
		c.webURL = strings.TrimRight(v, "/")
	} else if boot := bootOf(c.BaseURL); boot != "" && bootOf(prettyURL) == boot {
		c.webURL = prettyURL
	}
	return c.webURL
}

// EditorURL is WebURL's editor page for a project.
func (c *Client) EditorURL(id string) string { return c.WebURL() + "/editor/" + id }

// bootOf returns the boot id of the server answering at base, or "".
func bootOf(base string) string {
	hc := &http.Client{Timeout: 500 * time.Millisecond}
	resp, err := hc.Get(base + "/api/health")
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	var h struct {
		Boot string `json:"boot"`
	}
	if resp.StatusCode != http.StatusOK || json.NewDecoder(resp.Body).Decode(&h) != nil {
		return ""
	}
	return h.Boot
}
