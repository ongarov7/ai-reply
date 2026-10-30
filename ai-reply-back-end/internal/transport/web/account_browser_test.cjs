const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const { createAccount, checkoutURL, pendingKey } = require('./static/account.js');

class Element {
  constructor(tag = 'div') {
    this.tag = tag; this.children = []; this.hidden = false; this.disabled = false;
    this.checked = false; this.value = ''; this.textContent = ''; this.listeners = {};
    this.classList = { toggle() {}, add() {}, remove() {} };
  }
  setAttribute() {} focus() {} select() {} scrollIntoView() {}
  addEventListener(name, fn) { this.listeners[name] = fn; }
  replaceChildren() { this.children = []; }
  append(...children) { this.children.push(...children); }
  querySelectorAll() {
    return this.children.flatMap(child => [...(['button', 'input'].includes(child.tag) ? [child] : []), ...child.querySelectorAll()]);
  }
}
const flush = () => new Promise(resolve => setImmediate(resolve));
const response = (payload, status = 200, retryAfter = '') => Promise.resolve({
  ok: status < 400, status, headers: { get: () => retryAfter }, json: () => Promise.resolve(structuredClone(payload))
});
function session(user = 'fixture-user-A', price = 10000, version = 1) {
  return { user: { id: user, email: user + '@example.test' }, usage: { remaining_total: 10, purchased_credits: 0, welcome: { remaining: 10 } },
    bank: { enabled: true, environment: 'sandbox', catalog_version: version, terms_version: '2026-10-30-web-purchase-v1',
      products: [{ code: 'pack30', kind: 'pack', units: 30, price_minor: price, currency: 'KZT', active: true, available_web: true, name: { en: 'Reply pack' } }] } };
}
function fixture(handler, options = {}) {
  const nodes = new Map(), timers = new Map(), stored = options.stored || new Map(), requests = [], redirects = [], historyCalls = [], events = [];
  let now = 100000, timerID = 0, random = 0;
  const document = { hidden: false, getElementById(id) { if (!nodes.has(id)) nodes.set(id, new Element()); return nodes.get(id); },
    createElement(tag) { return new Element(tag); }, querySelectorAll(selector) { return selector === '[data-public-offer]' ? options.publicLinks || [] : []; } };
  const prefix = options.prefix === undefined ? '/sandbox' : options.prefix;
  const location = { origin: 'https://example.test', href: 'https://example.test' + prefix + '/payment' + (options.search || '') + (options.fragment ? '#' + options.fragment : ''),
    search: options.search || '', assign(url) { redirects.push(url); } };
  const browser = { location, history: { replaceState(state, title, value) {
    historyCalls.push(value); events.push('scrub'); location.href = new URL(value, location.origin).href;
  } },
    storage: { getItem(key) { return stored.get(key) || null; }, setItem(key, value) { stored.set(key, value); }, removeItem(key) { stored.delete(key); } },
    crypto: { getRandomValues(bytes) { bytes.fill(++random); return bytes; } }, now: () => now,
    setTimeout(fn, ms) { const id = ++timerID; timers.set(id, { fn, at: now + ms, ms }); return id; },
    clearTimeout(id) { timers.delete(id); }, fetch(url, opts) { events.push('fetch'); requests.push({ url, opts }); return handler(url, opts); } };
  const words = Object.fromEntries(Object.entries(JSON.parse(fs.readFileSync(path.join(__dirname, '../../localization/locales/en.json'), 'utf8')))
    .filter(([key]) => key.startsWith('account.')).map(([key, value]) => [key.slice(8), value]));
  const app = createAccount(document, { locale: 'en', base_path: prefix, messages: words }, browser);
  function accept() { const box = nodes.get('account-purchase-terms'); box.checked = true; box.listeners.change(); }
  async function advance(ms) {
    const end = now + ms;
    for (;;) {
      const next = [...timers].filter(([, item]) => item.at <= end).sort((a, b) => a[1].at - b[1].at)[0];
      if (!next) break;
      now = next[1].at; timers.delete(next[0]); next[1].fn(); await flush();
    }
    now = end; await flush();
  }
  return { app, nodes, stored, requests, redirects, timers, accept, advance, words, location, historyCalls, events };
}
function created(id = 'fixture-order-1') {
  return { payment_id: id, status: 'created', redirect_url: 'https://example.test/sandbox/api/v1/payments/bcc/orders/' + id + '/checkout?state=fixture' };
}

