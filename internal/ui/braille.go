package ui

import (
	"fmt"
	"math"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Braille dot bitmasks for a 2x4 subpixel cell (col 0..1, row 0..3).
var brailleBit = [4][2]rune{
	{0x01, 0x08},
	{0x02, 0x10},
	{0x04, 0x20},
	{0x40, 0x80},
}

// BrailleChart renders a multi-row, color-coded filled Braille area chart with Y-axis labels.
// Each character column packs 2 horizontal data samples x 4 vertical subpixels.
func BrailleChart(
	values []float64,
	chartW int,
	chartH int,
	minVal float64,
	maxVal float64,
	allowNegative bool,
	unit string,
	colorFn func(float64) lipgloss.Color,
) string {
	if chartW < 10 {
		chartW = 10
	}
	if chartH < 3 {
		chartH = 3
	}

	const axisW = 8 // e.g. " -35dBm " or " 100ms "
	plotW := chartW - axisW - 1
	if plotW < 6 {
		plotW = 6
	}
	pxW := plotW * 2
	pxH := chartH * 4

	// Auto-range if minVal >= maxVal.
	lo, hi := minVal, maxVal
	if hi <= lo {
		lo = math.MaxFloat64
		hi = -math.MaxFloat64
		for _, v := range values {
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
			lo, hi = 0, 10
		} else {
			if lo > 0 && minVal == 0 {
				lo = 0
			}
			if hi-lo < 1.0 {
				hi = lo + 1.0
			} else {
				hi = hi * 1.15
			}
		}
	}

	// Build subpixel buffer of latest pxW samples (right-aligned).
	samples := make([]float64, pxW)
	hasSample := make([]bool, pxW)
	if len(values) > 0 {
		start := 0
		if len(values) > pxW {
			start = len(values) - pxW
		}
		src := values[start:]
		offset := pxW - len(src)
		for i, v := range src {
			samples[offset+i] = v
			hasSample[offset+i] = true
		}
	}

	// Rasterize filled area into Braille cells.
	grid := make([][]rune, chartH)
	cellColor := make([][]lipgloss.Color, chartH)
	for r := 0; r < chartH; r++ {
		grid[r] = make([]rune, plotW)
		cellColor[r] = make([]lipgloss.Color, plotW)
		for c := 0; c < plotW; c++ {
			grid[r][c] = 0x2800
			cellColor[r][c] = ColTrackHi
		}
	}

	for x := 0; x < pxW; x++ {
		if !hasSample[x] {
			continue
		}
		v := samples[x]
		colIdx := x / 2
		subX := x % 2

		if !allowNegative && v < 0 {
			// Packet loss marker: full vertical coral bar on that sub-column.
			for py := 0; py < pxH; py++ {
				grid[py/4][colIdx] |= brailleBit[py%4][subX]
				cellColor[py/4][colIdx] = ColCoral
			}
			continue
		}

		norm := (v - lo) / (hi - lo)
		if norm < 0 {
			norm = 0
		}
		if norm > 1 {
			norm = 1
		}
		topY := int(math.Round((1.0 - norm) * float64(pxH-1)))
		if topY < 0 {
			topY = 0
		}
		if topY >= pxH {
			topY = pxH - 1
		}

		col := ColCyan
		if colorFn != nil {
			col = colorFn(v)
		}
		for py := topY; py < pxH; py++ {
			rowIdx := py / 4
			grid[rowIdx][colIdx] |= brailleBit[py%4][subX]
			// Let hotter/higher-severity sample win if two sub-columns share a cell.
			if cellColor[rowIdx][colIdx] == ColTrackHi || col == ColCoral || (col == ColAmber && cellColor[rowIdx][colIdx] != ColCoral) {
				cellColor[rowIdx][colIdx] = col
			} else if cellColor[rowIdx][colIdx] == ColTrackHi {
				cellColor[rowIdx][colIdx] = col
			}
		}
	}

	formatTick := func(v float64) string {
		if math.Abs(v) >= 100 || unit == "dBm" {
			return fmt.Sprintf("%*.0f%s", axisW-len(unit)-1, v, unit)
		}
		return fmt.Sprintf("%*.1f%s", axisW-len(unit)-1, v, unit)
	}

	midVal := (lo + hi) / 2.0
	midRow := chartH / 2
	var lines []string
	for r := 0; r < chartH; r++ {
		var label string
		switch r {
		case 0:
			label = formatTick(hi)
		case chartH - 1:
			label = formatTick(lo)
		case midRow:
			label = formatTick(midVal)
		default:
			label = strings.Repeat(" ", axisW-1)
		}
		axis := StyleMuted.Render(fmt.Sprintf("%*s│", axisW-1, label))

		var rowBuf strings.Builder
		for c := 0; c < plotW; c++ {
			ch := grid[r][c]
			if ch == 0x2800 {
				if r == chartH-1 {
					rowBuf.WriteString(lipgloss.NewStyle().Foreground(ColTrackHi).Render("┈"))
				} else {
					rowBuf.WriteString(" ")
				}
			} else {
				rowBuf.WriteString(lipgloss.NewStyle().Foreground(cellColor[r][c]).Render(string(ch)))
			}
		}
		lines = append(lines, axis+rowBuf.String())
	}
	return strings.Join(lines, "\n")
}

// RenderSignalTower renders a bold 5-bar stepped Wi-Fi signal tower (4 rows tall)
// so signal strength is immediately readable across the room.
func RenderSignalTower(rssi int, connected bool, isEthernet bool) string {
	bars := 0
	if isEthernet {
		bars = 5
	} else if connected {
		switch {
		case rssi >= -50:
			bars = 5
		case rssi >= -60:
			bars = 4
		case rssi >= -68:
			bars = 3
		case rssi >= -76:
			bars = 2
		default:
			bars = 1
		}
	}

	barColors := []lipgloss.Color{ColCoral, ColAmber, ColCyan, ColTeal, ColEmerald}
	// 4 rows tall, 5 bars of heights 1, 2, 3, 4, 4 (with top accent)
	heights := []int{1, 2, 3, 4, 4}
	var rows []string
	for row := 3; row >= 0; row-- {
		var line strings.Builder
		for i := 0; i < 5; i++ {
			if i > 0 {
				line.WriteString(" ")
			}
			active := i < bars
			tallEnough := heights[i] > row
			if i == 4 && row == 3 {
				tallEnough = true
			}
			if i == 3 && row == 3 {
				// Step bar 4 slightly shorter than bar 5 using lower half-block.
				if active {
					line.WriteString(lipgloss.NewStyle().Foreground(barColors[i]).Render("▄▄"))
				} else {
					line.WriteString(lipgloss.NewStyle().Foreground(ColTrackHi).Render("▄▄"))
				}
				continue
			}
			if !tallEnough {
				line.WriteString("  ")
				continue
			}
			if active {
				line.WriteString(lipgloss.NewStyle().Foreground(barColors[i]).Render("██"))
			} else {
				line.WriteString(lipgloss.NewStyle().Foreground(ColTrackHi).Render("░░"))
			}
		}
		rows = append(rows, line.String())
	}
	return strings.Join(rows, "\n")
}

// RenderBigNumber renders clean 3-line tall Unicode block digits (e.g. "-64", "92").
var bigDigits = map[rune][3]string{
	'0': {"┏━┓", "┃ ┃", "┗━┛"},
	'1': {" ┓ ", " ┃ ", " ┻ "},
	'2': {"━┓ ", "┏┛ ", "┗━━"},
	'3': {"━━┓", " ━┫", "━━┛"},
	'4': {"╻ ╻", "┗━┫", "  ╹"},
	'5': {"┏━━", "┗━┓", "━━┛"},
	'6': {"┏━╸", "┣━┓", "┗━┛"},
	'7': {"━━┓", "  ┃", "  ╹"},
	'8': {"┏━┓", "┣━┫", "┗━┛"},
	'9': {"┏━┓", "┗━┫", "╺━┛"},
	'-': {"   ", "━━ ", "   "},
	' ': {" ", " ", " "},
}

func RenderBigNumber(text string, color lipgloss.Color) string {
	var r0, r1, r2 strings.Builder
	for i, ch := range text {
		g, ok := bigDigits[ch]
		if !ok {
			continue
		}
		if i > 0 {
			r0.WriteString(" ")
			r1.WriteString(" ")
			r2.WriteString(" ")
		}
		r0.WriteString(g[0])
		r1.WriteString(g[1])
		r2.WriteString(g[2])
	}
	st := lipgloss.NewStyle().Bold(true).Foreground(color)
	return st.Render(r0.String()) + "\n" + st.Render(r1.String()) + "\n" + st.Render(r2.String())
}
