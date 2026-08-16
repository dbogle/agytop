package supervisor

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// CronSchedule is a parsed standard 5-field cron expression:
//
//	minute hour day-of-month month day-of-week
//
// It supports "*", fixed values, ranges ("a-b"), steps ("*/n" or "a-b/n"),
// comma-separated lists of any of the above, and the common '@' aliases
// (see cronAliases). Field bounds are minute 0-59, hour 0-23, day-of-month
// 1-31, month 1-12, and day-of-week 0-6 (7 is also accepted as an alias for
// Sunday, matching common cron dialects).
//
// Lives in internal/supervisor rather than internal/config because it is
// purely a runtime scheduling concern consumed by runBuiltinScheduleLoop
// (and, indirectly, NewSidecarState's initial NextScheduleRun estimate) --
// internal/config stays a declarative discovery/schema package that never
// otherwise reasons about time.
type CronSchedule struct {
	minute [60]bool // index 0-59
	hour   [24]bool // index 0-23
	dom    [32]bool // index 1-31 (0 unused)
	month  [13]bool // index 1-12 (0 unused)
	dow    [7]bool  // index 0-6, Sunday=0 (7 folds into 0 at parse time)

	// domRestricted/dowRestricted record whether the day-of-month / day-of-week
	// field was anything other than "*". Standard cron semantics: when BOTH
	// fields are restricted, a day matches if EITHER matches (logical OR),
	// not both (logical AND). This is the most commonly mis-implemented part
	// of cron -- see dayMatches below.
	domRestricted bool
	dowRestricted bool

	expr string
}

// String returns the original expression this schedule was parsed from.
func (c *CronSchedule) String() string {
	return c.expr
}

// cronAliases expands the common non-standard '@' shorthands to their
// 5-field equivalents before normal parsing.
var cronAliases = map[string]string{
	"@hourly":   "0 * * * *",
	"@daily":    "0 0 * * *",
	"@midnight": "0 0 * * *",
	"@weekly":   "0 0 * * 0",
	"@monthly":  "0 0 1 * *",
	"@yearly":   "0 0 1 1 *",
	"@annually": "0 0 1 1 *",
}

// fieldSpec describes the syntactic bounds of one cron field for parsing and
// validation purposes. Note this is the *raw accepted* range -- for
// day-of-week, min/max is 0-7 (7 accepted as an alias for Sunday), even
// though the underlying storage folds 7 down to index 0.
type fieldSpec struct {
	name     string
	min, max int
}

// ParseCron parses a standard 5-field cron expression (or one of the '@'
// aliases) and returns a CronSchedule that can compute future fire times via
// Next. It rejects malformed input with a descriptive error rather than
// guessing at intent.
func ParseCron(expr string) (*CronSchedule, error) {
	original := expr
	trimmed := strings.TrimSpace(expr)
	if trimmed == "" {
		return nil, fmt.Errorf("cron: empty expression")
	}

	if strings.HasPrefix(trimmed, "@") {
		alias := strings.ToLower(trimmed)
		expanded, ok := cronAliases[alias]
		if !ok {
			return nil, fmt.Errorf("cron: unknown alias %q", original)
		}
		trimmed = expanded
	}

	fields := strings.Fields(trimmed)
	if len(fields) != 5 {
		return nil, fmt.Errorf("cron: expected 5 fields (minute hour day-of-month month day-of-week), got %d in %q", len(fields), original)
	}

	cs := &CronSchedule{expr: original}

	minuteSpec := fieldSpec{"minute", 0, 59}
	if err := parseField(fields[0], minuteSpec, func(v int) { cs.minute[v] = true }); err != nil {
		return nil, err
	}

	hourSpec := fieldSpec{"hour", 0, 23}
	if err := parseField(fields[1], hourSpec, func(v int) { cs.hour[v] = true }); err != nil {
		return nil, err
	}

	domSpec := fieldSpec{"day-of-month", 1, 31}
	if err := parseField(fields[2], domSpec, func(v int) { cs.dom[v] = true }); err != nil {
		return nil, err
	}
	cs.domRestricted = strings.TrimSpace(fields[2]) != "*"

	monthSpec := fieldSpec{"month", 1, 12}
	if err := parseField(fields[3], monthSpec, func(v int) { cs.month[v] = true }); err != nil {
		return nil, err
	}

	dowSpec := fieldSpec{"day-of-week", 0, 7}
	if err := parseField(fields[4], dowSpec, func(v int) { cs.dow[v%7] = true }); err != nil {
		return nil, err
	}
	cs.dowRestricted = strings.TrimSpace(fields[4]) != "*"

	return cs, nil
}

