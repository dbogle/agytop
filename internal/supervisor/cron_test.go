package supervisor

import (
	"strings"
	"testing"
	"time"
)

func TestParseCronValidForms(t *testing.T) {
	cases := []struct {
		name string
		expr string
	}{
		{"wildcard everywhere", "* * * * *"},
		{"fixed values", "5 4 3 2 1"},
		{"simple range", "1-5 * * * *"},
		{"step on wildcard", "*/15 * * * *"},
		{"range with step", "10-30/5 * * * *"},
		{"comma list", "1,15,30,45 * * * *"},
		{"mixed list of value/range/step", "0,10-20,*/30 * * * *"},
		{"dow accepts 7 as Sunday", "0 0 * * 7"},
		{"dow range crossing 7/0", "0 0 * * 5-7"},
		{"leading/trailing whitespace", "  0 0 * * *  "},
		{"extra internal spacing", "0   0   *  *  *"},
		{"alias hourly", "@hourly"},
		{"alias daily", "@daily"},
		{"alias midnight", "@midnight"},
		{"alias weekly", "@weekly"},
		{"alias monthly", "@monthly"},
		{"alias yearly", "@yearly"},
		{"alias annually", "@annually"},
		{"alias case insensitive", "@Daily"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cs, err := ParseCron(tc.expr)
			if err != nil {
				t.Fatalf("ParseCron(%q) unexpected error: %v", tc.expr, err)
			}
			if cs == nil {
				t.Fatalf("ParseCron(%q) returned nil schedule with no error", tc.expr)
			}
		})
	}
}

func TestParseCronInvalidForms(t *testing.T) {
	cases := []struct {
		name string
		expr string
	}{
		{"empty string", ""},
		{"whitespace only", "   "},
		{"too few fields", "* * * *"},
		{"too many fields", "* * * * * *"},
		{"single garbage token", "not-a-cron-expr"},
		{"minute out of range", "60 * * * *"},
		{"minute negative-ish garbage", "-1 * * * *"},
		{"hour out of range", "* 24 * * *"},
		{"day-of-month zero", "* * 0 * *"},
		{"day-of-month out of range", "* * 32 * *"},
		{"month zero", "* * * 0 *"},
		{"month out of range", "* * * 13 *"},
		{"day-of-week out of range", "* * * * 8"},
		{"step zero", "*/0 * * * *"},
		{"step negative", "*/-5 * * * *"},
		{"step non-numeric", "*/abc * * * *"},
		{"range reversed", "5-3 * * * *"},
		{"range missing end", "5- * * * *"},
		{"range missing start", "-5 * * * *"},
		{"range non-numeric", "a-b * * * *"},
		{"list with empty item", "1,,3 * * * *"},
		{"unknown alias", "@bogus"},
		{"alias with fields", "@daily * * * *"},
		{"value non-numeric", "x * * * *"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cs, err := ParseCron(tc.expr)
			if err == nil {
				t.Fatalf("ParseCron(%q) expected an error, got schedule %+v", tc.expr, cs)
			}
			if cs != nil {
				t.Errorf("ParseCron(%q) returned non-nil schedule alongside an error", tc.expr)
			}
		})
	}
}

// utc is a small helper to keep the Next() table below terse and immune to
// host timezone -- every reference and expected time in this file is
// constructed explicitly in UTC.
func utc(year int, month time.Month, day, hour, min, sec int) time.Time {
	return time.Date(year, month, day, hour, min, sec, 0, time.UTC)
}

