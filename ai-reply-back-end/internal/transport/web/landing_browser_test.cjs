const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const script = fs.readFileSync(path.join(__dirname, 'static/landing.js'), 'utf8');
const names = ['incoming', 'instruction', 'reply'];
const flush = () => new Promise(resolve => setImmediate(resolve));
function scenesFor(locale) {
  const words = JSON.parse(fs.readFileSync(path.join(__dirname, '../../localization/locales', locale + '.json'), 'utf8'));
  return ['hero.demo', 'hero.demo2', 'hero.demo3'].map(prefix => Object.fromEntries(names.map(name => [name, words[prefix + '_' + name]])));
}

// The DOM fixture models wrapping to exercise reservation and resize scheduling.
// Device font metrics, viewport overflow and visual appearance are checked in
// a real browser separately; this does not pretend to be a layout engine.
function fixture(locale = 'en', { width = 262, reduced = false, resizeObserver = true } = {}) {
  const scenes = scenesFor(locale), timers = new Map(), fieldNodes = {}, rows = {}, motionListeners = [];
  let now = 0, timerID = 0, observer, probes = 0, fontScale = 1;
  const resizeListeners = [], fontReady = [], fontListeners = [];
  class Element {
    constructor(text = '') {
      this.textContent = text; this.style = {}; this.children = []; this.listeners = {};
      const classes = new Set();
      this.classList = { add: (...values) => values.forEach(value => classes.add(value)),
        remove: (...values) => values.forEach(value => classes.delete(value)),
        toggle(value, on) { if (on) classes.add(value); else classes.delete(value); }, contains: value => classes.has(value) };
    }
    setAttribute() {} removeAttribute() {}
    addEventListener(name, fn) { this.listeners[name] = fn; }
    appendChild(child) { child.parentNode = this; this.children.push(child); }
    remove() { this.parentNode.children = this.parentNode.children.filter(child => child !== this); }
    cloneNode() { probes += 1; return new Element(); }
    getBoundingClientRect() {
      const actualWidth = parseFloat(this.style.width) || width;
      const cursor = this.classList.contains('is-typing') ? 5 : 0;
      const lines = Math.max(1, Math.ceil((this.textContent.length * 7.4 * fontScale + cursor) / (actualWidth - 28)));
      return { width: actualWidth, height: Math.max(parseFloat(this.style.minHeight) || 0, lines * 21.75 * fontScale + 24) };
    }
  }
  names.forEach((name, i) => {
    const row = rows[name] = new Element(); row.classList.add('is-on');
    const field = fieldNodes[name] = new Element(scenes[0][name]); row.appendChild(field);
    row.stage = i + 1;
  });
  const dots = new Element(), screen = new Element();
  const root = { querySelector(selector) {
    if (selector === '[data-dots]') return dots;
    if (selector === '.phone-screen') return screen;
    for (const name of names) {
      if (selector === '[data-field="' + name + '"]') return fieldNodes[name];
      if (selector === '[data-stage="' + rows[name].stage + '"]') return rows[name];
    }
    return null;
  } };
  const motion = { matches: reduced, addEventListener: (_, fn) => motionListeners.push(fn) };
  const document = { documentElement: new Element(), getElementById: id => id === 'hero-demo' ? root : id === 'landing-data' ? { textContent: JSON.stringify({ scenes }) } : null,
    createElement: () => new Element(), querySelectorAll: () => [], querySelector: () => null,
    fonts: { ready: { then: fn => fontReady.push(fn) }, addEventListener: (_, fn) => fontListeners.push(fn) } };
  class ResizeObserver { constructor(fn) { observer = fn; } observe(node) { assert.equal(node, screen); } }
  const window = { matchMedia: () => motion, addEventListener: (_, fn) => resizeListeners.push(fn) };
  if (resizeObserver) window.ResizeObserver = ResizeObserver;
  vm.runInNewContext(script, { document, window, ResizeObserver,
    setTimeout(fn, ms) { const id = ++timerID; timers.set(id, { fn, at: now + ms }); return id; },
    clearTimeout: id => timers.delete(id) });
  async function advance(ms, check = () => {}) {
    const end = now + ms;
    for (;;) {
      const next = [...timers].filter(([, timer]) => timer.at <= end).sort((a, b) => a[1].at - b[1].at)[0];
      if (!next) break;
      now = next[1].at; timers.delete(next[0]); next[1].fn(); await flush(); check();
    }
    now = end;
  }
  return { fields: fieldNodes, rows, dots, scenes, timers, advance,
    get probes() { return probes; }, resize(nextWidth) { width = nextWidth; (observer || resizeListeners[0])(); },
    notifyResize() { observer(); }, loadFonts(scale) { fontScale = scale; fontReady.forEach(fn => fn()); },
    changeMotion(matches) { motion.matches = matches; motionListeners.forEach(fn => fn({ matches })); } };
}

