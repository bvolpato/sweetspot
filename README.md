<p align="center">
  <img src="assets/logo.svg" alt="sweetspot Go Gopher Wi-Fi logo" width="180" />
</p>

<h1 align="center">sweetspot</h1>

`sweetspot` is an interactive terminal dashboard for finding the best Wi-Fi spot in a home or office and diagnosing real network bottlenecks on macOS, Linux, and Windows.

It renders live multi-row Braille charts for signal strength, throughput, and dual-hop latency without triggering off-channel Wi-Fi scan stutters, compares pinned spots side by side, races DNS resolvers, and analyzes DOCSIS cable modem diagnostics.

## Features

- **Cross-platform live Wi-Fi telemetry:** On macOS, `sweetspot` calls `CoreWLAN` directly through cgo and parses `ifconfig -v` for zero-sudo RSSI, noise, SNR, PHY rate, effective throughput estimate, link quality (LQM), band, channel, width, and QoS scheduler state. On Linux, it uses `iw`, `/proc/net/dev`, and `ip route`. On Windows, it uses `netsh wlan`, `route`, `ipconfig`, and `netstat`.
- **Dual-hop latency and jitter:** Streams continuous ICMP pings to your router gateway and to a WAN target (`1.1.1.1` by default) to separate local Wi-Fi contention from ISP issues.
- **On-demand and continuous throughput:** Run an 8-second multi-stream Cloudflare download and upload burst (`b`), toggle continuous walk-around speed probing (`c`), or run macOS `networkQuality` (`n`) to measure loaded responsiveness (RPM) and bufferbloat.
- **Spot Finder leaderboard:** Walk around your home or office, press `Space` to pin named spots, rank them by a 0 to 100 SweetSpot score, and export a Markdown report (`e`).
- **RF spectrum scan and DNS racer:** Trigger an explicit nearby channel scan (`s`) to identify the cleanest 2.4 GHz, 5 GHz, and 6 GHz channels, and race System DNS against `1.1.1.1`, `9.9.9.9`, `8.8.8.8`, and your router (`r`).
- **Network and cable modem advisor:** Flags concrete bottlenecks including 40 MHz 5 GHz width caps, crowded channel blocks, high channel airtime utilization (CCA), 1x1 MIMO drops, uplink bufferbloat, slower DNS resolvers, macOS AirDrop off-channel hops, and DOCSIS cable modem power, SNR, or uncorrectable codeword issues (paste a modem status page and press `m`, or pass `--modem`).

## Install

### Homebrew (macOS and Linux)

```sh
brew tap bvolpato/tap
brew install sweetspot
```

### Go

Requires Go 1.23 or newer (and Xcode Command Line Tools on macOS for the `CoreWLAN` bridge).

```sh
go install github.com/bvolpato/sweetspot/cmd/sweetspot@latest
```

### Prebuilt binaries (macOS, Linux, Windows)

