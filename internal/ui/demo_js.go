package ui

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/bvolpato/sweetspot/internal/advisor"
	"github.com/bvolpato/sweetspot/internal/probe"
	"github.com/bvolpato/sweetspot/internal/spots"
	"github.com/bvolpato/sweetspot/internal/wifi"
)

// Demo drives a Model with simulated telemetry for the website. It sends the same messages as a
// live run and never executes the commands that Update returns, so no probe can start.
type Demo struct {
	m    *Model
	last DemoSample

	clock     time.Time
	inBytes   uint64
	outBytes  uint64
	toast     string
	toastLeft float64
	speedStep int
	contWait  int
	scanWait  int
	dnsWait   int
	modemOpen bool
}

// DemoSample is one simulated reading. Seconds is the simulated time since the last one.
type DemoSample struct {
	Seconds   float64        `json:"seconds"`
	Room      string         `json:"room"`
	RSSI      float64        `json:"rssi"`
	Noise     float64        `json:"noise"`
	Band      string         `json:"band"` // "2.4", "5", or "6"
	Channel   int            `json:"channel"`
	WidthMHz  int            `json:"width"`
	PhyMbps   float64        `json:"phy"`
	EffMbps   float64        `json:"eff"`
	RxMbps    float64        `json:"rx"`
	TxMbps    float64        `json:"tx"`
	GwRTT     []float64      `json:"gw"`  // a negative value is a lost packet
	WanRTT    []float64      `json:"wan"` // a negative value is a lost packet
	Down      float64        `json:"down"`
	Up        float64        `json:"up"`
	Bloat     float64        `json:"bloat"`
	AirDrop   bool           `json:"airdrop"`
	Line      string         `json:"line"` // "none", "good", "weak", or "hot"
	DNS       string         `json:"dns"`  // "isp", "cf", or "quad9"
	Neighbors []DemoNeighbor `json:"neighbors"`
}

// DemoNeighbor is one simulated network nearby.
type DemoNeighbor struct {
	Band     string `json:"band"`
	Channel  int    `json:"ch"`
	WidthMHz int    `json:"width"`
	RSSI     int    `json:"rssi"`
}

const demoGateway = "192.168.1.1"

// NewDemo returns a demo with an empty in-memory spot store.
func NewDemo(cols, rows int) *Demo {
	lipgloss.SetColorProfile(0) // true color
	lipgloss.SetHasDarkBackground(true)
	m := NewModel(Options{WANTarget: "1.1.1.1"})
	m.spotsStore = spots.NewStore()
	m.activeGW = demoGateway // a known gateway stops Update from starting a ping process
	m.gwRing = probe.NewPingRing(demoGateway, 96, 40)
	m.wanRing = probe.NewPingRing("1.1.1.1", 96, 80)
	m.dnsRunning = true // the real program starts with a DNS race, too
	d := &Demo{m: m, clock: time.Now(), dnsWait: 4}
	d.Resize(cols, rows)
	return d
}

// Resize sets the terminal size.
func (d *Demo) Resize(cols, rows int) {
	d.m.Update(tea.WindowSizeMsg{Width: cols, Height: rows})
	d.m.advisorScroll = min(d.m.advisorScroll, d.m.advisorMaxScroll())
}

// Step applies one simulated reading.
func (d *Demo) Step(s DemoSample) {
	was := d.last
	d.last = s
	d.clock = d.clock.Add(time.Duration(s.Seconds * float64(time.Second)))
	d.inBytes += uint64(s.RxMbps * 1e6 / 8 * s.Seconds)
	d.outBytes += uint64(s.TxMbps * 1e6 / 8 * s.Seconds)
	for _, v := range s.GwRTT {
		d.m.gwRing.Add(probe.PingSample{Timestamp: d.clock, RTTMs: v, Lost: v < 0})
	}
	for _, v := range s.WanRTT {
		d.m.wanRing.Add(probe.PingSample{Timestamp: d.clock, RTTMs: v, Lost: v < 0})
	}
	d.m.Update(linkSampleMsg(d.link()))
	d.m.Update(d.m.spinner.Tick())
	d.refresh(was)
	d.progress()
	d.ageToast(s.Seconds)
}

