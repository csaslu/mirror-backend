package parser

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	modelErr "mirror/internal/model/errors"
)

// TestMatch pins upstream detection. It matches on the parsed hostname, so
// lookalike hosts and paths that merely contain another site's name must not
// be confused with each other.
func TestMatch(t *testing.T) {
	tests := []struct {
		name      string
		source    string
		wantName  string
		wantMatch bool
	}{
		{name: "tuna root", source: "https://mirrors.tuna.tsinghua.edu.cn", wantName: "tuna", wantMatch: true},
		{name: "tuna subdomain", source: "https://pypi.mirrors.tuna.tsinghua.edu.cn", wantName: "tuna", wantMatch: true},
		{name: "tuna with path", source: "https://mirrors.tuna.tsinghua.edu.cn/ubuntu", wantName: "tuna", wantMatch: true},
		{name: "nju root", source: "https://mirror.nju.edu.cn", wantName: "nju", wantMatch: true},
		{name: "nju with path", source: "https://mirror.nju.edu.cn/ubuntu", wantName: "nju", wantMatch: true},
		{name: "bare host", source: "mirror.nju.edu.cn", wantName: "nju", wantMatch: true},
		{name: "uppercase host", source: "HTTPS://MIRROR.NJU.EDU.CN", wantName: "nju", wantMatch: true},
		{name: "ustc root", source: "https://mirrors.ustc.edu.cn", wantName: "ustc", wantMatch: true},
		{name: "ustc rsync source", source: "rsync://mirrors.ustc.edu.cn/ubuntu", wantName: "ustc", wantMatch: true},
		{name: "unknown site", source: "https://mirrors.example.edu", wantMatch: false},
		{name: "lookalike host", source: "https://mirrors.tuna.tsinghua.edu.cn.attacker.example", wantMatch: false},
		{name: "name in path only", source: "https://example.com/mirrors.tuna.tsinghua.edu.cn", wantMatch: false},
		{name: "empty", source: "", wantMatch: false},
		{name: "garbage", source: "://::", wantMatch: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			upstream, ok := Match(tc.source)
			if ok != tc.wantMatch {
				t.Fatalf("Match(%q) matched = %v, want %v", tc.source, ok, tc.wantMatch)
			}
			if ok && upstream.Name != tc.wantName {
				t.Errorf("Match(%q) = %q, want %q", tc.source, upstream.Name, tc.wantName)
			}
		})
	}
}

// TestUpstreamStatusURLs pins the per-site document paths. Every site publishes
// tunasync.json somewhere different, and getting one wrong silently turns into
// "mirror status unknown".
func TestUpstreamStatusURLs(t *testing.T) {
	want := map[string]string{
		"tuna": "https://mirrors.tuna.tsinghua.edu.cn/static/tunasync.json",
		"nju":  "https://mirror.nju.edu.cn/configs/tunasync.json",
		"ustc": "https://mirrors.ustc.edu.cn/status/json",
	}

	if len(Upstreams) != len(want) {
		t.Fatalf("Upstreams has %d entries, want %d", len(Upstreams), len(want))
	}

	for _, upstream := range Upstreams {
		expected, ok := want[upstream.Name]
		if !ok {
			t.Errorf("unexpected upstream %q in the registry", upstream.Name)
			continue
		}
		if got := upstream.StatusURL(); got != expected {
			t.Errorf("%s StatusURL() = %q, want %q", upstream.Name, got, expected)
		}
	}
}

// statusServer serves a status document and counts requests, so tests can
// assert that one document is fetched once and reused. The document served
// depends on the requested path, which is how the two shapes are tested with
// one server.
func statusServer(t *testing.T, documents map[string]any, counter *atomic.Int64) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		document, ok := documents[r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		if counter != nil {
			counter.Add(1)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(document)
	}))
	t.Cleanup(server.Close)

	return server
}

// upstreamFor points an Upstream at a local test server.
func upstreamFor(t *testing.T, server *httptest.Server, format StatusFormat) Upstream {
	t.Helper()

	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parsing test server URL: %v", err)
	}

	return Upstream{
		Name:       "test",
		Host:       parsed.Host,
		StatusPath: "/status",
		Scheme:     parsed.Scheme,
		Format:     format,
	}
}

