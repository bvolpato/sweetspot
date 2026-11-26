// Paints terminal cells on a canvas. Box drawing, blocks, Braille, and symbols are vector shapes,
// so frames line up and look the same on every system.

const BG = '#0a0f1c', FG = '#f1f5f9';
const css = new Map();
const color = (v, fallback) => {
  if (!(v & 0x1000000)) return fallback;
  let s = css.get(v & 0xffffff);
  if (!s) css.set(v & 0xffffff, (s = '#' + (v & 0xffffff).toString(16).padStart(6, '0')));
  return s;
};

// Arms of each box character: left, right, up, down. 1 is light and 2 is heavy.
const BOX = { '─': [1, 1, 0, 0], '━': [2, 2, 0, 0], '│': [0, 0, 1, 1], '┃': [0, 0, 2, 2], '┏': [0, 2, 0, 2], '┓': [2, 0, 0, 2], '┗': [0, 2, 2, 0], '┛': [2, 0, 2, 0], '┣': [0, 2, 2, 2], '┫': [2, 0, 2, 2], '┻': [2, 2, 2, 0], '┳': [2, 2, 0, 2], '╸': [2, 0, 0, 0], '╹': [0, 0, 2, 0], '╺': [0, 2, 0, 0], '╻': [0, 0, 0, 2], '┌': [0, 1, 0, 1], '┐': [1, 0, 0, 1], '└': [0, 1, 1, 0], '┘': [1, 0, 1, 0], '├': [0, 1, 1, 1], '┤': [1, 0, 1, 1], '┬': [1, 1, 0, 1], '┴': [1, 1, 1, 0], '┼': [1, 1, 1, 1] };
const ROUND = { '╭': [1, 1], '╮': [-1, 1], '╰': [1, -1], '╯': [-1, -1] };
const DOTS = [[0, 0], [0, 1], [0, 2], [1, 0], [1, 1], [1, 2], [0, 3], [1, 3]];

function poly(c, pts, fill = true) {
  c.beginPath();
  pts.forEach(([x, y], i) => (i ? c.lineTo(x, y) : c.moveTo(x, y)));
  if (fill) { c.closePath(); c.fill(); } else c.stroke();
}
const disc = (c, x, y, r) => { c.beginPath(); c.arc(x, y, r, 0, 7); c.fill(); };
const ring = (c, x, y, r) => { c.beginPath(); c.arc(x, y, r, 0, 7); c.stroke(); };

// Each symbol draws around the center (x, y) with radius r.
const SYMBOL = {
  '●': (c, x, y, r) => disc(c, x, y, r), '•': (c, x, y, r) => disc(c, x, y, r * 0.5), '○': (c, x, y, r) => ring(c, x, y, r * 0.9),
  '◉': (c, x, y, r) => { ring(c, x, y, r); disc(c, x, y, r * 0.55); }, '◎': (c, x, y, r) => { ring(c, x, y, r); ring(c, x, y, r * 0.45); },
  '◈': (c, x, y, r) => { const d = (k, f) => poly(c, [[x, y - k], [x + k, y], [x, y + k], [x - k, y], [x, y - k]], f); d(r * 1.15, false); d(r * 0.6, true); },
  '▲': (c, x, y, r) => poly(c, [[x, y - r], [x + r, y + r * 0.8], [x - r, y + r * 0.8]]),
  '★': (c, x, y, r) => poly(c, Array.from({ length: 10 }, (_, i) => { const a = (i * Math.PI) / 5 - Math.PI / 2, k = i % 2 ? r * 0.45 : r * 1.15; return [x + Math.cos(a) * k, y + Math.sin(a) * k]; })),
  '✦': (c, x, y, r) => poly(c, Array.from({ length: 8 }, (_, i) => { const a = (i * Math.PI) / 4 - Math.PI / 2, k = i % 2 ? r * 0.35 : r * 1.25; return [x + Math.cos(a) * k, y + Math.sin(a) * k]; })),
  '⚡': (c, x, y, r) => poly(c, [[x + r * 0.45, y - r * 1.3], [x - r * 0.75, y + r * 0.2], [x - r * 0.05, y + r * 0.2], [x - r * 0.45, y + r * 1.3], [x + r * 0.75, y - r * 0.25], [x + r * 0.05, y - r * 0.25]]),
  '✓': (c, x, y, r) => poly(c, [[x - r, y + r * 0.1], [x - r * 0.25, y + r * 0.85], [x + r, y - r * 0.8]], false),
  '→': (c, x, y, r) => { poly(c, [[x - r, y], [x + r, y]], false); poly(c, [[x + r * 0.2, y - r * 0.7], [x + r, y], [x + r * 0.2, y + r * 0.7]], false); },
  '▤': (c, x, y, r) => { c.strokeRect(x - r, y - r, r * 2, r * 2); for (const k of [-0.4, 0, 0.4]) poly(c, [[x - r, y + r * k], [x + r, y + r * k]], false); },
  '∿': (c, x, y, r) => { c.beginPath(); c.moveTo(x - r * 1.1, y); c.bezierCurveTo(x - r * 0.5, y - r * 1.9, x + r * 0.5, y + r * 1.9, x + r * 1.1, y); c.stroke(); },
  '⌁': (c, x, y, r) => poly(c, [[x - r, y + r * 0.3], [x + r * 0.2, y - r * 0.5], [x - r * 0.2, y + r * 0.5], [x + r, y - r * 0.3]], false),
};

