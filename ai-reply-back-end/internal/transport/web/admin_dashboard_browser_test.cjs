const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

function dashboardHarness(prefix = '') {
  const requests = [], timers = new Map(), listeners = new Map();
  let timerID = 0, root;
  const context = vm.createContext({ console, URL, URLSearchParams, Intl, AbortController,
    setTimeout(fn, delay) { const id = ++timerID; timers.set(id, { fn, delay }); return id; },
    clearTimeout(id) { timers.delete(id); }
  });
  vm.runInContext(fs.readFileSync(path.join(__dirname, 'static/vendor/vue.global.prod.js'), 'utf8'), context);
  const runtime = context.Vue;
  const boot = { base_path: prefix, locale: 'en', admin: {}, i18n: { en: {} } };
  const document = { hidden: false,
    getElementById() { return { textContent: JSON.stringify(boot), classList: { remove() {} } }; },
    addEventListener(name, fn) { listeners.set(name, fn); },
    removeEventListener(name) { listeners.delete(name); }
  };
  Object.assign(context, { document,
    window: { Vue: { ...runtime, createApp(value) { root = value; return { config: { globalProperties: {} }, component() {}, mount() {} }; } },
      addEventListener() {}, scrollTo() {} },
    location: { pathname: prefix + '/admin', search: '' }, history: { pushState() {} },
    fetch(url, options) {
      return new Promise((resolve, reject) => {
        const request = { url, options, resolve(value, status = 200) { resolve({ status, ok: status < 400, json: async () => value }); } };
        requests.push(request);
        if (options.signal) {
          const abort = () => { const error = new Error('aborted'); error.name = 'AbortError'; reject(error); };
          if (options.signal.aborted) abort();
          else options.signal.addEventListener('abort', abort, { once: true });
        }
      });
    }
  });
  vm.runInContext(fs.readFileSync(path.join(__dirname, 'static/admin-app.js'), 'utf8'), context);
  const component = root.components.Dashboard;
  const model = component.data();
  for (const [key, fn] of Object.entries(component.methods)) model[key] = fn.bind(model);
  for (const [key, fn] of Object.entries(component.computed)) Object.defineProperty(model, key, { get: () => fn.call(model) });
  const render = runtime.compile(component.template, { decodeEntities: value => value, onError(error) { throw error; } });
  return { model, component, requests, timers, listeners, document, render };
}

const summary = (key = '30d', count = 8) => ({
  range: { key, from: '2026-10-17', to: '2026-11-15' }, stats: { purchases_range: count }, series: {},
  purchase_breakdown: [
    { product_code: 'pack30', name: '30 replies', kind: 'pack', purchases: 5, buyers: 4, units: 150,
      catalog_price_minor: 13000, catalog_currency: 'KZT', unknown_price_purchases: 0,
      amounts: [{ currency: 'KZT', amount_milliunits: 650000, priced_purchases: 5 }] },
    { product_code: 'pack500', name: '500 replies', kind: 'pack', purchases: 2, buyers: 2, units: 1000,
      catalog_price_minor: 99000, catalog_currency: 'KZT', unknown_price_purchases: 1,
      amounts: [{ currency: 'KZT', amount_milliunits: 990000, priced_purchases: 1 }, { currency: 'USD', amount_milliunits: 2500, priced_purchases: 1 }] }
  ]
});
const history = (product = '', total = 8) => ({ product, total, purchases: [{ id: 'iap:one', user_id: 'buyer-one', product_code: product || 'pack30' }] });
async function flush() { for (let i = 0; i < 12; i++) await Promise.resolve(); }
function settleBatch(h, start, data = summary(), rows = history()) {
  for (const request of h.requests.slice(start, start + 2)) {
    request.resolve(request.url.includes('/dashboard/purchases?') ? rows : data);
  }
}

test('dashboard refresh keeps known data, one timer and separate receipt currencies', async () => {
  const h = dashboardHarness('/sandbox');
  const loading = h.model.load();
  assert.equal(h.requests.length, 2);
  assert(h.requests.every(request => request.url.startsWith('/sandbox/api/v1/admin/')));
  settleBatch(h, 0);
  await loading;
  assert.equal(h.model.data.stats.purchases_range, 8);
  assert.equal(h.model.purchaseRows.length, 1);
  assert.equal(h.model.unknownPrices, 1);
  assert.deepEqual(Array.from(h.model.receivedAmounts, a => [a.currency, a.amount_milliunits]), [['KZT', 1640000], ['USD', 2500]]);
  assert.equal([...h.timers.values()].filter(timer => timer.delay === 30000).length, 1);
  const known = h.model.data;
  const next = h.model.load();
  assert.equal(h.model.data, known);
  assert.equal(h.model.loading, false);
  settleBatch(h, 2, summary('30d', 9));
  await next;
  assert.equal(h.model.data.stats.purchases_range, 9);
  assert.equal([...h.timers.values()].filter(timer => timer.delay === 30000).length, 1);
  assert(h.model.amount(130000, 'KZT', 1000).includes('130'));
});

