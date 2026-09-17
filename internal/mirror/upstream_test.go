package mirror

import "testing"

func TestParseUpstream(t *testing.T) {
	tests := []struct {
		name     string
		source   string
		key      string
		wantBase string
		wantPath string
		wantErr  bool
	}{
		{
			name:     "site root gains the key as its directory",
			source:   "https://mirrors.tuna.tsinghua.edu.cn",
			key:      "ubuntu",
			wantBase: "https://mirrors.tuna.tsinghua.edu.cn/ubuntu",
			wantPath: "/ubuntu",
		},
		{
			name:     "trailing slash on the root is ignored",
			source:   "https://mirrors.tuna.tsinghua.edu.cn/",
			key:      "debian",
			wantBase: "https://mirrors.tuna.tsinghua.edu.cn/debian",
			wantPath: "/debian",
		},
		{
			name:     "explicit path equal to the key is kept",
			source:   "https://mirror.nju.edu.cn/fedora-archive",
			key:      "fedora-archive",
			wantBase: "https://mirror.nju.edu.cn/fedora-archive",
			wantPath: "/fedora-archive",
		},
		{
			name:     "rsync sources are served over https",
			source:   "rsync://mirrors.tuna.tsinghua.edu.cn/openwrt",
			key:      "openwrt",
			wantBase: "https://mirrors.tuna.tsinghua.edu.cn/openwrt",
			wantPath: "/openwrt",
		},
		{
			name:     "explicit port is preserved",
			source:   "http://10.0.0.5:8080",
			key:      "ubuntu",
			wantBase: "http://10.0.0.5:8080/ubuntu",
			wantPath: "/ubuntu",
		},
		{
			name:    "path disagreeing with the key is an error",
			source:  "http://10.0.0.5/ubuntu",
			key:     "ubuntu-local",
			wantErr: true,
		},
		{
			name:    "empty source",
			source:  "",
			key:     "ubuntu",
			wantErr: true,
		},
		{
			name:     "key with a leading slash is normalised",
			source:   "https://mirrors.tuna.tsinghua.edu.cn",
			key:      "/ubuntu/",
			wantBase: "https://mirrors.tuna.tsinghua.edu.cn/ubuntu",
			wantPath: "/ubuntu",
		},
		{
			name:    "unknown scheme",
			source:  "ftp://example.com/ubuntu",
			key:     "ubuntu",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseUpstream(tc.source, tc.key)

			if tc.wantErr {
				if err == nil {
					t.Fatalf("ParseUpstream(%q, %q) error = nil, want an error", tc.source, tc.key)
				}
				return
			}

			if err != nil {
				t.Fatalf("ParseUpstream(%q, %q) error = %v", tc.source, tc.key, err)
			}
			if got.BaseURL != tc.wantBase {
				t.Errorf("BaseURL = %q, want %q", got.BaseURL, tc.wantBase)
			}
			if got.Path != tc.wantPath {
				t.Errorf("Path = %q, want %q", got.Path, tc.wantPath)
			}
		})
	}
}

func TestJoinPath(t *testing.T) {
	tests := []struct {
		name   string
		base   string
		rel    string
		want   string
		wantOK bool
	}{
		{name: "empty relative path is the base", base: "/", rel: "", want: "/", wantOK: true},
		{name: "slash relative path is the base", base: "/", rel: "/", want: "/", wantOK: true},
		{name: "single segment from root", base: "/", rel: "dists", want: "/dists", wantOK: true},
		{name: "leading slash is normalised", base: "/", rel: "/dists", want: "/dists", wantOK: true},
		{name: "nested segments", base: "/", rel: "dists/noble", want: "/dists/noble", wantOK: true},
		{name: "trailing slash is dropped", base: "/", rel: "dists/noble/", want: "/dists/noble", wantOK: true},
		{name: "base with a path", base: "/ubuntu", rel: "pool", want: "/ubuntu/pool", wantOK: true},
		{name: "empty base and rel", base: "", rel: "", want: "/", wantOK: true},
		{name: "duplicate slashes collapse", base: "/", rel: "a//b", want: "/a/b", wantOK: true},
		{name: "dot segments are dropped", base: "/", rel: "./a/./b", want: "/a/b", wantOK: true},
		{name: "backslashes are treated as separators", base: "/", rel: `a\b`, want: "/a/b", wantOK: true},
		{name: "parent traversal is refused", base: "/", rel: "..", wantOK: false},
		{name: "nested traversal is refused", base: "/", rel: "a/../../etc", wantOK: false},
		{name: "percent-encoded traversal is refused", base: "/", rel: "a/%2e%2e/b", wantOK: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := JoinPath(tc.base, tc.rel)
			if ok != tc.wantOK {
				t.Fatalf("JoinPath(%q, %q) ok = %v, want %v (got %q)", tc.base, tc.rel, ok, tc.wantOK, got)
			}
			if ok && got != tc.want {
				t.Errorf("JoinPath(%q, %q) = %q, want %q", tc.base, tc.rel, got, tc.want)
			}
		})
	}
}
