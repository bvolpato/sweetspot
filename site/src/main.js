import { PLAN, START, Sim, newWorld, field, rssiAt, roomAt } from './sim.js';
import { LEVELS, COLORS, bandColor, rings, trace } from './bands.js';
import { Screen } from './terminal.js';

const $ = (s, el = document) => el.querySelector(s);
const $$ = (s, el = document) => [...el.querySelectorAll(s)];
const clamp = (v, lo, hi) => Math.min(Math.max(v, lo), hi);
const calm = matchMedia('(prefers-reduced-motion: reduce)');

/* ---------- The Go program, in a worker ---------- */

const terms = [];
let worker = null;
const post = (m) => worker?.postMessage(m);

function startWorker() {
  if (worker) return;
  worker = new Worker('worker.js');
  worker.onmessage = ({ data }) => {
    if (data.error) return fail(data.error);
    terms[data.id]?.frame(data);
  };
  worker.onerror = (e) => fail(e.message);
}

function fail(reason) {
  console.error('sweetspot demo:', reason);
  for (const t of terms) t.dead = true;
  say('The live terminal could not start in this browser. The floor plan still works.');
}

class Term {
  constructor(name, { minFont = 11, maxFont = 14, size, world = newWorld(), seed = 7, warm = 90, boot } = {}) {
    this.box = $(`[data-term="${name}"]`);
    this.canvas = this.box ? $('canvas', this.box) : null;
    this.world = world;
    this.sim = new Sim(world, seed);
    this.size = size;
    this.font = [minFont, maxFont];
    this.warm = warm;
    this.boot = boot;
    this.id = terms.push(this) - 1;
    this.visible = false;
    this.waiting = false;
    this.onstate = null;
    if (this.canvas) {
      this.screen = new Screen(this.canvas);
      this.fit();
      new ResizeObserver(() => this.refit()).observe(this.box.parentElement);
    } else {
      this.cols = 80; this.rows = 24;
    }
  }

  // Chooses a grid for the available width.
  fit() {
    const style = getComputedStyle(this.box);
    const room = (this.box.parentElement.clientWidth ? this.box.clientWidth : 320) - parseFloat(style.paddingLeft) * 2;
    const { cols, rows, font } = this.size(room);
    const changed = cols !== this.cols || rows !== this.rows || font !== this.screen.font;
    this.cols = cols; this.rows = rows;
    this.screen.setFont(font);
    if (changed) this.canvas.style.cssText = `width:${cols * this.screen.cw}px;height:${rows * this.screen.ch}px`;
    return changed;
  }

  refit() {
    if (this.fit() && this.started) post({ op: 'resize', id: this.id, cols: this.cols, rows: this.rows, draw: true });
    else if (this.last) this.screen.paint(this.last.cells, this.last.cols, this.last.rows);
  }

  start() {
    if (this.started || !worker) return;
    this.started = true;
    post({ op: 'create', id: this.id, cols: this.cols, rows: this.rows });
    if (this.boot) this.boot(this);
    this.steps(this.warm);
  }

  steps(n, draw = true) {
    this.waiting = draw;
    post({ op: 'step', id: this.id, samples: Array.from({ length: n }, () => this.sim.step()), draw });
  }

  key(key) { post({ op: 'key', id: this.id, key, draw: true }); }

  tick() {
    if (!this.started || this.dead || this.waiting) return;
    this.steps(1);
  }

  frame({ cols, rows, cells, state }) {
    this.waiting = false;
    this.state = state;
    if (this.screen && cols === this.cols && rows === this.rows) {
      this.last = { cells: new Uint32Array(cells), cols, rows };
      this.screen.paint(this.last.cells, cols, rows);
    }
    this.onstate?.(state);
  }
}

const fitWidth = (wantCols, rowsFor, lo, hi) => (room) => {
  const font = clamp(Math.floor((room / wantCols / 0.6) * 4) / 4, lo, hi);
  const cols = Math.floor(room / (font * 0.6));
  return { cols, rows: rowsFor(cols), font };
};

/* ---------- Hero: floor plan, radio field, and terminal ---------- */

const world = newWorld();
const plan = $('#plan'), laptop = $('#laptop'), router = $('#router');
const air = $('#air'), pulse = $('#pulse'), heroEl = $('.hero');
const hero = new Term('hero', { world, size: fitWidth(78, (c) => (c < 60 ? 30 : 33), 10.5, 14) });

const say = (t) => { $('#say').textContent = t; };

