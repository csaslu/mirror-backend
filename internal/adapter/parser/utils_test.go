package parser

import "testing"

func TestUnifySize(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want int64
	}{
		// Real values observed in TUNA's tunasync.json.
		{"tuna gigabytes", "69.98G", 75140452843}, // 69.98 * 2^30, truncated
		{"tuna whole gigabytes", "549G", 589484261376},
		{"tuna terabytes", "1.39T", 1528321162608},
		{"tuna megabytes", "188M", 197132288},

		// Regression: the previous parser matched the "B" case first, stripped
		// only the trailing "B" and failed to parse "10K" as a number, so every
		// KB/MB/GB/TB value silently became 0.
		{"kilobytes", "10KB", 10240},
		{"megabytes with B", "1.5MB", 1572864},
		{"gigabytes with B", "2GB", 2147483648},
		{"terabytes with B", "1TB", 1099511627776},
		{"binary kibibytes", "10KiB", 10240},
		{"binary mebibytes", "3MiB", 3145728},
		{"gibibytes", "1GiB", 1073741824},

		// Defensive cases.
		{"bare bytes", "1024", 1024},
		{"plain B", "512B", 512},
		{"lowercase", "10gb", 10737418240},
		{"padding and spacing", " 1.5 GB ", 1610612736},
		{"petabytes", "2PB", 2251799813685248},
		{"empty", "", 0},
		{"garbage", "abc", 0},
		{"unknown unit", "10X", 0},
		{"negative", "-5G", 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := UnifySize(tc.in); got != tc.want {
				t.Errorf("UnifySize(%q) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
}
