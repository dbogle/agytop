package ui

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"antigravity-sidecars/internal/config"
	"antigravity-sidecars/internal/supervisor"
)

type tickMsg time.Time

// Model is the main state container for the Bubble Tea TUI
type Model struct {
	supervisor *supervisor.Supervisor
	keymap     KeyMap

	sidecars       []supervisor.SidecarState
	filteredStates []supervisor.SidecarState
	cursor         int
	focusedPane    int // 0: Sidecar list, 1: Details/Inspector, 2: Logs

	filterInput textinput.Model
	filtering   bool

	logViewport viewport.Model
	autoScroll  bool
	maximized   int // -1: normal, 2: logs maximized

	dryRunModalOpen bool
	helpModalOpen   bool
	configModalOpen bool

	notification string

	width  int
	height int
	ready  bool
}

// NewModel creates a new Bubble Tea model
func NewModel(sup *supervisor.Supervisor) Model {
	ti := textinput.New()
	ti.Placeholder = "Filter sidecars (/)..."
	ti.Prompt = "> "
	ti.CharLimit = 64

	vp := viewport.New(0, 0)
	vp.SetContent("")

	m := Model{
		supervisor:  sup,
		keymap:      DefaultKeyMap(),
		focusedPane: 0,
		filterInput: ti,
		logViewport: vp,
		autoScroll:  true,
		maximized:   -1,
	}

	m.refreshStates()
	return m
}

func (m *Model) refreshStates() {
	m.sidecars = m.supervisor.GetAllStates()

	filterText := strings.ToLower(strings.TrimSpace(m.filterInput.Value()))
	if filterText == "" {
		m.filteredStates = m.sidecars
	} else {
		filtered := make([]supervisor.SidecarState, 0)
		for _, s := range m.sidecars {
			match := strings.Contains(strings.ToLower(s.Config.ID), filterText) ||
				strings.Contains(strings.ToLower(s.Config.DisplayName), filterText) ||
				strings.Contains(strings.ToLower(string(s.Status)), filterText) ||
				strings.Contains(strings.ToLower(s.Config.Scope), filterText)
			if match {
				filtered = append(filtered, s)
			}
		}
		m.filteredStates = filtered
	}

	if m.cursor >= len(m.filteredStates) {
		m.cursor = len(m.filteredStates) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

// Init sets up the periodic refresh ticker
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		tea.Tick(200*time.Millisecond, func(t time.Time) tea.Msg {
			return tickMsg(t)
		}),
	)
}