test('purchase requires separate unchecked terms and sends the exact account and quote', async () => {
  const f = fixture((url) => url.endsWith('/checkout') ? response(created()) : response(session()));
  await f.app.init(); await f.app.checkout('pack30');
  assert.equal(f.requests.some(x => x.url.endsWith('/checkout')), false);
  assert.equal(f.nodes.get('account-purchase-terms').checked, false);
  f.accept(); await f.app.checkout('pack30');
  const request = f.requests.find(x => x.url.endsWith('/checkout'));
  assert.equal(request.url, '/sandbox/account/api/checkout');
  assert.equal(request.opts.credentials, 'same-origin');
  assert.equal(request.opts.headers['X-Requested-With'], 'AIReplyAccount');
  assert.deepEqual(JSON.parse(request.opts.body), { product_code: 'pack30', expected_user_id: 'fixture-user-A',
    expected_catalog_version: 1, expected_price_minor: 10000, expected_units: 30, terms_version: '2026-10-30-web-purchase-v1', accept_terms: true });
  assert.equal(f.redirects.length, 1);
  assert.match(f.nodes.get('account-products').children[0].children[1].textContent, /100/);
  assert.match(f.nodes.get('account-products').children[0].children[2].textContent, /30/);
});

test('lost checkout response has a working manual recovery with the same body and idempotency key', async () => {
  let calls = 0;
  const f = fixture(url => url.endsWith('/checkout') ? ++calls === 1 ? Promise.reject(new Error('network lost')) : response(created()) : response(session()));
  await f.app.init(); f.accept(); await f.app.checkout('pack30');
  assert.equal(f.nodes.get('account-retry-checkout').hidden, false);
  assert.equal(f.nodes.get('account-retry-checkout').disabled, false);
  await f.app.retryCheckout();
  const posts = f.requests.filter(x => x.url.endsWith('/checkout'));
  assert.equal(posts.length, 2);
  assert.equal(posts[0].opts.body, posts[1].opts.body);
  assert.equal(posts[0].opts.headers['Idempotency-Key'], posts[1].opts.headers['Idempotency-Key']);
  assert.equal(f.redirects.length, 1);
  assert.doesNotMatch([...f.stored.values()].join(''), /access_token|refresh_token|@example/);
});

test('an unresolved request survives reload and preserves its original approved quote', async () => {
  const first = fixture(url => url.endsWith('/checkout') ? Promise.reject(new Error('lost')) : response(session()));
  await first.app.init(); first.accept(); await first.app.checkout('pack30');
  const previous = first.requests.find(x => x.url.endsWith('/checkout'));
  const resumed = fixture(url => url.endsWith('/checkout') ? response(created()) : response(session('fixture-user-A', 20000, 2)), { stored: first.stored });
  await resumed.app.init(); await resumed.app.retryCheckout();
  const replay = resumed.requests.find(x => x.url.endsWith('/checkout'));
  assert.equal(replay.opts.body, previous.opts.body);
  assert.equal(replay.opts.headers['Idempotency-Key'], previous.opts.headers['Idempotency-Key']);
});

