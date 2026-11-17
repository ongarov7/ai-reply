const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const {
  createCabinet, contextUnits, contextCost, chars, normalizeText, normalizeDocument, downsample, encodeWAV,
  voiceLanguages, sectionFromHash, niceTicks, requestID
} = require('./static/cabinet.js');

const RECIPIENTS = ['friend', 'client', 'colleague', 'partner'];
const SECTIONS = ['overview', 'profile', 'recipients', 'billing', 'apps'];
const flush = () => new Promise(resolve => setImmediate(resolve));
async function settle(rounds = 8) { for (let i = 0; i < rounds; i++) await flush(); }

/* ------------------------------------------------------------ a small DOM */

function makeEvent(type, extra = {}) {
  return Object.assign({
    type, defaultPrevented: false, stopped: false, immediate: false,
    preventDefault() { this.defaultPrevented = true; },
    stopPropagation() { this.stopped = true; },
    stopImmediatePropagation() { this.stopped = true; this.immediate = true; }
  }, extra);
}
function callListeners(list, event, capture, self) {
  for (const entry of (list || []).slice()) {
    if (entry.capture !== capture) continue;
    entry.fn.call(self, event);
    if (event.immediate) break;
  }
}
class FakeText {
  constructor(text) { this.textContent = String(text); this.parentNode = null; }
  remove() { if (this.parentNode) this.parentNode.childNodes = this.parentNode.childNodes.filter(child => child !== this); this.parentNode = null; }
}
class FakeElement {
  constructor(document, tag, ns) {
    this.ownerDocument = document; this.localName = String(tag).toLowerCase(); this.namespaceURI = ns || 'http://www.w3.org/1999/xhtml';
    this.childNodes = []; this.parentNode = null; this.attributes = new Map(); this.listeners = {}; this.style = {};
    this._text = ''; this.hidden = false; this.disabled = false; this.checked = false; this.value = ''; this.className = '';
    this.clientWidth = 0; this.open = false;
    const element = this;
    this.classList = {
      contains: name => element.className.split(/\s+/).includes(name),
      add(...names) { names.forEach(name => { if (!this.contains(name)) element.className = (element.className + ' ' + name).trim(); }); },
      remove(...names) { element.className = element.className.split(/\s+/).filter(name => !names.includes(name)).join(' '); },
      toggle(name, force) { const on = force === undefined ? !this.contains(name) : !!force; if (on) this.add(name); else this.remove(name); return on; }
    };
    document.all.push(this);
  }
  get id() { return this.attributes.get('id') || ''; }
  set id(value) { this.setAttribute('id', value); }
  get children() { return this.childNodes.filter(child => child instanceof FakeElement); }
  get firstChild() { return this.childNodes[0] || null; }
  get textContent() { return this.childNodes.length ? this.childNodes.map(child => child.textContent).join('') : this._text; }
  set textContent(value) { this.childNodes.forEach(child => { child.parentNode = null; }); this.childNodes = []; this._text = String(value); }
  setAttribute(name, value) {
    value = String(value);
    if (name === 'class') { this.className = value; return; }
    this.attributes.set(name, value);
    if (name === 'id') this.ownerDocument.ids.set(value, this);
    if (name === 'hidden') this.hidden = true;
  }
  getAttribute(name) {
    if (name === 'class') return this.className || null;
    return this.attributes.has(name) ? this.attributes.get(name) : null;
  }
  hasAttribute(name) { return this.attributes.has(name); }
  removeAttribute(name) { this.attributes.delete(name); if (name === 'hidden') this.hidden = false; }
  appendChild(child) { if (child.remove) child.remove(); child.parentNode = this; this.childNodes.push(child); this._text = ''; return child; }
  append(...children) { children.forEach(child => this.appendChild(typeof child === 'string' ? new FakeText(child) : child)); }
  replaceChildren(...children) { this.childNodes.forEach(child => { child.parentNode = null; }); this.childNodes = []; this._text = ''; this.append(...children); }
  remove() { if (this.parentNode) this.parentNode.childNodes = this.parentNode.childNodes.filter(child => child !== this); this.parentNode = null; }
  contains(node) { for (let current = node; current; current = current.parentNode) if (current === this) return true; return false; }
  matches(selector) {
    if (selector[0] === '#') return this.id === selector.slice(1);
    if (selector[0] === '.') return this.classList.contains(selector.slice(1));
    const attribute = /^\[([\w-]+)(?:="([^"]*)")?\]$/.exec(selector);
    if (attribute) return this.attributes.has(attribute[1]) && (attribute[2] === undefined || this.attributes.get(attribute[1]) === attribute[2]);
    return this.localName === selector.toLowerCase();
  }
  descendants() { return this.children.flatMap(child => [child, ...child.descendants()]); }
  querySelectorAll(selector) { return this.descendants().filter(node => node.matches(selector)); }
  querySelector(selector) { return this.querySelectorAll(selector)[0] || null; }
  addEventListener(type, fn, options) { (this.listeners[type] = this.listeners[type] || []).push({ fn, capture: options === true || !!(options && options.capture) }); }
  removeEventListener(type, fn) { this.listeners[type] = (this.listeners[type] || []).filter(entry => entry.fn !== fn); }
  dispatchEvent(event) {
    event.target = event.target || this;
    const ancestors = [];
    for (let node = this.parentNode; node; node = node.parentNode) ancestors.unshift(node);
    const documentListeners = this.ownerDocument.listeners[event.type];
    callListeners(documentListeners, event, true, this.ownerDocument);
    for (const node of ancestors) { if (event.stopped) break; callListeners(node.listeners[event.type], event, true, node); }
    if (!event.stopped) { callListeners(this.listeners[event.type], event, true, this); callListeners(this.listeners[event.type], event, false, this); }
    for (const node of ancestors.reverse()) { if (event.stopped) break; callListeners(node.listeners[event.type], event, false, node); }
    if (!event.stopped) callListeners(documentListeners, event, false, this.ownerDocument);
    return !event.defaultPrevented;
  }
  click() { return this.dispatchEvent(makeEvent('click')); }
  focus() { this.ownerDocument.activeElement = this; }
  scrollIntoView() {}
  getBoundingClientRect() { return { left: 0, top: 20, width: this.clientWidth || 640, height: 220 }; }
  setSelectionRange(start, end) { this.selectionStart = start; this.selectionEnd = end; }
  showModal() { this.open = true; }
  close() { this.open = false; }
}
class FakeDocument {
  constructor() { this.ids = new Map(); this.all = []; this.listeners = {}; this.title = 'Личный кабинет — AI Reply'; this.hidden = false; this.activeElement = null; }
  // Like the account tests, an id the fixture did not build still resolves.
  getElementById(id) { if (!this.ids.has(id)) this.createElement('div').id = id; return this.ids.get(id); }
  createElement(tag) { return new FakeElement(this, tag); }
  createElementNS(ns, tag) { return new FakeElement(this, tag, ns); }
  querySelectorAll(selector) { return this.all.filter(node => node.matches(selector)); }
  addEventListener(type, fn, options) { (this.listeners[type] = this.listeners[type] || []).push({ fn, capture: options === true || !!(options && options.capture) }); }
  dispatchEvent(event) { callListeners(this.listeners[event.type], event, true, this); callListeners(this.listeners[event.type], event, false, this); return true; }
}

