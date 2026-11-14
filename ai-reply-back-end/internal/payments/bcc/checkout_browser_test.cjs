const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const path = require('node:path');
const html = fs.readFileSync(path.join(__dirname, 'checkout.html'), 'utf8');
const script = html.match(/<script nonce="\{\{\.CSPNonce\}\}">([\s\S]*?)<\/script>/)[1];

function checkout(changes = {}, overrides = {}) {
  const { storage = new Map(), window: windowOverrides = {}, ...globals } = overrides;
  const storageWrites = [];
  const navigations = [];
  const historyWrites = [];
  const windowListeners = {};
  const elements = {
    'bank-checkout': {
      dataset: {
        orderId: '123e4567-e89b-42d3-a456-426614174000',
        accountUrl: 'https://ai-reply.kz/payment?order=123e4567-e89b-42d3-a456-426614174000&lang=ru',
        invalidDetails: 'Проверьте данные', preparationError: 'Не удалось подготовить платёж', submitting: 'Переходим в банк…'
      },
      attributes: {},
      addEventListener(name, fn) { this[name] = fn; },
      setAttribute(name, value) { this.attributes[name] = value; }
    },
    'bank-submit': { disabled: true, textContent: 'Перейти к оплате' },
    'checkout-error': { textContent: '' },
    'phone-cc': { value: '7' },
    'phone-subscriber': { value: '7475558888' },
    'billing-address': { value: 'Тестовая улица, 88' },
    'm-info': { value: '' }
  };
  for (const [key, value] of Object.entries(changes)) elements[key].value = value;
  vm.runInNewContext(script, {
    document: { getElementById: (id) => elements[id] },
    window: {
      screen: { height: 844, width: 390 },
      sessionStorage: {
        getItem(key) { return storage.get(key) ?? null; },
        setItem(key, value) { storageWrites.push([key, value]); storage.set(key, value); }
      },
      location: { replace(url) { navigations.push(url); } },
      history: { replaceState(state, title, url) { historyWrites.push(url); } },
      addEventListener(name, handler) { windowListeners[name] = handler; },
      ...windowOverrides
    },
    TextEncoder,
    btoa: (value) => Buffer.from(value, 'binary').toString('base64'),
    ...globals
  });
  function submit() {
    let prevented = false;
    elements['bank-checkout'].submit({ preventDefault() { prevented = true; } });
    return { prevented };
  }
  return { elements, submit, storage, storageWrites, navigations, historyWrites, pageshow(event) { windowListeners.pageshow(event); } };
}

test('checkout waits for the buyer and safely disables payment without JavaScript', () => {
  assert.match(html, /<button[^>]+id="bank-submit"[^>]+type="submit"[^>]+disabled>/);
  assert.match(html, /<noscript>[\s\S]*?checkout\.no_js[\s\S]*?<\/noscript>/);
  const { elements, storage, navigations, historyWrites } = checkout();
  assert.equal(elements['bank-submit'].disabled, false);
  assert.equal(elements['m-info'].value, '');
  assert.equal(elements['bank-checkout'].attributes['aria-busy'], undefined);
  assert.equal(storage.size, 0);
  assert.deepEqual(navigations, []);
  assert.deepEqual(historyWrites, []);
});

test('3DS payload encodes actual screen strings and UTF-8 billing address', () => {
  const { elements, submit, storageWrites, navigations, historyWrites } = checkout();
  assert.equal(submit().prevented, false);
  const info = JSON.parse(Buffer.from(elements['m-info'].value, 'base64').toString('utf8'));
  assert.equal(info.browserScreenHeight, '844');
  assert.equal(info.browserScreenWidth, '390');
  assert.deepEqual(info.mobilePhone, { cc: '7', subscriber: '7475558888' });
  assert.equal(info.billAddrLine1, elements['billing-address'].value);
  assert.equal(elements['bank-submit'].disabled, true);
  assert.equal(elements['bank-submit'].textContent, elements['bank-checkout'].dataset.submitting);
  assert.equal(elements['bank-checkout'].attributes['aria-busy'], 'true');
  assert.deepEqual(storageWrites, [['bcc-checkout-submitted:' + elements['bank-checkout'].dataset.orderId, '1']]);
  assert.deepEqual(historyWrites, [elements['bank-checkout'].dataset.accountUrl]);
  assert.deepEqual(navigations, []);
});

test('a second submit event cannot resend an already prepared payment', () => {
  const { elements, submit, storageWrites, navigations, historyWrites } = checkout();
  assert.equal(submit().prevented, false);
  const originalPayload = elements['m-info'].value;
  elements['billing-address'].value = 'Другой адрес';
  assert.equal(submit().prevented, true);
  assert.equal(elements['m-info'].value, originalPayload);
  assert.equal(elements['bank-submit'].disabled, true);
  assert.equal(storageWrites.length, 1);
  assert.equal(historyWrites.length, 1);
  assert.deepEqual(navigations, []);
});