test('changed account between preflight and checkout never redirects and resets consent', async () => {
  let afterConflict = false;
  const f = fixture(url => {
    if (url.endsWith('/checkout')) { afterConflict = true; return response({ error: { code: 'WEB_ACCOUNT_CHANGED' } }, 409); }
    return response(session(afterConflict ? 'fixture-user-B' : 'fixture-user-A'));
  });
  await f.app.init(); f.accept(); await f.app.checkout('pack30');
  assert.equal(f.redirects.length, 0);
  assert.equal(f.app.state.session.user.id, 'fixture-user-B');
  assert.equal(f.nodes.get('account-purchase-terms').checked, false);
  assert.equal(f.app.state.pending, null);
  assert.ok(f.stored.has(pendingKey('/sandbox', 'sandbox', 'fixture-user-A')));
  assert.equal(f.stored.has(pendingKey('/sandbox', 'sandbox', 'fixture-user-B')), false);
});

test('changed price during preflight is displayed and requires a new manual consent', async () => {
  let sessions = 0;
  const f = fixture(url => url.endsWith('/checkout') ? response(created()) : response(++sessions === 1 ? session() : session('fixture-user-A', 20000, 2)));
  await f.app.init(); f.accept(); await f.app.checkout('pack30');
  assert.equal(f.requests.some(x => x.url.endsWith('/checkout')), false);
  assert.equal(f.nodes.get('account-purchase-terms').checked, false);
  assert.match(f.nodes.get('account-products').children[0].children[1].textContent, /200/);
  f.accept(); await f.app.checkout('pack30');
  assert.equal(JSON.parse(f.requests.find(x => x.url.endsWith('/checkout')).opts.body).expected_price_minor, 20000);
});

test('definitive quote conflict refreshes the offer without automatically starting a replacement order', async () => {
  let declined = false;
  const f = fixture(url => {
    if (url.endsWith('/checkout')) { declined = true; return response({ error: { code: 'WEB_QUOTE_CHANGED' } }, 409); }
    return response(declined ? session('fixture-user-A', 20000, 2) : session());
  });
  await f.app.init(); f.accept(); await f.app.checkout('pack30');
  assert.equal(f.app.state.pending, null);
  assert.equal(f.nodes.get('account-purchase-terms').checked, false);
  assert.equal(f.requests.filter(x => x.url.endsWith('/checkout')).length, 1);
  assert.equal(f.redirects.length, 0);
});

test('status errors stop automatic polling and honor Retry-After for manual checks', async () => {
  const f = fixture(url => url.includes('/orders/') ? response({ error: { code: 'RATE_LIMITED' } }, 429, '30') : response(session()), { search: '?order=fixture-order-1' });
  await f.app.init();
  assert.equal(f.app.state.polling, false);
  assert.equal(f.nodes.get('account-check-order').disabled, true);
  await f.advance(29999);
  assert.equal(f.requests.filter(x => x.url.includes('/orders/')).length, 1);
  assert.equal(f.nodes.get('account-check-order').disabled, true);
  await f.advance(1);
  assert.equal(f.nodes.get('account-check-order').disabled, false);
  await f.app.checkOrder();
  assert.equal(f.requests.filter(x => x.url.includes('/orders/')).length, 2);
});

test('automatic checks are eight seconds apart, capped at two minutes, with manual recovery', async () => {
  const f = fixture(url => url.includes('/orders/') ? response({ payment_id: 'fixture-order-1', status: 'verification_pending' }) : response(session()), { search: '?order=fixture-order-1' });
  await f.app.init(); await f.advance(7999);
  assert.equal(f.requests.filter(x => x.url.includes('/orders/')).length, 1);
  await f.advance(112001);
  assert.equal(f.requests.filter(x => x.url.includes('/orders/')).length, 15);
  assert.equal(f.app.state.polling, false);
  assert.equal(f.nodes.get('account-check-order').disabled, false);
});

test('a hung request times out and restores retry controls', async () => {
  const f = fixture(() => new Promise(() => {}));
  const initializing = f.app.init();
  await f.advance(15000); await initializing;
  assert.equal(f.app.state.busy, false);
  assert.equal(f.nodes.get('account-reload').hidden, false);
  assert.match(f.nodes.get('account-error').textContent, /respond in time/);
});

