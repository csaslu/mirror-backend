package parser

import (
	"errors"
	"sync/atomic"
	"testing"

	modelErr "mirror/internal/model/errors"
)

// ustcDocumentFixture is a real entry from https://mirrors.ustc.edu.cn/status/json
// (trimmed to the fields the decoder reads).
const ustcDocumentFixture = `[
  {
    "name": "adoptium.apt",
    "disable": false,
    "syncing": false,
    "exitCode": 0,
    "lastSuccess": 1789511201,
    "nextRun": 1789597500,
    "prevRun": 1789511108,
    "size": 236419689472,
    "updatedAt": 1789511201,
    "upstream": "https://packages.adoptium.net/artifactory/",
    "mirrorz": [{"name": "adoptium"}]
  },
  {
    "name": "anaconda",
    "disable": false,
    "syncing": true,
    "exitCode": 0,
    "lastSuccess": 1789500000,
    "prevRun": 1789511000,
    "size": 1234567890,
    "upstream": "https://repo.anaconda.com/"
  },
  {
    "name": "broken-mirror",
    "disable": false,
    "syncing": false,
    "exitCode": 12,
    "lastSuccess": 1789000000,
    "prevRun": 1789511000,
    "size": 4096,
    "upstream": "https://example.invalid/"
  },
  {
    "name": "retired-mirror",
    "disable": true,
    "syncing": false,
    "exitCode": 0,
    "lastSuccess": 1789000000,
    "prevRun": 1789000000,
    "size": 0,
    "upstream": "https://example.invalid/"
  }
]`

// TestUSTCDecoder pins the mapping from USTC's own document shape onto the
// neutral form: flags and an exit code become a status word, and the exact byte
// size is preserved.
func TestUSTCDecoder(t *testing.T) {
	items, err := ustcDecoder{}.Decode([]byte(ustcDocumentFixture))
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}

	if len(items) != 4 {
		t.Fatalf("got %d entries, want 4", len(items))
	}

	byName := map[string]struct {
		status string
		size   int64
		last   int64
	}{}
	for _, item := range items {
		if item.SizeBytes == nil {
			t.Fatalf("%s: SizeBytes is nil", item.Name)
		}
		byName[item.Name] = struct {
			status string
			size   int64
			last   int64
		}{item.Status, *item.SizeBytes, item.LastUpdateTS}
	}

	want := map[string]struct {
		status string
		size   int64
		last   int64
	}{
		"adoptium.apt":   {"success", 236419689472, 1789511201},
		"anaconda":       {"syncing", 1234567890, 1789500000},
		"broken-mirror":  {"failed", 4096, 1789000000},
		"retired-mirror": {"disabled", 0, 1789000000},
	}

	for name, expected := range want {
		got, ok := byName[name]
		if !ok {
			t.Errorf("missing entry %q", name)
			continue
		}
		if got.status != expected.status {
			t.Errorf("%s status = %q, want %q", name, got.status, expected.status)
		}
		if got.size != expected.size {
			t.Errorf("%s size = %d, want %d", name, got.size, expected.size)
		}
		if got.last != expected.last {
			t.Errorf("%s lastSuccess = %d, want %d", name, got.last, expected.last)
		}
	}
}

// TestUSTCStatusPrecedence pins the order of the flags: a disabled mirror is
// disabled even while a sync is running, because that is what the site shows.
func TestUSTCStatusPrecedence(t *testing.T) {
	cases := []struct {
		name  string
		entry ustcDocument
		want  string
	}{
		{"disabled wins over syncing", ustcDocument{Disable: true, Syncing: true, ExitCode: 0}, "disabled"},
		{"syncing wins over a stale failure", ustcDocument{Syncing: true, ExitCode: 3}, "syncing"},
		{"exit code 0 is success", ustcDocument{ExitCode: 0}, "success"},
		{"non-zero exit code is failure", ustcDocument{ExitCode: 25}, "failed"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ustcStatus(tc.entry); got != tc.want {
				t.Errorf("ustcStatus() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestFetchStatusUSTC runs the whole path — fetch, decode, match — against a
// local server serving USTC's shape.
func TestFetchStatusUSTC(t *testing.T) {
	ResetStatusDocumentCache()
	t.Cleanup(ResetStatusDocumentCache)

	var requests atomic.Int64
	server := statusServer(t, map[string]any{
		"/status": rawDocument(ustcDocumentFixture),
	}, &requests)
	upstream := upstreamFor(t, server, FormatUSTC)

	status, err := FetchStatus(upstream, "adoptium.apt")
	if err != nil {
		t.Fatalf("FetchStatus() error = %v", err)
	}

	if status.Status == nil || *status.Status != "success" {
		t.Errorf("Status = %v, want success", status.Status)
	}
	// The exact byte count from the document, not a re-parsed string.
	if status.Size == nil || *status.Size != 236419689472 {
		t.Errorf("Size = %v, want 236419689472", status.Size)
	}
	if status.LastUpdate == nil || *status.LastUpdate != 1789511201 {
		t.Errorf("LastUpdate = %v, want 1789511201", status.LastUpdate)
	}

	if _, err = FetchStatus(upstream, "not-a-mirror"); !errors.Is(err, modelErr.ErrStatusNotFound) {
		t.Errorf("FetchStatus() error = %v, want ErrStatusNotFound", err)
	}

	if got := requests.Load(); got != 1 {
		t.Errorf("document fetched %d times, want 1 (cache should serve the second lookup)", got)
	}
}

// TestDecoderForDefaultsToTunaSync keeps the zero value of StatusFormat usable:
// a new Upstream entry that forgets the field still reads tunasync documents.
func TestDecoderForDefaultsToTunaSync(t *testing.T) {
	if _, ok := decoderFor("").(tunaSyncDecoder); !ok {
		t.Error("decoderFor(\"\") is not the tunasync decoder")
	}
	if _, ok := decoderFor(FormatUSTC).(ustcDecoder); !ok {
		t.Error("decoderFor(ustc) is not the USTC decoder")
	}
}

func TestFormatBytes(t *testing.T) {
	cases := map[int64]string{
		0:             "0",
		512:           "512",
		1024:          "1K",
		1536:          "1.5K",
		236419689472:  "220.18G",
		1099511627776: "1T",
	}

	for input, want := range cases {
		if got := formatBytes(input); got != want {
			t.Errorf("formatBytes(%d) = %q, want %q", input, got, want)
		}
	}
}

// rawDocument lets a test serve a hand-written JSON string instead of an
// encoded Go value, so the fixture stays readable.
type rawDocument string

func (d rawDocument) MarshalJSON() ([]byte, error) { return []byte(d), nil }