test('required cardholder information is never silently fabricated', () => {
  for (const change of [
    { 'billing-address': '' }, { 'billing-address': ' '.repeat(10) }, { 'billing-address': 'A'.repeat(51) },
    { 'phone-cc': '' }, { 'phone-cc': '0' }, { 'phone-cc': '+7' }, { 'phone-cc': '1234' },
    { 'phone-subscriber': '' }, { 'phone-subscriber': 'not-a-number' }, { 'phone-subscriber': '1'.repeat(15) }
  ]) {
    const { elements, submit, storage, historyWrites } = checkout(change);
    assert.equal(submit().prevented, true, JSON.stringify(change));
    assert.equal(elements['m-info'].value, '');
    assert.equal(elements['bank-submit'].disabled, false);
    assert.equal(elements['checkout-error'].textContent, elements['bank-checkout'].dataset.invalidDetails);
    assert.equal(storage.size, 0);
    assert.deepEqual(historyWrites, []);
  }
});

test('invalid browser dimensions fail closed without a fabricated 3DS payload', () => {
  for (const screen of [{ height: 0, width: 390 }, { height: 844, width: 0 }, { height: -1, width: 390 }, { height: undefined, width: 390 }]) {
    const { elements, submit, storage, historyWrites } = checkout({}, { window: { screen } });
    assert.equal(submit().prevented, true);
    assert.equal(elements['m-info'].value, '');
    assert.equal(elements['bank-submit'].disabled, false);
    assert.equal(elements['checkout-error'].textContent, elements['bank-checkout'].dataset.invalidDetails);
    assert.equal(storage.size, 0);
    assert.deepEqual(historyWrites, []);
  }
});

test('a validation error can be corrected before the single successful submit', () => {
  const { elements, submit } = checkout({ 'billing-address': '' });
  assert.equal(submit().prevented, true);
  elements['billing-address'].value = 'улица Абая, 10';
  assert.equal(submit().prevented, false);
  assert.equal(elements['checkout-error'].textContent, '');
  assert.equal(submit().prevented, true);
});

test('payload preparation failures show a localized error and allow correction', () => {
  let fail = true;
  const { elements, submit, storage, storageWrites, historyWrites } = checkout({}, { btoa(value) {
    if (fail) throw new Error('encoding unavailable');
    return Buffer.from(value, 'binary').toString('base64');
  } });
  assert.equal(submit().prevented, true);
  assert.equal(elements['checkout-error'].textContent, elements['bank-checkout'].dataset.preparationError);
  assert.equal(elements['m-info'].value, '');
  assert.equal(elements['bank-submit'].disabled, false);
  assert.equal(storage.size, 0);
  assert.deepEqual(historyWrites, []);
  fail = false;
  assert.equal(submit().prevented, false);
  assert.equal(elements['checkout-error'].textContent, '');
  assert.equal(storageWrites.length, 1);
  assert.equal(historyWrites.length, 1);
});

test('reopening an already submitted order goes to its status without another form submission', () => {
  const initial = checkout();
  assert.equal(initial.submit().prevented, false);
  const reopened = checkout({}, { storage: initial.storage });
  assert.deepEqual(reopened.navigations, [reopened.elements['bank-checkout'].dataset.accountUrl]);
  assert.equal(reopened.elements['bank-submit'].disabled, true);
  assert.equal(reopened.elements['m-info'].value, '');
  assert.equal(reopened.elements['bank-checkout'].submit, undefined);
  assert.deepEqual(reopened.storageWrites, []);
});

test('a restored back-forward-cache checkout goes to status only after a submission', () => {
  const page = checkout();
  page.pageshow({ persisted: true });
  assert.deepEqual(page.navigations, []);
  assert.equal(page.submit().prevented, false);
  page.pageshow({ persisted: false });
  assert.deepEqual(page.navigations, []);
  page.pageshow({ persisted: true });
  assert.deepEqual(page.navigations, [page.elements['bank-checkout'].dataset.accountUrl]);
});

test('unavailable storage and history do not break the first payment or the duplicate-event guard', () => {
  const page = checkout({}, { window: {
    sessionStorage: { getItem() { throw new Error('storage disabled'); }, setItem() { throw new Error('storage disabled'); } },
    history: { replaceState() { throw new Error('history disabled'); } }
  } });
  assert.equal(page.elements['bank-submit'].disabled, false);
  assert.equal(page.submit().prevented, false);
  assert.notEqual(page.elements['m-info'].value, '');
  assert.equal(page.elements['bank-submit'].disabled, true);
  assert.equal(page.submit().prevented, true);
  assert.deepEqual(page.navigations, []);
  page.pageshow({ persisted: true });
  assert.deepEqual(page.navigations, [page.elements['bank-checkout'].dataset.accountUrl]);
});
