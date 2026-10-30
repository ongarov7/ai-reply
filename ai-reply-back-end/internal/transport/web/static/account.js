(function () {
  'use strict';

  var MIN_CHECK_INTERVAL = 8000;
  var MAX_POLL_TIME = 120000;
  var REQUEST_TIMEOUT = 15000;
  var ID = /^[A-Za-z0-9_-]{8,64}$/;
  var TERMINAL = ['succeeded', 'failed', 'cancelled', 'expired', 'refunded'];

  function pendingKey(base, environment, userID) {
    return 'aireply-account-order:' + (base || '/') + ':' + environment + ':' + userID;
  }
  function orderID(order) { return order && (order.payment_id || order.id) || ''; }
  function finalStatus(status) { return TERMINAL.indexOf(status) !== -1; }
  function takeHandoff(browser) {
    try {
      var url = new URL(browser.location.href);
      var fragment = new URLSearchParams(url.hash.slice(1));
      if (!fragment.has('handoff')) return { present: false, code: '' };
      var code = fragment.get('handoff') || '';
      // Scrub before any API request. The capability never enters storage,
      // rendered markup, query parameters, or a subsequent navigation URL.
      browser.history.replaceState(null, '', url.pathname + url.search);
      return { present: true, code: /^[A-Za-z0-9_-]{43}$/.test(code) ? code : '' };
    } catch (_) {
      // Refuse redemption if the browser could not remove the fragment.
      try { if (url && browser.location.replace) browser.location.replace(url.pathname + url.search); } catch (_) {}
      return { present: true, code: '' };
    }
  }
  function checkoutURL(raw, location, base) {
    try {
      var url = new URL(raw, location.origin);
      var allowed = (base || '') + '/api/v1/payments/bcc/orders/';
      if (url.origin !== location.origin || url.username || url.password ||
          url.pathname.indexOf(allowed) !== 0 || !/^[A-Za-z0-9_-]{8,64}\/checkout$/.test(url.pathname.slice(allowed.length))) return '';
      return url.href;
    } catch (_) { return ''; }
  }

  function createAccount(document, boot, browser) {
    var handoff = takeHandoff(browser);
    var handoffCode = handoff.code;
    handoff.code = '';
    var handoffExpiresAt = handoffCode ? browser.now() + 60000 : 0;
    var handoffTimer = null;
    var base = boot.base_path === '/sandbox' ? '/sandbox' : '';
    var apiBase = base + '/account/api';
    var words = boot.messages || {};
    var locale = boot.locale || 'en';
    var state = { session: null, pending: null, terminalOrder: null, busy: false, checking: false, nextCheckAt: 0, pollStart: 0, timer: null, cooldownTimer: null, polling: false, epoch: 0, queryUsed: false, acceptedQuote: '' };
    var email = '';
    var resendAt = 0;
    var resendTimer = null;
    var nodes = {};
    ['loading', 'error', 'reload', 'login', 'content', 'switch', 'switch-message', 'switch-confirm', 'switch-cancel', 'email-form', 'email', 'code-form', 'code', 'code-hint', 'change-email', 'resend', 'resend-hint', 'confirmed-email', 'user-id', 'remaining', 'purchased', 'welcome-row', 'welcome', 'current-plan', 'logout', 'refresh', 'sandbox', 'bank-unavailable', 'products', 'terms-box', 'purchase-terms', 'order', 'order-id', 'order-status', 'order-hint', 'check-order', 'retry-checkout', 'continue-order'].forEach(function (id) {
      nodes[id] = document.getElementById('account-' + id);
    });
    var app = document.getElementById('account-app');
    function t(key, values) {
      var result = words[key] || words['error.generic'] || '';
      Object.keys(values || {}).forEach(function (name) { result = result.split('{' + name + '}').join(String(values[name])); });
      return result;
    }
    function text(id, value) { nodes[id].textContent = String(value == null ? '' : value); }
    function hidden(id, value) { nodes[id].hidden = value; }
    function notice(message) { text('error', message || ''); hidden('error', !message); }
    function setBusy(value) {
      state.busy = value;
      app.setAttribute('aria-busy', String(value));
      ['email-form', 'code-form', 'products'].forEach(function (id) {
        nodes[id].querySelectorAll('input, button').forEach(function (node) { node.disabled = value; });
      });
      nodes.logout.disabled = value;
      nodes.refresh.disabled = value;
      nodes['switch-confirm'].disabled = value;
      nodes['switch-cancel'].disabled = value;
      nodes['purchase-terms'].disabled = value || !!state.pending;
      if (!value) updateResend();
      updateOrderButtons();
    }
    function updateOrderButtons() {
      nodes['check-order'].disabled = state.busy || state.checking || browser.now() < state.nextCheckAt;
      nodes['retry-checkout'].disabled = state.busy;
      nodes.products.querySelectorAll('button').forEach(function (node) {
        node.disabled = state.busy || !!state.pending || !nodes['purchase-terms'].checked || state.acceptedQuote !== quoteFingerprint(state.session);
      });
      if (state.cooldownTimer) browser.clearTimeout(state.cooldownTimer);
      var wait = state.nextCheckAt - browser.now();
      state.cooldownTimer = wait > 0 ? browser.setTimeout(updateOrderButtons, wait) : null;
    }
    function api(path, body, extraHeaders) {
      var headers = { Accept: 'application/json' };
      var options = { method: body === undefined ? 'GET' : 'POST', credentials: 'same-origin', cache: 'no-store', headers: headers };
      if (body !== undefined) {
        headers['Content-Type'] = 'application/json';
        headers['X-Requested-With'] = 'AIReplyAccount';
        options.body = JSON.stringify(body);
      }
      Object.keys(extraHeaders || {}).forEach(function (key) { headers[key] = extraHeaders[key]; });
      var controller = browser.AbortController ? new browser.AbortController() : null;
      if (controller) options.signal = controller.signal;
      var timer;
      var timeout = new Promise(function (_, reject) {
        timer = browser.setTimeout(function () {
          if (controller) controller.abort();
          reject(new Error('REQUEST_TIMEOUT'));
        }, REQUEST_TIMEOUT);
      });
      var request = browser.fetch(apiBase + path, options).then(function (response) {
        return response.json().catch(function () { return {}; }).then(function (payload) {
          if (response.ok) return payload;
          var error = new Error(payload.error && payload.error.code || 'NETWORK_ERROR');
          error.status = response.status;
          error.details = payload.error && payload.error.details || {};
          error.retryAfter = Number(response.headers && response.headers.get('Retry-After')) || 0;
          throw error;
        });
      });
      return Promise.race([request, timeout]).then(function (payload) {
        browser.clearTimeout(timer); return payload;
      }, function (error) {
        browser.clearTimeout(timer); throw error;
      });
    }
    function stopPolling() {
      if (state.timer) browser.clearTimeout(state.timer);
      state.timer = null;
      state.polling = false;
    }
    function signedOut() {
      stopPolling();
      state.epoch += 1;
      state.session = null;
      state.pending = null;
      state.terminalOrder = null;
      state.acceptedQuote = '';
      nodes['purchase-terms'].checked = false;
      state.checking = false;
      hidden('content', true);
      hidden('login', false);
      hidden('loading', true);
      hidden('switch', true);
      setBusy(false);
    }
    function fail(error) {
      if (error && error.status === 401) {
        signedOut();
        notice(t('error.session'));
        return;
      }
      var code = error && error.message || '';
      notice(words['error.' + code] || t('error.generic'));
    }
    function clearHandoff() {
      handoffCode = '';
      handoff.present = false;
      handoffExpiresAt = 0;
      if (handoffTimer) browser.clearTimeout(handoffTimer);
      handoffTimer = null;
      hidden('switch', true);
    }
    function handoffFallback(message) {
      clearHandoff();
      signedOut();
      hidden('reload', true);
      hidden('email-form', false);
      hidden('code-form', true);
      nodes.code.value = '';
      notice(message || t('handoff_invalid'));
    }
    function redeemHandoff(confirmSwitch) {
      if (!handoffCode || browser.now() >= handoffExpiresAt) {
        handoffFallback(t('handoff_invalid'));
        return Promise.resolve();
      }
      setBusy(true); notice(''); hidden('reload', true);
      if (!handoffTimer) handoffTimer = browser.setTimeout(function () {
        handoffFallback(t('handoff_invalid'));
      }, Math.max(1, handoffExpiresAt - browser.now()));
      return api('/handoff', { code: handoffCode, confirm_account_switch: !!confirmSwitch }).then(function (payload) {
        if (!handoffCode) return;
        clearHandoff();
        try { renderSession(payload, true); }
        catch (_) { handoffFallback(t('handoff_invalid')); return; }
        setBusy(false);
        if (state.pending && state.pending.order_id) return beginPolling();
      }).catch(function (error) {
        if (!handoffCode) return;
        if (error.status === 409 && error.message === 'WEB_ACCOUNT_SWITCH_REQUIRED') {
          hidden('loading', true); hidden('login', true); hidden('content', true); hidden('switch', false);
          text('switch-message', t('switch_message', { email: error.details.email || '', current_email: error.details.current_email || '' }));
        } else if (error.status === 400 || error.status === 401) {
          handoffFallback(t('handoff_invalid'));
        } else {
          notice(t('handoff_retry'));
          hidden('loading', true); hidden('reload', false);
        }
      }).then(function () { setBusy(false); });
    }
    function storageName() {
      return pendingKey(base, state.session.bank.environment, state.session.user.id);
    }
    function savePending() {
      if (!state.session) return;
      try {
        if (!state.pending) browser.storage.removeItem(storageName());
        else browser.storage.setItem(storageName(), JSON.stringify({
          order_id: state.pending.order_id || '',
          idempotency: state.pending.idempotency || '',
          product_code: state.pending.product_code || '',
          quote: state.pending.quote || null
        }));
      } catch (_) { /* A disabled storage area does not prevent a signed-in purchase. */ }
    }
    function restorePending() {
      var saved;
      try { saved = JSON.parse(browser.storage.getItem(storageName()) || 'null'); } catch (_) { saved = null; }
      if (saved && (saved.order_id === '' || ID.test(saved.order_id)) &&
          typeof saved.idempotency === 'string' && saved.idempotency.length <= 128 &&
          typeof saved.product_code === 'string' && saved.product_code.length <= 64) {
        if (saved.order_id || validQuote(saved.quote) && saved.quote.expected_user_id === state.session.user.id && saved.idempotency.length >= 8) {
          state.pending = { order_id: saved.order_id, idempotency: saved.idempotency, product_code: saved.product_code, quote: saved.quote || null };
        }
      }
      if (!state.queryUsed) {
        state.queryUsed = true;
        var requested = new URLSearchParams(browser.location.search).get('order');
        if (requested && ID.test(requested)) {
          if (!state.pending || state.pending.order_id !== requested) state.pending = { order_id: requested, idempotency: '', product_code: '' };
          savePending();
        }
      }
    }
    function money(amount, currency) {
      try { return new Intl.NumberFormat(locale === 'kk' ? 'kk-KZ' : locale, { style: 'currency', currency: currency, maximumFractionDigits: amount % 100 ? 2 : 0 }).format(amount / 100); }
      catch (_) { return String(amount / 100) + ' ' + currency; }
    }
    function purchaseQuote(payload, productCode) {
      if (!payload || !payload.bank || !payload.user) return null;
      var bank = payload.bank;
      var product = (Array.isArray(bank.products) ? bank.products : []).find(function (p) { return p.code === productCode; });
      if (!bank.enabled || !product || product.kind !== 'pack' || product.active === false || product.available_web === false || product.currency !== 'KZT') return null;
      var quote = { product_code: productCode, expected_user_id: payload.user.id, expected_catalog_version: bank.catalog_version,
        expected_price_minor: product.price_minor, expected_units: product.units, terms_version: payload.terms_version || bank.terms_version || '', accept_terms: true };
      return validQuote(quote) ? quote : null;
    }
    function validQuote(quote) {
      return !!quote && ID.test(quote.expected_user_id || '') && typeof quote.product_code === 'string' &&
        Number.isSafeInteger(quote.expected_catalog_version) && quote.expected_catalog_version > 0 &&
        Number.isSafeInteger(quote.expected_price_minor) && quote.expected_price_minor > 0 &&
        Number.isSafeInteger(quote.expected_units) && quote.expected_units > 0 &&
        quote.accept_terms === true &&
        typeof quote.terms_version === 'string' && quote.terms_version.length > 0 && quote.terms_version.length <= 128;
    }
    function quoteFingerprint(payload) {
      if (!payload || !payload.bank) return '';
      return JSON.stringify([payload.user && payload.user.id, payload.bank.environment, payload.bank.catalog_version,
        payload.terms_version || payload.bank.terms_version,
        (payload.bank.products || []).map(function (p) { return [p.code, p.price_minor, p.currency, p.units, p.active, p.available_web]; })]);
    }
    function renderProducts() {
      nodes.products.replaceChildren();
      var bank = state.session.bank;
      document.querySelectorAll('[data-public-product]').forEach(function (card) {
        var code = card.getAttribute('data-public-product');
        var product = (bank.products || []).find(function (p) { return p.code === code; });
        if (!product || !Number.isSafeInteger(product.price_minor) || product.currency !== 'KZT') return;
        card.querySelector('[data-offer-price]').textContent = money(product.price_minor, product.currency);
        card.querySelector('[data-offer-units]').textContent = String(product.units);
        card.querySelector('[data-offer-name]').textContent = product.name && (product.name[locale] || product.name.en) || t('top_up');
      });
      var products = bank.enabled && Array.isArray(bank.products) ? bank.products.filter(function (p) {
        return !!purchaseQuote(state.session, p.code) && p.kind === 'pack' && p.active !== false && p.available_web !== false &&
          typeof p.code === 'string' && Number.isInteger(p.units) && p.units > 0 &&
          Number.isSafeInteger(p.price_minor) && p.price_minor > 0 && /^[A-Z]{3}$/.test(p.currency);
      }) : [];
      hidden('bank-unavailable', products.length > 0);
      hidden('terms-box', products.length === 0);
      hidden('sandbox', !bank.enabled || bank.environment !== 'sandbox');
      products.forEach(function (product) {
        var card = document.createElement('div');
        card.className = 'account-product';
        var name = document.createElement('h3');
        name.textContent = (product.name && (product.name[locale] || product.name.en)) || t('top_up');
        var price = document.createElement('p');
        price.className = 'account-product-price';
        price.textContent = money(product.price_minor, product.currency);
        var units = document.createElement('p');
        units.className = 'account-product-units';
        units.textContent = t('pack_units', { count: product.units });
        var button = document.createElement('button');
        button.type = 'button'; button.className = 'btn btn-primary'; button.textContent = t('buy');
        button.addEventListener('click', function () { checkout(product.code); });
        card.append(name, price, units, button);
        nodes.products.append(card);
      });
      updateOrderButtons();
    }
    function renderSession(payload, restore) {
      if (!payload.user || !ID.test(payload.user.id || '')) throw new Error('INVALID_SESSION');
      var previous = state.session && state.session.user.id;
      var previousQuote = quoteFingerprint(state.session);
      if (previous && previous !== payload.user.id) {
        stopPolling(); state.pending = null; state.terminalOrder = null; state.epoch += 1; state.nextCheckAt = 0;
        restore = true;
        notice(t('error.ACCOUNT_CHANGED'));
      }
      payload.bank = payload.bank || { enabled: false, environment: base ? 'sandbox' : 'production', products: [] };
      if (payload.bank.environment !== 'sandbox' && payload.bank.environment !== 'production') payload.bank.enabled = false;
      state.session = payload;
      if (previousQuote !== quoteFingerprint(payload)) {
        state.acceptedQuote = '';
        nodes['purchase-terms'].checked = false;
      }
      var usage = payload.usage || {};
      text('confirmed-email', payload.user.email || t('email_unavailable'));
      text('user-id', payload.user.id);
      text('remaining', usage.unlimited ? '∞' : Math.max(0, Number(usage.remaining_total) || 0));
      text('purchased', Math.max(0, Number(usage.purchased_credits) || 0));
      hidden('welcome-row', !usage.welcome);
      if (usage.welcome) text('welcome', Math.max(0, Number(usage.welcome.remaining) || 0));
      var plan = payload.subscription && payload.subscription.plan;
      var planName = plan && ((plan.name && (plan.name[locale] || plan.name.en)) || plan.code);
      text('current-plan', planName ? t('current_plan', { name: planName }) : '');
      hidden('loading', true); hidden('reload', true); hidden('login', true); hidden('content', false);
      if (restore) restorePending();
      renderProducts();
      renderPending();
      return payload;
    }
    function renderPending() {
      if (!state.pending) {
        hidden('order', !state.terminalOrder);
        return;
      }
      state.terminalOrder = null;
      hidden('order', false);
      text('order-id', state.pending.order_id || '—');
      text('order-status', t('status.' + (state.pending.status || 'pending')));
      text('order-hint', t(state.pending.order_id ? 'pending_hint' : 'checkout_retry_hint'));
      nodes.order.classList.toggle('is-success', false);
      hidden('check-order', !state.pending.order_id);
      hidden('retry-checkout', !!state.pending.order_id || !validQuote(state.pending.quote));
      hidden('continue-order', true);
      updateOrderButtons();
    }
    function loadSession(restore) {
      return api('/session').then(function (payload) { return renderSession(payload, restore); });
    }
    function updateResend() {
      var remaining = Math.max(0, Math.ceil((resendAt - browser.now()) / 1000));
      nodes.resend.disabled = state.busy || remaining > 0;
      text('resend-hint', remaining ? t('resend_in', { seconds: remaining }) : '');
      if (resendTimer) browser.clearTimeout(resendTimer);
      resendTimer = remaining ? browser.setTimeout(updateResend, 1000) : null;
    }
    function requestEmail(event) {
      if (event) event.preventDefault();
      if (state.busy) return Promise.resolve();
      if (handoff.present) clearHandoff();
      email = nodes.email.value.trim();
      if (!email) return Promise.resolve();
      notice(''); setBusy(true);
      return api('/email/request', { email: email, locale: locale }).then(function (challenge) {
        resendAt = browser.now() + Math.max(1, Number(challenge.resend_after) || 32) * 1000;
        text('code-hint', t('code_sent', { email: challenge.masked_email || email }));
        hidden('email-form', true); hidden('code-form', false);
        nodes.code.value = '';
      }).catch(fail).then(function () { setBusy(false); if (!nodes['code-form'].hidden) nodes.code.focus(); });
    }
    function verifyEmail(event) {
      event.preventDefault();
      if (state.busy) return Promise.resolve();
      var code = nodes.code.value.trim();
      if (!/^[0-9]{4}$/.test(code)) { notice(t('error.INVALID_OTP')); return Promise.resolve(); }
      notice(''); setBusy(true);
      return api('/email/verify', { email: email, code: code, device: { platform: 'web', locale: locale, model: 'Web account' } }).then(function (payload) {
        clearHandoff();
        nodes.code.value = '';
        if (payload.user && payload.bank) return renderSession(payload, true);
        return loadSession(true);
      }).then(function () { if (state.pending && state.pending.order_id) beginPolling(); }).catch(fail).then(function () { setBusy(false); });
    }
    function clearOrderQuery() {
      try {
        var url = new URL(browser.location.href);
        url.searchParams.delete('order');
        browser.history.replaceState(null, '', url.pathname + url.search + url.hash);
      } catch (_) { /* The order is still owned and verified by the server. */ }
    }
    function renderOrder(order) {
      var id = orderID(order);
      if (!state.pending || id !== state.pending.order_id) throw new Error('INVALID_ORDER');
      state.pending.status = order.status;
      hidden('order', false); text('order-id', id);
      var key = 'status.' + order.status;
      text('order-status', words[key] ? t(key) : t('status.pending'));
      var terminal = finalStatus(order.status);
      text('order-hint', t(order.status === 'succeeded' ? 'balance_updating' : terminal ? 'finished_hint' : 'pending_hint'));
      nodes.order.classList.toggle('is-success', order.status === 'succeeded');
      var link = order.status === 'created' ? checkoutURL(order.redirect_url, browser.location, base) : '';
      hidden('continue-order', !link);
      if (link) nodes['continue-order'].href = link;
      hidden('check-order', terminal);
      hidden('retry-checkout', true);
      if (terminal) {
        stopPolling();
        var owner = state.session.user.id;
        state.terminalOrder = { owner: owner, order: order };
        state.pending = null; savePending(); clearOrderQuery();
        nodes['purchase-terms'].checked = false; state.acceptedQuote = '';
        return loadSession(false).then(function () {
          if (state.session && state.session.user.id === owner && state.terminalOrder) {
            hidden('order', false);
            text('order-hint', t(order.status === 'succeeded' ? 'success_hint' : 'finished_hint'));
          }
          return order;
        }).catch(function (error) {
          fail(error);
          if (state.session && state.session.user.id === owner) text('order-hint', t('balance_refresh_failed'));
          return order;
        });
      }
      updateOrderButtons();
      return Promise.resolve(order);
    }
    function checkOrder() {
      if (!state.session || !state.pending || !state.pending.order_id || state.checking) return Promise.resolve();
      if (browser.now() < state.nextCheckAt) return Promise.resolve();
      state.nextCheckAt = browser.now() + MIN_CHECK_INTERVAL; state.checking = true;
      var id = state.pending.order_id;
      var epoch = state.epoch;
      updateOrderButtons();
      return api('/orders/' + encodeURIComponent(id)).then(function (order) {
        if (epoch !== state.epoch || !state.pending || state.pending.order_id !== id) return;
        notice('');
        return renderOrder(order);
      }).catch(function (error) {
        if (epoch !== state.epoch) return;
        stopPolling();
        if (error.retryAfter) state.nextCheckAt = Math.max(state.nextCheckAt, browser.now() + error.retryAfter * 1000);
        if (error.status === 404 || error.status === 403) {
          state.pending = null; savePending(); clearOrderQuery();
          hidden('order', true);
          notice(t('error.ORDER_NOT_AVAILABLE'));
        } else {
          fail(error);
          text('order-hint', t('manual_check_hint'));
        }
      }).then(function () {
        state.checking = false;
        if (epoch !== state.epoch) return;
        updateOrderButtons();
        if (state.polling && state.session && state.pending && state.pending.order_id) {
          state.timer = browser.setTimeout(function () {
            state.timer = null;
            updateOrderButtons();
            if (browser.now() - state.pollStart < MAX_POLL_TIME && !document.hidden) checkOrder();
            else { state.polling = false; text('order-hint', t('manual_check_hint')); }
          }, MIN_CHECK_INTERVAL);
        }
      });
    }
    function beginPolling() {
      stopPolling(); state.pollStart = browser.now(); state.polling = true;
      return checkOrder();
    }
    function randomKey() {
      if (!browser.crypto || !browser.crypto.getRandomValues) throw new Error('NETWORK_ERROR');
      var bytes = new Uint8Array(16);
      browser.crypto.getRandomValues(bytes);
      return Array.from(bytes).map(function (value) { return value.toString(16).padStart(2, '0'); }).join('');
    }
    function checkout(productCode) {
      if (state.busy || !state.session || state.pending) return Promise.resolve();
      var displayed = purchaseQuote(state.session, productCode);
      if (!displayed || !nodes['purchase-terms'].checked || state.acceptedQuote !== quoteFingerprint(state.session)) {
        notice(t('error.TERMS_REQUIRED')); return Promise.resolve();
      }
      var owner = state.session.user.id;
      var epoch = state.epoch;
      notice(''); setBusy(true);
      return api('/session').then(function (payload) {
        if (epoch !== state.epoch) throw new Error('ACCOUNT_CHANGED');
        if (!payload.user || payload.user.id !== owner) {
          renderSession(payload, true);
          throw new Error('ACCOUNT_CHANGED');
        }
        if (JSON.stringify(displayed) !== JSON.stringify(purchaseQuote(payload, productCode))) {
          renderSession(payload, false);
          throw new Error('QUOTE_CHANGED');
        }
        state.pending = { order_id: '', product_code: productCode, idempotency: randomKey(), quote: displayed };
        state.terminalOrder = null;
        savePending();
        renderPending();
        return submitCheckout(owner, epoch);
      }).catch(fail).then(function () { setBusy(false); });
    }
    function retryCheckout() {
      if (state.busy || !state.session || !state.pending || state.pending.order_id || !validQuote(state.pending.quote)) return Promise.resolve();
      if (state.pending.quote.expected_user_id !== state.session.user.id) { notice(t('error.ACCOUNT_CHANGED')); return Promise.resolve(); }
      notice(''); setBusy(true);
      return submitCheckout(state.session.user.id, state.epoch).then(function () { setBusy(false); });
    }
    function submitCheckout(owner, epoch) {
      var intent = state.pending;
      // The backend checks the expected account and the exact approved quote.
      // Retrying a lost response reuses this same body and idempotency key.
      return api('/checkout', intent.quote, { 'Idempotency-Key': intent.idempotency }).then(function (order) {
        if (epoch !== state.epoch || !state.session || state.session.user.id !== owner || state.pending !== intent) return;
        var id = orderID(order);
        if (!ID.test(id)) throw new Error('INVALID_ORDER');
        state.pending.order_id = id; state.pending.status = order.status; savePending();
        nodes['purchase-terms'].checked = false; state.acceptedQuote = '';
        if (order.status === 'created') {
          var link = checkoutURL(order.redirect_url, browser.location, base);
          if (!link) throw new Error('INVALID_ORDER');
          browser.location.assign(link);
        } else return renderOrder(order).then(function () { if (state.pending) return beginPolling(); });
      }).catch(function (error) {
        if (epoch !== state.epoch) return;
        if (error.status === 409) {
          // A conflict must never silently create a replacement payment.
          nodes['purchase-terms'].checked = false; state.acceptedQuote = '';
          if (error.message === 'WEB_QUOTE_CHANGED' || error.message === 'WEB_TERMS_CHANGED') {
            state.pending = null; savePending();
          }
          return loadSession(false).then(function () { fail(error); }).catch(fail);
        }
        fail(error);
        if (state.pending) {
          renderPending();
          text('order-hint', t(state.pending.order_id ? 'manual_check_hint' : 'checkout_retry_hint'));
        }
      }).then(function (order) {
        updateOrderButtons(); return order;
      });
    }
    function refresh() {
      if (state.busy) return Promise.resolve();
      notice(''); setBusy(true);
      return loadSession(false).catch(fail).then(function () { setBusy(false); });
    }
    function logout() {
      if (state.busy) return Promise.resolve();
      setBusy(true); notice(''); stopPolling(); state.epoch += 1;
      return api('/logout', {}).then(function () {
        state.pending = null; savePending();
        signedOut();
        nodes.email.value = ''; nodes.code.value = '';
        hidden('email-form', false); hidden('code-form', true);
        clearOrderQuery();
      }).catch(fail).then(function () { setBusy(false); });
    }
    nodes['email-form'].addEventListener('submit', requestEmail);
    nodes['code-form'].addEventListener('submit', verifyEmail);
    nodes['change-email'].addEventListener('click', function () {
      if (state.busy) return;
      nodes.code.value = ''; hidden('email-form', false); hidden('code-form', true); notice(''); nodes.email.focus();
    });
    nodes.resend.addEventListener('click', function () { if (browser.now() >= resendAt) requestEmail(); });
    nodes.logout.addEventListener('click', logout);
    nodes.refresh.addEventListener('click', refresh);
    nodes['purchase-terms'].addEventListener('change', function () {
      state.acceptedQuote = nodes['purchase-terms'].checked ? quoteFingerprint(state.session) : '';
      updateOrderButtons();
    });
    nodes['retry-checkout'].addEventListener('click', retryCheckout);
    nodes['switch-confirm'].addEventListener('click', function () { if (!state.busy) return redeemHandoff(true); });
    nodes['switch-cancel'].addEventListener('click', function () { if (!state.busy) handoffFallback(t('handoff_cancelled')); });
    document.querySelectorAll('[data-public-offer]').forEach(function (link) {
      link.addEventListener('click', function (event) {
        event.preventDefault();
        if (handoff.present) {
          (nodes['switch'].hidden ? nodes.error : nodes['switch']).scrollIntoView({ behavior: 'smooth', block: 'start' });
          return;
        }
        if (!state.session) hidden('login', false);
        var target = state.session ? nodes.products : nodes.login;
        target.scrollIntoView({ behavior: 'smooth', block: 'start' });
        if (!state.session && !state.busy) nodes.email.focus();
      });
    });
    nodes.reload.addEventListener('click', init);
    nodes['check-order'].addEventListener('click', function () { stopPolling(); checkOrder(); });
    function init() {
      notice(''); hidden('reload', true); hidden('loading', false); setBusy(true);
      if (handoff.present) return redeemHandoff(false);
      return loadSession(true).then(function () { setBusy(false); if (state.pending && state.pending.order_id) return beginPolling(); }).catch(function (error) {
        if (error.status === 401) signedOut();
        else { fail(error); hidden('reload', false); }
      }).then(function () { hidden('loading', true); setBusy(false); });
    }
    return { init: init, checkout: checkout, retryCheckout: retryCheckout, checkOrder: checkOrder, refresh: refresh, logout: logout, state: state };
  }

  if (typeof module !== 'undefined' && module.exports) module.exports = { createAccount: createAccount, pendingKey: pendingKey, checkoutURL: checkoutURL };
  if (typeof document !== 'undefined' && document.getElementById('account-app')) {
    var boot;
    try { boot = JSON.parse(document.getElementById('account-data').textContent); } catch (_) { return; }
    var storage;
    try { storage = window.sessionStorage; } catch (_) { storage = { getItem: function () { return null; }, setItem: function () {}, removeItem: function () {} }; }
    var account = createAccount(document, boot, { fetch: window.fetch.bind(window), location: window.location, history: window.history, storage: storage, crypto: window.crypto, AbortController: window.AbortController, now: Date.now, setTimeout: window.setTimeout.bind(window), clearTimeout: window.clearTimeout.bind(window) });
    account.init();
  }
})();
