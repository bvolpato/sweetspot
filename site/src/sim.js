// A small indoor radio model: log-distance path loss, wall loss, shadowing, and link adaptation.
// It feeds the terminal, so the numbers only have to be plausible. It is not a site survey tool.

const BRICK = 13, DRY = 4, TILE = 8, GLASS = 3, CORE = 17, METAL = 15;

// Units are meters. Each wall is [x1, y1, x2, y2, loss in dB at 5 GHz, kind].
export const PLAN = {
  w: 14, h: 9,
  walls: [
    [0, 0, 1.2, 0, BRICK, 'ext'], [1.2, 0, 3.4, 0, GLASS, 'win'], [3.4, 0, 6.2, 0, BRICK, 'ext'], [6.2, 0, 8.6, 0, GLASS, 'win'], [8.6, 0, 14, 0, BRICK, 'ext'],
    [14, 0, 14, 1.6, BRICK, 'ext'], [14, 1.6, 14, 3.6, GLASS, 'win'], [14, 3.6, 14, 6.4, BRICK, 'ext'], [14, 6.4, 14, 8, GLASS, 'win'], [14, 8, 14, 9, BRICK, 'ext'],
    [14, 9, 8.4, 9, BRICK, 'ext'], [7.3, 9, 4.6, 9, BRICK, 'ext'], [4.6, 9, 3.4, 9, BRICK, 'ext'], [3.4, 9, 1.4, 9, GLASS, 'win'], [1.4, 9, 0, 9, BRICK, 'ext'],
    [0, 9, 0, 0, BRICK, 'ext'],
    [4.6, 0, 4.6, 2.9, DRY, 'int'], [4.6, 3.8, 4.6, 5.6, DRY, 'int'], [4.6, 6.5, 4.6, 9, DRY, 'int'],
    [0, 3.8, 3.2, 3.8, DRY, 'int'], [4.1, 3.8, 4.6, 3.8, DRY, 'int'],
    [0, 5.6, 2.5, 5.6, TILE, 'int'], [2.5, 3.8, 2.5, 4.3, TILE, 'int'], [2.5, 5.1, 2.5, 5.6, TILE, 'int'],
    [2.5, 5.6, 4.6, 5.6, DRY, 'int'],
    [9.6, 0, 9.6, 1.4, DRY, 'int'], [9.6, 3.2, 9.6, 5.2, DRY, 'int'],
    [9.6, 5.2, 11, 5.2, DRY, 'int'], [11.9, 5.2, 14, 5.2, DRY, 'int'], [9.6, 5.2, 9.6, 9, DRY, 'int'],
    [5.5, 4.2, 6.6, 4.2, CORE, 'core'], [6.6, 4.2, 6.6, 5.4, CORE, 'core'], [6.6, 5.4, 5.5, 5.4, CORE, 'core'], [5.5, 5.4, 5.5, 4.2, CORE, 'core'],
    [13.1, 0.5, 13.1, 1.5, METAL, 'hide'], [13.1, 1.5, 13.9, 1.5, METAL, 'hide'],
  ],
  rooms: [
    { name: 'Bedroom', x: 0, y: 0, w: 4.6, h: 3.8, label: [2.3, 2.95] },
    { name: 'Bathroom', x: 0, y: 3.8, w: 2.5, h: 1.8, label: [1.25, 4.72] },
    { name: 'Hall', x: 2.5, y: 3.8, w: 2.1, h: 1.8, label: [3.55, 4.95] },
    { name: 'Guest room', x: 0, y: 5.6, w: 4.6, h: 3.4, label: [2.9, 6.15] },
    { name: 'Living room', x: 4.6, y: 0, w: 5, h: 9, label: [7.1, 6.35] },
    { name: 'Kitchen', x: 9.6, y: 0, w: 4.4, h: 5.2, label: [11.8, 3.9] },
    { name: 'Office', x: 9.6, y: 5.2, w: 4.4, h: 3.8, label: [11.1, 6.25] },
  ],
  microwave: [11.2, 0.45],
};

export const START = { router: { x: 11.9, y: 2.3 }, laptop: { x: 1.3, y: 7.7 } };

// Every setting a visitor can change. Each band keeps its own channel and width.
export const DEFAULTS = {
  band: '5', w24: 20, w5: 80, w6: 80, ch24: 6, ch5: 44, ch6: 37, power: 100, sqm: false,
  neighbors: 9, walls: 'wood', microwave: false, airdrop: false,
  plan: '500', traffic: 'quiet', line: 'good', dns: 'isp',
};