function build(document) {
  const el = (tag, attributes = {}, parent) => {
    const node = document.createElement(tag);
    Object.entries(attributes).forEach(([name, value]) => node.setAttribute(name, value));
    if (parent) parent.appendChild(node);
    return node;
  };
  const app = el('main', { id: 'account-app' });
  const nav = el('nav', {}, app);
  SECTIONS.forEach(name => {
    const item = el('li', name === 'profile' || name === 'recipients' ? { 'data-feature': 'ai_context', hidden: '' } : {}, nav);
    el('a', { href: '#' + name, 'data-section-link': name }, item);
    const panel = el('section', { id: 'cabinet-' + name, 'data-section': name }, app);
    el('h2', { id: 'cabinet-' + name + '-title', tabindex: '-1' }, panel);
  });
  el('a', { 'data-feature': 'ai_context', hidden: '' }, app);
  el('a', { 'data-header-sign-in': '' });
  el('strong', { 'data-cabinet-email': '' }, app);
  el('button', { id: 'account-logout' }, app);
  const chart = el('div', { id: 'cabinet-chart' }, document.getElementById('cabinet-overview'));
  chart.clientWidth = 720;
  const mics = ['cabinet-about', 'cabinet-audience'].concat(RECIPIENTS.map(key => 'cabinet-notes-' + key));
  mics.forEach(id => {
    const panel = document.getElementById(id.indexOf('notes') !== -1 ? 'cabinet-recipients' : 'cabinet-profile');
    el('textarea', { id }, panel);
    const button = el('button', { 'data-mic-for': id, hidden: '' }, panel);
    el('span', { class: 'cabinet-mic-label' }, button);
    el('span', { class: 'cabinet-mic-timer', hidden: '' }, button);
    el('span', { id: id + '-voice' }, panel);
  });
  el('input', { id: 'cabinet-occupation' }, document.getElementById('cabinet-profile'));
  const dialog = el('dialog', { id: 'cabinet-consent' }, app);
  el('a', { id: 'cabinet-consent-terms' }, dialog).href = 'https://example.test/offer?lang=ru';
  el('a', { id: 'cabinet-consent-privacy' }, dialog).href = 'https://example.test/privacy?lang=ru';
  el('button', { id: 'cabinet-consent-accept' }, dialog);
  el('button', { id: 'cabinet-consent-cancel' }, dialog);
}

function words(locale) {
  const all = JSON.parse(fs.readFileSync(path.join(__dirname, '../../localization/locales', locale + '.json'), 'utf8'));
  return Object.fromEntries(Object.entries(all).filter(([key]) => key.startsWith('account.')).map(([key, value]) => [key.slice(8), value]));
}
const respond = (payload, status = 200) => Promise.resolve({ ok: status < 400, status, json: () => Promise.resolve(structuredClone(payload)) });
function routes(table) {
  return (url, opts) => {
    const key = ((opts && opts.method) || 'GET') + ' ' + url.replace(/\?.*$/, '');
    if (!table[key]) return respond({ error: { code: 'NOT_FOUND' } }, 404);
    return table[key](url, opts);
  };
}
function session({ id = 'fixture-user-A', aiContext = true, voice = true, unlimited = false, consentRequired = false } = {}) {
  return {
    user: { id, email: id + '@example.test' },
    usage: { remaining_total: 42, unlimited, purchased_credits: 30 },
    subscription: { status: 'active', plan: { code: 'free', name: { ru: 'Бесплатный', en: 'Free' } }, expires_at: '2026-12-17T00:00:00Z', auto_renews: true },
    bank: { enabled: false, environment: 'production', products: [] },
    features: { ai_context: aiContext, voice_transcription: voice },
    legal: { consent_required: consentRequired, terms_version: '2026-10-08', privacy_version: '2026-10-14',
      terms_url: 'https://example.test/offer?lang=ru', privacy_url: 'https://example.test/privacy?lang=ru' }
  };
}
function contextResponse({ revision = 3, doc = {}, pricing = {} } = {}) {
  return {
    context: Object.assign({ enabled: true, roles: ['seller'], occupation: 'Продаю детскую одежду', traits: ['brief'], about: '', audience: '',
      recipients: { friend: { notes: '' }, client: { notes: 'Всегда на «Вы»' }, colleague: { notes: '' }, partner: { notes: '' } } }, doc),
    revision, updated_at: '2026-11-17T10:00:00Z',
    limits: { roles: 5, traits: 6, occupation: 120, about: 2000, audience: 300, notes: 1000 },
    catalog: { roles: ['seller', 'business_owner', 'blogger'], traits: ['brief', 'detailed', 'no_emoji'], recipients: RECIPIENTS },
    pricing: Object.assign({ chars_per_reply: 400, free_chars: 0, max_extra_replies: 2, unlimited: false }, pricing),
    // 21 characters of occupation already add one unit at free_chars = 0.
    cost: { base_chars: 21, general: 2, friend: 2, client: 2, colleague: 2, partner: 2 }
  };
}
function usageResponse() {
  const days = Array.from({ length: 30 }, (_, i) => ({ date: '2026-10-' + String(19 + i).padStart(2, '0'), replies: i % 3, units: i % 3 === 2 ? 4 : i % 3 }))
    .map((day, i) => i >= 13 ? Object.assign(day, { date: '2026-11-' + String(i - 12).padStart(2, '0') }) : day);
  days[29] = { date: '2026-11-17', replies: 5, units: 7 };
  return { days, platforms: [{ platform: 'web', replies: 2 }, { platform: 'ios', replies: 12 }, { platform: 'quantum', replies: 1 }],
    total_replies: 40, total_units: 52, timezone: 'Asia/Almaty' };
}
function recorderFactory(samples = new Float32Array(48000).fill(0.25), sampleRate = 48000) {
  const calls = [];
  return { calls, create: () => ({
    start() { calls.push('start'); return Promise.resolve(); },
    stop() { calls.push('stop'); return Promise.resolve({ samples, sampleRate }); },
    cancel() { calls.push('cancel'); }
  }) };
}

