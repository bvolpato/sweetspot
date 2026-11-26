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

export function newWorld() {
  return {
    router: { ...START.router }, laptop: { ...START.laptop },
    flags: { band24: false, microwave: false, narrow: false, crowded: false, upload: false, airdrop: false, coax: false, sudo: false },
  };
}

export function roomAt(x, y) {
  return PLAN.rooms.find((r) => x >= r.x && x <= r.x + r.w && y >= r.y && y <= r.y + r.h)?.name ?? 'Outside';
}

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

// Mean signal in dBm. Lower frequencies lose less through walls.
export function rssiAt(world, x, y) {
  const { router, flags } = world;
  const d = Math.max(Math.hypot(x - router.x, y - router.y), 0.45);
  let loss = 0;
  for (const w of PLAN.walls) if (crosses(router.x, router.y, x, y, w)) loss += w[4];
  // Reflections fill deep shadows, so stacked walls count for less and less.
  const eff = loss < 14 ? loss : 14 + (loss - 14) * 0.72;
  return flags.band24 ? -25 - 21 * Math.log10(d) - eff * 0.62 : -29 - 24 * Math.log10(d) - eff;
}

// Signal on a grid, smoothed a little: real fields do not have razor-sharp shadows.
export function field(world, x0, y0, cols, rows, step) {
  const f = new Float32Array(cols * rows);
  for (let j = 0; j < rows; j++) for (let i = 0; i < cols; i++) f[j * cols + i] = rssiAt(world, x0 + i * step, y0 + j * step);
  const tmp = new Float32Array(f.length);
  const r = Math.max(1, Math.round(0.22 / step));
  for (let pass = 0; pass < 2; pass++) {
    blur(f, tmp, cols, rows, 1, r);
    blur(tmp, f, cols, rows, cols, r);
  }
  return f;
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

// Wi-Fi 6 rates for two streams at an 800 ns guard interval, MCS 0 to 11.
const RATES = {
  20: [17.2, 34.4, 51.6, 68.8, 103.2, 137.6, 154.9, 172.1, 206.5, 229.4, 258.1, 286.8],
  40: [34.4, 68.8, 103.2, 137.6, 206.5, 275.3, 309.7, 344.1, 412.9, 458.8, 516.2, 573.5],
  80: [72.1, 144.1, 216.2, 288.2, 432.4, 576.5, 648.5, 720.6, 864.7, 960.8, 1080.9, 1201],
};
const NEED = [4, 7, 10, 13, 17, 21, 23, 25, 29, 31, 35, 38];

const gauss = (rnd) => Math.sqrt(-2 * Math.log(1 - rnd())) * Math.cos(2 * Math.PI * rnd());
const clamp = (v, lo, hi) => Math.min(Math.max(v, lo), hi);

export function seeded(seed) {
  let s = seed >>> 0;
  return () => {
    s = (s + 0x6d2b79f5) >>> 0;
    let t = Math.imul(s ^ (s >>> 15), 1 | s);
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

export class Sim {
  constructor(world, seed = 7) {
    this.world = world;
    this.rnd = seeded(seed);
    this.fade = 0;
    this.rx = 3;
    this.tick = 0;
  }

  step(seconds = 0.5) {
    const { world, rnd } = this;
    const { flags, laptop } = world;
    this.tick++;
    this.fade = this.fade * 0.72 + gauss(rnd) * 0.8;
    const rssi = clamp(rssiAt(world, laptop.x, laptop.y) + this.fade, -94, -24);

    let noise = flags.band24 ? -92 : -95;
    if (flags.crowded) noise += 4;
    if (flags.microwave && flags.band24) {
      // Microwave ovens leak around 2.45 GHz. They do not touch 5 GHz.
      noise += 34 * Math.exp(-Math.hypot(laptop.x - PLAN.microwave[0], laptop.y - PLAN.microwave[1]) / 9) * (0.75 + 0.25 * rnd());
    }
    noise += gauss(rnd) * 0.35;
    const snr = rssi - noise;

    const width = flags.band24 ? 20 : flags.narrow ? 40 : 80;
    let mcs = 0;
    while (mcs < 11 && snr >= NEED[mcs + 1]) mcs++;
    const nss = snr < 15 ? 1 : 2;
    const phy = (RATES[width][mcs] * nss) / 2;
    const cca = Math.round(clamp((flags.crowded ? 71 : 17) + gauss(rnd) * 3 + (flags.microwave && flags.band24 ? 18 : 0), 3, 97));
    const air = 1 - cca / 130;
    const retries = clamp((24 - snr) * 1.1 + (flags.crowded ? 6 : 0) + gauss(rnd), 0.2, 42);

    const hop = () => {
      if (snr < 9 ? rnd() < 0.12 : snr < 14 && rnd() < 0.025) return -1;
      let ms = 2.1 + Math.abs(gauss(rnd)) * 0.5;
      if (snr < 26) ms += (26 - snr) * rnd() * rnd() * 2.4;
      if (flags.crowded) ms += rnd() ** 3 * 46;
      return ms;
    };
    // AirDrop makes macOS leave the channel now and then.
    const away = flags.airdrop && this.tick % 7 === 0 ? 70 + rnd() * 80 : 0;
    const gw = [hop(), hop()].map((v, i) => (v < 0 ? v : v + (i === 0 ? away : 0)));
    const wan = gw.map((v) => {
      if (v < 0 || (flags.coax && rnd() < 0.07)) return -1;
      return v + 9.2 + Math.abs(gauss(rnd)) * 0.9 + (flags.upload ? 95 + rnd() * 120 : 0);
    });

    const capacity = phy * 0.62 * air * (1 - retries / 120);
    this.rx = clamp(this.rx * 0.86 + rnd() ** 4 * 34, 0.2, Math.min(120, capacity * 0.7));
    const up = Math.min(41, capacity * 0.5);
    const tx = flags.upload ? up * (0.86 + rnd() * 0.1) : this.rx * 0.18 + rnd();

    const r1 = (v) => Math.round(v * 10) / 10;
    return {
      seconds, room: roomAt(laptop.x, laptop.y), rssi: r1(rssi), noise: r1(noise), band24: flags.band24, channel: flags.band24 ? 6 : 44,
      width, phy: r1(phy), eff: r1(capacity), mcs, nss, cca, retries: r1(retries), rx: r1(this.rx), tx: r1(tx),
      gw: gw.map(r1), wan: wan.map(r1), down: r1(Math.min(508, capacity) * (0.95 + rnd() * 0.04)), up: r1(up * (flags.upload ? 0.4 : 0.96)),
      bloat: r1(flags.upload ? 214 + rnd() * 30 : 13 + rnd() * 5 + (flags.crowded ? 38 : 0)),
      sudo: flags.sudo, airdrop: flags.airdrop, crowded: flags.crowded, badCoax: flags.coax, modemSeen: flags.coax,
    };
  }
}