for (const locale of ['en', 'ru', 'kk', 'uz']) {
  test(locale + ': typing and scene switches keep every bubble within its reserved height', async () => {
    const f = fixture(locale, { width: 234 });
    const heights = names.map(name => f.fields[name].style.minHeight);
    names.forEach(name => {
      assert.equal(f.fields[name].textContent, f.scenes[0][name], 'initial server-rendered scene stays visible');
      assert.equal(f.rows[name].children.length, 1, 'measurement probes are removed');
    });
    assert.equal(f.dots.children.length, 3);
    let checked = 0;
    await f.advance(45000, () => {
      checked += 1;
      names.forEach((name, i) => {
        assert.equal(f.fields[name].style.minHeight, heights[i]);
        assert.equal(f.fields[name].getBoundingClientRect().height, parseFloat(heights[i]), name + ' grew during typing');
      });
    });
    assert.ok(checked > 200, 'the test crosses full typing runs and automatic scene switches');
  });
}

test('resize remeasures only width changes without resetting the current conversation', async () => {
  const f = fixture('kk');
  await f.advance(4000);
  const before = names.map(name => f.fields[name].textContent), oldHeight = f.fields.reply.style.minHeight;
  const probes = f.probes, pending = [...f.timers.keys()];
  f.notifyResize(); f.notifyResize();
  assert.equal(f.probes, probes, 'height-only ResizeObserver callbacks cannot make a measurement loop');
  f.resize(200);
  assert.equal(f.probes, probes + 3);
  assert.ok(parseFloat(f.fields.reply.style.minHeight) > parseFloat(oldHeight));
  assert.deepEqual(names.map(name => f.fields[name].textContent), before);
  assert.deepEqual([...f.timers.keys()], pending);
  f.notifyResize();
  assert.equal(f.probes, probes + 3);
  f.resize(270);
  assert.ok(parseFloat(f.fields.reply.style.minHeight) <= parseFloat(oldHeight), 'wider layouts can release unused space');
});

test('font readiness and resize fallback update reservations without losing visible text', () => {
  const f = fixture('ru', { resizeObserver: false });
  const before = names.map(name => f.fields[name].textContent), height = parseFloat(f.fields.reply.style.minHeight);
  f.loadFonts(1.2);
  assert.ok(parseFloat(f.fields.reply.style.minHeight) > height);
  f.resize(220);
  assert.deepEqual(names.map(name => f.fields[name].textContent), before);
});

test('reduced motion shows complete scenes, cancels a running animation and can resume', async () => {
  const f = fixture('uz', { reduced: true });
  assert.equal(f.timers.size, 0);
  f.dots.children[2].listeners.click();
  names.forEach(name => assert.equal(f.fields[name].textContent, f.scenes[2][name]));
  f.changeMotion(false);
  assert.equal(f.timers.size, 1);
  await f.advance(4000);
  f.changeMotion(true);
  assert.equal(f.timers.size, 0);
  names.forEach(name => {
    assert.equal(f.fields[name].textContent, f.scenes[0][name]);
    assert.equal(f.fields[name].classList.contains('is-typing'), false);
  });
});
