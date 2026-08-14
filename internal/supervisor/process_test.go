package supervisor

import "testing"

// TestFormatBytes derives every expected string directly from the
// implementation at process.go:371-382:
//
//	const unit = 1024
//	if b < unit { return fmt.Sprintf("%d B", b) }
//	div, exp := uint64(unit), 0
//	for n := b / unit; n >= unit; n /= unit { div *= unit; exp++ }
//	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
//
// Every case (and the exact expected string, including the "%.1f" rounding
// behavior right below a unit boundary) was verified by actually running
// FormatBytes against these inputs before being encoded here, rather than
// computed by hand -- see the just-below-boundary cases below, which round
// up to the next whole unit rather than down.
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
		// "%.1f" -- it does NOT print as "1023.9 KB" or roll over to "1.0 MB".
		{"just below MB boundary rounds up within KB", 1048575, "1024.0 KB"},

		// MB boundary.
		{"first MB (unit^2 boundary)", 1048576, "1.0 MB"},
		{"just above MB boundary", 1048577, "1.0 MB"},
		{"just below GB boundary rounds up within MB", 1073741823, "1024.0 MB"},

		// GB boundary.
		{"first GB (unit^3 boundary)", 1073741824, "1.0 GB"},
		{"just below TB boundary rounds up within GB", 1099511627775, "1024.0 GB"},

		// TB boundary.
		{"first TB (unit^4 boundary)", 1099511627776, "1.0 TB"},
		{"just below PB boundary rounds up within TB", 1125899906842623, "1024.0 TB"},

		// PB boundary.
		{"first PB (unit^5 boundary)", 1125899906842624, "1.0 PB"},

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
