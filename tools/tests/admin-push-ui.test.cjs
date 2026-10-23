'use strict';
// Dependency-free checks for the admin's recipient preview and send-confirmation guards.
// Run with Node.js: node tools/tests/admin-push-ui.test.cjs
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const file = path.resolve(__dirname, '../../ai-reply-back-end/internal/transport/web/static/admin-app.js');
const script = fs.readFileSync(file, 'utf8');
const start = script.indexOf('  var RecipientsPreview = {');
const end = script.indexOf('  var CampaignList = {', start);
assert(start > 0 && end > start, 'admin component boundaries must be present');
const context = vm.createContext({ helpers: {}, pushMeta: (v) => v });
vm.runInContext(script.slice(start, end), context, { filename: file });
const confirm = context.SendConfirm;
const cases = [
  ['loaded recipients', {}, true],
  ['loading preview', { loading: true }, false],
  ['preview failed', { error: 'Network error' }, false],
  ['provider blocked', { blocked: true }, false],
  ['already submitting', { busy: true }, false],
  ['no preview', { preview: null }, false],
  ['no eligible devices', { preview: { devices: 0 } }, false],
  ['missing device count', { preview: {} }, false],
];
for (const [name, changes, allowed] of cases) {
  const emitted = [];
  const state = Object.assign({ busy: false, loading: false, blocked: false, error: '', preview: { devices: 2 }, $emit: (event) => emitted.push(event) }, changes);
  Object.defineProperty(state, 'canSend', { get: () => confirm.computed.canSend.call(state) });
  assert.equal(state.canSend, allowed, name);
  confirm.methods.confirm.call(state);
  assert.equal(emitted.length, allowed ? 1 : 0, name + ' must guard the handler as well as the button');
}
assert(confirm.template.includes(':disabled="!canSend"'), 'visible button follows the same guard');
const preview = context.RecipientsPreview;
const reasons = preview.computed.exclusions.call({ preview: { exclusions: { permission_denied: { users: 2, devices: 3 } } } });
assert.equal(reasons[0].code, 'permission_denied');
assert.equal(reasons[0].users, 2);
assert.equal(reasons[0].devices, 3);
assert.equal(preview.computed.records.call({ preview: {} }).length, 0, 'older preview responses remain supported');
console.log('Admin push UI: 8 confirmation scenarios and recipient diagnostics passed');
// Compile the actual templates with the repository's shipped Vue compiler.
const vue = fs.readFileSync(path.resolve(path.dirname(file), 'vendor/vue.global.prod.js'), 'utf8');
vm.runInContext(vue, context, { filename: 'vue.global.prod.js' });
for (const component of [preview, confirm]) {
  assert.equal(typeof context.Vue.compile(component.template, { decodeEntities: (value) => value, onError: (error) => { throw error; } }), 'function');
}
console.log('Admin push UI: both modified Vue templates compiled');
