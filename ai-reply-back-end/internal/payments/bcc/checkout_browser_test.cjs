const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const path = require('node:path');
const html = fs.readFileSync(path.join(__dirname, 'checkout.html'), 'utf8');
const script = html.match(/<script nonce="\{\{\.CSPNonce\}\}">([\s\S]*?)<\/script>/)[1];
function submit(changes = {}) {
  const elements = {
    'bank-checkout': { addEventListener(name, fn) { this.submit = fn; } },
    'bank-submit': { disabled: true },
    'checkout-error': { textContent: '' },
    'phone-cc': { value: '7' },
    'phone-subscriber': { value: '7475558888' },
    'billing-address': { value: '\u0422\u0435\u0441\u0442\u043e\u0432\u0430\u044f \u0443\u043b\u0438\u0446\u0430, 88' },
    'm-info': { value: '' }
  };
  for (const [key, value] of Object.entries(changes)) elements[key].value = value;
  vm.runInNewContext(script, { document: { getElementById: (id) => elements[id] }, window: { screen: { height: 844, width: 390 } }, TextEncoder, btoa: (value) => Buffer.from(value, 'binary').toString('base64') });
  let prevented = false;
  elements['bank-checkout'].submit({ preventDefault() { prevented = true; } });
  return { elements, prevented };
}
test('3DS payload encodes actual screen strings and UTF-8 billing address', () => {
  const { elements, prevented } = submit();
  assert.equal(prevented, false);
  const info = JSON.parse(Buffer.from(elements['m-info'].value, 'base64').toString('utf8'));
  assert.equal(info.browserScreenHeight, '844');
  assert.equal(info.browserScreenWidth, '390');
  assert.deepEqual(info.mobilePhone, { cc: '7', subscriber: '7475558888' });
  assert.equal(info.billAddrLine1, elements['billing-address'].value);
  assert.equal(elements['bank-submit'].disabled, true);
});
test('required cardholder information is never silently fabricated', () => {
  for (const change of [{ 'billing-address': '' }, { 'billing-address': 'A'.repeat(51) }, { 'phone-cc': '' }, { 'phone-subscriber': '' }, { 'phone-subscriber': 'not-a-number' }]) {
    const { elements, prevented } = submit(change);
    assert.equal(prevented, true);
    assert.equal(elements['m-info'].value, '');
  }
});