func (d *Demo) link() wifi.LinkInfo {
	s := d.last
	rssi, noise := int(math.Round(s.RSSI)), int(math.Round(s.Noise))
	l := wifi.LinkInfo{
		Timestamp: d.clock, Interface: "en0", DefaultIface: "en0", Gateway: demoGateway,
		PowerOn: true, Connected: true,
		RSSI: rssi, Noise: noise, SNR: rssi - noise,
		TxRateMbps: math.Round(s.PhyMbps), EffDownMbps: math.Round(s.EffMbps),
		LinkQuality: int(math.Max(0, math.Min(100, float64(rssi-noise)*2.4))),
		Scheduler:   "FQ_CODEL", Channel: s.Channel, Band: demoBand(s.Band), WidthMHz: s.WidthMHz,
		PHYMode: "802.11ax (Wi-Fi 6)", Security: "WPA3 Personal",
		DNSServers: []string{demoServers[s.DNS]}, InBytes: d.inBytes, OutBytes: d.outBytes, AirDropActive: s.AirDrop,
	}
	return l
}

// refresh repeats a scan, a DNS race, or a modem report that a settings change made stale.
func (d *Demo) refresh(was DemoSample) {
	m, s := d.m, d.last
	stale := false
	if m.scan.TotalAPs > 0 && (was.Band != s.Band || was.Channel != s.Channel || was.WidthMHz != s.WidthMHz || len(was.Neighbors) != len(s.Neighbors)) {
		m.scan = wifi.SummarizeScan(demoNeighbors(s.Neighbors), m.link, 1400*time.Millisecond)
		stale = true
	}
	if len(m.dns.Entries) > 0 && was.DNS != s.DNS {
		m.dns = demoDNS(s.DNS)
		stale = true
	}
	if was.Line != s.Line {
		d.modemOpen = s.Line == "weak" || s.Line == "hot" || (d.modemOpen && s.Line == "good")
		m.modem = advisor.ModemReport{}
		if d.modemOpen {
			m.modem = advisor.ParseModemDiagnostics(demoModem(s.Line))
		}
		stale = true
	}
	if stale {
		m.recompute()
	}
}

func demoBand(b string) wifi.Band {
	switch b {
	case "2.4":
		return wifi.Band2GHz
	case "6":
		return wifi.Band6GHz
	}
	return wifi.Band5GHz
}

// progress moves the simulated speed, scan, and DNS runs forward by one step.
func (d *Demo) progress() {
	m, s := d.m, d.last
	const downSteps, upSteps = 9, 6
	if d.speedStep > 0 {
		i := d.speedStep
		d.speedStep++
		ramp := func(n, total int) float64 { return 1 - math.Exp(-3.2*float64(n)/float64(total)) }
		switch {
		case i <= downSteps:
			m.Update(speedProgressMsg{Phase: "download", DownloadMbps: s.Down * ramp(i, downSteps)})
		case i <= downSteps+upSteps:
			m.Update(speedProgressMsg{Phase: "upload", DownloadMbps: s.Down, UploadMbps: s.Up * ramp(i-downSteps, upSteps)})
		default:
			d.speedStep = 0
			idle := m.wanStats.AvgMs
			m.Update(speedDoneMsg{Timestamp: d.clock, Source: "burst", Phase: "done", DownloadMbps: s.Down, UploadMbps: s.Up,
				IdleRTTMs: idle, LoadedRTTMs: idle + s.Bloat, BloatDeltaMs: s.Bloat, RPM: int(60000 / (idle + s.Bloat + 22))})
			d.contWait = 8
		}
	} else if m.continuousMode {
		if d.contWait--; d.contWait <= 0 {
			d.startSpeed()
		}
	}
	if d.scanWait > 0 {
		if d.scanWait--; d.scanWait == 0 {
			m.Update(scanDoneMsg(wifi.SummarizeScan(demoNeighbors(s.Neighbors), m.link, 1400*time.Millisecond)))
		}
	}
	if d.dnsWait > 0 {
		if d.dnsWait--; d.dnsWait == 0 {
			m.Update(dnsDoneMsg(demoDNS(s.DNS)))
		}
	}
}

func (d *Demo) startSpeed() {
	d.speedStep = 1
	d.m.speedRunning = true
	d.m.speedProgress = probe.SpeedProgress{Phase: "download"}
}