test('a timed-out checkout remains recoverable with its original approved request', async () => {
  const f = fixture(url => url.endsWith('/checkout') ? new Promise(() => {}) : response(session()));
  await f.app.init(); f.accept();
  const checkout = f.app.checkout('pack30');
  await flush(); await f.advance(15000); await checkout;
  assert.equal(f.app.state.busy, false);
  assert.equal(f.nodes.get('account-retry-checkout').hidden, false);
  assert.equal(f.nodes.get('account-retry-checkout').disabled, false);
  assert.equal(f.app.state.pending.quote.expected_price_minor, 10000);
  assert.equal(f.redirects.length, 0);
});

test('choosing a public offer and completing email sign-in never starts checkout automatically', async () => {
  const publicLink = new Element('a'); let signedIn = false;
  const f = fixture(url => {
    if (url.endsWith('/email/request')) return response({ masked_email: 'f***@example.test', resend_after: 32 });
    if (url.endsWith('/email/verify')) { signedIn = true; return response(session()); }
    return signedIn ? response(session()) : response({ error: { code: 'UNAUTHORIZED' } }, 401);
  }, { publicLinks: [publicLink] });
  await f.app.init();
  publicLink.listeners.click({ preventDefault() {} });
  f.nodes.get('account-email').value = 'fixture@example.test';
  await f.nodes.get('account-email-form').listeners.submit({ preventDefault() {} });
  f.nodes.get('account-code').value = '1234';
  await f.nodes.get('account-code-form').listeners.submit({ preventDefault() {} });
  assert.equal(f.app.state.session.user.id, 'fixture-user-A');
  assert.equal(f.requests.some(x => x.url.endsWith('/checkout')), false);
  assert.equal(f.nodes.get('account-purchase-terms').checked, false);
  assert.equal(f.redirects.length, 0);
});

test('bank success remains truthful when the balance refresh fails', async () => {
  let sessions = 0;
  const f = fixture(url => url.includes('/orders/') ? response({ payment_id: 'fixture-order-1', status: 'succeeded' }) : ++sessions === 1 ? response(session()) : response({ error: { code: 'INTERNAL_ERROR' } }, 500), { search: '?order=fixture-order-1' });
  await f.app.init();
  assert.equal(f.nodes.get('account-order-status').textContent, 'Payment confirmed');
  assert.equal(f.nodes.get('account-order-hint').textContent, f.words.balance_refresh_failed);
  assert.equal(f.nodes.get('account-order').hidden, false);
});

test('unauthorized returned orders do not pin the account to an inaccessible pending purchase', async () => {
  const f = fixture(url => url.includes('/orders/') ? response({ error: { code: 'NOT_FOUND' } }, 404) : response(session()), { search: '?order=fixture-order-1' });
  await f.app.init();
  assert.equal(f.app.state.pending, null);
  assert.equal(f.nodes.get('account-order').hidden, true);
  assert.equal(f.redirects.length, 0);
});

test('checkout redirects cannot cross the origin or production/sandbox path', () => {
  const location = { origin: 'https://example.test' };
  assert.equal(checkoutURL('https://attacker.test/checkout', location, '/sandbox'), '');
  assert.equal(checkoutURL('https://example.test/api/v1/payments/bcc/orders/fixture-order-1/checkout', location, '/sandbox'), '');
  assert.equal(checkoutURL('https://example.test/sandbox/api/v1/payments/bcc/orders/fixture-order-1/checkout', location, ''), '');
  assert.ok(checkoutURL(created().redirect_url, location, '/sandbox'));
});

const handoffCode = 'A'.repeat(43); // Synthetic, nonredeemable test capability.

