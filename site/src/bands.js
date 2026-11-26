// Filled signal bands from a grid (marching squares, with closed rings).

export const LEVELS = [-85, -76, -68, -60, -51, -41];
export const COLORS = ['#0a4d57', '#0b7a80', '#3fd0b0', '#ffe14a', '#ff8b3a', '#ff2d78'];

export function bandColor(dbm) {
  let i = LEVELS.length - 1;
  while (i > 0 && dbm < LEVELS[i]) i--;
  return COLORS[i];
}

// Returns closed rings around the area where f >= level, in grid units.
export function rings(f, cols, rows, level) {
  const at = (i, j) => (i < 0 || j < 0 || i >= cols || j >= rows ? -200 : f[j * cols + i]);
  const W = cols + 2, next = new Map(), pts = new Map();
  const H = (i, j) => 2 * ((j + 1) * W + i + 1), V = (i, j) => H(i, j) + 1;
  const mix = (a, b) => (level - a) / (b - a);
  const link = (a, b) => next.set(a, b);
  for (let j = -1; j < rows; j++) {
    let a = at(-1, j), d = at(-1, j + 1);
    for (let i = -1; i < cols; i++) {
      const b = at(i + 1, j), c = at(i + 1, j + 1);
      const k = (a >= level ? 1 : 0) | (b >= level ? 2 : 0) | (c >= level ? 4 : 0) | (d >= level ? 8 : 0);
      if (k && k !== 15) {
        const T = H(i, j), B = H(i, j + 1), L = V(i, j), R = V(i + 1, j);
        if ((k & 1) !== ((k >> 1) & 1)) pts.set(T, [i + mix(a, b), j]);
        if (((k >> 3) & 1) !== ((k >> 2) & 1)) pts.set(B, [i + mix(d, c), j + 1]);
        if ((k & 1) !== ((k >> 3) & 1)) pts.set(L, [i, j + mix(a, d)]);
        if (((k >> 1) & 1) !== ((k >> 2) & 1)) pts.set(R, [i + 1, j + mix(b, c)]);
        // The inside is always on the same side, so every edge has one start and one end.
        switch (k) {
          case 1: link(L, T); break;
          case 2: link(T, R); break;
          case 3: link(L, R); break;
          case 4: link(R, B); break;
          case 5: link(L, T); link(R, B); break;
          case 6: link(T, B); break;
          case 7: link(L, B); break;
          case 8: link(B, L); break;
          case 9: link(B, T); break;
          case 10: link(T, R); link(B, L); break;
          case 11: link(B, R); break;
          case 12: link(R, L); break;
          case 13: link(R, T); break;
          case 14: link(T, L); break;
        }
      }
      a = b; d = c;
    }
  }
  const out = [];
  while (next.size) {
    const start = next.keys().next().value;
    const ring = [];
    for (let e = start; e !== undefined; ) {
      const n = next.get(e);
      next.delete(e);
      ring.push(pts.get(e));
      e = n === start || !next.has(n) ? undefined : n;
    }
    if (ring.length > 3) out.push(ring);
  }
  return out;
}

// Adds rings to a path as smooth curves through the midpoints.
export function trace(path, list, sx, sy, ox, oy) {
  for (const ring of list) {
    const n = ring.length;
    const X = (k) => ox + ring[k % n][0] * sx, Y = (k) => oy + ring[k % n][1] * sy;
    path.moveTo((X(0) + X(n - 1)) / 2, (Y(0) + Y(n - 1)) / 2);
    for (let k = 0; k < n; k++) path.quadraticCurveTo(X(k), Y(k), (X(k) + X(k + 1)) / 2, (Y(k) + Y(k + 1)) / 2);
    path.closePath();
  }
}
