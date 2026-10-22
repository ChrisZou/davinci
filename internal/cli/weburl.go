package cli

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"time"
)

// localDomains are where a local reverse proxy may route davinci (port 80 →
// proxy → whatever port the server listens on), the dev server first. They are
// the addresses a person uses, so the CLI prefers them to any port.
var localDomains = []string{"http://davinci-dev.localhost", "http://davinci.localhost"}

// WebURL is the address to hand a person for opening the editor:
// DAVINCI_WEB_URL if set, else the first local domain that reaches this same
// server (same boot id), else BaseURL. Machines without the proxy thus keep
// printing 127.0.0.1 links that actually work.
func (c *Client) WebURL() string {
	if c.webURL != "" {
		return c.webURL
	}
	c.webURL = c.BaseURL
	if v := os.Getenv("DAVINCI_WEB_URL"); v != "" {
		c.webURL = strings.TrimRight(v, "/")
		return c.webURL
	}
	boot := bootOf(c.BaseURL)
	for _, d := range localDomains {
		if d == c.BaseURL || boot != "" && bootOf(d) == boot {
			c.webURL = d
			break
		}
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
