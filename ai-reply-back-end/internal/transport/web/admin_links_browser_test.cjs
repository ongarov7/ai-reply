const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

test('user card links and SPA navigation preserve the primary or sandbox environment', () => {
  for (const prefix of ['', '/sandbox']) {
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