func TestCronScheduleNext(t *testing.T) {
	cases := []struct {
		name  string
		expr  string
		after time.Time
		want  time.Time
	}{
		{
			name:  "daily midnight, same day",
			expr:  "0 0 * * *",
			after: utc(2024, 1, 10, 12, 0, 0),
			want:  utc(2024, 1, 11, 0, 0, 0),
		},
		{
			name:  "next is strictly after an exact match instant",
			expr:  "* * * * *",
			after: utc(2024, 1, 1, 0, 5, 0),
			want:  utc(2024, 1, 1, 0, 6, 0),
		},
		{
			name:  "month rollover: first of month from mid-month",
			expr:  "0 0 1 * *",
			after: utc(2024, 1, 15, 9, 0, 0),
			want:  utc(2024, 2, 1, 0, 0, 0),
		},
		{
			name:  "year rollover: Jan 1 from mid-year",
			expr:  "0 0 1 1 *",
			after: utc(2024, 6, 1, 0, 0, 0),
			want:  utc(2025, 1, 1, 0, 0, 0),
		},
		{
			name:  "leap year Feb 29 skips non-leap years",
			expr:  "0 0 29 2 *",
			after: utc(2023, 1, 1, 0, 0, 0),
			want:  utc(2024, 2, 29, 0, 0, 0),
		},
		{
			name:  "leap year Feb 29 from just after itself lands on next leap year",
			expr:  "0 0 29 2 *",
			after: utc(2024, 2, 29, 0, 1, 0),
			want:  utc(2028, 2, 29, 0, 0, 0),
		},
		{
			name: "dom/dow both restricted: either matches (not both) -- next Monday " +
				"beats waiting for day-1-that-is-also-Monday",
			expr:  "0 12 1 * 1",
			after: utc(2024, 3, 1, 13, 0, 0), // Fri Mar 1 2024, just after that day's noon fire
			want:  utc(2024, 3, 4, 12, 0, 0), // Mon Mar 4 2024 -- matches via dow, not dom
		},
		{
			name:  "step on wildcard",
			expr:  "*/15 * * * *",
			after: utc(2024, 1, 1, 0, 5, 0),
			want:  utc(2024, 1, 1, 0, 15, 0),
		},
		{
			name:  "ranged step",
			expr:  "10-30/5 * * * *",
			after: utc(2024, 1, 1, 0, 12, 0),
			want:  utc(2024, 1, 1, 0, 15, 0),
		},
		{
			name:  "ranged step, past the range falls to next hour's start",
			expr:  "10-30/5 * * * *",
			after: utc(2024, 1, 1, 0, 31, 0),
			want:  utc(2024, 1, 1, 1, 10, 0),
		},
		{
			name:  "comma list of minutes",
			expr:  "0,15,30,45 * * * *",
			after: utc(2024, 1, 1, 0, 16, 0),
			want:  utc(2024, 1, 1, 0, 30, 0),
		},
		{
			name:  "dow-only restriction: every Sunday midnight",
			expr:  "0 0 * * 0",
			after: utc(2024, 1, 1, 0, 0, 0), // Monday Jan 1 2024
			want:  utc(2024, 1, 7, 0, 0, 0), // Sunday Jan 7 2024
		},
		{
			name:  "dow 7 is an alias for Sunday",
			expr:  "0 0 * * 7",
			after: utc(2024, 1, 1, 0, 0, 0),
			want:  utc(2024, 1, 7, 0, 0, 0),
		},
		{
			name:  "end-of-year exact boundary rolls to next year",
			expr:  "30 23 31 12 *",
			after: utc(2023, 12, 31, 23, 31, 0),
			want:  utc(2024, 12, 31, 23, 30, 0),
		},
		{
			name:  "@hourly alias fires on the hour",
			expr:  "@hourly",
			after: utc(2024, 1, 1, 5, 30, 0),
			want:  utc(2024, 1, 1, 6, 0, 0),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cs, err := ParseCron(tc.expr)
			if err != nil {
				t.Fatalf("ParseCron(%q) failed: %v", tc.expr, err)
			}
			got := cs.Next(tc.after)
			if !got.Equal(tc.want) {
				t.Errorf("Next(%v) for %q = %v, want %v", tc.after, tc.expr, got, tc.want)
			}
		})
	}
}

// TestCronScheduleNextUnsatisfiable exercises the loop guard: a day-of-month
// value that can never coexist with a month restricted to a shorter month
// (Feb never has 30 days) must return the zero time rather than spinning
// forever searching for a day that will never arrive.
func TestCronScheduleNextUnsatisfiable(t *testing.T) {
	cs, err := ParseCron("0 0 30 2 *")
	if err != nil {
		t.Fatalf("ParseCron failed: %v", err)
	}

	done := make(chan time.Time, 1)
	go func() { done <- cs.Next(utc(2024, 1, 1, 0, 0, 0)) }()

	select {
	case got := <-done:
		if !got.IsZero() {
			t.Errorf("Next() for an unsatisfiable schedule = %v, want zero time", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Next() did not return within 5s for an unsatisfiable schedule -- possible infinite loop")
	}
}

// TestCronScheduleString confirms the original (unexpanded) expression is
// preserved for display purposes even when it was an '@' alias.
func TestCronScheduleString(t *testing.T) {
	cs, err := ParseCron("@daily")
	if err != nil {
		t.Fatalf("ParseCron failed: %v", err)
	}
	if got := cs.String(); got != "@daily" {
		t.Errorf("String() = %q, want %q", got, "@daily")
	}
}

// TestParseCronErrorMessagesAreDescriptive is a light sanity check that
// error messages name the field/expression at fault rather than being a bare
// generic string -- useful when this surfaces in LastError in the TUI.
func TestParseCronErrorMessagesAreDescriptive(t *testing.T) {
	_, err := ParseCron("60 * * * *")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "minute") {
		t.Errorf("error %q does not mention the offending field", err.Error())
	}
}