function drawPlan() {
  const P = (v) => v * 100;
  const walls = PLAN.walls.filter((w) => w[5] !== 'hide');
  const line = (w, cls) => `<line class="${cls}" x1="${P(w[0])}" y1="${P(w[1])}" x2="${P(w[2])}" y2="${P(w[3])}"/>`;
  const rect = (x, y, w, h, r = 8) => `<rect x="${P(x)}" y="${P(y)}" width="${P(w)}" height="${P(h)}" rx="${r}"/>`;
  const furniture = [
    rect(0.3, 0.5, 1.9, 2.1, 14), rect(0.3, 0.5, 0.5, 2.1, 10), rect(3.5, 0.25, 0.85, 1.5),
    rect(0.2, 4.05, 1.5, 0.65, 26), rect(0.2, 5.1, 0.6, 0.35),
    rect(0.3, 7.15, 1.9, 1.5, 14), rect(0.3, 7.15, 0.45, 1.5, 10),
    rect(5.0, 0.5, 0.85, 2.6, 20), rect(6.4, 1.2, 1.3, 1.1, 60), rect(5.2, 7.2, 1.9, 1.1, 14), rect(8.3, 7.1, 0.9, 0.9, 45),
    rect(9.8, 0.2, 3.1, 0.6, 4), rect(10.75, 0.27, 0.9, 0.45, 4), rect(10.7, 2.9, 2.3, 0.7, 14),
    rect(12.4, 6.6, 1.35, 1.9), rect(11.5, 7.15, 0.65, 0.7, 30),
  ].join('');
  const labels = PLAN.rooms.map((r, i) => `<text data-room="${i}" x="${P(r.label[0])}" y="${P(r.label[1])}">${r.name}</text>`).join('');
  plan.insertAdjacentHTML('afterbegin', `<svg viewBox="0 0 1400 900" aria-hidden="true">
    <g fill="none" stroke="#062229" stroke-opacity=".42" stroke-width="5">${furniture}</g>
    <path d="M550 420h110v120H550z" fill="#062229"/><rect x="1312" y="18" width="72" height="130" rx="8" fill="#062229"/>
    <g class="walls" fill="none" stroke-linecap="square">
      <g stroke="#062229" stroke-width="18">${walls.filter((w) => w[5] === 'ext').map((w) => line(w)).join('')}</g>
      <g stroke="#062229" stroke-width="18">${walls.filter((w) => w[5] === 'win').map((w) => line(w)).join('')}</g>
      <g stroke="#effbf4" stroke-width="6" stroke-linecap="butt">${walls.filter((w) => w[5] === 'win').map((w) => line(w)).join('')}</g>
      <g stroke="#062229" stroke-width="10">${walls.filter((w) => w[5] === 'int').map((w) => line(w)).join('')}</g>
    </g>
    <g font-family="Anybody, sans-serif" font-weight="700" font-size="27" text-anchor="middle">${labels}</g>
    <g id="micro" opacity="0"><path d="M1090 70q30-34 60 0t60 0M1090 100q30-34 60 0t60 0" fill="none" stroke="#ff2d78" stroke-width="9" stroke-linecap="round"/></g>
  </svg>`);
}

let geo = null, cached = null, pulseField = null, dirty = true;

function measure() {
  const h = heroEl.getBoundingClientRect(), p = plan.getBoundingClientRect();
  const s = p.width / PLAN.w;
  geo = { W: h.width, H: h.height, s, ox: p.left - h.left, oy: p.top - h.top, dpr: Math.min(devicePixelRatio || 1, 2) };
  for (const c of [air, pulse]) { c.width = Math.round(geo.W * geo.dpr); c.height = Math.round(geo.H * geo.dpr); }
  dirty = true;
}