// parseField parses one comma-separated cron field ("*", a value, a range
// "a-b", or any of those with a "/n" step) and invokes set(v) for every
// matching value in spec's bounds. set is expected to record v (e.g. by
// setting dst[v] = true); it is a callback rather than a plain slice so the
// day-of-week field can fold 7 -> 0 at the point of assignment without
// distorting range comparisons (a "5-7" range must consider 7 the literal
// upper bound, not 0).
func parseField(raw string, spec fieldSpec, set func(int)) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fmt.Errorf("cron: empty %s field", spec.name)
	}

	for _, part := range strings.Split(raw, ",") {
		if part == "" {
			return fmt.Errorf("cron: %s field has an empty list item in %q", spec.name, raw)
		}

		base := part
		step := 1
		if idx := strings.IndexByte(part, '/'); idx >= 0 {
			base = part[:idx]
			stepStr := part[idx+1:]
			n, err := strconv.Atoi(stepStr)
			if err != nil || n <= 0 {
				return fmt.Errorf("cron: invalid step %q in %s field %q", stepStr, spec.name, raw)
			}
			step = n
		}

		rangeStart, rangeEnd := spec.min, spec.max

		switch {
		case base == "*":
			// Full field range; rangeStart/rangeEnd already set above.

		case strings.Contains(base, "-"):
			bounds := strings.SplitN(base, "-", 2)
			if len(bounds) != 2 || bounds[0] == "" || bounds[1] == "" {
				return fmt.Errorf("cron: invalid range %q in %s field %q", base, spec.name, raw)
			}
			a, errA := strconv.Atoi(bounds[0])
			b, errB := strconv.Atoi(bounds[1])
			if errA != nil || errB != nil {
				return fmt.Errorf("cron: invalid range %q in %s field %q", base, spec.name, raw)
			}
			if a < spec.min || a > spec.max || b < spec.min || b > spec.max {
				return fmt.Errorf("cron: %s value out of range [%d-%d] in %q", spec.name, spec.min, spec.max, raw)
			}
			if a > b {
				return fmt.Errorf("cron: invalid range %q in %s field %q (start > end)", base, spec.name, raw)
			}
			rangeStart, rangeEnd = a, b

		default:
			v, err := strconv.Atoi(base)
			if err != nil {
				return fmt.Errorf("cron: invalid value %q in %s field %q", base, spec.name, raw)
			}
			if v < spec.min || v > spec.max {
				return fmt.Errorf("cron: %s value %d out of range [%d-%d]", spec.name, v, spec.min, spec.max)
			}
			rangeStart, rangeEnd = v, v
		}

		for v := rangeStart; v <= rangeEnd; v += step {
			set(v)
		}
	}

	return nil
}

// maxCronSearchYears bounds how far into the future Next will search before
// giving up. Guards against spinning forever on a schedule that can never
// match (e.g. day-of-month 31 combined with month restricted to February).
const maxCronSearchYears = 4

// Next returns the next instant strictly after 'after' at which the schedule
// fires, truncated to second-zero precision (cron has no sub-minute
// resolution). It returns the zero time.Time if no match exists within
// maxCronSearchYears of 'after' -- rather than spinning indefinitely -- which
// can legitimately happen for an unsatisfiable day-of-month/month
// combination.
//
// Day-of-month/day-of-week semantics: when both fields are restricted
// (neither is "*"), a day matches if EITHER field matches (the standard,
// if frequently misimplemented, cron rule) -- not both. See dayMatches.
func (c *CronSchedule) Next(after time.Time) time.Time {
	loc := after.Location()

	// Candidate starts at the next whole minute strictly after 'after'.
	t := time.Date(after.Year(), after.Month(), after.Day(), after.Hour(), after.Minute(), 0, 0, loc).Add(time.Minute)

	limit := t.AddDate(maxCronSearchYears, 0, 0)

	for {
		if t.After(limit) {
			return time.Time{}
		}

		if !c.month[int(t.Month())] {
			// Jump to the first moment of next month; always > t.
			t = time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, loc).AddDate(0, 1, 0)
			continue
		}

		if !c.dayMatches(t.Day(), t.Weekday()) {
			// Jump to midnight the next day; always > t.
			t = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, 1)
			continue
		}

		if !c.hour[t.Hour()] {
			// Jump to the top of the next hour; always > t.
			t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 0, 0, 0, loc).Add(time.Hour)
			continue
		}

		if !c.minute[t.Minute()] {
			t = t.Add(time.Minute)
			continue
		}

		return t
	}
}

// dayMatches implements the dom/dow "either restricted field matches" rule.
// time.Weekday numbers Sunday=0..Saturday=6, matching cron's day-of-week
// convention (with 7 folded to 0 at parse time), so weekday can be used to
// index c.dow directly.
func (c *CronSchedule) dayMatches(day int, weekday time.Weekday) bool {
	domOK := c.dom[day]
	dowOK := c.dow[int(weekday)]

	switch {
	case c.domRestricted && c.dowRestricted:
		return domOK || dowOK
	case c.domRestricted:
		return domOK
	case c.dowRestricted:
		return dowOK
	default:
		return true
	}
}