export const PLANS = { 100: { down: 104, up: 11 }, 500: { down: 508, up: 41 }, 1000: { down: 940, up: 930, fiber: true } };

export function newWorld(set = {}) {
  return { router: { ...START.router }, laptop: { ...START.laptop }, set: { ...DEFAULTS, ...set } };
}

export function roomAt(x, y) {
  return PLAN.rooms.find((r) => x >= r.x && x <= r.x + r.w && y >= r.y && y <= r.y + r.h)?.name ?? 'Outside';
}

/* ---------- Propagation ---------- */

// Signal at one meter, loss per decade of distance, and how much a wall counts.
const RADIO = { 2.4: [-25, 21, 0.62], 5: [-29, 24, 1], 6: [-31, 24.5, 1.18] };
const WALL = { wood: 1, brick: 2.1, concrete: 3.2 };

// With one network name for both bands, the router moves a client to 2.4 GHz below this signal.
export const STEER = -72;

function crosses(ax, ay, bx, by, w) {
  const rx = bx - ax, ry = by - ay, sx = w[2] - w[0], sy = w[3] - w[1];
  const d = rx * sy - ry * sx;
  if (d === 0) return false;
  const qx = w[0] - ax, qy = w[1] - ay;
  const t = (qx * sy - qy * sx) / d;
  if (t <= 0.0001 || t >= 0.9999) return false;
  const u = (qx * ry - qy * rx) / d;
  return u >= 0 && u <= 1;
}

// The distance term and the wall loss on the straight path. Reflections fill deep shadows, so
// stacked walls count for less and less.
function path(world, x, y) {
  const { router, set } = world, k = WALL[set.walls];
  let loss = 0;
  for (const w of PLAN.walls) if (crosses(router.x, router.y, x, y, w)) loss += w[5] === 'int' ? w[4] * k : w[4];
  return [Math.log10(Math.max(Math.hypot(x - router.x, y - router.y), 0.45)), loss < 14 ? loss : 14 + (loss - 14) * 0.72];
}

const level = (band, [far, loss], power) => RADIO[band][0] - RADIO[band][1] * far - RADIO[band][2] * loss + 10 * Math.log10(power / 100);

// Mean signal and band at a point. "now" is the band in use, so a client does not flap between bands.
export function linkAt(world, x, y, now) {
  const { band, power } = world.set, p = path(world, x, y);
  if (band !== 'dual') return { band, rssi: level(band, p, power) };
  const five = level('5', p, power);
  const stay = five >= (now === '2.4' ? STEER + 5 : STEER);
  return stay ? { band: '5', rssi: five } : { band: '2.4', rssi: level('2.4', p, power) };
}

export const rssiAt = (world, x, y) => linkAt(world, x, y).rssi;

// Signal on a grid, smoothed a little: real fields do not have razor-sharp shadows. "steer" is the
// 5 GHz field when the router offers both bands.
export function field(world, x0, y0, cols, rows, step) {
  const { band, power } = world.set, n = cols * rows, dual = band === 'dual';
  const a = new Float32Array(n), b = dual ? new Float32Array(n) : null, tmp = new Float32Array(n);
  for (let j = 0; j < rows; j++) {
    for (let i = 0; i < cols; i++) {
      const p = path(world, x0 + i * step, y0 + j * step), k = j * cols + i;
      a[k] = level(dual ? '5' : band, p, power);
      if (dual) b[k] = level('2.4', p, power);
    }
  }
  const r = Math.max(1, Math.round(0.22 / step));
  for (const g of dual ? [a, b] : [a]) {
    for (let pass = 0; pass < 2; pass++) {
      blur(g, tmp, cols, rows, 1, r);
      blur(tmp, g, cols, rows, cols, r);
    }
  }
  if (!dual) return { f: a, steer: null };
  for (let k = 0; k < n; k++) if (a[k] >= STEER) b[k] = a[k];
  return { f: b, steer: a };
}

function blur(src, dst, cols, rows, stride, r) {
  const along = stride === 1 ? cols : rows, across = stride === 1 ? rows : cols, jump = stride === 1 ? cols : 1;
  for (let a = 0; a < across; a++) {
    const base = a * jump;
    let sum = 0;
    for (let k = -r; k <= r; k++) sum += src[base + Math.min(Math.max(k, 0), along - 1) * stride];
    for (let i = 0; i < along; i++) {
      dst[base + i * stride] = sum / (2 * r + 1);
      sum += src[base + Math.min(i + r + 1, along - 1) * stride] - src[base + Math.max(i - r, 0) * stride];
    }
  }
}

/* ---------- Channels and neighbors ---------- */

