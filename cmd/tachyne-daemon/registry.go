package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// Registry integration: the manager consults one or more tachyne plugin
// registries (comma-separated -registry / TACHYNE_REGISTRY URLs, merged like
// package sources) for name→module resolution, search, freshness ("is this
// shard's plugin out of date?"), and install-count pings. Everything here is
// best-effort — no registry, no problem, module paths still work.

type registryClient struct {
	urls []string
	http *http.Client
}

func newRegistryClient(flagURLs string) *registryClient {
	raw := flagURLs
	if raw == "" {
		raw = os.Getenv("TACHYNE_REGISTRY")
	}
	var urls []string
	for _, u := range strings.Split(raw, ",") {
		if u = strings.TrimRight(strings.TrimSpace(u), "/"); u != "" {
			urls = append(urls, u)
		}
	}
	return &registryClient{urls: urls, http: &http.Client{Timeout: 10 * time.Second}}
}

func (rc *registryClient) enabled() bool { return len(rc.urls) > 0 }

// listing mirrors the registry's public view.
type listing struct {
	Module      string  `json:"module"`
	Name        string  `json:"name"`
	Type        string  `json:"type"`
	Description string  `json:"description"`
	Latest      string  `json:"latest"`
	UpdatedAt   string  `json:"updated_at"`
	Installs    int     `json:"installs"`
	Rating      float64 `json:"rating"`
	Ratings     int     `json:"ratings"`
}

// search merges results from every configured registry (first hit per
// module wins — earlier registries take precedence, like apt sources).
func (rc *registryClient) search(q string) []listing {
	seen := map[string]bool{}
	var out []listing
	for _, base := range rc.urls {
		var body struct {
			Plugins []listing `json:"plugins"`
		}
		if err := rc.getJSON(base+"/v1/plugins?q="+urlQueryEscape(q), &body); err != nil {
			continue
		}
		for _, l := range body.Plugins {
			if !seen[l.Module] {
				seen[l.Module] = true
				out = append(out, l)
			}
		}
	}
	return out
}

// resolve maps a bare plugin name to its module path via the registries; a
// value that already looks like a path passes through.
func (rc *registryClient) resolve(nameOrModule string) (string, error) {
	if strings.Contains(nameOrModule, "/") {
		return nameOrModule, nil
	}
	if !rc.enabled() {
		return "", fmt.Errorf("%q is not a module path and no registry is configured (-registry / TACHYNE_REGISTRY)", nameOrModule)
	}
	var exact []listing
	for _, l := range rc.search(nameOrModule) {
		if l.Name == nameOrModule {
			exact = append(exact, l)
		}
	}
	switch len(exact) {
	case 0:
		return "", fmt.Errorf("no plugin named %q in the configured registries", nameOrModule)
	case 1:
		return exact[0].Module, nil
	default:
		var mods []string
		for _, l := range exact {
			mods = append(mods, l.Module)
		}
		return "", fmt.Errorf("%q is ambiguous: %s — use the module path", nameOrModule, strings.Join(mods, ", "))
	}
}

// latest asks the registries for a module's newest version ("" = unlisted).
func (rc *registryClient) latest(module string) string {
	for _, base := range rc.urls {
		var l listing
		if err := rc.getJSON(base+"/v1/plugins/"+module, &l); err == nil && l.Latest != "" {
			return l.Latest
		}
	}
	return ""
}

// pingInstalled bumps the install counter (fire and forget).
func (rc *registryClient) pingInstalled(module string) {
	for _, base := range rc.urls {
		req, err := http.NewRequest("POST", base+"/v1/plugins/"+module+"/installed", bytes.NewReader(nil))
		if err != nil {
			continue
		}
		if res, err := rc.http.Do(req); err == nil {
			res.Body.Close()
			if res.StatusCode == 200 {
				return // counted by the first registry that knows it
			}
		}
	}
}

func (rc *registryClient) getJSON(url string, v any) error {
	res, err := rc.http.Get(url)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return fmt.Errorf("%s: %s", url, res.Status)
	}
	return json.NewDecoder(res.Body).Decode(v)
}

func urlQueryEscape(s string) string {
	r := strings.NewReplacer(" ", "+", "&", "%26", "?", "%3F", "#", "%23")
	return r.Replace(s)
}

// builtVersion reads the module version baked into a built binary
// (`go version -m`), so a shard knows exactly what it runs — the fleet
// out-of-date check compares this against the registry's latest.
var modLine = regexp.MustCompile(`(?m)^\s+mod\s+\S+\s+(\S+)`)

func builtVersion(bin string) string {
	out, err := exec.Command("go", "version", "-m", bin).Output()
	if err != nil {
		return ""
	}
	if m := modLine.FindSubmatch(out); m != nil {
		return string(m[1])
	}
	return ""
}