// The field covers the whole hero. Outside the walls it stays in the dark bands, so text keeps its contrast.
function drawAir() {
  const { W, H, s, ox, oy, dpr } = geo;
  const step = Math.max(0.16, Math.sqrt((W / s) * (H / s) / 17000));
  const x0 = -ox / s - step, y0 = -oy / s - step;
  const cols = Math.ceil(W / s / step) + 3, rows = Math.ceil(H / s / step) + 3;
  const f = field(world, x0, y0, cols, rows, step);
  pulseField = { f, cols, rows, step, x0, y0 };
  const g = new Float32Array(f);
  for (let k = 0; k < g.length; k++) g[k] = Math.min(g[k] - 4, LEVELS[1] - 1);
  const c = air.getContext('2d');
  c.setTransform(dpr, 0, 0, dpr, 0, 0);
  c.fillStyle = '#06303a';
  c.fillRect(0, 0, W, H);
  const paint = (data, from) => {
    for (let l = from; l < LEVELS.length; l++) {
      const path = new Path2D();
      trace(path, rings(data, cols, rows, LEVELS[l]), step * s, step * s, ox + x0 * s, oy + y0 * s);
      c.fillStyle = COLORS[l];
      c.fill(path, 'evenodd');
    }
  };
  paint(g, 0);
  // Inside the apartment, draw the true field up to the walls.
  c.save();
  c.beginPath();
  c.rect(ox, oy, PLAN.w * s, PLAN.h * s);
  c.clip();
  c.fillStyle = '#06303a';
  c.fillRect(ox, oy, PLAN.w * s, PLAN.h * s);
  paint(f, 0);
  c.restore();
  // Room names stay readable on every band.
  for (const t of $$('[data-room]', plan)) {
    const [x, y] = PLAN.rooms[t.dataset.room].label;
    t.classList.toggle('light', rssiAt(world, x, y - 0.1) < LEVELS[2]);
  }
}

const trail = [];
function drawPulse(now) {
  const { W, H, s, ox, oy, dpr } = geo;
  const c = pulse.getContext('2d');
  c.setTransform(dpr, 0, 0, dpr, 0, 0);
  c.clearRect(0, 0, W, H);
  c.save();
  c.beginPath();
  c.rect(ox, oy, PLAN.w * s, PLAN.h * s);
  c.clip();
  if (!calm.matches && pulseField) {
    const { f, cols, rows, step, x0, y0 } = pulseField;
    for (let k = 0; k < 2; k++) {
      const t = ((now / 3600 + k / 2) % 1);
      const level = -26 - t * 68;
      const path = new Path2D();
      trace(path, rings(f, cols, rows, level), step * s, step * s, ox + x0 * s, oy + y0 * s);
      c.strokeStyle = `rgba(239,251,244,${0.6 * (1 - t) ** 1.2})`;
      c.lineWidth = 2.5 - t;
      c.stroke(path);
    }
  }
  c.restore();
  trail.forEach((p, i) => {
    const a = (i + 1) / trail.length;
    c.beginPath();
    c.arc(ox + p.x * s, oy + p.y * s, 2 + a * 2.2, 0, 7);
    c.fillStyle = `rgba(6,34,41,${a * 0.75})`;
    c.fill();
  });
}

function place(el, p) { el.style.left = (p.x / PLAN.w) * 100 + '%'; el.style.top = (p.y / PLAN.h) * 100 + '%'; }

function readout() {
  const { x, y } = world.laptop;
  const dbm = hero.lastRssi ?? Math.round(rssiAt(world, x, y));
  $('#tag-dbm').textContent = `${dbm} dBm`;
  laptop.style.setProperty('--now', bandColor(dbm));
  laptop.style.setProperty('--on-now', dbm < LEVELS[2] ? '#effbf4' : '#062229');
  if (geo) {
    const half = $('.tag').offsetWidth / 2 + 2, px = x * geo.s;
    laptop.style.setProperty('--tx', `${clamp(0, half - px, geo.s * PLAN.w - half - px)}px`);
  }
  laptop.setAttribute('aria-label', `Laptop in the ${roomAt(x, y)}, ${dbm} dBm. Use the arrow keys to move it.`);
}

let nudged = false;
function moved(which) {
  if (which === 'laptop') {
    const last = trail[trail.length - 1];
    if (!last || Math.hypot(last.x - world.laptop.x, last.y - world.laptop.y) > 0.28) { trail.push({ ...world.laptop }); if (trail.length > 46) trail.shift(); }
    place(laptop, world.laptop);
    hero.lastRssi = Math.round(rssiAt(world, world.laptop.x, world.laptop.y));
    if (!nudged) { nudged = true; $('#nudge').classList.add('off'); }
  } else {
    place(router, world.router);
    hero.lastRssi = Math.round(rssiAt(world, world.laptop.x, world.laptop.y));
    dirty = true;
  }
  readout();
  wave.target(which === 'laptop' ? world.laptop : world.router);
}