function fixture({ locale = 'ru', handler = routes({}), hash = '', payment = false, recorder = recorderFactory(), media = true, confirm = true } = {}) {
  const document = new FakeDocument();
  build(document);
  const requests = [], timers = new Map(), windowListeners = {}, signals = [];
  let now = 1000000, timerID = 0;
  const location = { origin: 'https://example.test', href: 'https://example.test/account', value: hash,
    get hash() { return this.value; }, set hash(next) { next = String(next); this.value = next && next[0] !== '#' ? '#' + next : next; } };
  const browser = {
    fetch(url, opts) { requests.push({ url, opts }); return handler(url, opts); },
    location, navigator: media ? { mediaDevices: { getUserMedia() {} } } : {}, AudioContext: media ? function AudioContext() {} : undefined,
    FormData, Blob, CustomEvent, crypto: { getRandomValues(bytes) { bytes.forEach((_, i) => { bytes[i] = (i * 37 + 11) & 255; }); return bytes; } },
    now: () => now, setTimeout(fn, ms) { const id = ++timerID; timers.set(id, { fn, at: now + ms }); return id; }, clearTimeout(id) { timers.delete(id); },
    addEventListener(type, fn) { (windowListeners[type] = windowListeners[type] || []).push(fn); },
    matchMedia: () => ({ matches: true }), confirm: () => confirm, createRecorder: () => recorder.create()
  };
  document.addEventListener('aireply:unauthorized', () => signals.push('unauthorized'));
  const cabinet = createCabinet(document, { locale, base_path: '', messages: words(locale), payment_page: payment, capture_url: '/static/cabinet-capture.js?v=test' }, browser);
  cabinet.init();
  const node = id => document.getElementById(id);
  return {
    cabinet, document, browser, requests, signals, recorder, words: words(locale), node,
    async signIn(payload = session()) { document.dispatchEvent(new CustomEvent('aireply:session', { detail: payload })); await settle(); },
    async signOut() { document.dispatchEvent(new CustomEvent('aireply:signedout')); await settle(); },
    navigate(next) { location.hash = next; (windowListeners.hashchange || []).forEach(fn => fn({})); },
    fireWindow(type, event) { return (windowListeners[type] || []).map(fn => fn(event)); },
    type(id, value) { const field = node(id); field.value = value; field.dispatchEvent(makeEvent('input')); },
    mic(id) { return document.querySelectorAll('[data-mic-for]').find(button => button.getAttribute('data-mic-for') === id); },
    visible(name) { return SECTIONS.filter(section => !node('cabinet-' + section).hidden).join(','); },
    posts(suffix) { return requests.filter(request => request.opts.method === 'POST' && request.url.endsWith(suffix)); },
    async advance(ms) {
      const end = now + ms;
      for (;;) {
        const next = [...timers].filter(([, timer]) => timer.at <= end).sort((a, b) => a[1].at - b[1].at)[0];
        if (!next) break;
        now = next[1].at; timers.delete(next[0]); next[1].fn(); await settle(2);
      }
      now = end; await settle();
    }
  };
}
const standard = (extra = {}) => routes(Object.assign({
  'GET /account/api/ai-context': () => respond(contextResponse()),
  'GET /account/api/usage': () => respond(usageResponse())
}, extra));

/* ------------------------------------------------------------ pure helpers */

test('cost follows the contract formula: unlimited, disabled, zero rate, cap, free characters and code points', () => {
  const pricing = { chars_per_reply: 400, free_chars: 0, max_extra_replies: 2, unlimited: false };
  const doc = text => ({ enabled: true, occupation: '', about: text, audience: '', roles: ['seller', 'blogger'], traits: ['brief'], recipients: {} });
  assert.equal(contextUnits(doc(''), pricing, ''), 1);
  assert.equal(contextUnits(doc('a'), pricing, ''), 2);
  assert.equal(contextUnits(doc('a'.repeat(400)), pricing, ''), 2);
  assert.equal(contextUnits(doc('a'.repeat(401)), pricing, ''), 3);
  assert.equal(contextUnits(doc('a'.repeat(5000)), pricing, ''), 3, 'the surcharge is capped at max_extra_replies');
  assert.equal(contextUnits(doc('a'.repeat(450)), Object.assign({}, pricing, { unlimited: true }), ''), 1, 'Pro pays no surcharge');
  assert.equal(contextUnits(Object.assign(doc('a'.repeat(450)), { enabled: false }), pricing, ''), 1, 'a disabled profile costs nothing');
  assert.equal(contextUnits(doc('a'.repeat(450)), Object.assign({}, pricing, { chars_per_reply: 0 }), ''), 1, 'zero chars per reply disables the surcharge');
  assert.equal(contextUnits(doc('a'.repeat(450)), Object.assign({}, pricing, { free_chars: 100 }), ''), 2, 'free characters are subtracted first');
  assert.equal(contextUnits(doc('a'.repeat(450)), Object.assign({}, pricing, { free_chars: 500 }), ''), 1);
  assert.equal(contextUnits(doc('a'.repeat(450)), Object.assign({}, pricing, { max_extra_replies: 0 }), ''), 1);
  assert.equal(contextUnits(doc('a'.repeat(399) + '😀'), pricing, ''), 2, 'an emoji is one code point');
  assert.equal(contextUnits(doc('a'.repeat(399) + '😀😀'), pricing, ''), 3);
  assert.equal(chars('😀'), 1);
  assert.equal(chars('  привет\r\n '), 6, 'text is trimmed and \\r removed before counting');
  assert.equal(normalizeText('a\u0000b\r\nc\td\u0007'), 'ab\nc\td');
  // Recipient notes add to the base text; the roles and traits never count.
  const shared = { enabled: true, occupation: 'o'.repeat(100), about: 'a'.repeat(250), audience: 'u'.repeat(50), roles: ['seller'], traits: ['warm'],
    recipients: { client: { notes: 'n'.repeat(200) } } };
  assert.deepEqual(contextCost(shared, pricing), { general: 2, friend: 2, client: 3, colleague: 2, partner: 2 });
  // The spec's example: 450 base characters at the defaults → 3 units.
  assert.equal(contextUnits({ enabled: true, about: 'x'.repeat(450) }, pricing, ''), 3);
});

