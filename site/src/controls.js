// The settings panel. It draws the controls, keeps them in step with the settings, and explains
// each change in one line.
import { CHANNELS, DEFAULTS, MAX_NEIGHBORS, START, crowd, isDFS, tuned, widest } from './sim.js';

const ONOFF = [[false, 'Off'], [true, 'On']];
const GROUPS = [
  ['Router', [
    ['band', 'Band', [['2.4', '2.4 GHz'], ['5', '5 GHz'], ['6', '6 GHz'], ['dual', 'Dual']]],
    ['width', 'Width', [[20, '20'], [40, '40'], [80, '80'], [160, '160 MHz']]],
    ['channel', 'Channel', 'select'],
    ['power', 'Power', { min: 10, max: 100, step: 5, show: (v) => `${v}%` }],
    ['sqm', 'Smart queues', ONOFF],
  ]],
  ['Building', [
    ['neighbors', 'Neighbors', { min: 0, max: MAX_NEIGHBORS, step: 1, show: (v) => `${v}` }],
    ['walls', 'Inner walls', [['wood', 'Wood'], ['brick', 'Brick'], ['concrete', 'Concrete']]],
    ['microwave', 'Microwave', ONOFF],
    ['airdrop', 'AirDrop', ONOFF],
  ]],
  ['Internet', [
    ['plan', 'Plan', [['100', '100/10'], ['500', '500/40'], ['1000', 'Gig fiber']]],
    ['traffic', 'At home', [['quiet', 'Quiet'], ['stream', 'Streaming'], ['backup', 'Backup']]],
    ['line', 'Cable signal', [['good', 'Good'], ['weak', 'Weak'], ['hot', 'Hot']]],
    ['dns', 'DNS', [['isp', 'ISP'], ['cf', '1.1.1.1'], ['quad9', '9.9.9.9']]],
  ]],
];

export const SETUPS = {
  start: { name: 'Start over', router: START.router, set: {}, say: 'Back to the start.' },
  tower: { name: 'Apartment tower', set: { band: 'dual', neighbors: 38, walls: 'concrete' }, say: 'Concrete walls and 38 networks. Where 5 GHz gets weak, the router moves you to 2.4 GHz.' },
  popcorn: { name: 'Popcorn', set: { band: '2.4', microwave: true }, say: 'A microwave leaks near 2.4 GHz. Switch the band to 5 GHz and it stops mattering.' },
  backup: { name: 'Backup night', set: { plan: '100', traffic: 'backup' }, say: 'A photo backup fills a slow upload. Turn on smart queues to fix the delay.' },
  coax: { name: 'Bad coax', set: { line: 'weak' }, say: 'Great Wi-Fi, bad cable. Advisor reads the modem page in tab 4.' },
  dream: { name: 'Dream setup', router: { x: 7.6, y: 3.1 }, set: { band: '6', w6: 160, sqm: true, neighbors: 2, plan: '1000', dns: 'cf' }, say: 'A central router on 6 GHz at 160 MHz, gig fiber, and two neighbors.' },
};

const bandOf = (set) => (set.band === 'dual' ? '5' : set.band);
const suffix = { 2.4: '24', 5: '5', 6: '6' };
const count = (n, one) => `${n} ${one}${n === 1 ? '' : 's'}`;

// Width and channel belong to the band in use.
const keyOf = (set, k) => (k === 'width' ? 'w' + suffix[bandOf(set)] : k === 'channel' ? 'ch' + suffix[bandOf(set)] : k);

