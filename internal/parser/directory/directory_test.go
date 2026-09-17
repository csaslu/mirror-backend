package directory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// readFixture loads a captured listing. The fixtures are real documents from
// the upstreams we support, because the whole point of this package is coping
// with what servers actually emit.
func readFixture(t *testing.T, name string) []byte {
	t.Helper()

	body, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("reading fixture %s: %v", name, err)
	}

	return body
}

func TestParseDocumentNginxFancyIndex(t *testing.T) {
	body := readFixture(t, "tuna-ubuntu-root.html")

	if !LooksLikeListing(body) {
		t.Fatal("LooksLikeListing() = false for a real nginx fancyindex listing")
	}

	parsed, err := ParseDocument(body, "/")
	if err != nil {
		t.Fatalf("ParseDocument() error = %v", err)
	}

	if parsed.Parser != "nginx-fancyindex" {
		t.Errorf("Parser = %q, want nginx-fancyindex", parsed.Parser)
	}
	if parsed.Path != "/" {
		t.Errorf("Path = %q, want /", parsed.Path)
	}
	if parsed.Parent != nil {
		t.Error("Parent should be nil at the mirror root")
	}

	// The fixture holds five directories and one file.
	if len(parsed.Entries) != 6 {
		t.Fatalf("got %d entries, want 6", len(parsed.Entries))
	}

	byName := map[string]Entry{}
	for _, entry := range parsed.Entries {
		byName[entry.Name] = entry
	}

	dists, ok := byName["dists"]
	if !ok {
		t.Fatal("missing the dists entry")
	}
	if dists.Kind != KindDirectory {
		t.Errorf("dists Kind = %q, want directory", dists.Kind)
	}
	if dists.Href != "dists/" {
		t.Errorf("dists Href = %q, want %q", dists.Href, "dists/")
	}
	if dists.Size != nil {
		t.Errorf("directories must not carry a size, got %d", *dists.Size)
	}
	if dists.ModTime == nil {
		t.Error("dists ModTime is nil, want the parsed date")
	}

	archive, ok := byName["ls-lR.gz"]
	if !ok {
		t.Fatal("missing the ls-lR.gz entry")
	}
	if archive.Kind != KindFile {
		t.Errorf("ls-lR.gz Kind = %q, want file", archive.Kind)
	}
	// "36.7 MiB" in the listing.
	if archive.Size == nil || *archive.Size != 38482739 {
		t.Errorf("ls-lR.gz Size = %v, want 38482739", archive.Size)
	}
	if archive.SizeText != "36.7 MiB" {
		t.Errorf("ls-lR.gz SizeText = %q, want %q", archive.SizeText, "36.7 MiB")
	}
}

func TestParseDocumentNestedPath(t *testing.T) {
	body := readFixture(t, "tuna-ubuntu-dists.html")

	parsed, err := ParseDocument(body, "/dists")
	if err != nil {
		t.Fatalf("ParseDocument() error = %v", err)
	}

	if parsed.Path != "/dists" {
		t.Errorf("Path = %q, want /dists", parsed.Path)
	}
	if parsed.Parent == nil {
		t.Fatal("Parent is nil for a nested directory")
	}
	if parsed.Parent.Href != "../" {
		t.Errorf("Parent Href = %q, want ../", parsed.Parent.Href)
	}
	if len(parsed.Entries) == 0 {
		t.Fatal("no entries parsed")
	}
}

func TestParseDocumentApacheAutoIndex(t *testing.T) {
	body := readFixture(t, "apache-autoindex.html")

	if !LooksLikeListing(body) {
		t.Fatal("LooksLikeListing() = false for an Apache autoindex page")
	}

	parsed, err := ParseDocument(body, "/pool")
	if err != nil {
		t.Fatalf("ParseDocument() error = %v", err)
	}

	if parsed.Parser != "apache-autoindex" {
		t.Errorf("Parser = %q, want apache-autoindex", parsed.Parser)
	}
	if parsed.Parent == nil {
		t.Fatal("Parent is nil; the fixture has a Parent Directory row")
	}

	names := map[string]Entry{}
	for _, entry := range parsed.Entries {
		names[entry.Name] = entry
	}

	if len(names) != 3 {
		t.Fatalf("got %d entries (%v), want 3", len(names), names)
	}
	if entry, ok := names["main"]; !ok || entry.Kind != KindDirectory {
		t.Errorf("main: got %+v, want a directory", entry)
	}
	// Sizes without a unit are bytes.
	if entry, ok := names["index.tar.xz"]; !ok || entry.Size == nil || *entry.Size != 12<<20 {
		t.Errorf("index.tar.xz size = %v, want %d", entry.Size, 12<<20)
	}
}

