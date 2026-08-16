package supervisor

import (
	"testing"
	"time"

	"agytop/internal/config"
)

func TestParseScheduleInfoDaemonArgs(t *testing.T) {
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC) // Saturday

	// 1. Weekly schedule: Wednesdays at 02:00
	cfgWeekly := config.SidecarConfig{
		Args: []string{"scanner.py", "--daemon", "--weekday", "2", "--hour", "2", "--minute", "0"},
	}
	infoWeekly := ParseScheduleInfo(cfgWeekly, now)
	if !infoWeekly.HasSchedule {
		t.Fatalf("expected HasSchedule true for weekly daemon args")
	}
	if infoWeekly.Description != "Wednesdays @ 02:00" {
		t.Errorf("expected Description 'Wednesdays @ 02:00', got %q", infoWeekly.Description)
	}
	// Next Wednesday from Saturday Aug 15 is Aug 19 at 02:00
	wantNextWeekly := time.Date(2026, 8, 19, 2, 0, 0, 0, time.UTC)
	if !infoWeekly.NextRun.Equal(wantNextWeekly) {
		t.Errorf("NextRun = %v, want %v", infoWeekly.NextRun, wantNextWeekly)
	}

	// 2. Daily schedule: Daily at 04:00
	cfgDaily := config.SidecarConfig{
		Args: []string{"scanner.py", "--daemon", "--hour", "4", "--minute", "0"},
	}
	infoDaily := ParseScheduleInfo(cfgDaily, now)
	if !infoDaily.HasSchedule {
		t.Fatalf("expected HasSchedule true for daily daemon args")
	}
	if infoDaily.Description != "Daily @ 04:00" {
		t.Errorf("expected Description 'Daily @ 04:00', got %q", infoDaily.Description)
	}
	// Next 04:00 from 12:00 on Aug 15 is Aug 16 at 04:00
	wantNextDaily := time.Date(2026, 8, 16, 4, 0, 0, 0, time.UTC)
	if !infoDaily.NextRun.Equal(wantNextDaily) {
		t.Errorf("NextRun = %v, want %v", infoDaily.NextRun, wantNextDaily)
	}
}

func TestParseScheduleInfoCron(t *testing.T) {
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)

	cfgCron := config.SidecarConfig{
		Builtin:  "schedule",
		Schedule: "0 0 * * *",
	}
	info := ParseScheduleInfo(cfgCron, now)
	if !info.HasSchedule {
		t.Fatalf("expected HasSchedule true for valid cron")
	}
	if info.Description != "Daily @ 00:00" {
		t.Errorf("expected Description 'Daily @ 00:00', got %q", info.Description)
	}
}

func TestFormatCountdown(t *testing.T) {
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		target time.Time
		want   string
	}{
		{now.Add(3*24*time.Hour + 6*time.Hour), "in 3d 6h"},
		{now.Add(3 * 24 * time.Hour), "in 3d"},
		{now.Add(5*time.Hour + 30*time.Minute), "in 5h 30m"},
		{now.Add(45 * time.Minute), "in 45m"},
		{now.Add(30 * time.Second), "in 30s"},
		{now.Add(-10 * time.Minute), "due now"},
		{time.Time{}, "N/A"},
	}

	for _, tc := range tests {
		got := FormatCountdown(tc.target, now)
		if got != tc.want {
			t.Errorf("FormatCountdown(%v) = %q, want %q", tc.target, got, tc.want)
		}
	}
}
