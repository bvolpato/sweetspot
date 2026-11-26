// Runs the demo in Node and saves the first frames, so the page shows a terminal before the
// WebAssembly file arrives. It also fails the build when the demo does not start.
import fs from 'node:fs';
import path from 'node:path';
import { pathToFileURL } from 'node:url';

const out = path.resolve(process.argv[2]);
globalThis.fs = fs;
globalThis.path = path;
await import(pathToFileURL(path.join(out, 'wasm_exec.js')));
const { Sim, newWorld } = await import(pathToFileURL(path.join(out, 'sim.js')));

const ready = new Promise((resolve) => { globalThis.sweetspotReady = resolve; });
const go = new Go();
const { instance } = await WebAssembly.instantiate(fs.readFileSync(path.join(out, '../.build/sweetspot.wasm')), go.importObject);
go.run(instance);
await ready;

const posters = [[78, 33], [50, 30]].map(([cols, rows], id) => {
  const sim = new Sim(newWorld());
  sweetspot.create(id, cols, rows);
  sweetspot.step(id, JSON.stringify(Array.from({ length: 90 }, () => sim.step())));
  const cells = sweetspot.cells(id);
  if (cells.length !== cols * rows * 12) throw new Error('unexpected frame size');
  if (id === 0) {
    const u = new Uint32Array(cells.buffer);
    for (let y = 0; y < rows; y++) {
      let s = '';
      for (let x = 0; x < cols; x++) s += String.fromCodePoint(u[(y * cols + x) * 3] || 32);
      console.log(s.trimEnd());
    }
    console.log(JSON.parse(sweetspot.state(id)).score);
  }
  return { cols, rows, cells: Buffer.from(cells).toString('base64') };
});
fs.writeFileSync(path.join(out, 'posters.json'), JSON.stringify(posters));
process.exit(0);
