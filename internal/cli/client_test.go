package cli

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// fakeDavinci answers /api/health like a server with the given boot id.
func fakeDavinci(t *testing.T, boot string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"ok":true,"boot":%q}`, boot)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestServerURLPrefersLocalDomains(t *testing.T) {
	for _, k := range []string{"DAVINCI_SERVER", "DAVINCI_PORT", "DAVINCI_DATA", "DAVINCI_WEB_URL"} {
		t.Setenv(k, "")
	}
	saved, savedFlags := localDomains, gf
	t.Cleanup(func() { localDomains, gf = saved, savedFlags })
	gf = globalFlags{}
	t.Chdir(t.TempDir()) // the default data directory: no server.json there

	dev, prod := fakeDavinci(t, "boot_dev"), fakeDavinci(t, "boot_prod")
	// A proxy whose server is down answers 502: not a davinci.
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusBadGateway) }))
	t.Cleanup(down.Close)

	localDomains = []string{dev.URL, prod.URL}
	if u, _ := serverURL(); u != dev.URL {
		t.Fatalf("dev domain first, got %s", u)
	}
	localDomains = []string{down.URL, prod.URL}
	if u, _ := serverURL(); u != prod.URL {
		t.Fatalf("a 502 domain must be skipped, got %s", u)
	}
	localDomains = []string{down.URL}
	if u, _ := serverURL(); u != "" {
		t.Fatalf("no port may be guessed, got %s", u)
	}
	// A named data directory means that data's own server, not a domain.
	gf.data = t.TempDir()
	localDomains = []string{dev.URL}
	if u, _ := serverURL(); u != "" {
		t.Fatalf("named data directory reached a domain: %s", u)
	}
	gf.data = ""
	t.Setenv("DAVINCI_SERVER", prod.URL+"/")
	if u, _ := serverURL(); u != prod.URL {
		t.Fatalf("explicit address first, got %s", u)
	}

	// Links handed to a person use the domain that reaches the same server.
	direct := fakeDavinci(t, "boot_prod")
	localDomains = []string{dev.URL, prod.URL}
	if got := (&Client{BaseURL: direct.URL}).WebURL(); got != prod.URL {
		t.Fatalf("web URL %s, want %s", got, prod.URL)
	}
	if got := (&Client{BaseURL: dev.URL}).WebURL(); got != dev.URL {
		t.Fatalf("web URL %s, want %s", got, dev.URL)
	}
}
