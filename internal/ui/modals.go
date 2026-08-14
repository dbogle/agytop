package ui

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"agytop/internal/supervisor"
)

// RenderDryRunModal formats the dry-run diagnostics dialog with the Stitch design system
func RenderDryRunModal(dr *supervisor.DryRunResult, width, height int) string {
	if dr == nil {
		return ""
	}

	statusPill := BadgeRunning.Copy().SetString(" ✓ VALIDATION PASSED ").Background(ColorPrimary).Foreground(lipgloss.Color("#002109"))
	if !dr.Success || dr.ExitCode != 0 {
		statusPill = BadgeFailed.Copy().SetString(fmt.Sprintf(" ✗ VALIDATION FAILED (EXIT %d) ", dr.ExitCode))
	}

	var b strings.Builder
	b.WriteString(ModalTitleStyle.Render("⚡ ANTIGRAVITY_2.0 // DRY_RUN_DIAGNOSTICS") + "\n\n")

	b.WriteString(fmt.Sprintf("%s  %s  %s\n\n",
		FooterKeyStyle.Render("TARGET: "+dr.SidecarID),
		statusPill.Render(),
		lipgloss.NewStyle().Foreground(ColorMuted).Render(fmt.Sprintf("LATENCY: %v", dr.Duration.Round(100*1000))),
	))

	// Diagnostics Checklist
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(ColorSecondary).Render("DIAGNOSTIC CHECKLIST") + "\n")
	b.WriteString(lipgloss.NewStyle().Foreground(ColorBorder).Render(strings.Repeat("─", 60)) + "\n")
	for _, msg := range dr.ValidationMsgs {
		if strings.HasPrefix(msg, "✓") {
			b.WriteString(lipgloss.NewStyle().Foreground(ColorPrimary).Render(" [✓] ") + lipgloss.NewStyle().Foreground(ColorText).Render(strings.TrimPrefix(msg, "✓ ")) + "\n")
		} else {
			b.WriteString(lipgloss.NewStyle().Foreground(ColorDanger).Render(" [✗] ") + lipgloss.NewStyle().Foreground(ColorDanger).Render(strings.TrimPrefix(msg, "✗ ")) + "\n")
		}
	}
	b.WriteString("\n")

	// Environment & Invocation Details
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(ColorSecondary).Render("EXECUTION ENVIRONMENT") + "\n")
	b.WriteString(lipgloss.NewStyle().Foreground(ColorBorder).Render(strings.Repeat("─", 60)) + "\n")
	b.WriteString(fmt.Sprintf("  BIN  : %s\n", dr.ExecutablePath))
	b.WriteString(fmt.Sprintf("  DIR  : %s\n", dr.WorkingDir))
	b.WriteString(fmt.Sprintf("  ENV  : AGY_DRY_RUN=1, DRY_RUN=true, ANTIGRAVITY_SIDECAR_DRY_RUN=1\n\n"))

	// Simulated Cron / Schedule Timeline
	if len(dr.NextSchedules) > 0 {
		b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(ColorSecondary).Render("SIMULATED CRON TIMELINE") + "\n")
		b.WriteString(lipgloss.NewStyle().Foreground(ColorBorder).Render(strings.Repeat("─", 60)) + "\n")
		b.WriteString("  [NOW] ───► [T+1m] ───► [T+2m] ───► [T+3m] ───► [T+4m] ───► [T+5m]\n")
		for _, s := range dr.NextSchedules {
			b.WriteString(fmt.Sprintf("  • Trigger: %s\n", s))
		}
		b.WriteString("\n")
	}

	// Captured output
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(ColorSecondary).Render("CAPTURED PROBE OUTPUT") + "\n")
	b.WriteString(lipgloss.NewStyle().Foreground(ColorBorder).Render(strings.Repeat("─", 60)) + "\n")
	if len(dr.Logs) == 0 {
		b.WriteString(lipgloss.NewStyle().Foreground(ColorMuted).Render("  (No stdout/stderr stream emitted during dry run)") + "\n")
	} else {
		for i, l := range dr.Logs {
			if i > 8 {
				b.WriteString(lipgloss.NewStyle().Foreground(ColorMuted).Render(fmt.Sprintf("  ... (+%d more lines)", len(dr.Logs)-8)) + "\n")
				break
			}
			srcBadge := lipgloss.NewStyle().Foreground(ColorInfo).Render(fmt.Sprintf("[%s]", l.Source))
			b.WriteString(fmt.Sprintf("  %s %s\n", srcBadge, l.Text))
		}
	}

	b.WriteString("\n" + lipgloss.NewStyle().Foreground(ColorMuted).Render("Press [Esc] or [d] to dismiss dialog"))

	boxWidth := width - 8
	if boxWidth > 86 {
		boxWidth = 86
	}
	if boxWidth < 50 {
		boxWidth = 50
	}

	modal := ModalBoxStyle.Width(boxWidth).Render(b.String())
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, modal)
}

