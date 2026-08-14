package supervisor

import (
	"math"
	"os"
	"testing"
)

// TestFormatBytes derives every expected string directly from the
// implementation at process.go:371ff:
//
//	const unit = 1024
//	const units = "KMGTPE"
//	if b < unit { return fmt.Sprintf("%d B", b) }
//	div, exp := uint64(unit), 0
//	for n := b / unit; n >= unit; n /= unit { div *= unit; exp++ }
//	val := float64(b) / float64(div)
//	if val >= 1023.95 && exp+1 < len(units) {
//	    div *= unit; exp++; val = float64(b) / float64(div)
//	}
//	return fmt.Sprintf("%.1f %cB", val, units[exp])
//
// The tier is picked by integer division but rendered by float division, so
// without the promotion step the top sliver of each tier (e.g. 1048575
// bytes, one below the MB boundary) would render as "1024.0 KB" instead of
// rolling over to "1.0 MB". Every case (and the exact expected string) was
// verified by actually running FormatBytes against these inputs before
// being encoded here, rather than computed by hand.
func TestFormatBytes(t *testing.T) {
	tests := []struct {
		name string
		b    uint64
		want string
	}{
		// Byte range: b < 1024 formats as "%d B", no decimal.
		{"zero bytes", 0, "0 B"},
		{"one byte", 1, "1 B"},
		{"mid-range byte value", 500, "500 B"},
		{"last byte-formatted value (unit - 1)", 1023, "1023 B"},

		// KB boundary.
		{"first KB (unit boundary)", 1024, "1.0 KB"},
		{"just above KB boundary", 1025, "1.0 KB"},
		{"mid KB value", 1500, "1.5 KB"},
		{"another mid KB value", 1536, "1.5 KB"},
		// 1048575/1024 = 1023.9990234375, which rounds to 1024.0 under
		// "%.1f". The fix promotes this to the next tier instead of
		// printing the nonsensical "1024.0 KB".
		{"just below MB boundary rolls over to MB", 1048575, "1.0 MB"},

		// MB boundary.
		{"first MB (unit^2 boundary)", 1048576, "1.0 MB"},
		{"just above MB boundary", 1048577, "1.0 MB"},
		{"just below GB boundary rolls over to GB", 1073741823, "1.0 GB"},

		// GB boundary.
		{"first GB (unit^3 boundary)", 1073741824, "1.0 GB"},
		{"just below TB boundary rolls over to TB", 1099511627775, "1.0 TB"},

		// TB boundary.
		{"first TB (unit^4 boundary)", 1099511627776, "1.0 TB"},
		{"just below PB boundary rolls over to PB", 1125899906842623, "1.0 PB"},

		// PB boundary.
		{"first PB (unit^5 boundary)", 1125899906842624, "1.0 PB"},
		// PB is one tier below the last unit ("E"), so this sliver must
		// still roll over correctly rather than getting stuck at "1024.0 PB".
		{"just below EB boundary rolls over to EB", 1152921504606846975, "1.0 EB"},

		// EB is the LAST unit in "KMGTPE" -- there is nothing to promote
		// to, so the promotion guard (exp+1 < len(units)) must prevent an
		// out-of-range index here even though the rendered value is large.
		{"first EB (unit^6 boundary)", 1152921504606846976, "1.0 EB"},

		// math.MaxUint64 sits within the EB tier (1024^7 vastly exceeds
		// it), so this exercises the top of the uint64 range without ever
		// needing to promote past "E" -- proving the loop and the
		// promotion guard don't run off the end of "KMGTPE".
		{"math.MaxUint64 stays within EB, no index overrun", math.MaxUint64, "16.0 EB"},

		// Representative non-boundary values across a few units, so the
		// table also documents ordinary (non-edge-case) formatting.
		{"999999 bytes formats as KB", 999999, "976.6 KB"},
		{"123456789 bytes formats as MB", 123456789, "117.7 MB"},
		{"999999999999 bytes formats as GB", 999999999999, "931.3 GB"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FormatBytes(tt.b); got != tt.want {
				t.Errorf("FormatBytes(%d) = %q, want %q", tt.b, got, tt.want)
			}
		})
	}
}

// TestParseStatmRSS covers the pure-parsing extraction of the resident page
// count from /proc/<pid>/statm content, off fixture bytes rather than a live
// process so it runs on any OS.
func TestParseStatmRSS(t *testing.T) {
	// Must come from os.Getpagesize(), exactly as parseStatmRSS does -- it is
	// 4096 on Linux but 16384 on Apple Silicon, and hardcoding 4096 fails the
	// macOS CI legs while passing locally on Linux.
	pageSize := uint64(os.Getpagesize())
	tests := []struct {
		name    string
		data    string
		wantMem uint64
		wantOK  bool
	}{
		{"well-formed statm", "1000 250 100 1 0 900 0\n", 250 * pageSize, true},
		{"too few fields", "1000\n", 0, false},
		{"empty input", "", 0, false},
		{"non-numeric resident field", "1000 abc 100 1 0 900 0\n", 0, false},
		{"exactly two fields", "1000 42\n", 42 * pageSize, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseStatmRSS([]byte(tt.data))
			if ok != tt.wantOK {
				t.Fatalf("parseStatmRSS(%q) ok = %v, want %v", tt.data, ok, tt.wantOK)
			}
			if ok && got != tt.wantMem {
				t.Errorf("parseStatmRSS(%q) = %d, want %d", tt.data, got, tt.wantMem)
			}
		})
	}
}

// TestParseStatusVmRSS covers the pure-parsing extraction of VmRSS from
// /proc/<pid>/status content, off fixture bytes so it runs on any OS.
func TestParseStatusVmRSS(t *testing.T) {
	tests := []struct {
		name    string
		data    string
		wantMem uint64
		wantOK  bool
	}{
		{
			name:    "VmRSS is the first line",
			data:    "VmRSS:\t  12345 kB\n",
			wantMem: 12345 * 1024,
			wantOK:  true,
		},
		{
			name: "realistic multi-line block, VmRSS not first",
			data: "Name:\tbash\n" +
				"State:\tS (sleeping)\n" +
				"VmPeak:\t   20000 kB\n" +
				"VmSize:\t   19000 kB\n" +
				"VmRSS:\t    8192 kB\n" +
				"VmData:\t    5000 kB\n",
			wantMem: 8192 * 1024,
			wantOK:  true,
		},
		{
			name:    "VmRSS absent entirely",
			data:    "Name:\tbash\nState:\tS (sleeping)\nVmSize:\t   19000 kB\n",
			wantMem: 0,
			wantOK:  false,
		},
		{
			name:    "empty input",
			data:    "",
			wantMem: 0,
			wantOK:  false,
		},
		{
			name:    "VmRSS line with non-numeric value",
			data:    "VmRSS:\t  notanumber kB\n",
			wantMem: 0,
			wantOK:  false,
		},
		{
			name:    "VmRSS line with too few fields",
			data:    "VmRSS:\n",
			wantMem: 0,
			wantOK:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseStatusVmRSS([]byte(tt.data))
			if ok != tt.wantOK {
				t.Fatalf("parseStatusVmRSS(%q) ok = %v, want %v", tt.data, ok, tt.wantOK)
			}
			if ok && got != tt.wantMem {
				t.Errorf("parseStatusVmRSS(%q) = %d, want %d", tt.data, got, tt.wantMem)
			}
		})
	}
}