// Key sends one key, named as in KeyboardEvent.key.
func (d *Demo) Key(key string) {
	m := d.m
	if m.pinning {
		if msg, ok := demoKey(key); ok {
			m.Update(msg)
		}
		return
	}
	switch key {
	case "q":
	case "b", "n":
		if !m.speedRunning {
			m.setToast("Running 8s download + upload + bufferbloat burst...")
			d.startSpeed()
		}
	case "c":
		m.continuousMode = !m.continuousMode
		d.contWait = 1
		if m.continuousMode {
			m.setToast("Continuous walk-around speed probing ON (press c to pause)")
		} else {
			m.setToast("Continuous speed probing PAUSED")
		}
	case "s":
		if !m.scanning {
			m.scanning, d.scanWait = true, 6
			m.setToast("Scanning nearby Wi-Fi channels (brief latency blip normal)...")
		}
	case "r":
		if !m.dnsRunning {
			m.dnsRunning, d.dnsWait = true, 4
			m.setToast("Racing DNS resolvers (System, 1.1.1.1, 9.9.9.9, 8.8.8.8, Router)...")
		}
	case "m":
		d.modemOpen = d.last.Line != "none"
		m.Update(modemLoadedMsg(advisor.ParseModemDiagnostics(demoModem(d.last.Line))))
	case "e":
		m.setToast("This demo has no disk. Install sweetspot to export reports.")
	default:
		msg, ok := demoKey(key)
		if !ok {
			return
		}
		m.Update(msg)
		if m.pinning {
			m.pinInput.SetValue(d.last.Room)
			m.pinInput.CursorEnd()
		}
	}
}

// Pin pins the current spot without the dialog.
func (d *Demo) Pin(name string) {
	d.m.spotsStore.Pin(name, d.m.link, d.m.gwStats, d.m.wanStats, d.m.speed, d.m.score)
}

func demoKey(key string) (tea.KeyMsg, bool) {
	switch key {
	case "Enter":
		return tea.KeyMsg{Type: tea.KeyEnter}, true
	case "Escape":
		return tea.KeyMsg{Type: tea.KeyEsc}, true
	case "Backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace}, true
	case "ArrowUp":
		return tea.KeyMsg{Type: tea.KeyUp}, true
	case "ArrowDown":
		return tea.KeyMsg{Type: tea.KeyDown}, true
	case "ArrowLeft":
		return tea.KeyMsg{Type: tea.KeyLeft}, true
	case "ArrowRight":
		return tea.KeyMsg{Type: tea.KeyRight}, true
	case " ":
		return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}, true
	}
	if r := []rune(key); len(r) == 1 {
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: r}, true
	}
	return tea.KeyMsg{}, false
}

func (d *Demo) ageToast(seconds float64) {
	if d.m.toast != d.toast {
		d.toast, d.toastLeft = d.m.toast, 4.5
	}
	if d.toast == "" {
		return
	}
	if d.toastLeft -= seconds; d.toastLeft <= 0 {
		d.m.Update(toastClearMsg{})
		d.toast = ""
	}
}

// State returns what the page shows outside the terminal, as JSON.
func (d *Demo) State() string {
	m := d.m
	best := ""
	if ranked := m.spotsStore.Ranked(); len(ranked) > 0 {
		best = ranked[0].Name
	}
	b, _ := json.Marshal(map[string]any{
		"score": m.score, "advice": m.advice, "tab": int(m.tab), "pinning": m.pinning, "busy": m.speedRunning, "measured": m.speed.DownloadMbps > 0,
		"spots": m.spotsStore.Len(), "best": best, "gw": m.gwStats, "wan": m.wanStats, "channel": m.link.ChannelLabel(), "bloat": m.speed.BloatDeltaMs, "down": m.speed.DownloadMbps,
	})
	return string(b)
}

// Cells renders the view as three values per cell: the code point, then the foreground and the
// background. Bit 24 marks a set color and bit 25 of the foreground marks bold text. A zero code
// point is the second half of a wide character.
func (d *Demo) Cells() []uint32 {
	cols, rows := d.m.width, d.m.height
	out := make([]uint32, cols*rows*3)
	var fg, bg uint32
	bold, reverse := false, false
	for y, line := range strings.Split(d.m.View(), "\n") {
		if y >= rows {
			break
		}
		x, state := 0, byte(0)
		for len(line) > 0 {
			seq, width, n, next := ansi.DecodeSequence(line, state, nil)
			state, line = next, line[n:]
			if width == 0 {
				if strings.HasPrefix(seq, "\x1b[") && strings.HasSuffix(seq, "m") {
					applySGR(seq[2:len(seq)-1], &fg, &bg, &bold, &reverse)
				}
				continue
			}
			if x+width > cols {
				break
			}
			f, b := fg, bg
			if reverse {
				f, b = bg, fg
				if b == 0 {
					b = 1<<24 | 0xF1F5F9
				}
				if f == 0 {
					f = 1<<24 | 0x0A0F1C
				}
			}
			if bold {
				f |= 1 << 25
			}
			for i := 0; i < width; i++ {
				at := (y*cols + x + i) * 3
				if i == 0 {
					out[at] = uint32([]rune(seq)[0])
				}
				out[at+1], out[at+2] = f, b
			}
			x += width
		}
	}
	return out
}

