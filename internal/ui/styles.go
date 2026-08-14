package ui

import (
	"github.com/charmbracelet/lipgloss"
)

var (
	// Stitch Theme Palette: Deep Zinc & High-Fidelity TUI
	ColorCanvas       = lipgloss.Color("#09090B") // Level 0: Deep Zinc Canvas
	ColorSurface      = lipgloss.Color("#18181B") // Level 1: Elevated Panels
	ColorSurfaceHigh  = lipgloss.Color("#201F22") // Level 1.5
	ColorModalBg      = lipgloss.Color("#27272A") // Level 2: Modal background
	ColorBorder       = lipgloss.Color("#27272A") // Structural lines
	ColorBorderActive = lipgloss.Color("#4BE277") // Active pane border (Emerald)
	ColorBorderCyan   = lipgloss.Color("#38BDF8") // Accent cyan border
	ColorHighlight    = lipgloss.Color("#2E2E33") // Selected row background

	// Semantic Accents
	ColorPrimary   = lipgloss.Color("#4BE277") // Emerald green
	ColorSecondary = lipgloss.Color("#FFB95F") // Warm amber
	ColorDanger    = lipgloss.Color("#EF4444") // Red
	ColorInfo      = lipgloss.Color("#38BDF8") // Sky cyan
	ColorMuted     = lipgloss.Color("#71717A") // Muted zinc
	ColorText      = lipgloss.Color("#E5E1E4") // Primary text
	ColorTextDim   = lipgloss.Color("#A1A1AA") // Secondary text

	// Header Styles
	HeaderLogoStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#003915")).
			Background(ColorPrimary).
			Padding(0, 1)

	HeaderSubStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorText).
			Padding(0, 1)

	HeaderMetaStyle = lipgloss.NewStyle().
			Foreground(ColorMuted)

	// Status Badges (Strict Rectangular TUI Pills)
	BadgeRunning = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#002109")).
			Background(ColorPrimary).
			Padding(0, 1).
			SetString(" RUNNING ")

	BadgeScheduled = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#082F49")).
			Background(ColorInfo).
			Padding(0, 1).
			SetString(" SCHEDULED ")

	BadgeExecuting = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#002109")).
			Background(lipgloss.Color("#86EFAC")).
			Padding(0, 1).
			SetString(" EXECUTING ")

	BadgeStopped = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#E5E1E4")).
			Background(lipgloss.Color("#3F3F46")).
			Padding(0, 1).
			SetString(" STOPPED ")

	BadgeFailed = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(ColorDanger).
			Padding(0, 1).
			SetString(" FAILED ")

	BadgeBackoff = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#2A1700")).
			Background(ColorSecondary).
			Padding(0, 1).
			SetString(" BACKOFF ")

	// Scope Tags
	TagScopeWs = lipgloss.NewStyle().
			Foreground(ColorInfo).
			Background(lipgloss.Color("#082F49")).
			Padding(0, 1).
			SetString("WS")

	TagScopeGlob = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#C084FC")).
			Background(lipgloss.Color("#3B0764")).
			Padding(0, 1).
			SetString("GLOBAL")

	TagScopePlug = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#F472B6")).
			Background(lipgloss.Color("#500724")).
			Padding(0, 1).
			SetString("PLUGIN")

	// Trigger Source Tags
	TagTriggerCron = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FDE047")).
			Background(lipgloss.Color("#422006")).
			Bold(true).
			Padding(0, 1).
			SetString("CRON")

	TagTriggerManual = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#38BDF8")).
			Background(lipgloss.Color("#082F49")).
			Bold(true).
			Padding(0, 1).
			SetString("MANUAL")

	// Run History Status Badges
	BadgeRunSuccess = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#003915")).
			Background(ColorPrimary).
			Bold(true).
			Padding(0, 1).
			SetString("✓ EXIT 0")

	BadgeRunFailed = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(ColorDanger).
			Bold(true).
			Padding(0, 1)

	// Pane Borders (Sharp TUI Technical Brutalism)
	FocusedBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.NormalBorder()).
				BorderForeground(ColorBorderActive).
				Background(ColorCanvas).
				Padding(0, 1)

	UnfocusedBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.NormalBorder()).
				BorderForeground(ColorBorder).
				Background(ColorCanvas).
				Padding(0, 1)

	PaneTitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorPrimary)

	// Row Styles
	SelectedRowStyle = lipgloss.NewStyle().
				Background(ColorHighlight).
				Foreground(lipgloss.Color("#FFFFFF")).
				Bold(true)

	NormalRowStyle = lipgloss.NewStyle().
			Foreground(ColorText)

	// Metric Gauges
	GaugeFilledStyle = lipgloss.NewStyle().
				Foreground(ColorPrimary)

	GaugeEmptyStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#27272A"))

	// Log Styles
	LogStdoutStyle = lipgloss.NewStyle().
			Foreground(ColorText)

	LogStderrStyle = lipgloss.NewStyle().
			Foreground(ColorDanger)

	LogSupervisorStyle = lipgloss.NewStyle().
				Foreground(ColorInfo).
				Italic(true)

	LogDryRunStyle = lipgloss.NewStyle().
			Foreground(ColorSecondary).
			Bold(true)

	// Modals
	ModalBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.DoubleBorder()).
			BorderForeground(ColorBorderActive).
			Background(ColorSurfaceHigh).
			Padding(1, 2)

	ModalTitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorPrimary).
			MarginBottom(1)

	// Footer
	FooterStyle = lipgloss.NewStyle().
			Foreground(ColorTextDim).
			Background(ColorSurface).
			Padding(0, 1)

	FooterKeyStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorPrimary)
)