function draggable(el, key) {
  let grab = null;
  el.addEventListener('pointerdown', (e) => {
    const r = plan.getBoundingClientRect(), p = world[key];
    grab = { dx: e.clientX - r.left - (p.x / PLAN.w) * r.width, dy: e.clientY - r.top - (p.y / PLAN.h) * r.height };
    el.setPointerCapture(e.pointerId);
  });
  el.addEventListener('pointermove', (e) => {
    if (!grab) return;
    const r = plan.getBoundingClientRect();
    world[key].x = clamp(((e.clientX - r.left - grab.dx) / r.width) * PLAN.w, 0.35, PLAN.w - 0.35);
    world[key].y = clamp(((e.clientY - r.top - grab.dy) / r.height) * PLAN.h, 0.35, PLAN.h - 0.35);
    moved(key);
  });
  for (const ev of ['pointerup', 'pointercancel']) el.addEventListener(ev, () => { grab = null; });
  el.addEventListener('keydown', (e) => {
    const d = { ArrowLeft: [-1, 0], ArrowRight: [1, 0], ArrowUp: [0, -1], ArrowDown: [0, 1] }[e.key];
    if (!d) return;
    e.preventDefault();
    world[key].x = clamp(world[key].x + d[0] * 0.3, 0.35, PLAN.w - 0.35);
    world[key].y = clamp(world[key].y + d[1] * 0.3, 0.35, PLAN.h - 0.35);
    moved(key);
  });
}

// A click on the floor walks the laptop there.
let walk = null;
plan.addEventListener('click', (e) => {
  if (e.target.closest('.thing')) return;
  const r = plan.getBoundingClientRect();
  walk = { x: clamp(((e.clientX - r.left) / r.width) * PLAN.w, 0.35, PLAN.w - 0.35), y: clamp(((e.clientY - r.top) / r.height) * PLAN.h, 0.35, PLAN.h - 0.35) };
});
laptop.addEventListener('pointerdown', () => { walk = null; });

const LINES = {
  band24: [() => '2.4 GHz goes through walls better, but it is slow and crowded. The laptop now uses channel 6.', 'Back on 5 GHz.'],
  microwave: [(f) => (f.band24 ? 'Popcorn time. The noise floor jumps near the kitchen.' : 'A microwave leaks near 2.4 GHz. Your 5 GHz link does not even notice. Turn on 2.4 GHz to feel it.'), 'Popcorn is ready.'],
  narrow: [() => 'Half the channel width, about half the top speed.', 'Back to 80 MHz.'],
  crowded: [() => 'Eleven neighbors now share your channel. Press s to scan for a cleaner one.', 'The neighbors went to bed.'],
  upload: [() => 'Somebody is backing up every photo they own. Press b to measure the damage.', 'The backup is done. Press b to measure again.'],
  airdrop: [() => 'macOS leaves your channel now and then to look for AirDrop peers. Watch the router chart.', 'AirDrop is off.'],
  coax: [() => 'The cable modem reports weak signal and errors. Open tab 4.', 'A technician tightened the connector.'],
  sudo: [() => 'sudo unlocks airtime, MCS, spatial streams, and the network name.', 'Back to a normal user. Nothing else changes.'],
};
for (const b of $$('[data-flag]')) {
  b.addEventListener('click', () => {
    const k = b.dataset.flag, on = (world.flags[k] = !world.flags[k]);
    b.setAttribute('aria-pressed', on);
    say(on ? LINES[k][0](world.flags) : LINES[k][1]);
    if (k === 'band24') { dirty = true; hero.lastRssi = null; readout(); }
    if (k === 'microwave') $('#micro').setAttribute('opacity', on ? 1 : 0);
    hero.tick();
  });
}

for (const b of $$('#hero-rig [data-key]')) {
  b.addEventListener('click', () => {
    if (b.dataset.key !== 'pin') return hero.key(b.dataset.key);
    if (hero.state?.pinning) return hero.key('Enter');
    const name = roomAt(world.laptop.x, world.laptop.y);
    hero.key(' ');
    setTimeout(() => hero.key('Enter'), 900);
    say(`Pinned the ${name.toLowerCase()}. Walk somewhere else and pin again, then open tab 2.`);
  });
}

