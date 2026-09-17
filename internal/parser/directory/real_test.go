package directory

import (
	"os"
	"testing"
)

// TestRealWorldListings parses documents captured from other campus/cloud
// mirrors, to check that the registry really covers what is out there rather
// than only the sites we happened to test by hand.
func TestRealWorldListings(t *testing.T) {
	cases := []struct {
		fixture string
		path    string
	}{
		{"tuna-ubuntu-root.html", "/"},
		{"tuna-ubuntu-dists.html", "/dists"},
		{"apache-autoindex.html", "/pool"},
		{"aliyun-ubuntu-pool.html", "/pool"},
		{"tencent-ubuntu-pool.html", "/pool"},
	}

	for _, tc := range cases {
		t.Run(tc.fixture, func(t *testing.T) {
			body, err := os.ReadFile("testdata/" + tc.fixture)
			if err != nil {
				t.Skipf("fixture missing: %v", err)
			}

			parsed, err := ParseDocument(body, tc.path)
			if err != nil {
				t.Fatalf("ParseDocument(%s, %s) error = %v", tc.fixture, tc.path, err)
			}

			if len(parsed.Entries) == 0 {
				t.Fatal("no entries parsed")
			}

			dirs, files := 0, 0
			for _, entry := range parsed.Entries {
				if entry.Kind == KindDirectory {
					dirs++
					continue
				}
				files++
			}

			t.Logf("parser=%s entries=%d (dirs=%d files=%d) parent=%v",
				parsed.Parser, len(parsed.Entries), dirs, files, parsed.Parent != nil)
		})
	}
}