// Update processes incoming messages and keystrokes
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.ready = true
		m.updateViewportDimensions()

	case tickMsg:
		m.refreshStates()
		m.updateLogContent()
		cmds = append(cmds, tea.Tick(200*time.Millisecond, func(t time.Time) tea.Msg {
			return tickMsg(t)
		}))

	case tea.KeyMsg:
		// Search input mode
		if m.filtering {
			switch msg.String() {
			case "enter", "esc":
				m.filtering = false
				m.filterInput.Blur()
			default:
				var cmd tea.Cmd
				m.filterInput, cmd = m.filterInput.Update(msg)
				m.refreshStates()
				return m, cmd
			}
			return m, nil
		}

		// Modals open
		if m.dryRunModalOpen {
			switch msg.String() {
			case "esc", "d", "enter", "q":
				m.dryRunModalOpen = false
				return m, nil
			}
			return m, nil
		}
		if m.helpModalOpen {
			switch msg.String() {
			case "esc", "?", "h", "enter", "q":
				m.helpModalOpen = false
				return m, nil
			}
			return m, nil
		}
		if m.configModalOpen {
			switch msg.String() {
			case "esc", "v", "enter", "q":
				m.configModalOpen = false
				return m, nil
			}
			return m, nil
		}

		// Keybindings
		switch msg.String() {
		case "q", "ctrl+c":
			m.supervisor.Shutdown()
			return m, tea.Quit

		case "?", "h":
			m.helpModalOpen = true
			return m, nil

		case "/":
			m.filtering = true
			m.filterInput.Focus()
			return m, textinput.Blink

		case "esc":
			if m.filterInput.Value() != "" {
				m.filterInput.SetValue("")
				m.refreshStates()
			}

		case "tab":
			m.focusedPane = (m.focusedPane + 1) % 3
		case "shift+tab":
			m.focusedPane = (m.focusedPane + 2) % 3

		case "l":
			if m.maximized == 2 {
				m.maximized = -1
			} else {
				m.maximized = 2
				m.focusedPane = 2
			}
			m.updateViewportDimensions()

		case "a":
			m.autoScroll = !m.autoScroll
			if m.autoScroll {
				m.logViewport.GotoBottom()
			}

		case "c":
			if cur := m.selectedState(); cur != nil {
				cur.ClearLogs()
				m.notification = fmt.Sprintf("Cleared logs for '%s'", cur.Config.GetDisplayName())
			}

		case "v":
			if m.selectedState() != nil {
				m.configModalOpen = true
			}

		case "up", "k":
			if m.focusedPane == 0 {
				if m.cursor > 0 {
					m.cursor--
					m.updateLogContent()
				}
			} else if m.focusedPane == 2 {
				m.logViewport.LineUp(1)
				m.autoScroll = false
			}

		case "down", "j":
			if m.focusedPane == 0 {
				if m.cursor < len(m.filteredStates)-1 {
					m.cursor++
					m.updateLogContent()
				}
			} else if m.focusedPane == 2 {
				m.logViewport.LineDown(1)
			}

		case "g", "home":
			if m.focusedPane == 0 {
				m.cursor = 0
				m.updateLogContent()
			} else if m.focusedPane == 2 {
				m.logViewport.GotoTop()
				m.autoScroll = false
			}

		case "G", "end":
			if m.focusedPane == 0 {
				m.cursor = len(m.filteredStates) - 1
				m.updateLogContent()
			} else if m.focusedPane == 2 {
				m.logViewport.GotoBottom()
				m.autoScroll = true
			}

		case "s": // Start
			if cur := m.selectedState(); cur != nil {
				err := m.supervisor.Start(cur.Config.ID)
				if err != nil {
					m.notification = fmt.Sprintf("Error starting: %v", err)
				} else {
					m.notification = fmt.Sprintf("Launched '%s'", cur.Config.GetDisplayName())
				}
			}

		case "x": // Stop
			if cur := m.selectedState(); cur != nil {
				err := m.supervisor.Stop(cur.Config.ID)
				if err != nil {
					m.notification = fmt.Sprintf("Error stopping: %v", err)
				} else {
					m.notification = fmt.Sprintf("Stopped '%s'", cur.Config.GetDisplayName())
				}
			}

		case "r": // Restart
			if cur := m.selectedState(); cur != nil {
				err := m.supervisor.Restart(cur.Config.ID)
				if err != nil {
					m.notification = fmt.Sprintf("Error restarting: %v", err)
				} else {
					m.notification = fmt.Sprintf("Restarted '%s'", cur.Config.GetDisplayName())
				}
			}

		case "d": // DRY RUN
			if cur := m.selectedState(); cur != nil {
				m.notification = fmt.Sprintf("Executing Dry-Run probe on '%s'...", cur.Config.GetDisplayName())
				res, err := m.supervisor.DryRun(cur.Config.ID)
				if err != nil {
					m.notification = fmt.Sprintf("Dry run error: %v", err)
				} else {
					m.dryRunModalOpen = true
					_ = res
				}
			}

		case "t": // Trigger Scheduled Task
			if cur := m.selectedState(); cur != nil {
				err := m.supervisor.TriggerScheduled(cur.Config.ID)
				if err != nil {
					m.notification = fmt.Sprintf("Error triggering: %v", err)
				} else {
					m.notification = fmt.Sprintf("Triggered immediate execution of '%s'", cur.Config.GetDisplayName())
				}
			}
		}
	}

	if m.focusedPane == 2 {
		var cmd tea.Cmd
		m.logViewport, cmd = m.logViewport.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

func (m *Model) selectedState() *supervisor.SidecarState {
	if m.cursor >= 0 && m.cursor < len(m.filteredStates) {
		return &m.filteredStates[m.cursor]
	}
	return nil
}

func (m *Model) updateViewportDimensions() {
	if !m.ready {
		return
	}

	headerHeight := 3
	footerHeight := 2
	availableHeight := m.height - headerHeight - footerHeight
	if availableHeight < 8 {
		availableHeight = 8
	}

	if m.maximized == 2 {
		m.logViewport.Width = m.width - 4
		m.logViewport.Height = availableHeight - 3
	} else {
		rightWidth := (m.width * 62) / 100
		logHeight := (availableHeight * 55) / 100
		m.logViewport.Width = rightWidth - 4
		m.logViewport.Height = logHeight - 3
	}
}

func (m *Model) updateLogContent() {
	cur := m.selectedState()
	if cur == nil {
		m.logViewport.SetContent(lipgloss.NewStyle().Foreground(ColorMuted).Render("No sidecar selected."))
		return
	}

	var b strings.Builder
	logs := cur.GetLogs()
	if len(logs) == 0 {
		b.WriteString(lipgloss.NewStyle().Foreground(ColorMuted).Render("No log entries recorded. Press [s] to start sidecar or [d] for dry run."))
	} else {
		for _, l := range logs {
			timeStr := lipgloss.NewStyle().Foreground(ColorMuted).Render(l.Timestamp.Format("15:04:05"))
			var sourceBadge string
			var textStyled string

			switch l.Source {
			case supervisor.SourceStdout:
				sourceBadge = lipgloss.NewStyle().Foreground(ColorPrimary).Render("[PROCESS]")
				textStyled = LogStdoutStyle.Render(l.Text)
			case supervisor.SourceStderr:
				sourceBadge = lipgloss.NewStyle().Foreground(ColorDanger).Render("[STDERR]")
				textStyled = LogStderrStyle.Render(l.Text)
			case supervisor.SourceSupervisor:
				sourceBadge = lipgloss.NewStyle().Foreground(ColorInfo).Render("[SYSTEM]")
				textStyled = LogSupervisorStyle.Render(l.Text)
			case supervisor.SourceDryRun:
				sourceBadge = lipgloss.NewStyle().Foreground(ColorSecondary).Render("[DRY-RUN]")
				textStyled = LogDryRunStyle.Render(l.Text)
			default:
				sourceBadge = fmt.Sprintf("[%s]", l.Source)
				textStyled = l.Text
			}

			b.WriteString(fmt.Sprintf("%s %s %s\n", timeStr, sourceBadge, textStyled))
		}
	}

	m.logViewport.SetContent(b.String())
	if m.autoScroll {
		m.logViewport.GotoBottom()
	}
}

// View assembles the terminal screen layout
func (m Model) View() string {
	if !m.ready {
		return "\n  Initializing Antigravity 2.0 Sidecar Supervisor..."
	}

	// Modals take priority overlay
	if m.dryRunModalOpen {
		if cur := m.selectedState(); cur != nil && cur.LastDryRun != nil {
			return RenderDryRunModal(cur.LastDryRun, m.width, m.height)
		}
	}
	if m.helpModalOpen {
		return RenderHelpModal(m.width, m.height)
	}
	if m.configModalOpen {
		if cur := m.selectedState(); cur != nil {
			return RenderConfigModal(*cur, m.width, m.height)
		}
	}

	// 1. Top Header Bar
	header := m.renderHeader()

	// 2. Main Content
	var content string
	headerHeight := 3
	footerHeight := 2
	availableHeight := m.height - headerHeight - footerHeight
	if availableHeight < 8 {
		availableHeight = 8
	}

	if m.maximized == 2 {
		logBox := FocusedBorderStyle.
			Width(m.width - 2).
			Height(availableHeight).
			Render(m.renderLogPane(m.width-4, availableHeight-2))
		content = logBox
	} else {
		leftWidth := (m.width * 38) / 100
		if leftWidth < 32 {
			leftWidth = 32
		}
		rightWidth := m.width - leftWidth - 3

		leftBorder := UnfocusedBorderStyle
		if m.focusedPane == 0 {
			leftBorder = FocusedBorderStyle
		}
		leftPane := leftBorder.
			Width(leftWidth).
			Height(availableHeight).
			Render(m.renderListPane(leftWidth-2, availableHeight-2))

		topHeight := (availableHeight * 45) / 100
		bottomHeight := availableHeight - topHeight - 2

		inspectorBorder := UnfocusedBorderStyle
		if m.focusedPane == 1 {
			inspectorBorder = FocusedBorderStyle
		}
		inspectorPane := inspectorBorder.
			Width(rightWidth).
			Height(topHeight).
			Render(m.renderInspectorPane(rightWidth-2, topHeight-2))

		logBorder := UnfocusedBorderStyle
		if m.focusedPane == 2 {
			logBorder = FocusedBorderStyle
		}
		logPane := logBorder.
			Width(rightWidth).
			Height(bottomHeight).
			Render(m.renderLogPane(rightWidth-2, bottomHeight-2))

		rightColumn := lipgloss.JoinVertical(lipgloss.Left, inspectorPane, logPane)
		content = lipgloss.JoinHorizontal(lipgloss.Top, leftPane, rightColumn)
	}

	// 3. Bottom Footer
	footer := m.renderFooter()

	return lipgloss.JoinVertical(lipgloss.Left, header, content, footer)
}

func (m Model) renderHeader() string {
	runningCount, scheduledCount, stoppedCount, failedCount := 0, 0, 0, 0
	for _, s := range m.sidecars {
		switch s.Status {
		case supervisor.StatusRunning, supervisor.StatusExecuting:
			runningCount++
		case supervisor.StatusScheduled:
			scheduledCount++
		case supervisor.StatusFailed:
			failedCount++
		default:
			stoppedCount++
		}
	}

	logo := HeaderLogoStyle.Render(" ANTIGRAVITY_2.0 ")
	subtitle := HeaderSubStyle.Render("// SIDECAR_SUPERVISOR")

	stats := fmt.Sprintf(" [%s] [%s] [%s] [%s]  TOTAL: %d",
		lipgloss.NewStyle().Foreground(ColorPrimary).Bold(true).Render(fmt.Sprintf("%d RUNNING", runningCount)),
		lipgloss.NewStyle().Foreground(ColorInfo).Bold(true).Render(fmt.Sprintf("%d SCHEDULED", scheduledCount)),
		lipgloss.NewStyle().Foreground(ColorSecondary).Bold(true).Render(fmt.Sprintf("%d STOPPED", stoppedCount)),
		lipgloss.NewStyle().Foreground(ColorDanger).Bold(true).Render(fmt.Sprintf("%d FAILED", failedCount)),
		len(m.sidecars),
	)

	return lipgloss.JoinHorizontal(lipgloss.Center, logo, subtitle, "  ", HeaderMetaStyle.Render(stats))
}

func (m Model) renderListPane(width, height int) string {
	var b strings.Builder

	// Header + Filter Input
	b.WriteString(PaneTitleStyle.Render("SIDECARS & DAEMONS") + "\n")
	if m.filtering {
		b.WriteString(m.filterInput.View() + "\n")
	} else if m.filterInput.Value() != "" {
		b.WriteString(lipgloss.NewStyle().Foreground(ColorMuted).Render(fmt.Sprintf("Filter: '%s' (Esc to clear)", m.filterInput.Value())) + "\n")
	}

	if len(m.filteredStates) == 0 {
		b.WriteString("\n" + lipgloss.NewStyle().Foreground(ColorMuted).Render("No sidecars discovered.\nPlace sidecar.json in ~/.gemini/config/sidecars/\nor .agents/sidecars/"))
		return b.String()
	}

	maxRows := height - 3
	if maxRows < 1 {
		maxRows = 1
	}

	start := 0
	if m.cursor >= maxRows {
		start = m.cursor - maxRows + 1
	}
	end := start + maxRows
	if end > len(m.filteredStates) {
		end = len(m.filteredStates)
	}

	for i := start; i < end; i++ {
		s := m.filteredStates[i]
		selected := i == m.cursor

		var statusBadge string
		switch s.Status {
		case supervisor.StatusRunning:
			statusBadge = BadgeRunning.Render()
		case supervisor.StatusScheduled:
			statusBadge = BadgeScheduled.Render()
		case supervisor.StatusExecuting:
			statusBadge = BadgeExecuting.Render()
		case supervisor.StatusFailed:
			statusBadge = BadgeFailed.Render()
		case supervisor.StatusBackoff:
			statusBadge = BadgeBackoff.Render()
		default:
			statusBadge = BadgeStopped.Render()
		}

		name := s.Config.GetDisplayName()
		availNameWidth := width - 26
		if availNameWidth < 10 {
			availNameWidth = 10
		}
		if len(name) > availNameWidth {
			name = name[:availNameWidth-3] + "..."
		}

		scopeTag := TagScopeWs.Render()
		if s.Config.Scope == config.ScopeGlobal {
			scopeTag = TagScopeGlob.Render()
		} else if s.Config.Scope == config.ScopePlugin {
			scopeTag = TagScopePlug.Render()
		}

		pidOrType := ""
		if (s.Status == supervisor.StatusRunning || s.Status == supervisor.StatusExecuting) && s.PID > 0 {
			pidOrType = lipgloss.NewStyle().Foreground(ColorInfo).Render(fmt.Sprintf("P:%d", s.PID))
		} else if s.Config.Builtin == "schedule" {
			pidOrType = lipgloss.NewStyle().Foreground(ColorInfo).Render("cron")
		} else {
			pidOrType = lipgloss.NewStyle().Foreground(ColorMuted).Render("cmd")
		}

		rowText := fmt.Sprintf("%s %s %-12s %s", statusBadge, scopeTag, name, pidOrType)
		if selected {
			rowText = SelectedRowStyle.Width(width).Render("▶ " + rowText)
		} else {
			rowText = NormalRowStyle.Width(width).Render("  " + rowText)
		}
		b.WriteString(rowText + "\n")
	}

	return b.String()
}

func (m Model) renderInspectorPane(width, height int) string {
	cur := m.selectedState()
	if cur == nil {
		return PaneTitleStyle.Render("INSPECTOR & DIAGNOSTICS") + "\n\nNo sidecar selected."
	}

	var b strings.Builder
	b.WriteString(PaneTitleStyle.Render(fmt.Sprintf("INSPECTOR // %s", cur.Config.GetDisplayName())) + "\n")

	// Status line
	var statusBadge string
	var statusExplain string
	switch cur.Status {
	case supervisor.StatusRunning:
		statusBadge = BadgeRunning.Render()
		statusExplain = "Continuous process active"
	case supervisor.StatusScheduled:
		statusBadge = BadgeScheduled.Render()
		statusExplain = "Scheduler armed (will run at scheduled time)"
	case supervisor.StatusExecuting:
		statusBadge = BadgeExecuting.Render()
		statusExplain = "Scheduled task executing now"
	case supervisor.StatusFailed:
		statusBadge = BadgeFailed.Render()
		statusExplain = "Process or task failed"
	case supervisor.StatusBackoff:
		statusBadge = BadgeBackoff.Render()
		statusExplain = "Crash backoff restart pending"
	default:
		statusBadge = BadgeStopped.Render()
		if cur.Config.Builtin == "schedule" {
			statusExplain = "Scheduler paused/inactive (press 's' to arm)"
		} else {
			statusExplain = "Process stopped (press 's' to start)"
		}
	}

	b.WriteString(fmt.Sprintf("STATUS: %s  (%s)\nSCOPE : %s  POLICY: %s\n", statusBadge, lipgloss.NewStyle().Foreground(ColorMuted).Render(statusExplain), cur.Config.Scope, cur.Config.RestartPolicy))

	// Live Telemetry Gauges
	if cur.Status == supervisor.StatusRunning || cur.Status == supervisor.StatusExecuting {
		uptime := time.Since(cur.StartedAt).Round(time.Second)
		cpuGauge := renderAsciiGauge(cur.CPUPercent, 100.0, 12)
		memMB := float64(cur.MemoryBytes) / (1024 * 1024)
		memGauge := renderAsciiGauge(memMB, 512.0, 12)

		b.WriteString(fmt.Sprintf("CPU [%s] %5.1f%%   RAM [%s] %s   PID: %d  UPTIME: %s\n",
			cpuGauge, cur.CPUPercent,
			memGauge, supervisor.FormatBytes(cur.MemoryBytes),
			cur.PID, uptime,
		))
	} else if cur.Status == supervisor.StatusFailed {
		b.WriteString(lipgloss.NewStyle().Foreground(ColorDanger).Render(fmt.Sprintf("FAILURE: %s (Exit Code: %d, Restarts: %d)\n", cur.LastError, cur.LastExitCode, cur.Restarts)))
	}

	// Execution Command
	if cur.Config.Command != "" && cur.Config.Builtin == "" {
		cmdStr := fmt.Sprintf("%s %s", cur.Config.Command, strings.Join(cur.Config.Args, " "))
		b.WriteString(fmt.Sprintf("EXEC : %s\n", lipgloss.NewStyle().Foreground(ColorInfo).Render(cmdStr)))
	} else if cur.Config.Builtin != "" {
		b.WriteString(fmt.Sprintf("BUILTIN: %s  CRON: %s\n", cur.Config.Builtin, lipgloss.NewStyle().Foreground(ColorSecondary).Render(cur.Config.Schedule)))
		if cur.Config.Command != "" {
			b.WriteString(fmt.Sprintf("TARGET : %s %s\n", cur.Config.Command, strings.Join(cur.Config.Args, " ")))
		}
		if !cur.NextScheduleRun.IsZero() {
			b.WriteString(lipgloss.NewStyle().Foreground(ColorInfo).Render(fmt.Sprintf("NEXT TRIGGER: %s   LAST RUN: %s\n", cur.NextScheduleRun.Format("15:04:05"), cur.LastScheduleRun.Format("15:04:05"))))
		}
	}

	// Environment Variables Table (Compact 2-column)
	if len(cur.Config.Env) > 0 {
		var envPairs []string
		for k, v := range cur.Config.Env {
			envPairs = append(envPairs, fmt.Sprintf("%s=%s", k, v))
		}
		b.WriteString(fmt.Sprintf("ENV  : %s\n", lipgloss.NewStyle().Foreground(ColorMuted).Render(strings.Join(envPairs, ", "))))
	}

	// Dry Run status line
	if cur.LastDryRun != nil {
		drStatus := lipgloss.NewStyle().Foreground(ColorPrimary).Render("✓ PASS")
		if !cur.LastDryRun.Success {
			drStatus = lipgloss.NewStyle().Foreground(ColorDanger).Render("✗ FAIL")
		}
		b.WriteString(fmt.Sprintf("PROBE: %s (Ran %v ago, Exit: %d) - Press [d] for full report\n", drStatus, time.Since(cur.LastDryRun.Timestamp).Round(time.Second), cur.LastDryRun.ExitCode))
	}

	return b.String()
}

func (m Model) renderLogPane(width, height int) string {
	var b strings.Builder
	title := PaneTitleStyle.Render("LIVE OUTPUT STREAM")
	scrollState := lipgloss.NewStyle().Foreground(ColorPrimary).Render("[FOLLOW: ON]")
	if !m.autoScroll {
		scrollState = lipgloss.NewStyle().Foreground(ColorSecondary).Render("[FOLLOW: PAUSED - 'a' to resume]")
	}
	b.WriteString(fmt.Sprintf("%s  %s  %s\n", title, scrollState, lipgloss.NewStyle().Foreground(ColorMuted).Render("('l' maximize, 'c' clear)")))
	b.WriteString(m.logViewport.View())
	return b.String()
}

func (m Model) renderFooter() string {
	var b strings.Builder

	if m.notification != "" {
		b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(ColorInfo).Render("ℹ " + m.notification))
		b.WriteString("  |  ")
	}

	hints := "[S] START  [X] STOP  [R] RESTART  [D] DRY RUN  [T] TRIGGER  [V] JSON  [/] FILTER  [?] HELP  [Q] QUIT"
	b.WriteString(FooterStyle.Render(hints))
	return b.String()
}

// renderAsciiGauge renders a high-precision block gauge like [████░░░░]
func renderAsciiGauge(val, maxVal float64, width int) string {
	if maxVal <= 0 {
		maxVal = 100
	}
	ratio := val / maxVal
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}

	filled := int(math.Round(ratio * float64(width)))
	empty := width - filled
	if empty < 0 {
		empty = 0
	}

	filledStr := GaugeFilledStyle.Render(strings.Repeat("█", filled))
	emptyStr := GaugeEmptyStyle.Render(strings.Repeat("░", empty))
	return filledStr + emptyStr
}