const KEYS = /^([1-4bcnsrmepdhjkl ]|Enter|Escape|Backspace|Arrow(Up|Down|Left|Right))$/;
hero.canvas.addEventListener('keydown', (e) => {
  if (e.metaKey || e.ctrlKey || e.altKey) return;
  const typing = hero.state?.pinning;
  if (!(typing ? e.key.length === 1 || KEYS.test(e.key) : KEYS.test(e.key))) return;
  e.preventDefault();
  hero.key(e.key);
});
hero.canvas.addEventListener('click', (e) => {
  const r = hero.canvas.getBoundingClientRect();
  const x = Math.floor(((e.clientX - r.left) / r.width) * hero.cols), y = Math.floor(((e.clientY - r.top) / r.height) * hero.rows);
  if (y > 2) return;
  for (const m of hero.screen.row(y).matchAll(/([1-4]) (Radar|Spots|RF|Advisor)[^1-4]*(\(\d+\))?/g)) if (x >= m.index - 1 && x < m.index + m[0].length) hero.key(m[1]);
});

hero.onstate = (st) => {
  $('#tag-score').textContent = `${st.score.total} ${st.score.grade}`;
};

/* ---------- Headline: letters swell like signal bands ---------- */

const wave = (() => {
  const h1 = $('.wave');
  const lines = $$('.line', h1).map((el) => {
    const text = el.textContent;
    el.textContent = '';
    return [...text].map((ch) => { const s = document.createElement('span'); s.textContent = ch; el.append(s); return s; });
  });
  let natural = null, pos = { x: 0.82, y: 1 }, goal = { ...pos }, hover = false, idle = true;
  const measureLetters = () => {
    natural = lines.map((l) => l.map((s) => { s.style.fontVariationSettings = '"wdth" 100,"wght" 800'; return s.getBoundingClientRect().width; }));
  };
  const apply = () => {
    if (!natural) return;
    const box = h1.getBoundingClientRect();
    lines.forEach((letters, li) => {
      const adv = natural[li], total = adv.reduce((a, b) => a + b, 0);
      let x = 0;
      const g = letters.map((_, i) => {
        const cx = (x + adv[i] / 2) / box.width; x += adv[i];
        const dx = (cx - pos.x) / 0.15, dy = (li - pos.y) / 0.9;
        return Math.exp(-(dx * dx + dy * dy));
      });
      const mean = g.reduce((a, v, i) => a + v * adv[i], 0) / total;
      letters.forEach((s, i) => {
        const w = clamp(96 + 82 * (g[i] - mean), 60, 150);
        s.style.fontVariationSettings = `"wdth" ${w.toFixed(1)},"wght" ${(640 + 260 * g[i]).toFixed(0)}`;
        s.style.color = g[i] > 0.82 ? COLORS[5] : g[i] > 0.6 ? COLORS[4] : g[i] > 0.36 ? COLORS[3] : g[i] > 0.15 ? COLORS[2] : '';
      });
    });
  };
  h1.addEventListener('pointermove', (e) => {
    const r = h1.getBoundingClientRect();
    hover = true; idle = false;
    goal = { x: (e.clientX - r.left) / r.width, y: clamp(((e.clientY - r.top) / r.height) * 2 - 0.5, 0, 1) };
  });
  h1.addEventListener('pointerleave', () => { hover = false; });
  return {
    ready() { measureLetters(); apply(); },
    target(p) {
      if (hover || !geo) return;
      const r = h1.getBoundingClientRect(), h = heroEl.getBoundingClientRect();
      idle = false;
      goal = { x: clamp((geo.ox + p.x * geo.s - (r.left - h.left)) / r.width, 0.02, 0.9), y: 1 - p.y / PLAN.h };
    },
    frame(now) {
      if (idle && !calm.matches) goal = { x: 0.70 + Math.sin(now / 2600) * 0.2, y: 1 };
      const dx = goal.x - pos.x, dy = goal.y - pos.y;
      if (Math.abs(dx) + Math.abs(dy) < 0.002) return;
      const k = calm.matches ? 1 : 0.14;
      pos = { x: pos.x + dx * k, y: pos.y + dy * k };
      apply();
    },
  };
})();

/* ---------- Tour: one terminal, four tabs ---------- */

