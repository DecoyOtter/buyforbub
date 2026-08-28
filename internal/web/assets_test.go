package web

import (
	"encoding/json"
	"net/http"
	"regexp"
	"testing"
)

var staticRefRE = regexp.MustCompile(`(?:href|src)="(/static/[^"]+)"`)

// TestTemplatesReferenceRealAssets catches a renamed or missing static file
// before it becomes a 404 in the browser.
func TestTemplatesReferenceRealAssets(t *testing.T) {
	s, _ := newServer(t)

	rec := do(t, s, http.MethodGet, "/", nil)
	assertStatus(t, rec, http.StatusOK)

	refs := staticRefRE.FindAllStringSubmatch(rec.Body.String(), -1)
	if len(refs) == 0 {
		t.Fatal("page references no static assets — has the layout changed?")
	}

	for _, m := range refs {
		path := m[1]
		t.Run(path, func(t *testing.T) {
			got := do(t, s, http.MethodGet, path, nil)
			assertStatus(t, got, http.StatusOK)
			if got.Body.Len() == 0 {
				t.Errorf("%s is empty", path)
			}
		})
	}
}

func TestManifestIsValid(t *testing.T) {
	s, _ := newServer(t)

	rec := do(t, s, http.MethodGet, "/static/manifest.webmanifest", nil)
	assertStatus(t, rec, http.StatusOK)

	var m struct {
		Name     string `json:"name"`
		StartURL string `json:"start_url"`
		Display  string `json:"display"`
		Icons    []struct {
			Src   string `json:"src"`
			Sizes string `json:"sizes"`
		} `json:"icons"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("manifest is not valid JSON: %v", err)
	}

	if m.Name == "" {
		t.Error("manifest has no name")
	}
	if m.StartURL != "/" {
		t.Errorf("start_url = %q, want %q", m.StartURL, "/")
	}
	if m.Display != "standalone" {
		t.Errorf("display = %q, want %q", m.Display, "standalone")
	}
	if len(m.Icons) == 0 {
		t.Fatal("manifest lists no icons")
	}

	// Every icon the manifest promises must actually be served.
	for _, icon := range m.Icons {
		got := do(t, s, http.MethodGet, icon.Src, nil)
		if got.Code != http.StatusOK {
			t.Errorf("icon %s (%s): status %d, want 200", icon.Src, icon.Sizes, got.Code)
		}
	}
}

// htmx must be vendored, not pulled from a CDN — the server is LAN-only.
func TestHtmxIsVendored(t *testing.T) {
	s, _ := newServer(t)

	rec := do(t, s, http.MethodGet, "/static/htmx.min.js", nil)
	assertStatus(t, rec, http.StatusOK)
	if rec.Body.Len() < 10_000 {
		t.Errorf("htmx.min.js is %d bytes, looks truncated", rec.Body.Len())
	}

	page := do(t, s, http.MethodGet, "/", nil).Body.String()
	assertNotContains(t, page, "//unpkg.com")
	assertNotContains(t, page, "//cdn.jsdelivr.net")
	assertNotContains(t, page, "//cdnjs.cloudflare.com")
}