test('documents are normalized to every field in a fixed order', () => {
  assert.deepEqual(normalizeDocument({ roles: ['seller', 'seller', 7], about: ' hi\r\n', recipients: { client: { notes: ' x ' } } }), {
    enabled: true, roles: ['seller'], occupation: '', traits: [], about: 'hi', audience: '',
    recipients: { friend: { notes: '' }, client: { notes: 'x' }, colleague: { notes: '' }, partner: { notes: '' } }
  });
  assert.equal(normalizeDocument({ enabled: false }).enabled, false);
});

test('helpers: section routing, spoken languages, request ids and axis ticks', () => {
  assert.equal(sectionFromHash('#profile', SECTIONS, 'overview'), 'profile');
  assert.equal(sectionFromHash('#handoff=AAAA', SECTIONS, 'overview'), 'overview');
  assert.equal(sectionFromHash('', SECTIONS, 'billing'), 'billing');
  assert.equal(sectionFromHash('#profile', ['overview', 'billing', 'apps'], 'overview'), 'overview');
  assert.deepEqual(['ru', 'kk', 'en', 'uz'].map(voiceLanguages), ['ru,kk,en', 'kk,ru,en', 'en,ru,kk', 'uz,ru,en']);
  const id = requestID({ getRandomValues(bytes) { bytes.fill(171); return bytes; } });
  assert.match(id, /^[0-9a-f]{32}$/);
  assert.throws(() => requestID(undefined));
  assert.deepEqual(niceTicks(0), [0, 1]);
  assert.deepEqual(niceTicks(1), [0, 1]);
  assert.deepEqual(niceTicks(3), [0, 1, 2, 3]);
  assert.deepEqual(niceTicks(7), [0, 2, 4, 6, 8]);
  assert.deepEqual(niceTicks(45), [0, 20, 40, 60]);
});

test('WAV encoder writes a canonical 16 kHz mono 16-bit header and clamps samples', () => {
  const samples = Float32Array.from([0, 1, -1, 0.5, 2, -3, NaN]);
  const buffer = encodeWAV(samples, 16000);
  const view = new DataView(buffer);
  const ascii = (offset, length) => String.fromCharCode(...new Uint8Array(buffer, offset, length));
  assert.equal(buffer.byteLength, 44 + samples.length * 2);
  assert.equal(ascii(0, 4), 'RIFF');
  assert.equal(view.getUint32(4, true), 36 + samples.length * 2);
  assert.equal(ascii(8, 4), 'WAVE');
  assert.equal(ascii(12, 4), 'fmt ');
  assert.equal(view.getUint32(16, true), 16);
  assert.equal(view.getUint16(20, true), 1, 'PCM');
  assert.equal(view.getUint16(22, true), 1, 'mono');
  assert.equal(view.getUint32(24, true), 16000);
  assert.equal(view.getUint32(28, true), 32000, 'byte rate = sample rate × block align');
  assert.equal(view.getUint16(32, true), 2);
  assert.equal(view.getUint16(34, true), 16);
  assert.equal(ascii(36, 4), 'data');
  assert.equal(view.getUint32(40, true), samples.length * 2);
  assert.deepEqual(Array.from({ length: samples.length }, (_, i) => view.getInt16(44 + i * 2, true)), [0, 32767, -32768, 16384, 32767, -32768, 0]);
});

test('downsampling to 16 kHz averages blocks and keeps the duration', () => {
  const second = new Float32Array(48000).map((_, i) => i % 3 === 0 ? 0.3 : 0);
  const out = downsample(second, 48000, 16000);
  assert.equal(out.length, 16000);
  assert.ok(Math.abs(out[0] - 0.1) < 1e-6);
  assert.equal(downsample(new Float32Array(44100), 44100, 16000).length, 16000);
  assert.equal(downsample(Float32Array.from([0, 1]), 8000, 16000).length, 4, 'a slower microphone is interpolated up');
  assert.equal(downsample(null, 48000, 16000).length, 0);
});