const range = (from, to, by) => Array.from({ length: (to - from) / by + 1 }, (_, i) => from + i * by);
const BLOCKS = { 5: [range(36, 64, 4), range(100, 144, 4), range(149, 161, 4)], 6: [range(1, 93, 4), range(97, 233, 4)] };

export const CHANNELS = {
  2.4: range(1, 11, 1), 5: BLOCKS[5].flat(), 6: range(5, 229, 16),
};
export const isDFS = (ch) => ch >= 52 && ch <= 144;

// The 20 MHz channels that a primary channel and a width cover.
function covers(band, ch, width) {
  const row = BLOCKS[band].find((b) => b.includes(ch));
  if (!row) return [ch];
  const n = width / 20, at = Math.floor(row.indexOf(ch) / n) * n;
  return row.slice(at, at + n);
}

// Some blocks are too short for 160 MHz.
export function widest(band, ch) {
  if (band === '2.4') return 40;
  for (const w of [160, 80, 40]) if (covers(band, ch, w).length === w / 20) return w;
  return 20;
}

export function overlaps(a, b) {
  if (a.band !== b.band) return false;
  if (a.band === '2.4') return Math.abs(a.ch - b.ch) < (Math.max(a.width, b.width) > 20 ? 6 : 4);
  const other = covers(b.band, b.ch, b.width);
  return covers(a.band, a.ch, a.width).some((c) => other.includes(c));
}