// RenderHelpModal renders the keybinding cheat sheet
func RenderHelpModal(width, height int) string {
	var b strings.Builder
	b.WriteString(ModalTitleStyle.Render("⚡ ANTIGRAVITY_2.0 // OPERATOR_KEYBINDINGS") + "\n\n")

	categories := []struct {
		Title string
		Items [][2]string
	}{
		{
			Title: "PROCESS LIFECYCLE & SUPERVISION",
			Items: [][2]string{
				{"s", "Start / launch selected sidecar process"},
				{"x", "Stop / terminate running process group"},
				{"r", "Restart active process"},
				{"d", "Execute Dry-Run probe & validation check"},
				{"t", "Trigger immediate run of scheduled cron task"},
			},
		},
		{
			Title: "NAVIGATION & WORKSPACE",
			Items: [][2]string{
				{"↑ / k, ↓ / j", "Navigate sidecar list"},
				{"Tab / Shift+Tab", "Switch active focus (List, Inspector, Logs)"},
				{"l", "Maximize / restore live log console"},
				{"a", "Toggle log stream follow mode (Auto-scroll)"},
				{"c", "Clear logs buffer for selected sidecar"},
				{"h", "View Execution Run History & Stats (for cron tasks)"},
				{"v", "Inspect raw sidecar.json configuration"},
				{"/", "Filter sidecars by ID, scope, or status"},
			},
		},
		{
			Title: "GLOBAL",
			Items: [][2]string{
				{"?", "Toggle help modal"},
				{"Esc", "Dismiss active modal or clear filter input"},
				{"q / Ctrl+C", "Shutdown supervisor and exit"},
			},
		},
	}

	for _, cat := range categories {
		b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(ColorSecondary).Render(cat.Title) + "\n")
		b.WriteString(lipgloss.NewStyle().Foreground(ColorBorder).Render(strings.Repeat("─", 60)) + "\n")
		for _, item := range cat.Items {
			key := lipgloss.NewStyle().Bold(true).Foreground(ColorPrimary).Width(16).Render(item[0])
			desc := lipgloss.NewStyle().Foreground(ColorText).Render(item[1])
			b.WriteString(fmt.Sprintf("  %s %s\n", key, desc))
		}
		b.WriteString("\n")
	}

	b.WriteString(lipgloss.NewStyle().Foreground(ColorMuted).Render("Press [Esc] or [?] to close"))

	boxWidth := width - 8
	if boxWidth > 82 {
		boxWidth = 82
	}
	modal := ModalBoxStyle.Width(boxWidth).Render(b.String())
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, modal)
}

// RenderConfigModal renders the raw sidecar.json definition
func RenderConfigModal(state supervisor.StateView, width, height int) string {
	var b strings.Builder
	b.WriteString(ModalTitleStyle.Render(fmt.Sprintf("📄 RAW_CONFIG // %s", state.Config.GetDisplayName())) + "\n\n")
	b.WriteString(fmt.Sprintf("PATH : %s\nSCOPE: %s\n\n", state.Config.Path, state.Config.Scope))

	jsonData, err := json.MarshalIndent(state.Config, "", "  ")
	if err != nil {
		b.WriteString("Error formatting JSON: " + err.Error())
	} else {
		b.WriteString(string(jsonData))
	}

	b.WriteString("\n\n" + lipgloss.NewStyle().Foreground(ColorMuted).Render("Press [Esc] or [v] to close"))

	boxWidth := width - 8
	if boxWidth > 82 {
		boxWidth = 82
	}
	modal := ModalBoxStyle.Width(boxWidth).Render(b.String())
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, modal)
}

