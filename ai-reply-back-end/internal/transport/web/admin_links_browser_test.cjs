const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

function loadAdmin(prefix = '') {
  const history = [];
  const context = vm.createContext({ console, URL, URLSearchParams, Intl, setTimeout() {} });
  vm.runInContext(fs.readFileSync(path.join(__dirname, 'static/vendor/vue.global.prod.js'), 'utf8'), context);
  const runtime = context.Vue;
  let root;
  const app = { config: { globalProperties: {} }, component() {}, mount() {} };
  const boot = { base_path: prefix, locale: 'en', locales: ['en'], i18n: { en: {} }, admin: {}, env: 'test' };
  Object.assign(context, {
    window: { Vue: { ...runtime, createApp(component) { root = component; return app; } }, addEventListener() {}, scrollTo() {} },
    document: { getElementById(id) { return id === 'boot' ? { textContent: JSON.stringify(boot) } : { classList: { remove() {} } }; } },
    location: { pathname: prefix + '/admin/users/fixture-user', search: '' },
    history: { pushState(state, title, url) { history.push(url); } },
    fetch() { throw new Error('Link rendering must not call the network'); }
  });
  vm.runInContext(fs.readFileSync(path.join(__dirname, 'static/admin-app.js'), 'utf8'), context);
  return { runtime, root, app, history };
}

// Compiles the simulator's real templates; only SIM.t and SIM.register are needed to read them.
function loadSimulator() {
  const context = vm.createContext({ console });
  vm.runInContext(fs.readFileSync(path.join(__dirname, 'static/vendor/vue.global.prod.js'), 'utf8'), context);
  const runtime = context.Vue;
  const views = {};
  context.window = { SIM: { t: key => key, register(name, view) { views[name] = view; } } };
  for (const file of ['views-admin.js', 'views-product.js']) {
    vm.runInContext(fs.readFileSync(path.join(__dirname, 'static/simulator', file), 'utf8'), context);
  }
  return { runtime, views };
}

// A v-for list is one fragment; its options carry their text as a single string.
function optionLabels(view) {
  return Array.from(view.children[0].children, option => option.children.trim());
}

const plans = [
  { id: 'b', code: 'base_monthly', daily_message_limit: 0 },
  { id: 'p', code: 'pro_monthly', daily_message_limit: 0 },
  { id: 'y', code: 'base_yearly', daily_message_limit: 40 }
];

test('user card links and SPA navigation preserve the primary or sandbox environment', () => {
  for (const prefix of ['', '/sandbox']) {
    const { runtime, root, app, history } = loadAdmin(prefix);
    // Compile the actual anchors from the user component; no Vue lifecycle,
    // API fetches or browser navigation run in this rendering test.
    const anchors = root.components.UserDetail.template.match(/<a\b[\s\S]*?<\/a>/g);
    assert.equal(anchors.length, 4);
    const render = runtime.compile('<div>' + anchors.join('') + '</div>');
    const id = 'fixture-user/? value';
    const view = render({ data: { user: { id, status: 'active' } }, t: key => key,
      appPath: app.config.globalProperties.appPath, $root: root.methods }, []);
    const links = view.children.filter(node => node.type === 'a');
    const targets = [
      '/admin/purchases/transactions?user_id=', '/admin/notifications/new?user_ids=',
      '/admin/notifications/deliveries?user_id=', '/admin/notifications/devices?user_id='
    ].map(route => prefix + route + encodeURIComponent(id));
    assert.deepEqual(Array.from(links, link => link.props.href), targets);
    for (let i = 0; i < links.length; i++) {
      let prevented = false;
      links[i].props.onClick({ preventDefault() { prevented = true; } });
      assert.equal(prevented, true);
      assert.equal(history.at(-1), targets[i]);
    }
  }
});

test('the admin plan picker states a daily number only for a plan that has one', () => {
  const { runtime, root } = loadAdmin();
  const option = root.components.UserDetail.template.match(/<option v-for="p in plans"[\s\S]*?<\/option>/)[0];
  const view = runtime.compile('<select>' + option + '</select>')({ plans, t: key => key }, []);
  assert.deepEqual(optionLabels(view), ['base_monthly', 'pro_monthly', 'base_yearly · 40/pricing.per_day']);
});

test('the simulator states a daily number only for a plan that has one', () => {
  const { runtime, views } = loadSimulator();
  const option = views['sim-step-panel'].template.match(/<option v-for="p in state.plans"[\s\S]*?<\/option>/)[0];
  const picker = runtime.compile('<select>' + option + '</select>')(
    { state: { plans }, planName: plan => plan.code, t: key => key }, []);
  assert.deepEqual(optionLabels(picker), ['base_monthly', 'pro_monthly', 'base_yearly · 40/sim.usage.day']);

  const line = views['view-pricing'].template.match(/<div class="muted"[^>]*daily_message_limit[\s\S]*?<\/div>/)[0];
  const card = runtime.compile('<div>' + line + '</div>');
  const rendered = plan => card({ plan, t: key => key }, []).children[0];
  assert.notEqual(rendered({ daily_message_limit: 0 }).type, 'div');
  assert.match(rendered({ daily_message_limit: 40 }).children, /40 sim\.pricing\.per_day/);
});