function explain(world, k) {
  const { set } = world, band = bandOf(set), t = tuned(set, band), n = crowd(world, band).length;
  switch (k) {
    case 'band': return {
      2.4: '2.4 GHz goes through walls better, but it is slower and more crowded.', 5: '5 GHz is fast. Walls stop more of it.',
      6: '6 GHz is the cleanest band, with the shortest reach.', dual: 'One name for both bands. In the hatched area, the router moves you to 2.4 GHz.',
    }[set.band];
    case 'width': return t.width === 20 ? 'Narrow channels reach farther and meet fewer neighbors. The top speed is low.'
      : `${t.width} MHz is ${t.width / 20} times as wide as 20 MHz: faster up close, with ${3 * Math.log2(t.width / 20)} dB more noise. ${count(n, 'neighbor')} overlap${n === 1 ? 's' : ''}.`;
    case 'channel': {
      const want = set['w' + suffix[band]];
      const cut = want > t.width ? ` This block has no room for ${want} MHz, so the router uses ${t.width}.` : '';
      if (band === '2.4' && ![1, 6, 11].includes(t.ch)) return `Channel ${t.ch} overlaps its neighbors on both sides. Use 1, 6, or 11.`;
      return `Channel ${t.ch}: ${count(n, 'neighbor')} overlap${n === 1 ? 's' : ''}.${band === '5' && isDFS(t.ch) ? ' Radar shares this channel, so fewer routers use it.' : ''}${cut}`;
    }
    case 'power': return 'Each halving of the power costs 3 dB.';
    case 'sqm': return set.sqm ? 'Smart queues (SQM) keep the delay low under load, for a little speed.' : 'Without smart queues, a full upload queue delays everything else.';
    case 'neighbors': return `${count(set.neighbors, 'network')} nearby, and ${n} on your channel. ${n >= 6 ? 'Press s to find a cleaner one.' : 'Plenty of free air here.'}`;
    case 'walls': return { wood: 'Wood and drywall barely stop Wi-Fi.', brick: 'Brick takes a bite out of every room.', concrete: 'Concrete turns each room into its own island.' }[set.walls];
    case 'microwave': return !set.microwave ? 'Popcorn is ready.' : set.band === '2.4' ? 'The noise floor jumps near the kitchen.' : set.band === 'dual' ? 'This hurts only where you are on 2.4 GHz.' : `Microwaves leak near 2.4 GHz. Your ${set.band} GHz link does not notice.`;
    case 'airdrop': return !set.airdrop ? 'AirDrop is off.' : t.ch === 149 ? 'AirDrop meets on channel 149. You are already there, so nothing changes.' : 'macOS leaves your channel now and then to look for AirDrop peers. Watch the router chart.';
    case 'plan': return set.plan === '1000' ? 'Fiber: no cable modem, and a short queue.' : 'Cable: a slow upload and a long queue.';
    case 'traffic': return { quiet: 'Nobody else is online.', stream: 'Someone streams in 4K.', backup: set.sqm ? 'A backup fills the upload, and smart queues keep the delay low.' : 'A backup fills the upload. Watch the internet chart.' }[set.traffic];
    case 'line': return { good: 'A clean cable line.', weak: 'Weak signal on the coax: errors and lost packets. Open tab 4.', hot: 'Too much signal on the coax. Open tab 4 before you remove a splitter.' }[set.line];
    case 'dns': return set.dns === 'isp' ? 'The DNS server from your ISP.' : 'A faster DNS server. The tip in tab 4 goes away.';
  }
}

// Builds the panel in "root" and calls change(key, message) after every change.
export function buildPanel(root, setups, world, change) {
  const id = (k) => `set-${k}`;
  root.innerHTML = GROUPS.map(([title, rows]) => `<fieldset><legend>${title}</legend>${rows.map(([k, label, how]) => {
    if (how === 'select') return `<div class="row"><label for="${id(k)}">${label}</label><select id="${id(k)}" data-set="${k}"></select></div>`;
    if (!Array.isArray(how)) return `<div class="row"><label for="${id(k)}">${label}</label><span class="slide"><input type="range" id="${id(k)}" data-set="${k}" min="${how.min}" max="${how.max}" step="${how.step}"><output for="${id(k)}"></output></span></div>`;
    return `<div class="row" role="radiogroup" aria-labelledby="${id(k)}"><span id="${id(k)}">${label}</span><span class="seg">${how.map(([v, text]) => `<label><input type="radio" name="${k}" value="${v}" data-set="${k}"><span>${text}</span></label>`).join('')}</span></div>`;
  }).join('')}</fieldset>`).join('');
  setups.innerHTML = '<span>Try a setup</span>' + Object.entries(SETUPS).map(([k, s]) => `<button type="button" data-setup="${k}">${s.name}</button>`).join('');

  const formats = Object.fromEntries(GROUPS.flatMap(([, rows]) => rows).filter((r) => r[2].show).map((r) => [r[0], r[2].show]));
  let listed = '';
  function sync() {
    const { set } = world, band = bandOf(set), select = root.querySelector('[data-set="channel"]');
    if (listed !== band) {
      listed = band;
      select.innerHTML = CHANNELS[band].map((c) => `<option value="${c}">${c}${band === '5' && isDFS(c) ? ' (DFS)' : ''}</option>`).join('');
    }
    for (const el of root.querySelectorAll('[data-set]')) {
      const k = el.dataset.set, v = k === 'width' ? tuned(set, band).width : set[keyOf(set, k)];
      if (el.type === 'radio') el.checked = el.value === String(v);
      else el.value = v;
      if (el.type === 'range') el.nextElementSibling.textContent = formats[k](v);
      if (k === 'width') el.disabled = Number(el.value) > widest(band, tuned(set, band).ch);
      if (k === 'line') el.disabled = set.plan === '1000';
    }
  }
  root.addEventListener('input', (e) => {
    const k = e.target.dataset.set;
    if (!k) return;
    const raw = e.target.value;
    const kind = typeof DEFAULTS[keyOf(world.set, k)];
    world.set[keyOf(world.set, k)] = kind === 'number' ? Number(raw) : kind === 'boolean' ? raw === 'true' : raw;
    sync();
    change(k, explain(world, k));
  });
  setups.addEventListener('click', (e) => {
    const s = SETUPS[e.target.dataset.setup];
    if (!s) return;
    Object.assign(world.set, DEFAULTS, s.set);
    if (s.router) Object.assign(world.router, s.router);
    sync();
    change('setup', s.say);
  });
  sync();
}
