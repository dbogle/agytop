package ui

import (
	"math"
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

var ansiRE = regexp.MustCompile("\x1b\\[[0-9;]*m")

func visible(s string) string { return ansiRE.ReplaceAllString(s, "") }

func TestRenderSparkline(t *testing.T) {
	tests := []struct {
		name      string
		values    []float64
		maxVal    float64
		width     int
		wantWidth int
	}{
		{"empty values pad to full width", nil, 100.0, 10, 10},
		{"single value is left-padded", []float64{50.0}, 100.0, 5, 5},
		{"more values than width clips to newest", []float64{10, 20, 30, 40, 50}, 100.0, 3, 3},
		{"zero width renders nothing", []float64{10, 20}, 100.0, 0, 0},
		{"non-positive maxVal must not divide by zero", []float64{5, 5}, 0, 4, 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := lipgloss.Width(renderSparkline(tt.values, tt.maxVal, tt.width)); got != tt.wantWidth {
				t.Errorf("visible width = %d, want %d", got, tt.wantWidth)
			}
		})
	}
}

// Pin the ratio-to-block mapping at both endpoints so a change to sparkBlocks
// cannot pass silently.
func TestRenderSparklineBlockSelection(t *testing.T) {
	got := visible(renderSparkline([]float64{0, 100}, 100.0, 2))
	want := string([]rune{sparkBlocks[0], sparkBlocks[len(sparkBlocks)-1]})
	if got != want {
		t.Errorf("block selection = %q, want %q", got, want)
	}
}

func TestRenderAsciiGauge(t *testing.T) {
	tests := []struct {
		name       string
		val        float64
		maxVal     float64
		width      int
		wantFilled int
	}{
		{"empty", 0.0, 100.0, 10, 0},
		{"half", 50.0, 100.0, 10, 5},
		{"full", 100.0, 100.0, 10, 10},
		{"overflow clamps to full", 250.0, 100.0, 10, 10},
		{"negative clamps to empty", -5.0, 100.0, 10, 0},
		{"rounds to nearest cell", 44.0, 100.0, 10, 4},
		{"non-positive maxVal defaults to 100", 50.0, 0, 10, 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := visible(renderAsciiGauge(tt.val, tt.maxVal, tt.width))
			filled := strings.Count(got, "█")
			empty := strings.Count(got, "░")

			if filled != tt.wantFilled {
				t.Errorf("filled cells = %d, want %d", filled, tt.wantFilled)
			}
			if filled+empty != tt.width {
				t.Errorf("total cells = %d, want %d", filled+empty, tt.width)
			}
		})
	}
}

// TestUint64sToFloat64s exercises element-wise conversion and the empty-input
// case. uint64sToFloat64s always allocates via make([]float64, len(values)),
// so even a nil input yields a non-nil, zero-length slice -- asserted here
// rather than assumed.
func TestUint64sToFloat64s(t *testing.T) {
	t.Run("element-wise conversion", func(t *testing.T) {
		in := []uint64{0, 1, 1024, 1048576, 3}
		got := uint64sToFloat64s(in)
		want := []float64{0, 1, 1024, 1048576, 3}
		if len(got) != len(want) {
			t.Fatalf("len = %d, want %d", len(got), len(want))
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("index %d = %v, want %v", i, got[i], want[i])
			}
		}
	})

	t.Run("empty input", func(t *testing.T) {
		got := uint64sToFloat64s(nil)
		if got == nil {
			t.Error("uint64sToFloat64s(nil) returned nil; want a non-nil zero-length slice (make([]float64, 0))")
		}
		if len(got) != 0 {
			t.Errorf("len = %d, want 0", len(got))
		}

		got2 := uint64sToFloat64s([]uint64{})
		if got2 == nil {
			t.Error("uint64sToFloat64s([]uint64{}) returned nil; want a non-nil zero-length slice")
		}
	})

	// NOTE: float64 has a 53-bit mantissa, so uint64 values above 2^53 cannot
	// all be represented exactly. This is plain `float64(v)` Go semantics
	// (uint64sToFloat64s does nothing beyond that conversion) -- documented
	// here, not treated as a bug. math.MaxUint64 and math.MaxUint64-1 are
	// adjacent, distinct uint64 values that collide onto the identical
	// float64 once converted, confirmed independently via `go run` before
	// being encoded as an assertion.
	t.Run("large value near top of uint64 loses precision", func(t *testing.T) {
		v1 := uint64(math.MaxUint64)
		v2 := v1 - 1 // distinct from v1, but collides after conversion

		got := uint64sToFloat64s([]uint64{v1, v2})

		wantF := float64(v1) // == 1.8446744073709552e+19
		if got[0] != wantF {
			t.Errorf("got[0] = %v, want %v", got[0], wantF)
		}
		if got[1] != wantF {
			t.Errorf("got[1] = %v, want %v (collided with got[0] due to float64 precision)", got[1], wantF)
		}
		if got[0] != got[1] {
			t.Errorf("expected got[0] == got[1] (precision collision), got %v != %v", got[0], got[1])
		}

		// A value 2000 below MaxUint64 is far enough from the rounding
		// boundary to convert to a distinguishable float64, showing the
		// collision above is specifically a top-of-range phenomenon rather
		// than uint64sToFloat64s being broken for all large inputs.
		v3 := v1 - 2000
		got3 := uint64sToFloat64s([]uint64{v3})
		if got3[0] == wantF {
			t.Errorf("got3[0] = %v unexpectedly collided with MaxUint64's float64 %v", got3[0], wantF)
		}
	})
}