export function seeded(seed) {
  let s = seed >>> 0;
  return () => {
    s = (s + 0x6d2b79f5) >>> 0;
    let t = Math.imul(s ^ (s >>> 15), 1 | s);
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

// The street, in the order in which the slider adds networks. Most routers stay on default channels.
export const MAX_NEIGHBORS = 40;
const STREET = (() => {
  const rnd = seeded(11), next = { 5: 0, 2.4: 0, 6: 0 };
  const use = {
    5: [149, 36, 157, 100, 44, 40, 149, 48, 36, 153, 44, 40, 161, 36, 48, 149, 44, 132, 40, 36],
    2.4: [1, 6, 11, 6, 1, 11, 6, 1, 3, 6, 11, 9, 1, 6, 11, 1], 6: [5, 37, 101, 37],
  };
  return Array.from({ length: MAX_NEIGHBORS }, (_, i) => {
    const band = ['5', '2.4', '5', '2.4', '5', '2.4', '2.4', '5', '5', '6'][i % 10];
    const ch = use[band][next[band]++ % use[band].length];
    return { band, ch, width: band === '2.4' ? 20 : 80, rssi: Math.round(-56 - 28 * rnd()) };
  });
})();
export const neighbors = (world) => STREET.slice(0, world.set.neighbors);

// The channel and width in use on a band.
export function tuned(set, band) {
  const ch = band === '2.4' ? set.ch24 : band === '6' ? set.ch6 : set.ch5;
  const want = band === '2.4' ? set.w24 : band === '6' ? set.w6 : set.w5;
  return { band, ch, width: Math.min(want, widest(band, ch)) };
}

export const crowd = (world, band) => neighbors(world).filter((n) => overlaps(tuned(world.set, band), n));

/* ---------- Link and traffic ---------- */

// Wi-Fi 6 rates for two streams at an 800 ns guard interval, MCS 0 to 11.
const RATES = {
  20: [17.2, 34.4, 51.6, 68.8, 103.2, 137.6, 154.9, 172.1, 206.5, 229.4, 258.1, 286.8],
  40: [34.4, 68.8, 103.2, 137.6, 206.5, 275.3, 309.7, 344.1, 412.9, 458.8, 516.2, 573.5],
  80: [72.1, 144.1, 216.2, 288.2, 432.4, 576.5, 648.5, 720.6, 864.7, 960.8, 1080.9, 1201],
  160: [144.1, 288.2, 432.4, 576.5, 864.7, 1152.9, 1297.1, 1441.2, 1729.4, 1921.6, 2161.8, 2401.9],
};
const NEED = [4, 7, 10, 13, 17, 21, 23, 25, 29, 31, 35, 38];
const FLOOR = { 2.4: -95, 5: -98, 6: -99 };

const gauss = (rnd) => Math.sqrt(-2 * Math.log(1 - rnd())) * Math.cos(2 * Math.PI * rnd());
const clamp = (v, lo, hi) => Math.min(Math.max(v, lo), hi);

export class Sim {
  constructor(world, seed = 7) {
    this.world = world;
    this.rnd = seeded(seed);
    this.fade = 0;
    this.rx = 3;
    this.tick = 0;
    this.band = undefined;
  }

  step(seconds = 0.5) {
    const { world, rnd } = this;
    const { set, laptop } = world;
    this.tick++;
    this.fade = this.fade * 0.72 + gauss(rnd) * 0.8;
    const at = linkAt(world, laptop.x, laptop.y, this.band);
    this.band = at.band;
    const { ch, width } = tuned(set, at.band);
    const rssi = clamp(at.rssi + this.fade, -94, -22);
    const near = crowd(world, at.band);
    const oven = set.microwave && at.band === '2.4';

    // A channel that is twice as wide collects twice the noise.
    let noise = FLOOR[at.band] + 3 * Math.log2(width / 20) + near.length * 0.2 + gauss(rnd) * 0.35;
    // Microwave ovens leak around 2.45 GHz. They do not touch 5 GHz or 6 GHz.
    if (oven) noise += 34 * Math.exp(-Math.hypot(laptop.x - PLAN.microwave[0], laptop.y - PLAN.microwave[1]) / 9) * (0.75 + 0.25 * rnd());
    const snr = rssi - noise;

    let mcs = 0;
    while (mcs < 11 && snr >= NEED[mcs + 1]) mcs++;
    const phy = RATES[width][mcs] * (snr < 8 ? 0.5 : 1);
    const busy = clamp(7 + near.reduce((s, n) => s + 3 + (5 * (n.rssi + 86)) / 28, 0) + (oven ? 20 : 0) + gauss(rnd) * 2, 3, 94);
    const retries = clamp((24 - snr) * 1.1 + Math.max(0, busy - 45) * 0.2 + gauss(rnd), 0.2, 42);

    const hop = () => {
      if (snr < 8 ? rnd() < 0.12 : snr < 12 && rnd() < 0.025) return -1;
      let ms = 2.1 + Math.abs(gauss(rnd)) * 0.5;
      if (snr < 24) ms += (24 - snr) * rnd() * rnd() * 2.4;
      return ms + Math.max(0, busy - 35) * 1.25 * rnd() ** 3;
    };
    // AirDrop makes macOS leave the channel now and then, unless the channel is 149.
    const away = set.airdrop && ch !== 149 && this.tick % 7 === 0 ? 70 + rnd() * 80 : 0;
    const gw = [hop(), hop()].map((v, i) => (v < 0 ? v : v + (i === 0 ? away : 0)));

    const plan = PLANS[set.plan], line = plan.fiber ? 'none' : set.line;
    const capacity = phy * 0.62 * (1 - busy / 115) * (1 - retries / 120);
    const up = Math.min(plan.up, capacity * 0.8), down = Math.min(plan.down, capacity);
    const backup = set.traffic === 'backup';
    // A full upload queue adds delay. Smart queues keep it short, for a little speed.
    const queue = set.sqm ? 4 : plan.fiber ? 14 : clamp(4600 / plan.up, 20, 260);
    const wan = gw.map((v) => {
      if (v < 0 || (line === 'weak' && rnd() < 0.07)) return -1;
      return v + (plan.fiber ? 4.1 : 9.2) + Math.abs(gauss(rnd)) * 0.9 + (backup ? queue * (0.6 + 0.6 * rnd()) : 0);
    });
    const bloat = (set.sqm ? 9 + rnd() * 5 : backup ? queue * 1.6 : plan.fiber ? 14 : clamp(2500 / plan.up, 14, 170)) * (0.95 + rnd() * 0.1) + (busy > 50 ? 20 : 0);

    const base = set.traffic === 'stream' ? 24 : 0;
    this.rx = clamp(this.rx * 0.86 + rnd() ** 4 * 34, 0.2, Math.min(120, capacity * 0.7));
    const rx = Math.min(this.rx + base * (0.8 + 0.4 * rnd()), capacity);
    const tx = backup ? up * (0.86 + rnd() * 0.1) : this.rx * 0.18 + rnd();
    const share = backup ? 0.4 : 0.96, shaped = set.sqm ? 0.92 : 1;

    const r1 = (v) => Math.round(v * 10) / 10;
    return {
      seconds, room: roomAt(laptop.x, laptop.y), rssi: r1(rssi), noise: r1(noise), band: at.band, channel: ch, width,
      phy: r1(phy), eff: r1(capacity), rx: r1(rx), tx: r1(tx), gw: gw.map(r1), wan: wan.map(r1),
      down: r1(Math.max(0.4, Math.min(down, plan.down * shaped) - base) * (0.95 + rnd() * 0.04)), up: r1(Math.min(up, plan.up * shaped) * share),
      bloat: r1(bloat), airdrop: set.airdrop, line, dns: set.dns, neighbors: neighbors(world),
    };
  }
}