Download the archive for your OS and architecture from the [GitHub Releases](https://github.com/bvolpato/sweetspot/releases) page.

## Usage

Launch the interactive TUI:

```sh
sweetspot
```

Start directly in continuous walk-around speed mode:

```sh
sweetspot --continuous
```

## CLI and AI Agent Workflows

`sweetspot` provides structured subcommands for shell scripts and AI coding agents. Every subcommand supports `--json` (with string severity values such as `"CRITICAL"`, `"HIGH"`, `"TIP"`, and `"GOOD"`) and clean exit codes (`0` success, `1` runtime error, `2` invalid arguments, `3` score below `--fail-below`). When stdout is not a TTY, bare `sweetspot` automatically falls back to a non-interactive diagnostic snapshot instead of failing.

| Command | Purpose |
|---|---|
| `sweetspot status --json` | Instant (<100 ms) RF link snapshot with zero active probes |
| `sweetspot diagnose --speed --scan --out before.json --json` | Sequential diagnostic (never overlaps idle ping with speed or RF scan) plus full Advisor and Playbook |
| `sweetspot scan --json` | On-demand 2.4 GHz, 5 GHz, and 6 GHz channel congestion scan with recommended clean channels |
| `sweetspot ping --count 20 --json` | Dual-hop idle ICMP latency, p95, jitter, and loss for router gateway and WAN |
| `sweetspot speed --json` | Multi-CDN download, upload, and loaded bufferbloat (RPM) test (add `--network-quality` on macOS) |
| `sweetspot dns --json` | Race System DNS against `1.1.1.1`, `9.9.9.9`, `8.8.8.8`, and your local router |
| `sweetspot modem <file\|-> --json` | Parse DOCSIS 3.0/3.1 cable modem status from a file, stdin, or `--clipboard` |
| `sweetspot spot pin "Desk" --speed` | Save a named spot to the persistent store (`~/.local/share/sweetspot/spots.json`), shared with the TUI |
| `sweetspot spot best --json` | Return the top-ranked pinned spot (also supports `list`, `rm`, and `clear`) |
| `sweetspot diff before.json after.json --format markdown` | Generate a Before vs. After comparison table from two diagnostic snapshots |

Example before-and-after tuning workflow for an agent:

```sh
sweetspot diagnose --speed --scan --out before.json --json
# Apply router or channel changes, then re-measure:
sweetspot diagnose --speed --out after.json --json
sweetspot diff before.json after.json --format markdown
```

## Running with `sudo` (Optional)

`sudo` is **not required**. Running `sweetspot` as a normal user gives full live RSSI, noise, SNR, channel, channel width, PHY rate, dual-hop latency, speed bursts, DNS racing, spot pinning, and modem analysis.

If you start `sweetspot` with elevated privileges (`sudo sweetspot`), extra radio telemetry automatically appears (indicated by a `SUDO+` badge in the header):

- **macOS (`sudo sweetspot` via `wdutil info`):**
  - **Channel airtime utilization (CCA %):** Shows how busy the radio channel actually is across all neighboring clients and APs, enabling the Advisor to detect airtime saturation even when RSSI is strong.
  - **MCS index, spatial streams (NSS), and guard interval:** Shows exact modulation (`MCS`) and whether the link is running 2x2 MIMO or dropped to 1x1.
  - **SSID, BSSID, and AP roam tracking:** Displays unredacted SSID and BSSID (when Location Services permission is also granted to your terminal on macOS 15+) and counts silent AP roams as you walk across a floor.
- **Linux (`sudo sweetspot` via `iw`):**
  - Unlocks active `iw dev <iface> scan` neighborhood scanning and `iw dev <iface> survey dump` channel busy time (CCA %) on kernels and distributions that restrict netlink Wi-Fi queries to `CAP_NET_ADMIN`.
- **Windows:**
  - `netsh wlan` works without Administrator privileges. Windows Location Services must be enabled for `netsh` to return SSID and BSSID fields on Windows 11 24H2 and newer.

## Platform Support

| Platform | Live RF Metrics | RF Neighbor Scan | Dual-Hop Ping | Speed & Bufferbloat | Clipboard Modem Import |
|---|---|---|---|---|---|
| **macOS** | `CoreWLAN` (cgo) + `ifconfig -v` (+ optional `sudo wdutil`) | `CoreWLAN` / `system_profiler` | `/sbin/ping` | Cloudflare + `networkQuality` | `pbpaste` |
| **Linux** | `iw link` / `station` / `survey` + `/proc/net/dev` | `iw scan` / `nmcli` | `ping` | Cloudflare | `wl-paste` / `xclip` |
| **Windows** | `netsh wlan show interfaces` + `netstat -e` | `netsh wlan show networks mode=bssid` | `ping` | Cloudflare | PowerShell `Get-Clipboard` |

## Keybindings

| Key | Action |
|---|---|
| `1` - `4` or `Tab` | Switch between Live Radar, Spots, RF and DNS, and Advisor |
| `Space` or `p` | Pin the current location to the Spot Leaderboard |
| `b` | Run an 8-second Cloudflare download, upload, and bufferbloat burst |
| `c` | Toggle continuous walk-around throughput probing |
| `n` | Run macOS `networkQuality` (macOS only) |
| `s` | Scan nearby Wi-Fi channels for RF congestion |
| `r` | Race DNS resolvers (`1.1.1.1`, `9.9.9.9`, `8.8.8.8`, router, system) |
| `m` | Parse DOCSIS cable modem diagnostics from the clipboard |
| `e` | Export a Markdown report (`sweetspot-report-<timestamp>.md`) |
| `q` | Quit |

## License

MIT License. See [LICENSE](LICENSE).
