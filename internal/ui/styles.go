package ui

import (
	"fmt"
	"math"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Theme colors: high-contrast observability palette tuned for dark terminals.
var (
	ColBgCard   = lipgloss.Color("#0F172A")
	ColBorder   = lipgloss.Color("#334155")
	ColBorderHi = lipgloss.Color("#38BDF8")
	ColCyan     = lipgloss.Color("#38BDF8")
	ColTeal     = lipgloss.Color("#2DD4BF")
	ColViolet   = lipgloss.Color("#A78BFA")
	ColEmerald  = lipgloss.Color("#34D399")
	ColAmber    = lipgloss.Color("#FBBF24")
	ColCoral    = lipgloss.Color("#FB7185")
	ColText     = lipgloss.Color("#F1F5F9")
	ColSubtle   = lipgloss.Color("#94A3B8")
	ColMuted    = lipgloss.Color("#64748B")
	ColTrack    = lipgloss.Color("#293548")
	ColTrackHi  = lipgloss.Color("#475569")
)

var (
	StyleTitle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColCyan)

	StyleSubtitle = lipgloss.NewStyle().
			Foreground(ColSubtle)

	StyleMuted = lipgloss.NewStyle().
			Foreground(ColMuted)

	StyleValue = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColText)

	StyleCard = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColBorder).
			Padding(0, 1)

	StyleCardHi = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColBorderHi).
			Padding(0, 1)

	StyleTabActive = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#0B1120")).
			Background(ColCyan).
			Padding(0, 1)

	StyleTabInactive = lipgloss.NewStyle().
				Foreground(ColSubtle).
				Background(ColTrack).
				Padding(0, 1)

	StylePillLive = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#052E16")).
			Background(ColEmerald).
			Padding(0, 1)

	StylePillBusy = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#1E1B4B")).
			Background(ColViolet).
			Padding(0, 1)

	StylePillWarn = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#451A03")).
			Background(ColAmber).
			Padding(0, 1)

	StyleFooter = lipgloss.NewStyle().
			Foreground(ColSubtle).
			Padding(0, 1)

	StyleToast = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColTeal)
)

func ColorForScore(score int) lipgloss.Color {
	switch {
	case score >= 88:
		return ColEmerald
	case score >= 75:
		return ColCyan
	case score >= 60:
		return ColAmber
	default:
		return ColCoral
	}
}

func ColorForRSSI(rssi int) lipgloss.Color {
	switch {
	case rssi >= -55:
		return ColEmerald
	case rssi >= -65:
		return ColCyan
	case rssi >= -75:
		return ColAmber
	default:
		return ColCoral
	}
}

func ColorForLatency(ms, goodMax, warnMax float64) lipgloss.Color {
	switch {
	case ms < 0:
		return ColCoral
	case ms <= goodMax:
		return ColEmerald
	case ms <= warnMax:
		return ColAmber
	default:
		return ColCoral
	}
}

// RenderBar renders a crisp horizontal meter with filled/empty styling.
func RenderBar(fraction float64, width int, fillColor lipgloss.Color) string {
	if width < 4 {
		width = 4
	}
	if fraction < 0 {
		fraction = 0
	}
	if fraction > 1 {
		fraction = 1
	}
	filled := int(math.Round(fraction * float64(width)))
	if filled > width {
		filled = width
	}
	empty := width - filled
	f := lipgloss.NewStyle().Foreground(fillColor).Render(strings.Repeat("━", filled))
	e := lipgloss.NewStyle().Foreground(ColTrackHi).Render(strings.Repeat("─", empty))
	return f + e
}

// RenderGradientBar renders a segmented signal-strength meter transitioning coral -> amber -> cyan -> emerald.
func RenderGradientBar(fraction float64, width int) string {
	if width < 6 {
		width = 6
	}
	if fraction < 0 {
		fraction = 0
	}
	if fraction > 1 {
		fraction = 1
	}
	filled := int(math.Round(fraction * float64(width)))
	stops := []lipgloss.Color{ColCoral, ColAmber, ColCyan, ColTeal, ColEmerald}

	var b strings.Builder
	for i := 0; i < width; i++ {
		if i < filled {
			pos := float64(i) / float64(max(width-1, 1))
			idx := int(pos * float64(len(stops)-1))
			if idx >= len(stops) {
				idx = len(stops) - 1
			}
			b.WriteString(lipgloss.NewStyle().Foreground(stops[idx]).Render("█"))
		} else {
			b.WriteString(lipgloss.NewStyle().Foreground(ColTrackHi).Render("░"))
		}
	}
	return b.String()
}

var sparkBlocks = []rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}

// RenderSparkline draws a color-coded Unicode sparkline from a rolling float slice.
// When allowNegative is false, negative values render as coral loss markers ('×').
func RenderSparkline(values []float64, width int, minVal, maxVal float64, allowNegative bool, colorFn func(float64) lipgloss.Color) string {
	if width <= 0 {
		return ""
	}
	if len(values) == 0 {
		return lipgloss.NewStyle().Foreground(ColMuted).Render(strings.Repeat("·", width))
	}
	start := 0
	if len(values) > width {
		start = len(values) - width
	}
	slice := values[start:]

	lo, hi := minVal, maxVal
	if hi <= lo {
		lo = math.MaxFloat64
		hi = -math.MaxFloat64
		for _, v := range slice {
			if !allowNegative && v < 0 {
				continue
			}
			if v < lo {
				lo = v
			}
			if v > hi {
				hi = v
			}
		}
		if lo == math.MaxFloat64 {
			lo, hi = 0, 1
		}
		if hi-lo < 1e-6 {
			hi = lo + 1
		}
	}

	var b strings.Builder
	pad := width - len(slice)
	if pad > 0 {
		b.WriteString(lipgloss.NewStyle().Foreground(ColTrack).Render(strings.Repeat("·", pad)))
	}
	for _, v := range slice {
		if !allowNegative && v < 0 {
			b.WriteString(lipgloss.NewStyle().Foreground(ColCoral).Bold(true).Render("×"))
			continue
		}
		norm := (v - lo) / (hi - lo)
		if norm < 0 {
			norm = 0
		}
		if norm > 1 {
			norm = 1
		}
		idx := int(math.Round(norm * float64(len(sparkBlocks)-1)))
		col := ColCyan
		if colorFn != nil {
			col = colorFn(v)
		}
		b.WriteString(lipgloss.NewStyle().Foreground(col).Render(string(sparkBlocks[idx])))
	}
	return b.String()
}

func Badge(label string, bg lipgloss.Color) string {
	fg := lipgloss.Color("#0B1120")
	return lipgloss.NewStyle().Bold(true).Foreground(fg).Background(bg).Padding(0, 1).Render(label)
}

// CardHeader renders a card title with a right-aligned note. It uses the first note that fits
// and leaves the note out when none does.
func CardHeader(icon, title string, innerWidth int, rights ...string) string {
	left := fit(lipgloss.NewStyle().Bold(true).Foreground(ColCyan).Render(icon+" "+title), innerWidth)
	for _, right := range rights {
		if gap := innerWidth - lipgloss.Width(left) - lipgloss.Width(right); right != "" && gap >= 2 {
			return left + strings.Repeat(" ", gap) + StyleMuted.Render(right)
		}
	}
	return left
}

func FormatMbps(v float64) string {
	switch {
	case v >= 100:
		return fmt.Sprintf("%.0f Mbps", v)
	case v >= 10:
		return fmt.Sprintf("%.1f Mbps", v)
	default:
		return fmt.Sprintf("%.2f Mbps", v)
	}
}