var testDocument = []map[string]any{
	{
		"name":           "ubuntu",
		"status":         "success",
		"size":           "1.5G",
		"last_update_ts": 1700000000,
	},
	{
		"name":   "pypi",
		"status": "syncing",
		// tunasync writes sizes without the "B" suffix; the parser accepts both.
		"size":           "10K",
		"last_update_ts": 1700000123,
	},
}

// TestTunaSyncParse exercises the parsing paths against a local server, so the
// test never depends on an upstream mirror being reachable.
func TestTunaSyncParse(t *testing.T) {
	ResetStatusDocumentCache()
	t.Cleanup(ResetStatusDocumentCache)

	server := statusServer(t, map[string]any{"/status": testDocument}, nil)
	upstream := upstreamFor(t, server, FormatTunaSync)

	t.Run("found", func(t *testing.T) {
		status, err := TunaSyncParse(upstream, "ubuntu")
		if err != nil {
			t.Fatalf("TunaSyncParse() error = %v", err)
		}
		if status.Status == nil || *status.Status != "success" {
			t.Errorf("Status = %v, want success", status.Status)
		}
		if status.Size == nil || *status.Size != 1610612736 {
			t.Errorf("Size = %v, want 1610612736", status.Size)
		}
		if status.LastUpdate == nil || *status.LastUpdate != 1700000000 {
			t.Errorf("LastUpdate = %v, want 1700000000", status.LastUpdate)
		}
	})

	t.Run("size without B suffix", func(t *testing.T) {
		status, err := TunaSyncParse(upstream, "pypi")
		if err != nil {
			t.Fatalf("TunaSyncParse() error = %v", err)
		}
		if status.Size == nil || *status.Size != 10240 {
			t.Errorf("Size = %v, want 10240 (10K)", status.Size)
		}
	})

	t.Run("case insensitive key match", func(t *testing.T) {
		if _, err := TunaSyncParse(upstream, "Ubuntu"); err != nil {
			t.Fatalf("TunaSyncParse() error = %v, want a match", err)
		}
	})

	t.Run("missing key", func(t *testing.T) {
		_, err := TunaSyncParse(upstream, "not-a-mirror")
		if !errors.Is(err, modelErr.ErrStatusNotFound) {
			t.Fatalf("TunaSyncParse() error = %v, want ErrStatusNotFound", err)
		}
	})

	t.Run("upstream 404", func(t *testing.T) {
		ResetStatusDocumentCache()

		notFound := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		t.Cleanup(notFound.Close)

		broken := Upstream{Name: "404", Host: strings.TrimPrefix(notFound.URL, "http://"), StatusPath: "/", Scheme: "http", Format: FormatTunaSync}
		if _, err := TunaSyncParse(broken, "ubuntu"); !errors.Is(err, modelErr.ErrStatusNotFound) {
			t.Fatalf("TunaSyncParse() error = %v, want ErrStatusNotFound", err)
		}
	})

	t.Run("transport failure", func(t *testing.T) {
		ResetStatusDocumentCache()

		dead := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		dead.Close() // nothing is listening any more

		gone := Upstream{Name: "dead", Host: strings.TrimPrefix(dead.URL, "http://"), StatusPath: "/", Scheme: "http", Format: FormatTunaSync}
		if _, err := TunaSyncParse(gone, "ubuntu"); err == nil {
			t.Fatal("TunaSyncParse() error = nil, want a transport error")
		}
	})
}

// TestStatusDocumentIsFetchedOnce guards the reason the document cache exists:
// a full refresh asks for every mirror of a site, and TUNA's document is 74 KB
// while NJU's is 152 KB.
func TestStatusDocumentIsFetchedOnce(t *testing.T) {
	ResetStatusDocumentCache()
	t.Cleanup(ResetStatusDocumentCache)

	var requests atomic.Int64
	server := statusServer(t, map[string]any{"/status": testDocument}, &requests)
	upstream := upstreamFor(t, server, FormatTunaSync)

	for range 5 {
		if _, err := TunaSyncParse(upstream, "ubuntu"); err != nil {
			t.Fatalf("TunaSyncParse() error = %v", err)
		}
		if _, err := TunaSyncParse(upstream, "pypi"); err != nil {
			t.Fatalf("TunaSyncParse() error = %v", err)
		}
	}

	if got := requests.Load(); got != 1 {
		t.Errorf("upstream was fetched %d times for 10 lookups, want 1", got)
	}
}
