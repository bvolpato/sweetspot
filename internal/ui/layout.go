package ui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// fit cuts one styled line to w cells. It never wraps.
func fit(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= w {
		return s
	}
	return ansi.Truncate(s, w, "…")
}

// pad cuts or fills one styled line to exactly w cells.
func pad(s string, w int) string {
	s = fit(s, w)
	return s + strings.Repeat(" ", max(w-lipgloss.Width(s), 0))
}

// firstFit returns the first variant that fits in w cells, or the last one cut to w.
func firstFit(w int, variants ...string) string {
	for _, v := range variants {
		if lipgloss.Width(v) <= w {
			return v
		}
	}
	return fit(variants[len(variants)-1], w)
}

// joinFit joins parts with separators. It tightens the separators first and then drops
// parts from the end, so put the most important part first.
func joinFit(w int, parts ...string) string {
	if len(parts) == 0 {
		return ""
	}
	for n := len(parts); n > 0; n-- {
		for _, sep := range []string{"  ·  ", " · "} {
			if s := strings.Join(parts[:n], sep); lipgloss.Width(s) <= w {
				return s
			}
		}
	}
	return fit(parts[0], w)
}

// clip returns exactly h lines that are at most w cells wide.
func clip(s string, w, h int) string {
	if h <= 0 {
		return ""
	}
	lines := strings.Split(s, "\n")
	out := make([]string, h)
	for i := range out {
		if i < len(lines) {
			out[i] = fit(lines[i], w)
		}
	}
	return strings.Join(out, "\n")
}

// vstack joins the non-empty sections of a view.
func vstack(sections ...string) string {
	var kept []string
	for _, s := range sections {
		if s != "" {
			kept = append(kept, s)
		}
	}
	return strings.Join(kept, "\n")
}

// cardBox draws a bordered card that is exactly w x h cells. Content that does not fit is cut
// instead of wrapped, so a card cannot grow and push other rows off the screen.
func cardBox(st lipgloss.Style, w, h int, lines []string) string {
	if h <= 0 || w <= 0 {
		return ""
	}
	body := strings.Join(lines, "\n")
	if w < 6 || h < 3 {
		return clip(body, w, h)
	}
	return st.Width(w - 2).Height(h - 2).Render(clip(body, w-4, h-2))
}

// block is one section of a card. stack keeps the blocks with the lowest prio first.
type block struct {
	prio int
	rows int // rows it needs (the minimum when flex is set)
	flex bool
	draw func(rows int) string
}

// text is a fixed block with one or more ready lines. Empty text adds one blank row.
func text(prio int, s string) block {
	return block{prio: prio, rows: lipgloss.Height(s), draw: func(int) string { return s }}
}

// flexible is a block that takes all the rows that are left, and at least minRows.
func flexible(prio, minRows int, draw func(rows int) string) block {
	return block{prio: prio, rows: minRows, flex: true, draw: draw}
}

// stack fits blocks into a row budget: it drops the least important blocks and gives the
// rows that are left to the flexible block. The display order does not change.
func stack(rows int, blocks ...block) []string {
	order := make([]int, len(blocks))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool { return blocks[order[a]].prio < blocks[order[b]].prio })

	kept := make([]bool, len(blocks))
	left := rows
	for _, i := range order {
		if blocks[i].rows <= left {
			kept[i] = true
			left -= blocks[i].rows
		}
	}

	var out []string
	for i, b := range blocks {
		if !kept[i] {
			continue
		}
		n := b.rows
		if b.flex {
			n += left
			left = 0
		}
		out = append(out, strings.Split(b.draw(n), "\n")...)
	}
	return out
}

// listRows shows at most rows lines and says how many lines it left out.
func listRows(lines []string, rows int) string {
	if rows <= 0 {
		return ""
	}
	if len(lines) > rows {
		more := StyleMuted.Render(fmt.Sprintf("+%d more", len(lines)-rows+1))
		lines = append(append([]string{}, lines[:rows-1]...), more)
	}
	return clip(strings.Join(lines, "\n"), 1<<16, rows)
}

// windowStart returns the first index to show so that the selected row stays visible.
func windowStart(selected, total, rows int) int {
	if rows <= 0 || total <= rows || selected < rows {
		return 0
	}
	return min(selected-rows+1, total-rows)
}