export class Screen {
  constructor(canvas) {
    this.canvas = canvas;
    this.ctx = canvas.getContext('2d', { alpha: false });
    this.cols = this.rows = 0;
    this.cells = null;
  }

  // Returns the largest grid that fits the box, and the cell size for it.
  static grid(width, height, font) {
    const cw = font * 0.6, ch = Math.round(font * 1.28);
    return { cols: Math.max(1, Math.floor(width / cw)), rows: Math.max(1, Math.floor(height / ch)), cw, ch };
  }

  setFont(px) { this.font = px; this.cw = px * 0.6; this.ch = Math.round(px * 1.28); }

  paint(cells, cols, rows) {
    this.cells = cells; this.cols = cols; this.rows = rows;
    const { canvas, ctx: c, font, cw, ch } = this;
    const dpr = Math.min(window.devicePixelRatio || 1, 3);
    const W = Math.round(cols * cw * dpr), H = Math.round(rows * ch * dpr);
    if (canvas.width !== W || canvas.height !== H) {
      canvas.width = W; canvas.height = H;
      canvas.style.width = cols * cw + 'px'; canvas.style.height = rows * ch + 'px';
    }
    c.setTransform(dpr, 0, 0, dpr, 0, 0);
    c.fillStyle = BG;
    c.fillRect(0, 0, cols * cw, rows * ch);
    c.textBaseline = 'alphabetic';
    c.textAlign = 'left';
    c.lineCap = 'butt';
    const base = Math.round(ch * 0.5 + font * 0.36), snap = (v) => Math.round(v * dpr) / dpr;

    for (let y = 0; y < rows; y++) {
      for (let x = 0; x < cols; ) {
        const bg = cells[(y * cols + x) * 3 + 2];
        let n = 1;
        while (x + n < cols && cells[(y * cols + x + n) * 3 + 2] === bg) n++;
        if (bg & 0x1000000) {
          c.fillStyle = color(bg, BG);
          const l = snap(x * cw), r = snap((x + n) * cw);
          c.fillRect(l, y * ch, r - l, ch);
        }
        x += n;
      }
    }
    let lastFont = '';
    for (let y = 0; y < rows; y++) {
      for (let x = 0; x < cols; x++) {
        const at = (y * cols + x) * 3, cp = cells[at];
        if (cp === 0 || cp === 32) continue;
        const fg = cells[at + 1];
        c.fillStyle = c.strokeStyle = color(fg, FG);
        const px = x * cw, py = y * ch;
        if (cp >= 0x2500 && cp <= 0x28ff && this.shape(cp, x, py, snap)) continue;
        const s = String.fromCodePoint(cp), sym = SYMBOL[s];
        if (sym) {
          c.lineWidth = Math.max(1, font * 0.1);
          const wide = x + 1 < cols && cells[at + 3] === 0;
          sym(c, px + (wide ? cw : cw / 2), py + ch / 2, font * 0.31);
          continue;
        }
        const f = `${fg & 0x2000000 ? 700 : 400} ${font}px "Sweet Mono", ui-monospace, Menlo, Consolas, monospace`;
        if (f !== lastFont) c.font = lastFont = f;
        c.fillText(s, px, py + base);
      }
    }
  }