const tourWorld = newWorld();
const tour = new Term('tour', {
  world: tourWorld, seed: 21, warm: 70,
  size: fitWidth(108, (c) => (c < 60 ? 34 : 36), 10.5, 14),
  boot(t) {
    for (const [name, x, y] of [['Kitchen table', 11.8, 3.2], ['Office desk', 12.7, 7.4], ['Sofa', 5.5, 1.8], ['Bed', 1.6, 1.4], ['Guest room', 1.3, 7.7]]) {
      Object.assign(t.world.laptop, { x, y });
      t.steps(52, false);
      post({ op: 'key', id: t.id, key: 'b' });
      t.steps(20, false);
      post({ op: 'pin', id: t.id, name });
    }
    Object.assign(t.world.laptop, { x: 7.6, y: 2.6 });
    t.steps(60, false);
    for (const key of ['s', 'r', 'b']) post({ op: 'key', id: t.id, key });
    t.steps(24, false);
    post({ op: 'key', id: t.id, key: 'm' });
    post({ op: 'key', id: t.id, key: '1' });
  },
});
const tourTabs = $$('[data-tour]');
function pickTour(b) {
  for (const t of tourTabs) { const on = t === b; t.setAttribute('aria-selected', on); t.tabIndex = on ? 0 : -1; }
  tour.key(b.dataset.tour);
}
tourTabs.forEach((b, i) => {
  b.addEventListener('click', () => pickTour(b));
  b.addEventListener('keydown', (e) => {
    const d = { ArrowDown: 1, ArrowRight: 1, ArrowUp: -1, ArrowLeft: -1 }[e.key];
    if (!d) return;
    e.preventDefault();
    const n = tourTabs[(i + d + 4) % 4];
    n.focus();
    pickTour(n);
  });
});
// Someone walks around in the tour terminal.
const tourPath = [[7.6, 2.6], [8.8, 2.2], [11.4, 2.6], [11.6, 4.6], [11.4, 6.6], [11.4, 4.4], [8.6, 2.4], [7.2, 6.4], [5.0, 6.0], [3.4, 4.6], [5.0, 6.2], [7.4, 4.4]];
let leg = 0;

/* ---------- Blame: which hop is bad ---------- */

const blameWorld = newWorld();
Object.assign(blameWorld.laptop, { x: 8.2, y: 2.6 });
const blame = new Term('none', { world: blameWorld, seed: 5, warm: 60 });
blame.mode = 'none';
const ms = (s) => (s.received ? `${s.avg_ms.toFixed(1)} ms avg, ${s.jitter_ms.toFixed(1)} jitter, ${s.loss_pct.toFixed(1)}% loss` : '…');
blame.onstate = (st) => {
  $('#gw').textContent = ms(st.gw);
  $('#wan').textContent = ms(st.wan);
  const skip = /Airtime|MIMO|DNS/;
  const a = blame.mode === 'none' ? st.advice.find((v) => v.severity === 'GOOD') : st.advice.find((v) => !skip.test(v.category));
  if (!a) return;
  const sev = $('#v-sev');
  sev.textContent = a.severity; sev.className = 'sev ' + a.severity;
  if ($('#v-title').textContent !== a.title) { $('#v-title').textContent = a.title; $('#v-action').textContent = a.action; }
};
for (const b of $$('[data-blame]')) {
  b.addEventListener('click', () => {
    for (const o of $$('[data-blame]')) o.setAttribute('aria-pressed', o === b);
    blame.mode = b.dataset.blame;
    blameWorld.flags.crowded = blame.mode === 'wifi';
    blame.isp = blame.mode === 'isp';
    blame.steps(50);
  });
}
// The ISP case loses packets after the router, without a modem page.
const plainStep = blame.sim.step.bind(blame.sim);
blame.sim.step = (dt) => {
  const s = plainStep(dt);
  if (blame.isp) s.wan = s.wan.map((v) => (blame.sim.rnd() < 0.09 ? -1 : v + blame.sim.rnd() ** 3 * 90));
  return s;
};