test('handoff is scrubbed before requests, redeemed once, and never stored or used for an automatic purchase', async () => {
  const f = fixture(url => {
    assert.equal(url, '/sandbox/account/api/handoff');
    return response(session());
  }, { fragment: 'handoff=' + handoffCode, search: '?lang=en' });
  assert.deepEqual(f.events, ['scrub']);
  assert.equal(f.location.href, 'https://example.test/sandbox/payment?lang=en');
  await f.app.init();
  assert.deepEqual(f.events, ['scrub', 'fetch']);
  assert.deepEqual(JSON.parse(f.requests[0].opts.body), { code: handoffCode, confirm_account_switch: false });
  assert.equal(f.nodes.get('account-confirmed-email').textContent, 'fixture-user-A@example.test');
  assert.equal(f.nodes.get('account-purchase-terms').checked, false);
  assert.equal(f.requests.some(x => x.url.includes('/checkout')), false);
  assert.equal(f.redirects.length, 0);
  assert.doesNotMatch([...f.stored.values()].join(''), new RegExp(handoffCode));
  assert.doesNotMatch(JSON.stringify(f.app.state), new RegExp(handoffCode));
  assert.equal([...f.nodes.values()].some(node => String(node.textContent).includes(handoffCode)), false);
  assert.equal(f.historyCalls.some(url => url.includes(handoffCode)), false);
});

test('expired or replayed handoff never falls through to an existing browser account', async () => {
  const f = fixture(url => url.endsWith('/handoff') ? response({ error: { code: 'WEB_HANDOFF_INVALID' } }, 401) : response(session('different-browser-user')), { fragment: 'handoff=' + handoffCode });
  await f.app.init();
  assert.equal(f.requests.length, 1);
  assert.equal(f.requests[0].url.endsWith('/handoff'), true);
  assert.equal(f.app.state.session, null);
  assert.equal(f.nodes.get('account-login').hidden, false);
  assert.equal(f.nodes.get('account-content').hidden, true);
  assert.equal(f.nodes.get('account-reload').hidden, true);
  assert.equal(f.nodes.get('account-error').textContent, f.words.handoff_invalid);
  assert.equal(f.redirects.length, 0);
});

test('malformed handoff is removed without making any authentication or purchase request', async () => {
  const f = fixture(() => { throw new Error('must not call an API'); }, { fragment: 'handoff=malformed' });
  await f.app.init();
  assert.deepEqual(f.events, ['scrub']);
  assert.equal(f.requests.length, 0);
  assert.equal(f.app.state.session, null);
  assert.equal(f.nodes.get('account-login').hidden, false);
  assert.equal(new URL(f.location.href).hash, '');
});

test('a different browser account is switched only after explicit confirmation of both identities', async () => {
  const f = fixture((url, opts) => JSON.parse(opts.body).confirm_account_switch
    ? response(session())
    : response({ error: { code: 'WEB_ACCOUNT_SWITCH_REQUIRED', details: { email: 'app@example.test', current_email: 'browser@example.test' } } }, 409), { fragment: 'handoff=' + handoffCode });
  await f.app.init();
  assert.equal(f.nodes.get('account-switch').hidden, false);
  assert.match(f.nodes.get('account-switch-message').textContent, /app@example.test/);
  assert.match(f.nodes.get('account-switch-message').textContent, /browser@example.test/);
  assert.equal(f.app.state.session, null);
  assert.equal(f.requests.length, 1);
  await f.nodes.get('account-switch-confirm').listeners.click();
  assert.equal(f.requests.length, 2);
  assert.equal(JSON.parse(f.requests[1].opts.body).confirm_account_switch, true);
  assert.equal(f.app.state.session.user.id, 'fixture-user-A');
  assert.equal(f.nodes.get('account-switch').hidden, true);
  assert.equal(f.nodes.get('account-purchase-terms').checked, false);
  assert.equal(f.requests.some(x => x.url.includes('/checkout')), false);
});