test('the script never writes HTML from strings', () => {
  for (const file of ['static/cabinet.js', 'static/cabinet-capture.js']) {
    const source = fs.readFileSync(path.join(__dirname, file), 'utf8');
    assert.doesNotMatch(source, /innerHTML|outerHTML|insertAdjacentHTML|document\.write|\beval\(|new Function/, file);
  }
});

/* ------------------------------------------------------------ navigation */

test('hash routing shows one section, follows features and never trusts other fragments', async () => {
  const f = fixture({ handler: standard(), hash: '#handoff=AAAA' });
  assert.equal(f.visible(), 'overview');
  await f.signIn(session({ aiContext: false }));
  f.navigate('#profile');
  assert.equal(f.visible(), 'overview', 'the AI profile is hidden while the server does not offer it');
  assert.equal(f.document.querySelectorAll('[data-feature]').every(node => node.hidden), true);
  assert.equal(f.requests.some(request => request.url.includes('/ai-context')), false, 'no AI context call without the feature');
  await f.signIn(session());
  assert.equal(f.visible(), 'profile', 'the requested section opens once the feature is known');
  assert.equal(f.document.querySelectorAll('[data-feature]').every(node => !node.hidden), true);
  f.navigate('#apps');
  assert.equal(f.visible(), 'apps');
  const current = f.document.querySelectorAll('[data-section-link]').filter(link => link.getAttribute('aria-current') === 'page');
  assert.deepEqual(current.map(link => link.getAttribute('data-section-link')), ['apps']);
  assert.equal(f.document.activeElement, f.node('cabinet-apps-title'), 'focus moves to the section heading');
  assert.match(f.document.title, /^Приложения — /);
  f.navigate('#account-login');
  assert.equal(f.visible(), 'overview');
  assert.equal(f.document.querySelectorAll('[data-header-sign-in]')[0].hidden, true, 'no «Sign in» button while signed in');
  await f.signOut();
  assert.equal(f.document.querySelectorAll('[data-header-sign-in]')[0].hidden, false);
  assert.equal(f.document.title, 'Личный кабинет — AI Reply');
});

test('/payment opens the balance section by default', async () => {
  const f = fixture({ handler: standard(), payment: true });
  await f.signIn();
  assert.equal(f.visible(), 'billing');
  f.navigate('#overview');
  assert.equal(f.visible(), 'overview');
});

/* ------------------------------------------------------------ overview */

test('overview tiles, the usage chart, its table and the platforms come from the session and usage history', async () => {
  const f = fixture({ handler: standard() });
  await f.signIn();
  assert.equal(f.requests.find(request => request.url.includes('/usage')).url, '/account/api/usage?days=30');
  assert.equal(f.node('cabinet-tile-available').textContent, '42');
  assert.equal(f.node('cabinet-tile-plan').textContent, 'Бесплатный');
  assert.match(f.node('cabinet-tile-plan-note').textContent, /^Продлится /);
  assert.equal(f.node('cabinet-tile-used').textContent, '40');
  assert.equal(f.node('cabinet-tile-used-note').textContent, f.words['overview.used_units'].replace('{count}', '52'));
  assert.match(f.node('cabinet-greeting').textContent, /fixture-user-A@example\.test/);
  const svg = f.node('cabinet-chart').children[0];
  assert.equal(svg.localName, 'svg');
  assert.equal(svg.getAttribute('role'), 'img');
  const bars = svg.querySelectorAll('path');
  assert.equal(bars.length, 30);
  assert.equal(bars.filter(bar => bar.classList.contains('is-today')).length, 1);
  assert.equal(bars[29].classList.contains('is-today'), true, 'today is the last day');
  assert.equal(svg.querySelectorAll('title')[0].textContent, f.words['chart.title']);
  assert.match(svg.querySelectorAll('desc')[0].textContent, /40.*52.*7/);
  assert.deepEqual(svg.querySelectorAll('text').filter(label => label.classList.contains('cabinet-tick')).map(label => label.textContent), ['0', '2', '4', '6', '8']);
  assert.equal(f.node('cabinet-chart-table').querySelectorAll('tr').length, 31, 'header + 30 days');
  const chart = f.node('cabinet-chart');
  assert.equal(chart.getAttribute('tabindex'), '0');
  chart.onfocus();
  const tip = f.node('cabinet-chart-tip');
  assert.equal(tip.hidden, false);
  assert.match(tip.textContent, /Сегодня/);
  chart.onkeydown({ key: 'ArrowLeft', preventDefault() {} });
  assert.equal(bars[28].classList.contains('is-active'), true);
  const platforms = f.node('cabinet-platforms').children.map(row => row.children[0].textContent);
  assert.deepEqual(platforms, ['iOS', 'Сайт', 'quantum']);
  assert.equal(f.node('cabinet-cost-card').hidden, false);
  assert.equal(f.node('cabinet-cost-value').textContent, '1 ответ = 2', 'any context text adds a unit at free_chars = 0');
});

test('usage history errors are explained, and only a temporary failure offers a retry', async () => {
  const missing = fixture({ handler: routes({ 'GET /account/api/ai-context': () => respond(contextResponse()) }) });
  await missing.signIn();
  assert.equal(missing.node('cabinet-chart-status').textContent, missing.words['chart.unavailable']);
  assert.equal(missing.node('cabinet-chart-retry').hidden, true);
  assert.equal(missing.node('cabinet-chart').hidden, true);
  let calls = 0;
  const flaky = fixture({ handler: standard({ 'GET /account/api/usage': () => ++calls === 1 ? respond({ error: { code: 'INTERNAL' } }, 500) : respond(usageResponse()) }) });
  await flaky.signIn();
  assert.equal(flaky.node('cabinet-chart-status').textContent, flaky.words['chart.error']);
  assert.equal(flaky.node('cabinet-chart-retry').hidden, false);
  flaky.node('cabinet-chart-retry').click(); await settle();
  assert.equal(flaky.node('cabinet-chart').hidden, false);
  assert.equal(flaky.node('cabinet-chart-retry').hidden, true);
});

/* ------------------------------------------------------------ AI profile */

test('editing shows a live cost and saves the whole document with the expected revision', async () => {
  let saved;
  const f = fixture({ handler: standard({
    'POST /account/api/ai-context': (url, opts) => { saved = JSON.parse(opts.body); return respond(contextResponse({ revision: 4, doc: saved.context })); }
  }) });
  await f.signIn();
  assert.equal(f.node('cabinet-occupation').value, 'Продаю детскую одежду');
  assert.equal(f.node('cabinet-notes-client').value, 'Всегда на «Вы»');
  assert.equal(f.node('cabinet-savebar').hidden, true);
  assert.equal(f.node('cabinet-roles-list').children.length, 3);
  assert.equal(f.node('cabinet-roles-list').children[0].children[1].textContent, 'Продавец');
  assert.equal(f.node('cabinet-cost-line').textContent, 'С этим профилем 1 ответ = 2 из пакета');
  f.type('cabinet-about', 'a'.repeat(420));
  assert.equal(f.node('cabinet-cost-line').textContent, 'С этим профилем 1 ответ = 3 из пакета', 'about + occupation pass 400 characters');
  assert.match(f.node('cabinet-about-count').textContent, /^420 \/ 2\s000$/u);
  assert.equal(f.node('cabinet-notes-client-cost').textContent, '1 ответ = 3');
  assert.equal(f.node('cabinet-savebar').hidden, false);
  assert.equal(f.node('cabinet-save-message').textContent, f.words['save.unsaved']);
  const blogger = f.node('cabinet-roles-list').children[2].children[0];
  blogger.checked = true; blogger.dispatchEvent(makeEvent('change'));
  assert.equal(f.cabinet.isDirty(), true);
  const before = { returnValue: undefined, preventDefault() { this.prevented = true; } };
  f.fireWindow('beforeunload', before);
  assert.equal(before.prevented, true, 'leaving with unsaved changes asks first');
  f.node('cabinet-save').click(); await settle();
  const post = f.posts('/ai-context')[0];
  assert.equal(post.opts.headers['X-Requested-With'], 'AIReplyAccount');
  assert.equal(post.opts.headers['Content-Type'], 'application/json');
  assert.equal(post.opts.credentials, 'same-origin');
  assert.equal(saved.expected_revision, 3);
  assert.equal(saved.expected_user_id, 'fixture-user-A', 'the save names the account the page shows');
  assert.deepEqual(Object.keys(saved.context), ['enabled', 'roles', 'occupation', 'traits', 'about', 'audience', 'recipients']);
  assert.deepEqual(saved.context.roles, ['seller', 'blogger']);
  assert.equal(saved.context.about.length, 420);
  assert.deepEqual(Object.keys(saved.context.recipients), RECIPIENTS);
  assert.equal(f.cabinet.state.context.revision, 4);
  assert.equal(f.node('cabinet-savebar').hidden, true);
  assert.equal(f.node('cabinet-live').textContent, f.words['save.saved']);
  const after = { preventDefault() { this.prevented = true; } };
  f.fireWindow('beforeunload', after);
  assert.equal(after.prevented, undefined);
});

test('text typed while a save is in flight is kept as an unsaved change', async () => {
  let release;
  const f = fixture({ handler: standard({
    'POST /account/api/ai-context': (url, opts) => new Promise(resolve => { release = () => resolve(respond(contextResponse({ revision: 4, doc: JSON.parse(opts.body).context }))); })
  }) });
  await f.signIn();
  f.type('cabinet-about', 'Первый');
  f.node('cabinet-save').click(); await settle();
  f.type('cabinet-about', 'Первый и второй');
  release(); await settle();
  assert.equal(f.node('cabinet-about').value, 'Первый и второй');
  assert.equal(f.cabinet.isDirty(), true);
  assert.equal(f.cabinet.state.context.revision, 4);
});

test('over-limit text is never sent and never cut', async () => {
  const f = fixture({ handler: standard() });
  await f.signIn();
  f.type('cabinet-occupation', 'я'.repeat(121));
  assert.equal(f.node('cabinet-occupation-error').hidden, false);
  assert.equal(f.node('cabinet-occupation').getAttribute('aria-invalid'), 'true');
  assert.equal(f.node('cabinet-save').disabled, true);
  await f.cabinet.save();
  assert.equal(f.posts('/ai-context').length, 0);
  assert.equal(f.node('cabinet-occupation').value.length, 121);
  f.type('cabinet-occupation', 'я'.repeat(120));
  assert.equal(f.node('cabinet-occupation-error').hidden, true);
  assert.equal(f.node('cabinet-save').disabled, false);
});

test('a revision conflict saves nothing, explains it and reloads only on request', async () => {
  let gets = 0;
  const f = fixture({ handler: standard({
    'GET /account/api/ai-context': () => respond(++gets === 1 ? contextResponse() : contextResponse({ revision: 5, doc: { about: 'С другого устройства' } })),
    'POST /account/api/ai-context': () => respond({ error: { code: 'AI_CONTEXT_CHANGED', details: { revision: 5 } } }, 409)
  }) });
  await f.signIn();
  f.type('cabinet-about', 'Моя версия');
  f.node('cabinet-save').click(); await settle();
  assert.equal(f.node('cabinet-save-message').textContent, f.words['save.conflict'] + ' ' + f.words['save.conflict_hint']);
  assert.equal(f.node('cabinet-reload-context').hidden, false);
  assert.equal(f.node('cabinet-save').hidden, true);
  assert.equal(f.node('cabinet-about').value, 'Моя версия', 'the text stays until the person reloads');
  f.node('cabinet-reload-context').click(); await settle();
  assert.equal(gets, 2);
  assert.equal(f.node('cabinet-about').value, 'С другого устройства');
  assert.equal(f.cabinet.state.context.revision, 5);
  assert.equal(f.node('cabinet-savebar').hidden, true);
});

test('validation errors are shown next to their field, also in another section', async () => {
  let answer = { field: 'about', reason: 'too_long', max: 2000 };
  const f = fixture({ handler: standard({ 'POST /account/api/ai-context': () => respond({ error: { code: 'INVALID_REQUEST', details: answer } }, 400) }) });
  await f.signIn();
  f.type('cabinet-about', 'Текст');
  f.node('cabinet-save').click(); await settle();
  assert.equal(f.node('cabinet-about-error').hidden, false);
  assert.match(f.node('cabinet-about-error').textContent, /^Слишком длинный текст\. Максимум — 2\s000\.$/u);
  assert.equal(f.node('cabinet-about').getAttribute('aria-invalid'), 'true');
  assert.equal(f.node('cabinet-save-message').textContent, f.words['save.invalid']);
  assert.equal(f.node('cabinet-save').disabled, true);
  f.type('cabinet-about', 'Текст короче');
  assert.equal(f.node('cabinet-about-error').hidden, true);
  answer = { field: 'recipients.client.notes', reason: 'invalid_text' };
  f.node('cabinet-save').click(); await settle();
  assert.equal(f.visible(), 'recipients');
  assert.equal(f.node('cabinet-notes-client-error').textContent, f.words['field.invalid_text']);
  assert.equal(f.document.activeElement, f.node('cabinet-notes-client'));
  answer = { field: 'roles', reason: 'too_many', max: 5 };
  f.type('cabinet-notes-client', 'На «Вы»');
  f.node('cabinet-save').click(); await settle();
  assert.equal(f.node('cabinet-roles-error').textContent, 'Можно выбрать не больше 5.');
  // A refusal that names no known field is explained without blaming the connection.
  answer = { field: 'expected_revision', reason: 'missing' };
  const chip = f.node('cabinet-roles-list').children[0].children[0];
  chip.checked = false; chip.dispatchEvent(makeEvent('change'));
  f.node('cabinet-save').click(); await settle();
  assert.equal(f.node('cabinet-save-message').textContent, f.words['save.rejected']);
  assert.equal(f.node('cabinet-savebar').hidden, false);
});

test('role chips stop at the limit from the server', async () => {
  const f = fixture({ handler: standard({ 'GET /account/api/ai-context': () => {
    const response = contextResponse();
    response.limits.roles = 2;
    return respond(response);
  } }) });
  await f.signIn();
  const chips = f.node('cabinet-roles-list').children.map(chip => chip.children[0]);
  chips[1].checked = true; chips[1].dispatchEvent(makeEvent('change'));
  assert.equal(f.node('cabinet-roles-count').textContent, 'Выбрано: 2 из 2');
  assert.equal(chips[2].disabled, true);
  chips[2].checked = true; chips[2].dispatchEvent(makeEvent('change'));
  assert.deepEqual(f.cabinet.state.draft.roles, ['seller', 'business_owner']);
  assert.equal(chips[2].checked, false);
});

test('Pro and a disabled profile show no surcharge', async () => {
  const f = fixture({ handler: standard({ 'GET /account/api/ai-context': () => respond(contextResponse({ doc: { about: 'x'.repeat(900) } })) }) });
  await f.signIn(session({ unlimited: true }));
  assert.equal(f.node('cabinet-cost-line').textContent, f.words['profile.cost_unlimited']);
  assert.equal(f.node('cabinet-notes-friend-cost').textContent, f.words['recipients.cost_unlimited']);
  await f.signIn(session());
  assert.equal(f.node('cabinet-cost-line').textContent, 'С этим профилем 1 ответ = 3 из пакета');
  f.node('cabinet-ai-enabled').checked = false;
  f.node('cabinet-ai-enabled').dispatchEvent(makeEvent('change'));
  assert.equal(f.node('cabinet-cost-line').textContent, f.words['profile.cost_disabled']);
  assert.equal(f.node('cabinet-notes-friend-cost').hidden, true);
});

test('another account never sees the previous document, and signing out clears it', async () => {
  const f = fixture({ handler: standard({ 'GET /account/api/ai-context': () => respond(contextResponse({ doc: { about: 'Секрет A' } })) }) });
  await f.signIn();
  f.type('cabinet-audience', 'Черновик A');
  await f.signIn(session({ id: 'fixture-user-B' }));
  assert.equal(f.requests.filter(request => request.url.endsWith('/ai-context')).length, 2, 'B loads its own document');
  assert.equal(f.node('cabinet-audience').value, '');
  await f.signOut();
  assert.equal(f.node('cabinet-about').value, '');
  assert.equal(f.node('cabinet-occupation').value, '');
  assert.equal(f.node('cabinet-roles-list').children.length, 0);
  assert.equal(f.cabinet.state.draft, null);
});

test('an ended session keeps unsaved text until the person signs in again', async () => {
  const f = fixture({ handler: standard({ 'POST /account/api/ai-context': () => respond({ error: { code: 'UNAUTHORIZED' } }, 401) }) });
  await f.signIn();
  f.type('cabinet-about', 'Важный текст');
  f.node('cabinet-save').click(); await settle();
  assert.deepEqual(f.signals, []);
  assert.equal(f.node('cabinet-save-message').textContent, f.words['save.session']);
  assert.equal(f.node('cabinet-relogin').hidden, false);
  assert.equal(f.node('cabinet-about').value, 'Важный текст');
  f.node('cabinet-relogin').click();
  assert.deepEqual(f.signals, ['unauthorized']);
  const clean = fixture({ handler: routes({ 'GET /account/api/ai-context': () => respond({ error: { code: 'UNAUTHORIZED' } }, 401) }) });
  await clean.signIn();
  assert.deepEqual(clean.signals, ['unauthorized'], 'nothing unsaved: account.js signs out right away');
});

test('signing out with unsaved changes asks first', async () => {
  const kept = fixture({ handler: standard(), confirm: false });
  await kept.signIn();
  let logout = 0;
  kept.node('account-logout').addEventListener('click', () => { logout++; });
  kept.type('cabinet-about', 'Не сохранено');
  kept.node('account-logout').click();
  assert.equal(logout, 0);
  const left = fixture({ handler: standard(), confirm: true });
  await left.signIn();
  left.node('account-logout').addEventListener('click', () => { logout++; });
  left.type('cabinet-about', 'Не сохранено');
  left.node('account-logout').click();
  assert.equal(logout, 1);
});

/* ------------------------------------------------------------ dictation */

test('microphones appear only with the feature and a capable browser', async () => {
  const off = fixture({ handler: standard() });
  await off.signIn(session({ voice: false }));
  assert.equal(off.mic('cabinet-about').hidden, true);
  const old = fixture({ handler: standard(), media: false });
  await old.signIn();
  assert.equal(old.mic('cabinet-about').hidden, true);
  const on = fixture({ handler: standard() });
  await on.signIn();
  assert.equal(on.document.querySelectorAll('[data-mic-for]').every(button => !button.hidden), true);
  assert.equal(on.mic('cabinet-about').querySelector('.cabinet-mic-label').textContent, on.words['voice.start']);
});

test('dictation records, shows a timer, stops at 40 seconds and sends a 16 kHz WAV', async () => {
  const recorder = recorderFactory(new Float32Array(48000 * 41).fill(0.1));
  const f = fixture({ handler: standard({ 'POST /account/api/transcribe': () => respond({ text: 'Доставка по Алматы', languages: ['ru'], duration_ms: 40000 }) }), recorder });
  await f.signIn();
  f.node('cabinet-about').value = 'Магазин.';
  f.mic('cabinet-about').click(); await settle();
  assert.deepEqual(recorder.calls, ['start']);
  assert.equal(f.mic('cabinet-about').querySelector('.cabinet-mic-label').textContent, f.words['voice.stop']);
  assert.equal(f.mic('cabinet-audience').disabled, true, 'one recording at a time');
  await f.advance(1000);
  assert.equal(f.mic('cabinet-about').querySelector('.cabinet-mic-timer').textContent, '0:01 / 0:40');
  await f.advance(39000);
  assert.deepEqual(recorder.calls, ['start', 'stop']);
  const request = f.posts('/transcribe')[0];
  assert.equal(request.url, '/account/api/transcribe');
  assert.equal(request.opts.headers['X-Requested-With'], 'AIReplyAccount');
  assert.equal(request.opts.headers['Content-Type'], undefined, 'the browser sets the multipart boundary');
  const form = request.opts.body;
  const audio = form.get('audio');
  assert.equal(audio.name, 'voice.wav');
  assert.equal(audio.type, 'audio/wav');
  const view = new DataView(await audio.arrayBuffer());
  assert.equal(view.getUint32(24, true), 16000);
  assert.equal(view.getUint32(40, true), 16000 * 40 * 2, 'never more than 40 seconds of audio');
  assert.equal(form.get('duration_ms'), '40000');
  assert.match(form.get('request_id'), /^[0-9a-f]{32}$/);
  assert.equal(form.get('languages'), 'ru,kk,en');
  assert.equal(form.get('purpose'), 'dictation');
  assert.equal(form.get('expected_user_id'), 'fixture-user-A');
  assert.equal(f.node('cabinet-about').value, 'Магазин. Доставка по Алматы');
  assert.equal(f.cabinet.state.draft.about, 'Магазин. Доставка по Алматы');
  assert.equal(f.node('cabinet-savebar').hidden, false);
  assert.equal(f.node('cabinet-about-voice').textContent, f.words['voice.done']);
});

test('dictation inserts at the caret and ignores recordings under 0.3 seconds', async () => {
  const short = recorderFactory(new Float32Array(48000 * 0.2));
  const f = fixture({ handler: standard(), recorder: short });
  await f.signIn();
  f.mic('cabinet-about').click(); await settle();
  f.mic('cabinet-about').click(); await settle();
  assert.equal(f.posts('/transcribe').length, 0);
  assert.equal(f.node('cabinet-about-voice').textContent, f.words['voice.too_short']);
  const g = fixture({ handler: standard({ 'POST /account/api/transcribe': () => respond({ text: 'быстро' }) }) });
  await g.signIn();
  const field = g.node('cabinet-notes-friend');
  field.value = 'Пишу коротко';
  field.selectionStart = field.selectionEnd = 4;
  g.mic('cabinet-notes-friend').click(); await settle();
  g.mic('cabinet-notes-friend').click(); await settle();
  assert.equal(field.value, 'Пишу быстро коротко');
  assert.equal(g.document.activeElement, field);
});

test('consent is asked once on CONSENT_REQUIRED and the same recording is retried', async () => {
  let transcribes = 0;
  const f = fixture({ handler: standard({
    'POST /account/api/transcribe': () => ++transcribes === 1 ? respond({ error: { code: 'CONSENT_REQUIRED' } }, 403) : respond({ text: 'Привет' }),
    'POST /account/api/consent': () => respond({ legal_consent: { terms_version: '2026-10-08', privacy_version: '2026-10-14' } })
  }) });
  await f.signIn();
  f.mic('cabinet-about').click(); await settle();
  f.mic('cabinet-about').click(); await settle();
  assert.equal(f.node('cabinet-consent').open, true);
  assert.equal(f.node('cabinet-consent-terms').href, 'https://example.test/offer?lang=ru');
  f.node('cabinet-consent-accept').click(); await settle();
  const consent = f.posts('/consent');
  assert.equal(consent.length, 1);
  assert.deepEqual(JSON.parse(consent[0].opts.body), { terms_version: '2026-10-08', privacy_version: '2026-10-14', locale: 'ru', expected_user_id: 'fixture-user-A' });
  assert.equal(consent[0].opts.headers['X-Requested-With'], 'AIReplyAccount');
  const sent = f.posts('/transcribe');
  assert.equal(sent.length, 2);
  assert.equal(sent[0].opts.body.get('request_id'), sent[1].opts.body.get('request_id'));
  assert.equal(f.node('cabinet-consent').open, false);
  assert.equal(f.node('cabinet-about').value, 'Привет');
});

test('declined or refused consent does not retry again', async () => {
  const f = fixture({ handler: standard({ 'POST /account/api/transcribe': () => respond({ error: { code: 'CONSENT_REQUIRED' } }, 403) }) });
  await f.signIn();
  f.mic('cabinet-about').click(); await settle();
  f.mic('cabinet-about').click(); await settle();
  f.node('cabinet-consent-cancel').click(); await settle();
  assert.equal(f.posts('/consent').length, 0);
  assert.equal(f.posts('/transcribe').length, 1);
  assert.equal(f.node('cabinet-about').value, '');
  const g = fixture({ handler: standard({
    'POST /account/api/transcribe': () => respond({ error: { code: 'CONSENT_REQUIRED' } }, 403),
    'POST /account/api/consent': () => respond({ legal_consent: {} })
  }) });
  await g.signIn();
  g.mic('cabinet-about').click(); await settle();
  g.mic('cabinet-about').click(); await settle();
  g.node('cabinet-consent-accept').click(); await settle();
  assert.equal(g.posts('/transcribe').length, 2, 'one retry only');
  assert.equal(g.node('cabinet-about-voice').textContent, g.words['voice.consent_needed']);
});

test('a known consent requirement is asked before the microphone opens', async () => {
  const recorder = recorderFactory();
  const f = fixture({ handler: standard({ 'POST /account/api/consent': () => respond({ legal_consent: {} }) }), recorder });
  await f.signIn(session({ consentRequired: true }));
  f.mic('cabinet-audience').click(); await settle();
  assert.deepEqual(recorder.calls, []);
  assert.equal(f.node('cabinet-consent').open, true);
  f.node('cabinet-consent-accept').click(); await settle();
  assert.equal(f.node('cabinet-audience-voice').textContent, f.words['voice.ready']);
  f.mic('cabinet-audience').click(); await settle();
  assert.deepEqual(recorder.calls, ['start'], 'the next press records without asking again');
});

test('hiding the tab or switching sections releases the microphone without sending', async () => {
  const recorder = recorderFactory();
  const f = fixture({ handler: standard(), recorder });
  await f.signIn();
  f.mic('cabinet-about').click(); await settle();
  f.document.hidden = true;
  f.document.dispatchEvent(makeEvent('visibilitychange'));
  assert.deepEqual(recorder.calls, ['start', 'cancel']);
  assert.equal(f.node('cabinet-about-voice').textContent, f.words['voice.cancelled']);
  f.document.hidden = false;
  f.mic('cabinet-about').click(); await settle();
  f.navigate('#apps');
  assert.deepEqual(recorder.calls, ['start', 'cancel', 'start', 'cancel']);
  assert.equal(f.posts('/transcribe').length, 0);
});

test('voice turned off on the server hides the microphones', async () => {
  const f = fixture({ handler: standard({ 'POST /account/api/transcribe': () => respond({ error: { code: 'NOT_FOUND' } }, 404) }) });
  await f.signIn();
  f.mic('cabinet-about').click(); await settle();
  f.mic('cabinet-about').click(); await settle();
  assert.equal(f.document.querySelectorAll('[data-mic-for]').every(button => button.hidden), true);
  assert.equal(f.node('cabinet-about-voice').textContent, f.words['voice.unavailable']);
});

test('opposite style chips replace each other instead of being sent together', async () => {
  const f = fixture({ handler: standard() });
  await f.signIn();
  const chips = f.node('cabinet-traits-list').children.map(chip => chip.children[0]);
  const detailed = chips.find(input => input.value === 'detailed');
  assert.deepEqual(f.cabinet.state.draft.traits, ['brief']);
  detailed.checked = true; detailed.dispatchEvent(makeEvent('change'));
  assert.deepEqual(f.cabinet.state.draft.traits, ['detailed'], '«detailed» unselects «brief»');
  assert.equal(chips.find(input => input.value === 'brief').checked, false);
});

test('a page left on another account saves nothing and says why', async () => {
  const f = fixture({ handler: standard({
    'POST /account/api/ai-context': () => respond({ error: { code: 'WEB_ACCOUNT_CHANGED' } }, 409)
  }) });
  await f.signIn();
  f.type('cabinet-about', 'Новый текст');
  f.node('cabinet-save').click(); await settle();
  assert.equal(f.cabinet.state.conflict, false, 'an account change is not a revision conflict');
  assert.equal(f.node('cabinet-save-message').textContent, f.words['account_changed']);
  assert.equal(f.node('cabinet-about').value, 'Новый текст', 'the typed text is kept');
});

test('the daily voice cap and an account change are explained', async () => {
  for (const [status, code, key] of [[429, 'VOICE_LIMIT_REACHED', 'voice.daily_limit'], [429, 'RATE_LIMITED', 'error.RATE_LIMITED'], [409, 'WEB_ACCOUNT_CHANGED', 'account_changed']]) {
    const f = fixture({ handler: standard({ 'POST /account/api/transcribe': () => respond({ error: { code } }, status) }) });
    await f.signIn();
    f.mic('cabinet-about').click(); await settle();
    f.mic('cabinet-about').click(); await settle();
    assert.equal(f.node('cabinet-about-voice').textContent, f.words[key], code);
  }
});
