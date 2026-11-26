// Runs the Go program off the main thread and returns painted cells.
importScripts('wasm_exec.js');

const go = new Go();
const queue = [];
const size = {};
let ready = false;

self.sweetspotReady = () => {
  ready = true;
  for (const m of queue.splice(0)) handle(m);
};

function handle(m) {
  const s = self.sweetspot;
  if (m.cols) size[m.id] = { cols: m.cols, rows: m.rows };
  switch (m.op) {
    case 'create': s.create(m.id, m.cols, m.rows); break;
    case 'resize': s.resize(m.id, m.cols, m.rows); break;
    case 'key': s.key(m.id, m.key); break;
    case 'pin': s.pin(m.id, m.name); break;
    case 'step': s.step(m.id, JSON.stringify(m.samples)); break;
  }
  if (m.draw) {
    const bytes = s.cells(m.id);
    postMessage({ id: m.id, ...size[m.id], cells: bytes.buffer, state: JSON.parse(s.state(m.id)) }, [bytes.buffer]);
  }
}

self.onmessage = (e) => (ready ? handle(e.data) : queue.push(e.data));

(async () => {
  const res = await fetch('sweetspot.wasm.gz');
  if (!res.ok) throw new Error(`sweetspot.wasm.gz: HTTP ${res.status}`);
  let buf = await res.arrayBuffer();
  const head = new Uint8Array(buf, 0, 2);
  // A host that sets Content-Encoding: gzip has already inflated the file.
  if (head[0] === 0x1f && head[1] === 0x8b) {
    buf = await new Response(new Blob([buf]).stream().pipeThrough(new DecompressionStream('gzip'))).arrayBuffer();
  }
  const { instance } = await WebAssembly.instantiate(buf, go.importObject);
  go.run(instance);
})().catch((err) => postMessage({ error: String(err) }));