  shape(cp, col, py, snap) {
    const { ctx: c, cw, ch, font } = this;
    const s = String.fromCodePoint(cp), px = col * cw;
    const l = snap(px), t = py, r = snap((col + 1) * cw), b = py + ch, mx = snap(px + cw / 2), my = snap(py + ch / 2);
    const light = Math.max(1, snap(font * 0.085)), heavy = Math.max(2, snap(font * 0.2));
    const box = BOX[s];
    if (box) {
      const [L, R, U, D] = box;
      const k = (v) => (v === 2 ? heavy : light);
      const pad = Math.max(...[L, R, U, D].map((v) => (v ? k(v) : 0))) / 2;
      const h = (x1, x2, v) => c.fillRect(x1, snap(my - k(v) / 2), x2 - x1, k(v));
      const u = (y1, y2, v) => c.fillRect(snap(mx - k(v) / 2), y1, k(v), y2 - y1);
      const p = snap(pad);
      if (L && L === R) h(l, r, L); else { if (L) h(l, mx + p, L); if (R) h(snap(mx - pad), r, R); }
      if (U && U === D) u(t, b, U); else { if (U) u(t, my + p, U); if (D) u(snap(my - pad), b, D); }
      return true;
    }
    if (ROUND[s]) {
      const [dx, dy] = ROUND[s], rad = Math.min(cw, ch) * 0.48;
      c.lineWidth = light;
      const x = snap(mx - light / 2) + light / 2, y = snap(my - light / 2) + light / 2;
      c.beginPath();
      c.moveTo(dx > 0 ? r : l, y);
      c.lineTo(x + dx * rad, y);
      c.quadraticCurveTo(x, y, x, y + dy * rad);
      c.lineTo(x, dy > 0 ? b : t);
      c.stroke();
      return true;
    }
    if (s === '┈') {
      for (let i = 0; i < 4; i++) c.fillRect(l + ((r - l) * (i + 0.25)) / 4, my - light / 2, (r - l) / 8, light);
      return true;
    }
    if (cp >= 0x2581 && cp <= 0x2588) {
      const h = (ch * (cp - 0x2580)) / 8;
      c.fillRect(l, b - h, r - l, h);
      return true;
    }
    if (cp >= 0x2591 && cp <= 0x2593) {
      c.globalAlpha = (cp - 0x2590) * 0.25;
      c.fillRect(l, t, r - l, ch);
      c.globalAlpha = 1;
      return true;
    }
    if (cp >= 0x2800 && cp <= 0x28ff) {
      const bits = cp - 0x2800, w = cw / 2, h = ch / 4, rad = Math.min(w, h) * 0.4;
      for (let i = 0; i < 8; i++) if (bits & (1 << i)) disc(c, px + (DOTS[i][0] + 0.5) * w, py + (DOTS[i][1] + 0.5) * h, rad);
      return true;
    }
    return false;
  }

  row(y) {
    let s = '';
    if (!this.cells || y < 0 || y >= this.rows) return s;
    for (let x = 0; x < this.cols; x++) s += String.fromCodePoint(this.cells[(y * this.cols + x) * 3] || 32);
    return s;
  }

  text() { return Array.from({ length: this.rows }, (_, y) => this.row(y).trimEnd()).join('\n'); }
}