test('cancelled account switching forgets the code and offers email sign-in without using the old session', async () => {
  const f = fixture(() => response({ error: { code: 'WEB_ACCOUNT_SWITCH_REQUIRED', details: { email: 'app@example.test', current_email: 'browser@example.test' } } }, 409), { fragment: 'handoff=' + handoffCode });
  await f.app.init();
  f.nodes.get('account-switch-cancel').listeners.click();
  assert.equal(f.nodes.get('account-switch').hidden, true);
  assert.equal(f.nodes.get('account-login').hidden, false);
  assert.equal(f.app.state.session, null);
  await f.nodes.get('account-switch-confirm').listeners.click();
  assert.equal(f.requests.length, 1);
  assert.equal(f.requests.some(x => x.url.endsWith('/logout') || x.url.endsWith('/session')), false);
});

test('unconfirmed handoff expires in memory and cannot be redeemed after the expiry timer', async () => {
  const f = fixture(() => response({ error: { code: 'WEB_ACCOUNT_SWITCH_REQUIRED', details: { email: 'app@example.test', current_email: 'browser@example.test' } } }, 409), { fragment: 'handoff=' + handoffCode });
  await f.app.init(); await f.advance(60000);
  await f.nodes.get('account-switch-confirm').listeners.click();
  assert.equal(f.requests.length, 1);
  assert.equal(f.nodes.get('account-login').hidden, false);
  assert.equal(f.nodes.get('account-error').textContent, f.words.handoff_invalid);
});

test('a temporary handoff error keeps only an in-memory retry until success', async () => {
  let attempts = 0;
  const f = fixture(() => ++attempts === 1 ? Promise.reject(new Error('temporary network error')) : response(session()), { fragment: 'handoff=' + handoffCode });
  await f.app.init();
  assert.equal(f.nodes.get('account-reload').hidden, false);
  assert.equal(f.stored.size, 0);
  await f.nodes.get('account-reload').listeners.click();
  assert.equal(f.requests.length, 2);
  assert.equal(JSON.parse(f.requests[1].opts.body).code, handoffCode);
  assert.equal(f.app.state.session.user.id, 'fixture-user-A');
  assert.equal(f.redirects.length, 0);
});

test('normal payment anchors keep the existing cookie-session flow', async () => {
  const f = fixture(() => response(session()), { fragment: 'account-login' });
  await f.app.init();
  assert.equal(f.historyCalls.length, 0);
  assert.equal(f.requests[0].url, '/sandbox/account/api/session');
});

test('sandbox account deletion sends both steps only to sandbox endpoints', async () => {
  const calls = [], nodes = new Map();
  const emailForm = new Element(), codeForm = new Element(), restart = new Element(), done = new Element(), error = new Element();
  emailForm.elements = { email: { value: 'fixture@example.test', focus() {} } };
  codeForm.elements = { code: { value: '1234', focus() {}, select() {} } };
  codeForm.querySelector = () => restart;
  const root = { getAttribute: () => 'en', querySelector(selector) { return selector.includes('email') ? emailForm : selector.includes('code') ? codeForm : selector.includes('done') ? done : error; } };
  nodes.set('landing-data', { textContent: JSON.stringify({ base_path: '/sandbox', errors: {} }) }); nodes.set('delete-account', root);
  const document = { documentElement: { classList: { add() {} } }, getElementById: id => nodes.get(id), querySelectorAll: () => [], querySelector: () => null };
  vm.runInNewContext(fs.readFileSync(path.join(__dirname, 'static/landing.js'), 'utf8'), {
    document, window: {}, fetch(url) { calls.push(url); return response({}); }
  });
  emailForm.listeners.submit({ preventDefault() {} }); await flush();
  codeForm.listeners.submit({ preventDefault() {} }); await flush();
  assert.deepEqual(calls, ['/sandbox/api/v1/account/delete/request', '/sandbox/api/v1/account/delete/confirm']);
});