test('a changed range replaces an unfinished response and unmount stops updates', async () => {
  const h = dashboardHarness();
  const old = h.model.load();
  h.model.setRange('today');
  await flush();
  // Serialized implementations may wait for the superseded batch; aborted ones
  // start immediately. In either case its response must never become visible.
  settleBatch(h, 0, summary('30d', 100));
  await old; await flush();
  assert.equal(h.requests.length, 4);
  assert(h.requests.slice(2).every(request => request.url.includes('range=today')));
  settleBatch(h, 2, summary('today', 2), history('', 2));
  await flush();
  assert.equal(h.model.data.stats.purchases_range, 2);
  assert.equal(h.model.data.range.key, 'today');
  const outstanding = h.model.load();
  h.component.beforeUnmount.call(h.model);
  settleBatch(h, 4, summary('today', 999));
  await outstanding; await flush();
  assert.equal(h.model.data.stats.purchases_range, 2);
  assert.equal(h.timers.size, 0);
});

test('background refresh uses applied dates and retains unsubmitted edits', async () => {
  const h = dashboardHarness();
  h.model.from = '2026-10-17'; h.model.to = '2026-11-15';
  h.model.applyCustom();
  settleBatch(h, 0, summary('custom'));
  await flush();
  h.model.from = '2026-11-01'; h.model.dateDraftEdited = true;
  const loading = h.model.load();
  assert(h.requests.slice(2).every(request => request.url.includes('from=2026-10-17')));
  settleBatch(h, 2, summary('custom'));
  await loading;
  assert.equal(h.model.from, '2026-11-01');
  h.model.selectProduct('pack500');
  assert.equal(h.model.purchaseRows.length, 0, 'old rows must not be shown under the new filter');
  assert(h.requests.at(-1).url.includes('product=pack500'));
  settleBatch(h, 4, summary('custom'), history('pack500', 2));
  await flush();
  assert.equal(h.model.purchaseTotal, 2);
  assert.equal(h.model.purchaseRows[0].product_code, 'pack500');
});

test('visibility and automatic refresh switches prevent hidden polling', async () => {
  const h = dashboardHarness();
  h.component.mounted.call(h.model);
  settleBatch(h, 0);
  await flush();
  h.document.hidden = true;
  h.listeners.get('visibilitychange')();
  assert.equal([...h.timers.values()].filter(timer => timer.delay === 30000).length, 0);
  h.document.hidden = false;
  h.listeners.get('visibilitychange')();
  assert.equal(h.requests.length, 4);
  settleBatch(h, 2);
  await flush();
  h.model.autoRefresh = false; h.model.toggleRefresh();
  assert.equal([...h.timers.values()].filter(timer => timer.delay === 30000).length, 0);
  h.component.beforeUnmount.call(h.model);
  assert.equal(h.listeners.size, 0);
});

test('failed refresh retains last successful data and displays an error', async () => {
  const h = dashboardHarness();
  const initial = h.model.load(); settleBatch(h, 0); await initial;
  const known = h.model.data;
  const refresh = h.model.load();
  h.requests[2].resolve({ error: { code: 'ERROR' } }, 500);
  h.requests[3].resolve(history());
  await refresh;
  assert.equal(h.model.data, known);
  assert.equal(h.model.error, 'admin.dashboard.refresh_error');
  assert.equal(h.model.refreshing, false);
});

test('an endpoint failure waits for its sibling before another refresh', async () => {
  const h = dashboardHarness();
  const loading = h.model.load();
  h.requests[0].resolve({ error: { code: 'ERROR' } }, 500);
  await flush();
  assert.equal(h.model.refreshing, true);
  assert.equal(h.requests.length, 2);
  h.requests[1].resolve(history());
  await loading;
  assert.equal(h.model.refreshing, false);
  assert.equal(h.requests.length, 2);
});

test('a stalled network request times out and preserves accepted data', async () => {
  const h = dashboardHarness();
  const initial = h.model.load(); settleBatch(h, 0); await initial;
  const known = h.model.data;
  const loading = h.model.load();
  const timeout = [...h.timers.values()].find(timer => timer.delay === 15000);
  assert(timeout, 'network requests must have a bounded wait');
  timeout.fn();
  await loading;
  assert.equal(h.model.data, known);
  assert.equal(h.model.error, 'admin.dashboard.refresh_error');
  assert.equal(h.model.refreshing, false);
  assert(h.requests.slice(2).every(request => request.options.signal.aborted));
});