func applySGR(params string, fg, bg *uint32, bold, reverse *bool) {
	p := strings.Split(params, ";")
	num := func(i int) int {
		if i >= len(p) {
			return 0
		}
		v, _ := strconv.Atoi(p[i])
		return v
	}
	for i := 0; i < len(p); i++ {
		switch v := num(i); v {
		case 0:
			*fg, *bg, *bold, *reverse = 0, 0, false, false
		case 1:
			*bold = true
		case 7:
			*reverse = true
		case 22:
			*bold = false
		case 27:
			*reverse = false
		case 39:
			*fg = 0
		case 49:
			*bg = 0
		case 38, 48:
			if num(i+1) == 2 {
				c := 1<<24 | uint32(num(i+2))<<16 | uint32(num(i+3))<<8 | uint32(num(i+4))
				if v == 38 {
					*fg = c
				} else {
					*bg = c
				}
				i += 4
			}
		}
	}
}

func demoNeighbors(list []DemoNeighbor) []wifi.NeighborAP {
	out := make([]wifi.NeighborAP, 0, len(list))
	for _, n := range list {
		out = append(out, wifi.NeighborAP{Band: demoBand(n.Band), Channel: n.Channel, WidthMHz: n.WidthMHz, RSSI: n.RSSI})
	}
	return out
}

var demoServers = map[string]string{"isp": demoGateway, "cf": "1.1.1.1", "quad9": "9.9.9.9"}

func demoDNS(kind string) probe.DNSRaceResult {
	r := probe.DNSRaceResult{Entries: []probe.DNSBenchmark{
		{Name: "Cloudflare", Server: "1.1.1.1", CachedMs: 9.4, UncachedMs: 17.8},
		{Name: "Quad9", Server: "9.9.9.9", CachedMs: 11.2, UncachedMs: 24.6},
		{Name: "Google", Server: "8.8.8.8", CachedMs: 12.7, UncachedMs: 29.3},
		{Name: "System DNS", Server: "system", CachedMs: 14.1, UncachedMs: 46.5, IsSystem: true},
		{Name: "Router", Server: demoGateway, CachedMs: 3.2, UncachedMs: 48.9},
	}}
	sys := &r.Entries[3]
	switch kind {
	case "cf":
		sys.CachedMs, sys.UncachedMs = 9.6, 18.3
	case "quad9":
		sys.CachedMs, sys.UncachedMs = 11.5, 25.1
	}
	r.Fastest, r.System = r.Entries[0], *sys
	return r
}

func demoModem(line string) string {
	switch line {
	case "good":
		return "System Up Time: 38Days, 06:39:01\n" +
			"1 Locked 256 QAM 24 603000000 Hz 1.3 dBmV 42.4 dB 34 0\n" +
			"2 Locked 256 QAM 1 465000000 Hz 0.6 dBmV 42.2 dB 12 0\n" +
			"1 Locked ATDMA 1 5120 Ksym/sec 18200000 Hz 41.3 dBmV\n"
	case "weak":
		return "System Up Time: 2Days, 04:11:09\n" +
			"1 Locked 256 QAM 24 603000000 Hz -9.8 dBmV 31.2 dB 88213 40718\n" +
			"2 Locked 256 QAM 1 465000000 Hz -10.6 dBmV 30.4 dB 91277 51930\n" +
			"1 Locked ATDMA 1 5120 Ksym/sec 18200000 Hz 52.3 dBmV\n"
	case "hot":
		return "System Up Time: 11Days, 19:02:44\n" +
			"1 Locked 256 QAM 24 603000000 Hz 13.4 dBmV 43.1 dB 20 0\n" +
			"2 Locked 256 QAM 1 465000000 Hz 12.2 dBmV 43.0 dB 18 0\n" +
			"1 Locked ATDMA 1 5120 Ksym/sec 18200000 Hz 33.8 dBmV\n"
	}
	return ""
}
