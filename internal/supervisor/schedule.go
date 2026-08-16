package supervisor

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"agytop/internal/config"
)

// ScheduleInfo represents parsed schedule details and next trigger timing
type ScheduleInfo struct {
	HasSchedule bool
	Description string
	NextRun     time.Time
}

var weekdayNames = []string{
	"Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday", "Sunday",
}

// ParseScheduleInfo extracts schedule configuration from sidecar args or cron expressions
func ParseScheduleInfo(cfg config.SidecarConfig, now time.Time) ScheduleInfo {
	// 1. Check CLI args for daemon schedules (e.g. --weekday 2 --hour 2 --minute 0 or --hour 4 --minute 0)
	var (
		weekday *int
		hour    *int
		minute  *int
	)

	args := cfg.Args
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--weekday":
			if i+1 < len(args) {
				if v, err := strconv.Atoi(args[i+1]); err == nil && v >= 0 && v <= 6 {
					w := v
					weekday = &w
				}
				i++
			}
		case "--hour":
			if i+1 < len(args) {
				if v, err := strconv.Atoi(args[i+1]); err == nil && v >= 0 && v <= 23 {
					h := v
					hour = &h
				}
				i++
			}
		case "--minute":
			if i+1 < len(args) {
				if v, err := strconv.Atoi(args[i+1]); err == nil && v >= 0 && v <= 59 {
					m := v
					minute = &m
				}
				i++
			}
		}
	}

	if hour != nil || minute != nil || weekday != nil {
		h := 0
		if hour != nil {
			h = *hour
		}
		m := 0
		if minute != nil {
			m = *minute
		}

		if weekday != nil {
			w := *weekday
			dayName := weekdayNames[w]
			desc := fmt.Sprintf("%ss @ %02d:%02d", dayName, h, m)
			next := calculateNextWeeklyTime(w, h, m, now)
			return ScheduleInfo{
				HasSchedule: true,
				Description: desc,
				NextRun:     next,
			}
		}

		desc := fmt.Sprintf("Daily @ %02d:%02d", h, m)
		next := calculateNextDailyTime(h, m, now)
		return ScheduleInfo{
			HasSchedule: true,
			Description: desc,
			NextRun:     next,
		}
	}

	// 2. Check declarative Schedule (cron expression in sidecar.json)
	if strings.TrimSpace(cfg.Schedule) != "" {
		if parsed, err := ParseCron(cfg.Schedule); err == nil {
			desc := formatCronDescription(cfg.Schedule)
			return ScheduleInfo{
				HasSchedule: true,
				Description: desc,
				NextRun:     parsed.Next(now),
			}
		}
		return ScheduleInfo{
			HasSchedule: false,
			Description: "",
			NextRun:     time.Time{},
		}
	}

	return ScheduleInfo{HasSchedule: false}
}

// formatCronDescription turns standard cron expressions or aliases into readable descriptions
func formatCronDescription(expr string) string {
	trimmed := strings.TrimSpace(expr)
	switch strings.ToLower(trimmed) {
	case "@daily", "@midnight":
		return "Daily @ 00:00"
	case "@hourly":
		return "Hourly (@ :00)"
	case "@weekly":
		return "Sundays @ 00:00"
	case "@monthly":
		return "Monthly (1st @ 00:00)"
	case "@yearly", "@annually":
		return "Annually (Jan 1st)"
	}

	fields := strings.Fields(trimmed)
	if len(fields) == 5 {
		minField, hourField, domField, monField, dowField := fields[0], fields[1], fields[2], fields[3], fields[4]
		if min, errM := strconv.Atoi(minField); errM == nil && min >= 0 && min <= 59 {
			if hr, errH := strconv.Atoi(hourField); errH == nil && hr >= 0 && hr <= 23 {
				if domField == "*" && monField == "*" {
					if dowField == "*" {
						return fmt.Sprintf("Daily @ %02d:%02d", hr, min)
					}
					if dow, errD := strconv.Atoi(dowField); errD == nil && dow >= 0 && dow <= 7 {
						pythonWeekday := 0
						if dow == 0 || dow == 7 {
							pythonWeekday = 6 // Sunday
						} else {
							pythonWeekday = dow - 1
						}
						return fmt.Sprintf("%ss @ %02d:%02d", weekdayNames[pythonWeekday], hr, min)
					}
				}
			}
		}
	}

	return fmt.Sprintf("Cron (%s)", expr)
}

// calculateNextDailyTime finds the next occurrence of daily HH:MM
func calculateNextDailyTime(targetHour, targetMinute int, now time.Time) time.Time {
	today := time.Date(now.Year(), now.Month(), now.Day(), targetHour, targetMinute, 0, 0, now.Location())
	if !today.After(now) {
		return today.AddDate(0, 0, 1)
	}
	return today
}

// calculateNextWeeklyTime finds the next occurrence of target weekday (0=Mon..6=Sun) at HH:MM
func calculateNextWeeklyTime(targetWeekday, targetHour, targetMinute int, now time.Time) time.Time {
	goTargetWeekday := time.Weekday((targetWeekday + 1) % 7)

	candidate := time.Date(now.Year(), now.Month(), now.Day(), targetHour, targetMinute, 0, 0, now.Location())

	daysAhead := int(goTargetWeekday - now.Weekday())
	if daysAhead < 0 {
		daysAhead += 7
	} else if daysAhead == 0 && !candidate.After(now) {
		daysAhead = 7
	}

	return candidate.AddDate(0, 0, daysAhead)
}

// FormatCountdown formats a duration until target time into a concise human string
func FormatCountdown(target, now time.Time) string {
	if target.IsZero() {
		return "N/A"
	}
	diff := target.Sub(now)
	if diff <= 0 {
		return "due now"
	}

	days := int(diff.Hours()) / 24
	hours := int(diff.Hours()) % 24
	minutes := int(diff.Minutes()) % 60

	if days > 0 {
		if hours > 0 {
			return fmt.Sprintf("in %dd %dh", days, hours)
		}
		return fmt.Sprintf("in %dd", days)
	}
	if hours > 0 {
		if minutes > 0 {
			return fmt.Sprintf("in %dh %dm", hours, minutes)
		}
		return fmt.Sprintf("in %dh", hours)
	}
	if minutes > 0 {
		return fmt.Sprintf("in %dm", minutes)
	}
	secs := int(diff.Seconds())
	if secs <= 0 {
		return "due now"
	}
	return fmt.Sprintf("in %ds", secs)
}