const packets = [];
let lastPacket = 0;
function drawHops(now, dt) {
  const c = $('#hops'), dpr = Math.min(devicePixelRatio || 1, 2), W = c.clientWidth, H = c.clientHeight;
  if (c.width !== Math.round(W * dpr)) { c.width = Math.round(W * dpr); c.height = Math.round(H * dpr); }
  const x = c.getContext('2d');
  x.setTransform(dpr, 0, 0, dpr, 0, 0);
  x.clearRect(0, 0, W, H);
  const y = H * 0.5, A = Math.min(58, W * 0.1), B = W / 2, C = W - A;
  const bad = { wifi: blame.mode === 'wifi', isp: blame.mode === 'isp' };
  const wire = (x1, x2, broken) => {
    x.strokeStyle = broken ? '#ff2d78' : '#3fd0b0'; x.lineWidth = 5; x.lineCap = 'round'; x.setLineDash(broken ? [3, 15] : []);
    x.beginPath(); x.moveTo(x1, y); x.lineTo(x2, y); x.stroke(); x.setLineDash([]);
  };
  wire(A, B, bad.wifi); wire(B, C, bad.isp);
  for (const [px, r] of [[A, 17], [B, 21], [C, 17]]) { x.beginPath(); x.arc(px, y, r, 0, 7); x.fillStyle = '#effbf4'; x.fill(); x.beginPath(); x.arc(px, y, r * 0.5, 0, 7); x.fillStyle = '#062229'; x.fill(); }
  if (!calm.matches && now - lastPacket > 260) {
    lastPacket = now;
    packets.push({ p: 0, v: 0.00062 * (bad.wifi ? 0.35 + Math.random() * 0.9 : 1), wan: 1, drop: bad.isp && Math.random() < 0.3 ? 0.58 + Math.random() * 0.36 : bad.wifi && Math.random() < 0.12 ? 0.1 + Math.random() * 0.35 : 2, slow: bad.isp ? 0.4 + Math.random() : 1, fall: 0 });
  }
  for (let i = packets.length - 1; i >= 0; i--) {
    const k = packets[i];
    if (k.p >= k.drop) { k.fall += dt * 0.0016; } else k.p += k.v * dt * (k.p > 0.5 ? k.slow : 1);
    if (k.p >= 1 || k.fall > 1) { packets.splice(i, 1); continue; }
    const px = k.p < 0.5 ? A + (B - A) * k.p * 2 : B + (C - B) * (k.p - 0.5) * 2;
    x.globalAlpha = 1 - k.fall;
    x.fillStyle = k.fall ? '#ff2d78' : '#ffe14a';
    x.beginPath(); x.arc(px, y + k.fall * k.fall * 60, 8, 0, 7); x.fill();
    x.globalAlpha = 1;
  }
}

/* ---------- Sizes: a terminal you can resize ---------- */

const sizerWorld = newWorld();
Object.assign(sizerWorld.laptop, { x: 10.6, y: 6.6 });
const grid = { cols: 128, rows: 34 };
const sizeTerm = new Term('size', { world: sizerWorld, seed: 13, warm: 90, size: () => {
  const pad = parseFloat(getComputedStyle($('#sizer')).paddingLeft) + parseFloat(getComputedStyle($('.sizer')).paddingLeft);
  const room = $('.sizer').clientWidth - 2 * pad;
  const font = innerWidth < 640 ? 10.5 : 12;
  const most = Math.floor(room / (font * 0.6));
  return { cols: Math.min(grid.cols, most), rows: grid.rows, font };
}, boot: (t) => { post({ op: 'key', id: t.id, key: 'b' }); t.steps(20, false); } });
function resized() {
  grid.cols = clamp(Math.round(grid.cols), 20, 200); grid.rows = clamp(Math.round(grid.rows), 6, 50);
  sizeTerm.refit();
  grid.cols = sizeTerm.cols;
  $('#size-now').textContent = `${sizeTerm.cols}×${sizeTerm.rows}`;
}
for (const b of $$('[data-size]')) b.addEventListener('click', () => { [grid.cols, grid.rows] = b.dataset.size.split('x').map(Number); resized(); });
{
  const grip = $('#grip');
  let from = null;
  grip.addEventListener('pointerdown', (e) => { from = { x: e.clientX, y: e.clientY, ...grid }; grip.setPointerCapture(e.pointerId); });
  grip.addEventListener('pointermove', (e) => {
    if (!from) return;
    grid.cols = from.cols + (e.clientX - from.x) / sizeTerm.screen.cw;
    grid.rows = from.rows + (e.clientY - from.y) / sizeTerm.screen.ch;
    resized();
  });
  for (const ev of ['pointerup', 'pointercancel']) grip.addEventListener(ev, () => { from = null; });
  grip.addEventListener('keydown', (e) => {
    const d = { ArrowLeft: [-4, 0], ArrowRight: [4, 0], ArrowUp: [0, -2], ArrowDown: [0, 2] }[e.key];
    if (!d) return;
    e.preventDefault();
    grid.cols += d[0]; grid.rows += d[1];
    resized();
  });
}

/* ---------- Header meter: scrolling walks toward the router ---------- */