// RenderRunHistoryModal renders the full execution run history table
func RenderRunHistoryModal(state supervisor.StateView, width, height int) string {
	var b strings.Builder
	b.WriteString(ModalTitleStyle.Render("⚡ ANTIGRAVITY_2.0 // EXECUTION_RUN_HISTORY") + "\n\n")

	total, rate, successes, failures := state.GetRunStats()
	rateColor := ColorPrimary
	if rate < 90.0 {
		rateColor = ColorSecondary
	}
	if rate < 70.0 {
		rateColor = ColorDanger
	}

	b.WriteString(fmt.Sprintf("%s  %s\n",
		FooterKeyStyle.Render("TASK: "+state.Config.GetDisplayName()),
		lipgloss.NewStyle().Foreground(ColorMuted).Render(fmt.Sprintf("(Schedule: %s)", state.Config.Schedule)),
	))

	statsBar := fmt.Sprintf("TOTAL: %d   SUCCESS: %d   FAILED: %d   SUCCESS RATE: %s",
		total, successes, failures,
		lipgloss.NewStyle().Bold(true).Foreground(rateColor).Render(fmt.Sprintf("%.1f%%", rate)),
	)
	b.WriteString(lipgloss.NewStyle().Foreground(ColorBorder).Render(strings.Repeat("─", 68)) + "\n")
	b.WriteString("  " + statsBar + "\n")
	b.WriteString(lipgloss.NewStyle().Foreground(ColorBorder).Render(strings.Repeat("─", 68)) + "\n\n")

	history := state.RunHistory
	if len(history) == 0 {
		b.WriteString(lipgloss.NewStyle().Foreground(ColorMuted).Render("  (No recorded execution runs yet for this task.)\n"))
		b.WriteString(lipgloss.NewStyle().Foreground(ColorInfo).Render("  • Press [t] to trigger an immediate run now.\n\n"))
	} else {
		// Table Headers
		headerStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorSecondary)
		b.WriteString(fmt.Sprintf("  %-10s %-10s %-10s %-12s %s\n",
			headerStyle.Render("TIME"),
			headerStyle.Render("TRIGGER"),
			headerStyle.Render("DURATION"),
			headerStyle.Render("STATUS"),
			headerStyle.Render("SUMMARY / SNIPPET"),
		))
		b.WriteString(lipgloss.NewStyle().Foreground(ColorBorder).Render("  "+strings.Repeat("─", 66)) + "\n")

		// Render from newest to oldest
		for i := len(history) - 1; i >= 0; i-- {
			r := history[i]
			timeStr := r.Timestamp.Format("15:04:05")

			triggerTag := TagTriggerCron.Render()
			if r.Trigger == supervisor.TriggerManual {
				triggerTag = TagTriggerManual.Render()
			}

			durStr := fmt.Sprintf("%v", r.Duration.Round(10*1000*1000))
			if r.Duration < 10*1000*1000 {
				durStr = fmt.Sprintf("%v", r.Duration.Round(100*1000))
			}

			statusBadge := BadgeRunSuccess.Render()
			if r.ExitCode != 0 {
				statusBadge = BadgeRunFailed.SetString(fmt.Sprintf("✗ EXIT %d", r.ExitCode)).Render()
			}

			snippet := r.Snippet
			if len(snippet) > 35 {
				snippet = snippet[:32] + "..."
			}

			b.WriteString(fmt.Sprintf("  %-10s %-10s %-10s %-12s %s\n",
				timeStr,
				triggerTag,
				durStr,
				statusBadge,
				lipgloss.NewStyle().Foreground(ColorText).Render(snippet),
			))
		}
		b.WriteString("\n")
	}

	b.WriteString(lipgloss.NewStyle().Foreground(ColorMuted).Render("Press [Esc] or [h] to close  •  Press [t] to trigger manual run"))

	boxWidth := width - 6
	if boxWidth > 90 {
		boxWidth = 90
	}
	if boxWidth < 50 {
		boxWidth = 50
	}

	modal := ModalBoxStyle.Width(boxWidth).Render(b.String())
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, modal)
}