// TestParseDocumentRejectsNonListings is the safety net: anything that is not a
// listing must be refused so the caller proxies the upstream response instead of
// showing an empty directory.
func TestParseDocumentRejectsNonListings(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"empty", ""},
		{"plain text", "hello world"},
		{"html article", "<html><body><h1>News</h1><p>Nothing here.</p></body></html>"},
		{"soft 404", `<html><body><h1>404 Not Found</h1><p>The requested URL was not found.</p></body></html>`},
		{"listing of a different directory", `<html><body><table id="list"><tbody>
			<tr><td class="link"><a href="dists/">dists/</a></td><td class="size">-</td><td class="date">-</td></tr>
		</tbody></table></body></html>`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.name == "listing of a different directory" {
				// The table parses, but the page never mentions the requested
				// path, so it must be refused.
				if _, err := ParseDocument([]byte(tc.body), "/something-else"); err == nil {
					t.Fatal("ParseDocument() = nil error, want ErrNotAListing")
				}
				return
			}

			if LooksLikeListing([]byte(tc.body)) {
				t.Fatal("LooksLikeListing() = true for a non-listing document")
			}
			if _, err := ParseDocument([]byte(tc.body), "/"); err == nil {
				t.Fatal("ParseDocument() = nil error, want ErrNotAListing")
			}
		})
	}
}

// TestEntryFromHrefRejectsUnsafeLinks covers the security boundary: hrefs come
// from upstream HTML and end up in URLs we hand to the browser.
func TestEntryFromHrefRejectsUnsafeLinks(t *testing.T) {
	tests := []struct {
		href string
		want bool
	}{
		{"dists/", true},
		{"ls-lR.gz", true},
		{"a%20file%20with%20spaces.txt", true},
		{"sub/dir/", true},
		{"../", false}, // handled as the parent row, never an entry
		{"../../etc/passwd", false},
		{"a/../../b", false},
		{"javascript:alert(1)", false},
		{"data:text/html,<script>alert(1)</script>", false},
		{"mailto:admin@example.com", false},
		{"https://evil.example/x", false},
		{"//evil.example/x", false},
		{"/absolute/path", false},
		{"?C=N&O=A", false},
		{"#anchor", false},
		{"", false},
	}

	for _, tc := range tests {
		entry, ok := entryFromHref(tc.href, "text", "")
		if ok != tc.want {
			t.Errorf("entryFromHref(%q) ok = %v, want %v", tc.href, ok, tc.want)
			continue
		}
		if ok && strings.Contains(entry.Href, "..") {
			t.Errorf("entryFromHref(%q) produced a traversing href %q", tc.href, entry.Href)
		}
	}
}

func TestParseSize(t *testing.T) {
	tests := []struct {
		in   string
		want *int64
	}{
		{"36.7 MiB", new(int64(38482739))},
		{"12M", new(int64(12 << 20))},
		{"1.5K", new(int64(1536))},
		{"549G", new(int64(549 * (1 << 30)))},
		{"1,234", new(int64(1234))},
		{"-", nil},
		{"", nil},
		{"[DIR]", nil},
		{"unknown", nil},
		{"10X", nil},
	}

	for _, tc := range tests {
		got := ParseSize(tc.in)
		switch {
		case tc.want == nil && got != nil:
			t.Errorf("ParseSize(%q) = %d, want nil", tc.in, *got)
		case tc.want != nil && got == nil:
			t.Errorf("ParseSize(%q) = nil, want %d", tc.in, *tc.want)
		case tc.want != nil && got != nil && *got != *tc.want:
			t.Errorf("ParseSize(%q) = %d, want %d", tc.in, *got, *tc.want)
		}
	}
}

func TestParseDateKeepsTimezone(t *testing.T) {
	parsed := ParseDate("16 Sep 2026 11:36:37 +0000")
	if parsed == nil {
		t.Fatal("ParseDate() = nil")
	}
	if got := parsed.UTC().Format("2006-01-02 15:04:05"); got != "2026-09-16 11:36:37" {
		t.Errorf("ParseDate() = %s, want 2026-09-16 11:36:37 UTC", got)
	}

	// A server that prints its own offset must be honoured, not assumed UTC.
	offset := ParseDate("16 Sep 2026 11:36:37 +0800")
	if offset == nil {
		t.Fatal("ParseDate(+0800) = nil")
	}
	// 11:36:37+0800 is 03:36:37 UTC, i.e. eight hours earlier than the +0000
	// value above; a parser ignoring the offset would report no difference.
	if delta := parsed.Sub(*offset); delta.Hours() != 8 {
		t.Errorf("offset difference = %v hours, want 8", delta.Hours())
	}

	for _, value := range []string{"", "-", "not a date"} {
		if ParseDate(value) != nil {
			t.Errorf("ParseDate(%q) should be nil", value)
		}
	}
}