const stops = $$('[data-dbm]');
function meter() {
  const y = scrollY + innerHeight * 0.45 * Math.min(1, scrollY / 400);
  let dbm = Number(stops[0].dataset.dbm);
  for (let i = 0; i < stops.length; i++) {
    const top = stops[i].offsetTop, v = Number(stops[i].dataset.dbm), n = stops[i + 1];
    if (y < top) break;
    dbm = n ? v + (Number(n.dataset.dbm) - v) * clamp((y - top) / (n.offsetTop - top), 0, 1) : v;
  }
  if (scrollY + innerHeight > document.documentElement.scrollHeight - 4) dbm = -34;
  $('#meter').textContent = Math.round(dbm);
  const on = clamp(Math.ceil((dbm + 92) / 11), 1, 5);
  const box = $('.meter');
  box.style.setProperty('--now', bandColor(dbm));
  $$('.bars i', box).forEach((b, i) => b.classList.toggle('on', i < on));
}

/* ---------- Copy buttons ---------- */

for (const b of $$('[data-copy]')) {
  b.addEventListener('click', async () => {
    try { await navigator.clipboard.writeText(b.dataset.copy); b.textContent = 'Copied'; } catch { b.textContent = 'Select it'; getSelection().selectAllChildren(b.previousElementSibling); }
    setTimeout(() => { b.textContent = 'Copy'; }, 1800);
  });
}

/* ---------- Run ---------- */

drawPlan();
place(laptop, world.laptop); place(router, world.router); place($('#nudge'), world.laptop);
measure();
readout();
meter();
draggable(laptop, 'laptop');
draggable(router, 'router');

new ResizeObserver(() => { measure(); readout(); wave.ready(); }).observe(heroEl);
addEventListener('scroll', meter, { passive: true });
addEventListener('resize', () => { meter(); resized(); });
document.fonts.ready.then(() => { wave.ready(); for (const t of terms) t.screen && t.last && t.screen.paint(t.last.cells, t.last.cols, t.last.rows); });

const seen = new IntersectionObserver((list) => {
  for (const e of list) {
    for (const t of e.target.demos) { t.visible = e.isIntersecting; if (e.isIntersecting) t.start(); }
    if (e.target === heroEl) heroVisible = e.isIntersecting;
    if (e.target.id === 'blame') hopsVisible = e.isIntersecting;
  }
}, { rootMargin: '120px' });
let heroVisible = true, hopsVisible = false;
for (const [el, demos] of [[heroEl, [hero]], [$('#tabs'), [tour]], [$('#blame'), [blame]], [$('#sizes'), [sizeTerm]]]) el.demos = demos;

// Show a saved frame at once, and load the program when the page is idle.
fetch('posters.json').then((r) => r.json()).then((list) => {
  if (hero.last) return;
  const p = hero.cols >= 60 ? list[0] : list[1];
  const cells = new Uint32Array(Uint8Array.from(atob(p.cells), (c) => c.charCodeAt(0)).buffer);
  const keep = hero.screen.font;
  hero.screen.setFont((hero.cols * keep) / p.cols);
  hero.screen.paint(cells, p.cols, p.rows);
  hero.screen.setFont(keep);
}).catch(() => {});

const begin = () => { startWorker(); for (const [el] of [[heroEl], [$('#tabs')], [$('#blame')], [$('#sizes')]]) seen.observe(el); };
(window.requestIdleCallback ?? ((f) => setTimeout(f, 300)))(begin, { timeout: 1500 });

setInterval(() => {
  if (document.hidden) return;
  const [tx, ty] = tourPath[leg % tourPath.length], l = tourWorld.laptop, d = Math.hypot(tx - l.x, ty - l.y);
  if (d < 0.3) leg++; else { l.x += ((tx - l.x) / d) * 0.24; l.y += ((ty - l.y) / d) * 0.24; }
  for (const t of terms) if (t.visible) t.tick();
}, 250);

let before = performance.now(), slow = 0;
requestAnimationFrame(function loop(now) {
  const dt = Math.min(now - before, 60);
  before = now;
  if (heroVisible && !document.hidden) {
    if (walk) {
      const l = world.laptop, d = Math.hypot(walk.x - l.x, walk.y - l.y), k = Math.min(1, (dt * 0.0052) / Math.max(d, 1e-6));
      l.x += (walk.x - l.x) * k; l.y += (walk.y - l.y) * k;
      if (d < 0.02) walk = null;
      moved('laptop');
    }
    if (dirty) { dirty = false; drawAir(); }
    wave.frame(now);
    if ((slow ^= 1)) drawPulse(now);
  }
  if (hopsVisible && !document.hidden) drawHops(now, dt);
  requestAnimationFrame(loop);
});

// Tests and the social image read this.
window.sweetspotPage = { terms, world, moved, hero, tour, START };
