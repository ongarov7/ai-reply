/*
 * Личный кабинет (/account, /payment): разделы, обзор, AI-профиль, собеседники
 * и диктовка. Sign-in, the balance and purchases stay in account.js; this
 * script follows the session through its 'aireply:session' and
 * 'aireply:signedout' events and talks only to /account/api/* (cookie session).
 *
 * Contract: docs/web-cabinet-ai-context.md. Every value that comes from the
 * server or the person is written with textContent or a DOM property — never
 * as HTML.
 */
(function () {
  'use strict';

  var SECTIONS = ['overview', 'profile', 'recipients', 'billing', 'apps'];
  var CONTEXT_SECTIONS = ['profile', 'recipients'];
  var RECIPIENTS = ['friend', 'client', 'colleague', 'partner'];
  var TEXT_FIELDS = ['occupation', 'about', 'audience'];
  var DEFAULT_LIMITS = { roles: 5, traits: 6, occupation: 120, about: 2000, audience: 300, notes: 1000 };
  // Spoken-language hints, the most likely first (≤ 3, see §5.4).
  var VOICE_LANGUAGES = { ru: 'ru,kk,en', kk: 'kk,ru,en', en: 'en,ru,kk', uz: 'uz,ru,en' };
  // Style chips that contradict each other: choosing one unselects the other
  // (the server refuses both together, docs/web-cabinet-ai-context.md §2.2).
  var EXCLUSIVE_TRAITS = [['introvert', 'extrovert'], ['brief', 'detailed'], ['formal', 'casual'], ['emoji', 'no_emoji']];
  function oppositeTrait(code) {
    for (var i = 0; i < EXCLUSIVE_TRAITS.length; i++) {
      if (EXCLUSIVE_TRAITS[i][0] === code) return EXCLUSIVE_TRAITS[i][1];
      if (EXCLUSIVE_TRAITS[i][1] === code) return EXCLUSIVE_TRAITS[i][0];
    }
    return '';
  }
  var PLATFORM_NAMES = { ios: 'iOS', android: 'Android', macos: 'macOS', windows: 'Windows', watchos: 'Apple Watch' };
  var VOICE_RATE = 16000;
  var MAX_RECORDING_MS = 40000;
  var MIN_RECORDING_MS = 300;
  var REQUEST_TIMEOUT = 15000;
  var VOICE_TIMEOUT = 60000;
  var USAGE_TTL = 60000;
  var SVG_NS = 'http://www.w3.org/2000/svg';
  // Like the server (aicontext cleanText): pasted CR, VT, NEL, LS and PS line
  // breaks become "\n" and bidirectional controls are dropped. The remaining
  // C0/C1 control characters other than tab and newline are refused by the
  // server; none of them is visible, so they are dropped here too.
  var LINE_BREAKS = /\r\n|[\r\u000B\u0085\u2028\u2029]/g;
  var BIDI_CONTROLS = /[\u061C\u200E\u200F\u202A-\u202E\u2066-\u2069]/g;
  var CONTROL = /[\u0000-\u0008\u000B-\u001F\u007F-\u009F]/g;

  /* ------------------------------------------------------------ pure helpers */

  // The text exactly as it is sent: what is counted is what the server stores.
  function normalizeText(value) {
    return String(value == null ? '' : value).replace(LINE_BREAKS, '\n').replace(BIDI_CONTROLS, '').replace(CONTROL, '').trim();
  }
  // "chars" are Unicode code points (§2): an emoji counts as one.
  function chars(value) { return Array.from(normalizeText(value)).length; }
  function wholeNumber(value) {
    var number = Number(value);
    return Number.isFinite(number) && number > 0 ? Math.floor(number) : 0;
  }
  function codeList(list) {
    var out = [];
    (Array.isArray(list) ? list : []).forEach(function (code) {
      if (typeof code === 'string' && code && out.indexOf(code) === -1) out.push(code);
    });
    return out;
  }
  // The whole document with every field, in a fixed key order (§2, §5.2).
  function normalizeDocument(doc) {
    doc = doc && typeof doc === 'object' ? doc : {};
    var out = {
      enabled: doc.enabled !== false, roles: codeList(doc.roles), occupation: normalizeText(doc.occupation),
      traits: codeList(doc.traits), about: normalizeText(doc.about), audience: normalizeText(doc.audience), recipients: {}
    };
    RECIPIENTS.forEach(function (key) {
      var entry = doc.recipients && doc.recipients[key];
      out.recipients[key] = { notes: normalizeText(entry && entry.notes) };
    });
    return out;
  }
  function sameDocument(a, b) { return JSON.stringify(normalizeDocument(a)) === JSON.stringify(normalizeDocument(b)); }

  // units(r) from §3: roles and traits never count; Pro, a disabled profile or
  // chars_per_reply = 0 mean no surcharge.
  function contextUnits(doc, pricing, recipient) {
    if (!doc || doc.enabled === false || !pricing || pricing.unlimited === true) return 1;
    var perReply = wholeNumber(pricing.chars_per_reply);
    if (perReply === 0) return 1;
    var total = chars(doc.occupation) + chars(doc.about) + chars(doc.audience);
    if (recipient) {
      var entry = doc.recipients && doc.recipients[recipient];
      total += chars(entry && entry.notes);
    }
    var extra = Math.ceil(Math.max(0, total - wholeNumber(pricing.free_chars)) / perReply);
    return 1 + Math.min(wholeNumber(pricing.max_extra_replies), extra);
  }
  function contextCost(doc, pricing) {
    var cost = { general: contextUnits(doc, pricing, '') };
    RECIPIENTS.forEach(function (key) { cost[key] = contextUnits(doc, pricing, key); });
    return cost;
  }

  function sectionFromHash(hash, available, fallback) {
    var name = String(hash || '').replace(/^#/, '');
    return available.indexOf(name) !== -1 ? name : fallback;
  }
  function voiceLanguages(locale) { return VOICE_LANGUAGES[locale] || VOICE_LANGUAGES.en; }

  // Clean axis ticks: 0, step, …, top ≥ max, with 1/2/5 × 10ⁿ whole steps.
  function niceTicks(max) {
    max = Math.max(0, Number(max) || 0);
    if (max === 0) return [0, 1];
    var rough = max / 4;
    var power = Math.pow(10, Math.floor(Math.log10(rough)));
    var scaled = rough / power;
    var step = Math.max(1, (scaled <= 1 ? 1 : scaled <= 2 ? 2 : scaled <= 5 ? 5 : 10) * power);
    var ticks = [];
    for (var value = 0; value < max + step; value += step) {
      ticks.push(value);
      if (value >= max) break;
    }
    return ticks;
  }

  // Box-filter decimation (an average per output sample is the low-pass that
  // keeps speech clean); linear interpolation in the rare upsampling case.
  function downsample(input, inputRate, outputRate) {
    if (!input || !(inputRate > 0) || !(outputRate > 0)) return new Float32Array(0);
    if (inputRate === outputRate) return Float32Array.from(input);
    var ratio = inputRate / outputRate;
    var length = Math.floor(input.length / ratio);
    var output = new Float32Array(length);
    for (var i = 0; i < length; i++) {
      if (ratio < 1) {
        var position = i * ratio, index = Math.floor(position), fraction = position - index;
        var next = index + 1 < input.length ? input[index + 1] : input[index];
        output[i] = input[index] + (next - input[index]) * fraction;
        continue;
      }
      var start = Math.floor(i * ratio), end = Math.min(input.length, Math.floor((i + 1) * ratio));
      var sum = 0;
      for (var j = start; j < end; j++) sum += input[j];
      output[i] = end > start ? sum / (end - start) : input[start] || 0;
    }
    return output;
  }
  function writeASCII(view, offset, text) {
    for (var i = 0; i < text.length; i++) view.setUint8(offset + i, text.charCodeAt(i));
  }
  // RIFF/WAVE, PCM, mono, 16-bit little-endian: the 44-byte canonical header.
  function encodeWAV(samples, sampleRate) {
    var dataLength = samples.length * 2;
    var buffer = new ArrayBuffer(44 + dataLength);
    var view = new DataView(buffer);
    writeASCII(view, 0, 'RIFF');
    view.setUint32(4, 36 + dataLength, true);
    writeASCII(view, 8, 'WAVE');
    writeASCII(view, 12, 'fmt ');
    view.setUint32(16, 16, true);
    view.setUint16(20, 1, true);
    view.setUint16(22, 1, true);
    view.setUint32(24, sampleRate, true);
    view.setUint32(28, sampleRate * 2, true);
    view.setUint16(32, 2, true);
    view.setUint16(34, 16, true);
    writeASCII(view, 36, 'data');
    view.setUint32(40, dataLength, true);
    for (var i = 0; i < samples.length; i++) {
      var sample = Math.max(-1, Math.min(1, Number(samples[i]) || 0));
      view.setInt16(44 + i * 2, sample < 0 ? Math.round(sample * 0x8000) : Math.round(sample * 0x7fff), true);
    }
    return buffer;
  }
  function requestID(crypto) {
    if (!crypto || typeof crypto.getRandomValues !== 'function') throw new Error('CRYPTO_UNAVAILABLE');
    var bytes = new Uint8Array(16);
    crypto.getRandomValues(bytes);
    return Array.from(bytes).map(function (value) { return value.toString(16).padStart(2, '0'); }).join('');
  }
  function concatSamples(chunks, length) {
    var out = new Float32Array(length), offset = 0;
    chunks.forEach(function (chunk) { out.set(chunk, offset); offset += chunk.length; });
    return out;
  }

  // Microphone capture: an AudioWorklet from our own origin, ScriptProcessor
  // where worklets are missing. The stream is released on every exit.
  function createRecorder(browser, captureURL) {
    var stream = null, context = null, source = null, node = null, chunks = [], length = 0, released = false;
    function collect(data) {
      if (released || !data || !data.length) return;
      var copy = new Float32Array(data);
      chunks.push(copy);
      length += copy.length;
    }
    function release() {
      released = true;
      try { if (node) { if (node.port) node.port.onmessage = null; node.onaudioprocess = null; node.disconnect(); } } catch (_) { /* already gone */ }
      try { if (source) source.disconnect(); } catch (_) { /* already gone */ }
      if (stream) stream.getTracks().forEach(function (track) { try { track.stop(); } catch (_) { /* already stopped */ } });
      if (context && context.state !== 'closed' && typeof context.close === 'function') {
        try {
          var closing = context.close();
          if (closing && typeof closing.catch === 'function') closing.catch(function () {});
        } catch (_) { /* already closed */ }
      }
      stream = null; source = null; node = null;
    }
    function scriptProcessor() {
      node = context.createScriptProcessor(4096, 1, 1);
      node.onaudioprocess = function (event) { collect(event.inputBuffer.getChannelData(0)); };
      source.connect(node);
      node.connect(context.destination);
    }
    return {
      start: function () {
        // Created inside the click: browsers start audio only from a gesture.
        try { context = new browser.AudioContext(); } catch (error) { return Promise.reject(error); }
        if (context.state === 'suspended' && typeof context.resume === 'function') context.resume().catch(function () {});
        return browser.navigator.mediaDevices.getUserMedia({ audio: { channelCount: 1, echoCancellation: true, noiseSuppression: true, autoGainControl: true } }).then(function (media) {
          stream = media;
          if (released) throw new Error('CANCELLED');
          source = context.createMediaStreamSource(stream);
          if (captureURL && context.audioWorklet && browser.AudioWorkletNode) {
            return context.audioWorklet.addModule(captureURL).then(function () {
              if (released) throw new Error('CANCELLED');
              node = new browser.AudioWorkletNode(context, 'aireply-capture', { numberOfInputs: 1, numberOfOutputs: 1, outputChannelCount: [1] });
              node.port.onmessage = function (event) { collect(event.data); };
              source.connect(node);
              node.connect(context.destination);
            }, function () {
              if (released) throw new Error('CANCELLED');
              scriptProcessor();
            });
          }
          scriptProcessor();
        }).catch(function (error) { release(); throw error; });
      },
      stop: function () {
        var rate = context ? context.sampleRate : 0;
        var samples = concatSamples(chunks, length);
        release();
        return Promise.resolve({ samples: samples, sampleRate: rate });
      },
      cancel: function () { chunks = []; length = 0; release(); }
    };
  }

  /* ------------------------------------------------------------ the cabinet */

  function createCabinet(document, boot, browser) {
    var base = boot.base_path === '/sandbox' ? '/sandbox' : '';
    var apiBase = base + '/account/api';
    var words = boot.messages || {};
    var locale = boot.locale || 'en';
    var defaultSection = boot.payment_page === true ? 'billing' : 'overview';
    var captureURL = typeof boot.capture_url === 'string' && boot.capture_url.indexOf(base + '/static/') === 0 ? boot.capture_url : '';
    var app = document.getElementById('account-app');
    var baseTitle = document.title || '';
    var state = {
      userId: '', session: null, section: '', features: { aiContext: false, voice: false },
      usage: null, usageStatus: 'idle', usageAt: 0, usageUser: '', usageEpoch: 0,
      context: null, saved: null, draft: null, contextStatus: 'idle', contextEpoch: 0,
      saving: false, saveError: '', conflict: false, unauthorized: false, accountChanged: false, fieldErrors: {},
      consentGiven: false, voiceUnavailable: false, chartIndex: -1, chartWidth: 0
    };
    var voice = { phase: 'idle', mic: null, recorder: null, job: null, startedAt: 0, tick: null, limit: null };
    var consent = { pending: null, resolve: null, opener: null, busy: false };
    var n = {};
    ['overview', 'profile', 'recipients', 'billing', 'apps', 'greeting', 'tile-available', 'tile-available-note', 'tile-plan', 'tile-plan-note',
      'tile-used', 'tile-used-note', 'chart', 'chart-status', 'chart-retry', 'chart-table', 'platforms', 'platforms-empty', 'cost-card',
      'cost-value', 'cost-note', 'profile-status', 'profile-retry', 'profile-editor', 'recipients-status', 'recipients-retry',
      'recipients-editor', 'ai-enabled', 'cost-line', 'saved-at', 'roles-list', 'roles-count', 'roles-error', 'traits-list',
      'traits-count', 'traits-error', 'occupation', 'occupation-count', 'occupation-error', 'about', 'about-count', 'about-error',
      'audience', 'audience-count', 'audience-error', 'savebar', 'save-message', 'discard', 'reload-context', 'relogin', 'reload-page', 'save',
      'live', 'consent', 'consent-terms', 'consent-privacy', 'consent-error', 'consent-cancel', 'consent-accept'].forEach(function (id) {
      n[id] = document.getElementById('cabinet-' + id);
    });
    var links = toArray(document.querySelectorAll('[data-section-link]'));
    var featureNodes = toArray(document.querySelectorAll('[data-feature]'));
    var headerSignIn = toArray(document.querySelectorAll('[data-header-sign-in]'));
    var emailNodes = toArray(document.querySelectorAll('[data-cabinet-email]'));
    var fields = TEXT_FIELDS.map(function (name) {
      return { name: name, section: 'profile', limit: name, node: n[name], count: n[name + '-count'], error: n[name + '-error'],
        read: function (doc) { return doc[name]; }, write: function (doc, value) { doc[name] = value; } };
    }).concat(RECIPIENTS.map(function (key) {
      var id = 'cabinet-notes-' + key;
      return { name: 'recipients.' + key + '.notes', section: 'recipients', recipient: key, limit: 'notes', node: document.getElementById(id),
        count: document.getElementById(id + '-count'), error: document.getElementById(id + '-error'), cost: document.getElementById(id + '-cost'),
        read: function (doc) { return doc.recipients[key].notes; }, write: function (doc, value) { doc.recipients[key].notes = value; } };
    }));
    var chips = {
      roles: { list: n['roles-list'], count: n['roles-count'], error: n['roles-error'], prefix: 'role.', inputs: [] },
      traits: { list: n['traits-list'], count: n['traits-count'], error: n['traits-error'], prefix: 'trait.', inputs: [] }
    };
    var mics = [];

    function toArray(list) { return Array.prototype.slice.call(list || []); }
    function t(key, values) {
      var result = words[key] == null ? '' : String(words[key]);
      Object.keys(values || {}).forEach(function (name) { result = result.split('{' + name + '}').join(String(values[name])); });
      return result;
    }
    function setText(node, value) { if (node) node.textContent = String(value == null ? '' : value); }
    function show(node, visible) { if (node) node.hidden = !visible; }
    function say(message) {
      if (!n.live || !message) return;
      // A repeated message must still be announced, so it is never identical.
      n.live.textContent = n.live.textContent === message ? message + ' ' : message;
    }
    // Kazakh and Uzbek group digits with spaces, like Russian; using ru-RU for
    // them gives the same result where a browser lacks kk/uz number data.
    function formatNumber(value) {
      try { return new Intl.NumberFormat(locale === 'en' ? 'en-GB' : 'ru-RU').format(value); } catch (_) { return String(value); }
    }
    function pad(value) { return String(value).padStart(2, '0'); }
    // Month names in the page's language; numeric dates where the browser only
    // has root data for it (some builds print kk/uz months as "M11").
    function formatWith(date, options, numeric) {
      try {
        var format = new Intl.DateTimeFormat({ ru: 'ru-RU', kk: 'kk-KZ', uz: 'uz-Latn-UZ' }[locale] || 'en-GB', options);
        if (!/M\d/.test(format.format(new Date(Date.UTC(2026, 10, 17, 12))))) return format.format(date);
      } catch (_) { /* numeric below */ }
      return numeric();
    }
    function formatDay(iso) {
      var parts = /^(\d{4})-(\d{2})-(\d{2})$/.exec(iso || '');
      if (!parts) return '';
      var date = new Date(Date.UTC(Number(parts[1]), Number(parts[2]) - 1, Number(parts[3])));
      return formatWith(date, { day: 'numeric', month: 'short', timeZone: 'UTC' }, function () { return parts[3] + '.' + parts[2]; });
    }
    function formatDate(value, withTime) {
      var date = new Date(value);
      if (!value || isNaN(date.getTime())) return '';
      var options = { day: 'numeric', month: 'long', year: 'numeric' };
      if (withTime) { options.hour = '2-digit'; options.minute = '2-digit'; }
      return formatWith(date, options, function () {
        var day = pad(date.getDate()) + '.' + pad(date.getMonth() + 1) + '.' + date.getFullYear();
        return withTime ? day + ' ' + pad(date.getHours()) + ':' + pad(date.getMinutes()) : day;
      });
    }
    function motion() {
      try { return browser.matchMedia && browser.matchMedia('(prefers-reduced-motion: reduce)').matches ? 'auto' : 'smooth'; } catch (_) { return 'auto'; }
    }
    function httpError(code, status, details) {
      var error = new Error(code);
      error.status = status;
      error.details = details || {};
      return error;
    }
    function api(path, options) {
      options = options || {};
      var headers = { Accept: 'application/json' };
      var init = { method: 'GET', credentials: 'same-origin', cache: 'no-store', headers: headers };
      if (options.json !== undefined) {
        init.method = 'POST';
        headers['Content-Type'] = 'application/json';
        headers['X-Requested-With'] = 'AIReplyAccount';
        init.body = JSON.stringify(options.json);
      } else if (options.form) {
        // multipart: the browser sets Content-Type with its boundary.
        init.method = 'POST';
        headers['X-Requested-With'] = 'AIReplyAccount';
        init.body = options.form;
      }
      var controller = browser.AbortController ? new browser.AbortController() : null;
      if (controller) init.signal = controller.signal;
      var timer;
      var timeout = new Promise(function (_, reject) {
        timer = browser.setTimeout(function () {
          if (controller) controller.abort();
          reject(httpError('REQUEST_TIMEOUT', 0));
        }, options.timeout || REQUEST_TIMEOUT);
      });
      var request = Promise.resolve().then(function () { return browser.fetch(apiBase + path, init); }).then(function (response) {
        return response.json().catch(function () { return {}; }).then(function (payload) {
          if (response.ok) return payload || {};
          var error = payload && payload.error || {};
          throw httpError(error.code || 'NETWORK_ERROR', response.status, error.details);
        });
      });
      return Promise.race([request, timeout]).then(function (payload) {
        browser.clearTimeout(timer); return payload;
      }, function (error) {
        browser.clearTimeout(timer); throw error;
      });
    }
    function signalSignOut() {
      if (typeof document.dispatchEvent === 'function' && typeof browser.CustomEvent === 'function') {
        document.dispatchEvent(new browser.CustomEvent('aireply:unauthorized'));
      }
    }
    // An ended session signs out through account.js — unless unsaved text is
    // on screen: then the save bar says so and the person decides when.
    function unauthorized() {
      if (isDirty()) {
        state.unauthorized = true;
        refreshEditor();
        say(t('error.session'));
        return;
      }
      signalSignOut();
    }
    // 409 WEB_ACCOUNT_CHANGED: this browser is now signed in to another
    // account (another tab), so the write named the wrong one and nothing was
    // saved. Saving and dictation stop and the save bar offers a reload;
    // unsaved text stays on screen until then.
    // A read answered for another account than the page shows: the browser was
    // signed in elsewhere since this page loaded. Its data is never adopted.
    function otherAccount(payload, user) {
      return !!payload && typeof payload === 'object' && typeof payload.user_id === 'string' && payload.user_id !== '' && payload.user_id !== user;
    }
    // Nothing typed here: reload to show the account that is signed in now.
    // Unsaved text: keep it on screen, block saving and offer the reload.
    function anotherAccountSignedIn() {
      if (isDirty()) { accountChanged(true); return; }
      browser.location.reload();
    }
    function isAccountChanged(error) { return !!error && error.status === 409 && error.message === 'WEB_ACCOUNT_CHANGED'; }
    function accountChanged(announce) {
      state.accountChanged = true;
      refreshEditor();
      renderMics();
      if (announce !== false) say(t('account_changed'));
    }

    /* ---------------------------------------------------------- navigation */

    function availableSections() {
      return SECTIONS.filter(function (name) { return CONTEXT_SECTIONS.indexOf(name) === -1 || state.features.aiContext; });
    }
    function route(fromUser) {
      var next = sectionFromHash(browser.location.hash, availableSections(), defaultSection);
      var changed = next !== state.section;
      if (changed && (voice.phase === 'recording' || voice.phase === 'starting')) cancelVoice('voice.cancelled');
      state.section = next;
      SECTIONS.forEach(function (name) { show(n[name], name === next); });
      links.forEach(function (link) {
        if (link.getAttribute('data-section-link') === next) link.setAttribute('aria-current', 'page');
        else link.removeAttribute('aria-current');
      });
      if (state.session) document.title = t('section.' + next) + ' — ' + baseTitle;
      if (next === 'overview') renderChart(false);
      if (fromUser && changed) reveal(next);
    }
    function reveal(name) {
      var link = links.filter(function (item) { return item.getAttribute('data-section-link') === name; })[0];
      try { if (link && link.scrollIntoView) link.scrollIntoView({ block: 'nearest', inline: 'nearest', behavior: motion() }); } catch (_) { /* old browsers */ }
      var panel = n[name];
      var heading = document.getElementById('cabinet-' + name + '-title');
      try {
        // Its top may be hidden under the sticky header (and the phone's tab strip).
        var margin = browser.getComputedStyle ? parseFloat(browser.getComputedStyle(panel).scrollMarginTop) || 0 : 0;
        if (panel && panel.getBoundingClientRect && panel.getBoundingClientRect().top < margin) panel.scrollIntoView({ block: 'start', behavior: motion() });
      } catch (_) { /* old browsers */ }
      if (heading && heading.focus) heading.focus({ preventScroll: true });
    }

    /* ---------------------------------------------------------- session */

    function onSession(payload) {
      if (!payload || !payload.user || typeof payload.user.id !== 'string' || !payload.user.id) return;
      if (state.userId && state.userId !== payload.user.id) resetAccountData();
      state.userId = payload.user.id;
      state.session = payload;
      var features = payload.features || {};
      state.features = { aiContext: features.ai_context === true, voice: features.voice_transcription === true };
      if (app) app.setAttribute('data-state', 'signed-in');
      headerSignIn.forEach(function (node) { node.hidden = true; });
      emailNodes.forEach(function (node) { node.textContent = payload.user.email || t('email_unavailable'); });
      featureNodes.forEach(function (node) { node.hidden = node.getAttribute('data-feature') === 'ai_context' && !state.features.aiContext; });
      route(false);
      if (state.features.aiContext && state.contextStatus === 'idle') loadContext();
      if (state.usageUser !== state.userId || state.usageStatus === 'idle' || state.usageStatus === 'ready' && browser.now() - state.usageAt > USAGE_TTL) loadUsage();
      renderAll();
    }
    function onSignedOut() {
      cancelVoice('');
      if (consent.pending) closeConsent(false);
      state.userId = '';
      state.session = null;
      state.features = { aiContext: false, voice: false };
      resetAccountData();
      if (app) app.setAttribute('data-state', 'signed-out');
      headerSignIn.forEach(function (node) { node.hidden = false; });
      emailNodes.forEach(function (node) { node.textContent = ''; });
      featureNodes.forEach(function (node) { node.hidden = node.getAttribute('data-feature') === 'ai_context'; });
      document.title = baseTitle;
    }
    // Nothing of one account may remain on screen or in memory for the next.
    function resetAccountData() {
      cancelVoice('');
      // A transcription still on its way belongs to the previous account.
      if (voice.phase === 'processing') resetVoice();
      // So does an open consent dialog.
      if (consent.pending) closeConsent(false);
      state.contextEpoch += 1;
      state.usageEpoch += 1;
      state.context = null; state.saved = null; state.draft = null; state.contextStatus = 'idle';
      state.saving = false; state.saveError = ''; state.conflict = false; state.unauthorized = false; state.accountChanged = false; state.fieldErrors = {};
      state.usage = null; state.usageStatus = 'idle'; state.usageAt = 0; state.usageUser = '';
      state.consentGiven = false; state.voiceUnavailable = false; state.chartIndex = -1;
      fields.forEach(function (field) { if (field.node) field.node.value = ''; });
      if (n['ai-enabled']) n['ai-enabled'].checked = false;
      Object.keys(chips).forEach(function (kind) { if (chips[kind].list) chips[kind].list.replaceChildren(); chips[kind].inputs = []; });
      mics.forEach(function (mic) { setText(mic.status, ''); });
      renderAll();
    }
    function renderAll() {
      renderOverview();
      renderUsage();
      renderContextStatus();
      refreshEditor();
      renderMics();
    }

    /* ---------------------------------------------------------- overview */

    function renderOverview() {
      var session = state.session;
      if (!session) {
        setText(n.greeting, '');
        ['tile-available', 'tile-plan', 'tile-used'].forEach(function (id) { setText(n[id], '—'); });
        ['tile-available-note', 'tile-plan-note', 'tile-used-note'].forEach(function (id) { setText(n[id], ''); });
        return;
      }
      var usage = session.usage || {};
      setText(n.greeting, t('overview.signed_in_as', { email: session.user.email || t('email_unavailable') }));
      if (usage.unlimited === true) {
        setText(n['tile-available'], '∞');
        setText(n['tile-available-note'], t('unlimited'));
      } else {
        setText(n['tile-available'], formatNumber(wholeNumber(usage.remaining_total)));
        setText(n['tile-available-note'], '');
      }
      var subscription = session.subscription || {};
      var plan = subscription.plan || {};
      var planName = plan.name && (plan.name[locale] || plan.name.en) || plan.code || '';
      setText(n['tile-plan'], planName || '—');
      var expires = formatDate(subscription.expires_at, false);
      setText(n['tile-plan-note'], expires ? t(subscription.auto_renews === true ? 'overview.renews' : 'overview.valid_until', { date: expires }) : '');
      if (state.usageStatus === 'ready') {
        setText(n['tile-used'], formatNumber(state.usage.total_replies));
        setText(n['tile-used-note'], state.usage.total_units !== state.usage.total_replies ? t('overview.used_units', { count: formatNumber(state.usage.total_units) }) : '');
      } else {
        setText(n['tile-used'], state.usageStatus === 'loading' ? '…' : '—');
        setText(n['tile-used-note'], '');
      }
      renderCostCard();
    }
    function currentPricing() {
      var pricing = state.context && state.context.pricing;
      if (!pricing) return null;
      var usage = state.session && state.session.usage || {};
      return { chars_per_reply: pricing.chars_per_reply, free_chars: pricing.free_chars, max_extra_replies: pricing.max_extra_replies,
        unlimited: pricing.unlimited === true || usage.unlimited === true };
    }
    // Live units when the server sent its pricing; its own numbers otherwise.
    function costFor(doc, key) {
      var pricing = currentPricing();
      if (pricing) return contextUnits(doc, pricing, key === 'general' ? '' : key);
      var units = Number(state.context && state.context.cost && state.context.cost[key]);
      return Number.isSafeInteger(units) && units > 0 ? units : 1;
    }
    function isUnlimited() {
      var pricing = currentPricing();
      return !!(pricing && pricing.unlimited) || !!(state.session && state.session.usage && state.session.usage.unlimited === true);
    }
    function renderCostCard() {
      show(n['cost-card'], state.features.aiContext && !!state.session);
      if (!state.features.aiContext) return;
      if (!state.saved) {
        setText(n['cost-value'], '…');
        setText(n['cost-note'], state.contextStatus === 'error' ? t('profile.load_error') : t('overview.loading'));
        return;
      }
      if (state.saved.enabled === false) {
        setText(n['cost-value'], t('cost.value', { count: 1 }));
        setText(n['cost-note'], t('cost.disabled'));
      } else if (isUnlimited()) {
        setText(n['cost-value'], t('cost.value', { count: 1 }));
        setText(n['cost-note'], t('cost.unlimited'));
      } else {
        setText(n['cost-value'], t('cost.value', { count: costFor(state.saved, 'general') }));
        setText(n['cost-note'], t('cost.note'));
      }
    }

    function acceptUsage(payload) {
      if (!payload || !Array.isArray(payload.days)) return null;
      var days = payload.days.map(function (day) {
        day = day || {};
        return { date: typeof day.date === 'string' && /^\d{4}-\d{2}-\d{2}$/.test(day.date) ? day.date : '', replies: wholeNumber(day.replies), units: wholeNumber(day.units) };
      }).filter(function (day) { return day.date; });
      var platforms = (Array.isArray(payload.platforms) ? payload.platforms : []).map(function (item) {
        item = item || {};
        return { platform: typeof item.platform === 'string' ? item.platform.slice(0, 40) : '', replies: wholeNumber(item.replies) };
      }).filter(function (item) { return item.platform && item.replies > 0; }).sort(function (a, b) { return b.replies - a.replies; });
      var sum = function (key) { return days.reduce(function (total, day) { return total + day[key]; }, 0); };
      return { days: days, platforms: platforms,
        total_replies: Number.isSafeInteger(payload.total_replies) && payload.total_replies >= 0 ? payload.total_replies : sum('replies'),
        total_units: Number.isSafeInteger(payload.total_units) && payload.total_units >= 0 ? payload.total_units : sum('units') };
    }
    function loadUsage() {
      if (!state.userId) return Promise.resolve();
      var user = state.userId, epoch = ++state.usageEpoch;
      state.usageStatus = 'loading';
      state.usageUser = user;
      renderUsage();
      return api('/usage?days=30').then(function (payload) {
        if (user !== state.userId || epoch !== state.usageEpoch) return;
        if (otherAccount(payload, user)) { state.usageStatus = 'error'; anotherAccountSignedIn(); return; }
        var usage = acceptUsage(payload);
        if (!usage) throw httpError('INVALID_RESPONSE', 0);
        state.usage = usage;
        state.usageStatus = 'ready';
        state.usageAt = browser.now();
      }).catch(function (error) {
        if (user !== state.userId || epoch !== state.usageEpoch) return;
        if (error.status === 401) { state.usageStatus = 'error'; unauthorized(); return; }
        state.usageStatus = error.status === 404 ? 'unavailable' : 'error';
      }).then(function () {
        if (user === state.userId && epoch === state.usageEpoch) { renderUsage(); renderOverview(); }
      });
    }
    function renderUsage() {
      var status = state.usageStatus;
      var empty = status === 'ready' && state.usage.days.every(function (day) { return day.units === 0 && day.replies === 0; });
      setText(n['chart-status'], status === 'loading' ? t('overview.loading') : status === 'error' ? t('chart.error')
        : status === 'unavailable' ? t('chart.unavailable') : empty ? t('chart.empty') : '');
      show(n['chart-status'], !!n['chart-status'] && n['chart-status'].textContent !== '');
      show(n['chart-retry'], status === 'error' && !!state.userId);
      renderPlatforms();
      renderChart(true);
    }
    function renderPlatforms() {
      if (!n.platforms) return;
      n.platforms.replaceChildren();
      var status = state.usageStatus;
      var items = status === 'ready' ? state.usage.platforms : [];
      setText(n['platforms-empty'], status === 'loading' ? t('overview.loading') : status === 'error' ? t('chart.error')
        : status === 'unavailable' ? t('chart.unavailable') : t('platforms.empty'));
      show(n['platforms-empty'], status !== 'idle' && items.length === 0);
      var max = items.reduce(function (top, item) { return Math.max(top, item.replies); }, 1);
      items.forEach(function (item) {
        var row = document.createElement('li');
        var name = document.createElement('span');
        name.className = 'cabinet-meter-name';
        var code = item.platform.toLowerCase();
        // The server reports any platform it does not know as "other".
        name.textContent = code === 'web' ? t('platform.web') : code === 'other' ? t('platform.other') : PLATFORM_NAMES[code] || item.platform;
        var track = document.createElement('span');
        track.className = 'cabinet-meter-track';
        track.setAttribute('aria-hidden', 'true');
        var fill = document.createElement('span');
        fill.className = 'cabinet-meter-fill';
        fill.style.width = Math.max(2, Math.round(item.replies / max * 100)) + '%';
        track.append(fill);
        var value = document.createElement('span');
        value.className = 'cabinet-meter-value';
        value.textContent = formatNumber(item.replies);
        row.append(name, track, value);
        n.platforms.append(row);
      });
    }

    function svg(tag, attributes, parent) {
      var node = document.createElementNS(SVG_NS, tag);
      Object.keys(attributes || {}).forEach(function (name) { node.setAttribute(name, String(attributes[name])); });
      if (parent) parent.appendChild(node);
      return node;
    }
    function fixed(value) { return Math.round(value * 100) / 100; }
    function barPath(x, y, width, height) {
      var r = Math.min(4, width / 2, height);
      return 'M' + fixed(x) + ',' + fixed(y + height) + 'V' + fixed(y + r) + 'A' + fixed(r) + ',' + fixed(r) + ' 0 0 1 ' + fixed(x + r) + ',' + fixed(y) +
        'H' + fixed(x + width - r) + 'A' + fixed(r) + ',' + fixed(r) + ' 0 0 1 ' + fixed(x + width) + ',' + fixed(y + r) + 'V' + fixed(y + height) + 'Z';
    }
    // Units per day: one series, today emphasised, a hairline grid, a
    // per-day tooltip on hover and arrow keys, and the same data as a table.
    function renderChart(force) {
      var host = n.chart;
      if (!host) return;
      var ready = state.usageStatus === 'ready' && state.usage.days.length > 0 &&
        !state.usage.days.every(function (day) { return day.units === 0 && day.replies === 0; });
      if (!ready) {
        host.replaceChildren();
        host.hidden = true;
        host.removeAttribute('tabindex');
        host.removeAttribute('role');
        host.removeAttribute('aria-label');
        if (n['chart-table']) { n['chart-table'].replaceChildren(); n['chart-table'].hidden = true; }
        state.chartWidth = 0;
        return;
      }
      if (state.section !== 'overview' && !force) return;
      var width = Math.max(280, Math.floor(host.clientWidth || (host.parentNode && host.parentNode.clientWidth) || 640));
      if (!force && width === state.chartWidth && host.firstChild) return;
      state.chartWidth = width;
      host.hidden = false;
      host.replaceChildren();
      var days = state.usage.days;
      var today = days.length - 1;
      var max = days.reduce(function (top, day) { return Math.max(top, day.units); }, 0);
      var ticks = niceTicks(max);
      var top = ticks[ticks.length - 1];
      var tickLabels = ticks.map(formatNumber);
      var compact = width < 480;
      var height = compact ? 180 : 220;
      var pad = { top: 14, right: 4, bottom: 26, left: 10 + 7 * tickLabels.reduce(function (m, label) { return Math.max(m, label.length); }, 1) };
      var plotWidth = width - pad.left - pad.right, plotHeight = height - pad.top - pad.bottom;
      var slot = plotWidth / days.length;
      var barWidth = Math.max(2, Math.min(24, slot - 2));
      var root = svg('svg', { width: width, height: height, viewBox: '0 0 ' + width + ' ' + height, role: 'img', focusable: 'false',
        'aria-labelledby': 'cabinet-chart-svg-title cabinet-chart-svg-desc' });
      var title = svg('title', { id: 'cabinet-chart-svg-title' }, root);
      title.textContent = t('chart.title');
      var desc = svg('desc', { id: 'cabinet-chart-svg-desc' }, root);
      desc.textContent = t('chart.summary', { replies: formatNumber(state.usage.total_replies), units: formatNumber(state.usage.total_units), today: formatNumber(days[today].units) });
      ticks.forEach(function (tick, index) {
        var y = fixed(pad.top + plotHeight - tick / top * plotHeight);
        svg('line', { x1: pad.left, x2: width - pad.right, y1: y, y2: y, 'class': tick === 0 ? 'cabinet-axis' : 'cabinet-grid' }, root);
        var label = svg('text', { x: pad.left - 8, y: y, 'class': 'cabinet-tick', 'text-anchor': 'end', 'dominant-baseline': 'middle' }, root);
        label.textContent = tickLabels[index];
      });
      var bars = days.map(function (day, index) {
        var x = pad.left + index * slot + (slot - barWidth) / 2;
        var barHeight = day.units > 0 ? Math.max(2, day.units / top * plotHeight) : 0;
        var bar = svg('path', { d: barHeight > 0 ? barPath(x, pad.top + plotHeight - barHeight, barWidth, barHeight) : 'M0,0', 'class': 'cabinet-bar' + (index === today ? ' is-today' : '') }, root);
        return bar;
      });
      var count = compact ? 3 : 5;
      var labelled = [];
      for (var i = 0; i < count; i++) {
        var position = Math.round(i * (days.length - 1) / Math.max(1, count - 1));
        if (labelled.indexOf(position) === -1) labelled.push(position);
      }
      labelled.forEach(function (index) {
        var anchor = index === 0 ? 'start' : index === today ? 'end' : 'middle';
        var x = index === 0 ? pad.left : index === today ? width - pad.right : pad.left + (index + 0.5) * slot;
        var label = svg('text', { x: fixed(x), y: height - 6, 'class': 'cabinet-xlabel' + (index === today ? ' is-today' : ''), 'text-anchor': anchor }, root);
        label.textContent = index === today ? t('chart.today') : formatDay(days[index].date);
      });
      svg('rect', { x: pad.left, y: pad.top, width: fixed(plotWidth), height: fixed(plotHeight), 'class': 'cabinet-hit' }, root);
      var tip = document.createElement('div');
      tip.className = 'cabinet-chart-tip';
      tip.id = 'cabinet-chart-tip';
      tip.setAttribute('role', 'status');
      tip.hidden = true;
      host.append(root, tip);
      // Focusable for the arrow keys: a named group, so its label is read.
      host.setAttribute('tabindex', '0');
      host.setAttribute('role', 'group');
      host.setAttribute('aria-label', t('chart.title') + '. ' + t('chart.keyboard_hint'));
      function showTip(index) {
        if (index < 0 || index >= days.length) { hideTip(); return; }
        if (state.chartIndex >= 0 && bars[state.chartIndex]) bars[state.chartIndex].setAttribute('class', 'cabinet-bar' + (state.chartIndex === today ? ' is-today' : ''));
        state.chartIndex = index;
        bars[index].setAttribute('class', 'cabinet-bar is-active' + (index === today ? ' is-today' : ''));
        var day = days[index];
        var date = document.createElement('span');
        date.className = 'cabinet-chart-tip-date';
        date.textContent = (index === today ? t('chart.today') + ', ' : '') + formatDay(day.date);
        var units = document.createElement('strong');
        units.textContent = t('chart.units', { count: formatNumber(day.units) });
        var replies = document.createElement('span');
        replies.textContent = t('chart.replies', { count: formatNumber(day.replies) });
        tip.replaceChildren(date, units, replies);
        var center = (pad.left + (index + 0.5) * slot) / width * 100;
        tip.style.left = fixed(center) + '%';
        tip.setAttribute('data-align', index < 3 ? 'start' : index > days.length - 4 ? 'end' : 'middle');
        tip.hidden = false;
      }
      function hideTip() {
        if (state.chartIndex >= 0 && bars[state.chartIndex]) bars[state.chartIndex].setAttribute('class', 'cabinet-bar' + (state.chartIndex === today ? ' is-today' : ''));
        state.chartIndex = -1;
        tip.hidden = true;
      }
      function indexAt(event) {
        var rect = root.getBoundingClientRect ? root.getBoundingClientRect() : { left: 0, width: width };
        var x = (event.clientX - rect.left) * (width / (rect.width || width));
        return Math.floor((x - pad.left) / slot);
      }
      root.addEventListener('pointermove', function (event) { showTip(indexAt(event)); });
      root.addEventListener('pointerdown', function (event) { showTip(indexAt(event)); });
      root.addEventListener('pointerleave', hideTip);
      host.onkeydown = function (event) {
        var index = state.chartIndex < 0 ? today : state.chartIndex;
        if (event.key === 'ArrowLeft') index = Math.max(0, index - 1);
        else if (event.key === 'ArrowRight') index = Math.min(today, index + 1);
        else if (event.key === 'Home') index = 0;
        else if (event.key === 'End') index = today;
        else if (event.key === 'Escape') { hideTip(); return; }
        else return;
        event.preventDefault();
        showTip(index);
      };
      host.onfocus = function () { showTip(state.chartIndex < 0 ? today : state.chartIndex); };
      host.onblur = hideTip;
      renderChartTable(days);
    }
    function renderChartTable(days) {
      var table = n['chart-table'];
      if (!table) return;
      table.replaceChildren();
      table.hidden = false;
      var caption = document.createElement('caption');
      caption.textContent = t('chart.title');
      var head = document.createElement('thead');
      var headRow = document.createElement('tr');
      ['chart.date', 'chart.col_replies', 'chart.col_units'].forEach(function (key) {
        var cell = document.createElement('th');
        cell.setAttribute('scope', 'col');
        cell.textContent = t(key);
        headRow.append(cell);
      });
      head.append(headRow);
      var body = document.createElement('tbody');
      days.forEach(function (day) {
        var row = document.createElement('tr');
        var date = document.createElement('th');
        date.setAttribute('scope', 'row');
        date.textContent = formatDay(day.date);
        var replies = document.createElement('td');
        replies.textContent = formatNumber(day.replies);
        var units = document.createElement('td');
        units.textContent = formatNumber(day.units);
        row.append(date, replies, units);
        body.append(row);
      });
      table.append(caption, head, body);
    }

    /* ---------------------------------------------------------- AI profile */

    function limit(name) {
      var value = Number(state.context && state.context.limits && state.context.limits[name]);
      return Number.isSafeInteger(value) && value > 0 ? value : DEFAULT_LIMITS[name];
    }
    function validPricing(pricing) {
      if (!pricing || typeof pricing !== 'object') return null;
      return { chars_per_reply: wholeNumber(pricing.chars_per_reply), free_chars: wholeNumber(pricing.free_chars),
        max_extra_replies: wholeNumber(pricing.max_extra_replies), unlimited: pricing.unlimited === true };
    }
    function cloneDocument(doc) { return JSON.parse(JSON.stringify(normalizeDocument(doc))); }
    // Adopts a server response. Keystrokes typed while a save was in flight
    // are kept: the draft is replaced only if it still equals what was sent.
    function acceptContext(response, sent) {
      if (!response || typeof response !== 'object' || !response.context || typeof response.context !== 'object') return false;
      var catalog = response.catalog || {};
      state.context = {
        revision: Number.isSafeInteger(response.revision) && response.revision >= 0 ? response.revision : 0,
        updatedAt: typeof response.updated_at === 'string' ? response.updated_at : '',
        limits: response.limits && typeof response.limits === 'object' ? response.limits : {},
        catalog: { roles: codeList(catalog.roles), traits: codeList(catalog.traits) },
        pricing: validPricing(response.pricing),
        cost: response.cost && typeof response.cost === 'object' ? response.cost : {}
      };
      state.saved = normalizeDocument(response.context);
      if (!sent || !state.draft || sameDocument(state.draft, sent)) {
        state.draft = cloneDocument(state.saved);
        fillForm();
      }
      return true;
    }
    function loadContext() {
      if (!state.features.aiContext || !state.userId) return Promise.resolve();
      var user = state.userId, epoch = ++state.contextEpoch;
      state.contextStatus = 'loading';
      state.saving = false;
      renderContextStatus();
      return api('/ai-context').then(function (response) {
        if (user !== state.userId || epoch !== state.contextEpoch) return;
        if (otherAccount(response, user)) { state.contextStatus = state.saved ? 'ready' : 'error'; anotherAccountSignedIn(); return; }
        state.draft = null;
        if (!acceptContext(response)) throw httpError('INVALID_RESPONSE', 0);
        state.contextStatus = 'ready';
        state.conflict = false; state.saveError = ''; state.unauthorized = false; state.fieldErrors = {};
      }).catch(function (error) {
        if (user !== state.userId || epoch !== state.contextEpoch) return;
        state.contextStatus = 'error';
        if (error.status === 401) unauthorized();
      }).then(function () {
        if (user === state.userId && epoch === state.contextEpoch) renderAll();
      });
    }
    function renderContextStatus() {
      var status = state.contextStatus;
      ['profile', 'recipients'].forEach(function (name) {
        show(n[name + '-editor'], status === 'ready');
        setText(n[name + '-status'], status === 'loading' ? t('profile.loading') : status === 'error' ? t('profile.load_error') : '');
        show(n[name + '-status'], status === 'loading' || status === 'error');
        show(n[name + '-retry'], status === 'error');
      });
    }
    function fillForm() {
      if (!state.draft) return;
      fields.forEach(function (field) {
        if (!field.node) return;
        var value = field.read(state.draft) || '';
        // Keep the person's own spacing when it means the same text.
        if (normalizeText(field.node.value) !== normalizeText(value)) field.node.value = value;
      });
      if (n['ai-enabled']) n['ai-enabled'].checked = state.draft.enabled !== false;
      renderChips('roles');
      renderChips('traits');
    }
    function renderChips(kind) {
      var group = chips[kind];
      if (!group.list || !state.context || !state.draft) return;
      group.list.replaceChildren();
      group.inputs = [];
      // A saved code the catalog no longer lists stays visible, so it can be removed.
      var codes = state.context.catalog[kind].concat(state.draft[kind].filter(function (code) { return state.context.catalog[kind].indexOf(code) === -1; }));
      codes.forEach(function (code) {
        var label = document.createElement('label');
        label.className = 'cabinet-chip';
        var input = document.createElement('input');
        input.type = 'checkbox';
        input.name = 'cabinet-' + kind;
        input.value = code;
        input.checked = state.draft[kind].indexOf(code) !== -1;
        input.addEventListener('change', function () { toggleCode(kind, code, input); });
        var text = document.createElement('span');
        text.textContent = chipLabel(kind, code);
        label.append(input, text);
        group.list.append(label);
        group.inputs.push(input);
      });
    }
    // The selected opposite of a style chip ("" — none), which choosing it replaces.
    function selectedOpposite(kind, code) {
      var opposite = kind === 'traits' ? oppositeTrait(code) : '';
      return opposite && state.draft && state.draft.traits.indexOf(opposite) !== -1 ? opposite : '';
    }
    function chipLabel(kind, code) { return words[chips[kind].prefix + code] || code.replace(/_/g, ' '); }
    function toggleCode(kind, code, input) {
      if (!state.draft) return;
      var list = state.draft[kind];
      var index = list.indexOf(code);
      if (input.checked && index === -1) {
        var opposite = selectedOpposite(kind, code);
        if (!opposite && list.length >= limit(kind)) {
          input.checked = false;
          say(t('field.too_many', { max: limit(kind) }));
          return;
        }
        if (opposite) list.splice(list.indexOf(opposite), 1);
        list.push(code);
        // The other chip unchecks itself: say so.
        if (opposite) say(t('profile.trait_replaced', { previous: chipLabel(kind, opposite), current: chipLabel(kind, code) }));
      } else if (!input.checked && index !== -1) {
        list.splice(index, 1);
      }
      delete state.fieldErrors[kind];
      refreshEditor();
    }
    function localError(field) {
      if (!state.draft || !field.node) return '';
      var count = chars(field.read(state.draft));
      return count > limit(field.limit) ? t('field.too_long', { max: formatNumber(limit(field.limit)) }) : '';
    }
    function hasLocalErrors() {
      if (!state.draft) return false;
      return fields.some(function (field) { return !!localError(field); }) ||
        state.draft.roles.length > limit('roles') || state.draft.traits.length > limit('traits');
    }
    function isDirty() { return !!(state.draft && state.saved) && !sameDocument(state.draft, state.saved); }
    function setFieldError(node, errorNode, message) {
      setText(errorNode, message);
      show(errorNode, !!message);
      if (!node) return;
      if (message) node.setAttribute('aria-invalid', 'true');
      else node.removeAttribute('aria-invalid');
    }
    // Counters, errors, chips, costs and the save bar, after every change.
    function refreshEditor() {
      var ready = !!(state.draft && state.context);
      fields.forEach(function (field) {
        if (!ready) { setText(field.count, ''); setFieldError(field.node, field.error, ''); setText(field.cost, ''); return; }
        var count = chars(field.read(state.draft)), max = limit(field.limit);
        setText(field.count, t('field.counter', { count: formatNumber(count), max: formatNumber(max) }));
        if (field.count && field.count.classList) field.count.classList.toggle('is-over', count > max);
        setFieldError(field.node, field.error, localError(field) || state.fieldErrors[field.name] || '');
      });
      Object.keys(chips).forEach(function (kind) {
        var group = chips[kind];
        if (!ready) { setText(group.count, ''); setFieldError(null, group.error, ''); return; }
        var selected = state.draft[kind].length, max = limit(kind);
        group.inputs.forEach(function (input) {
          input.checked = state.draft[kind].indexOf(input.value) !== -1;
          // At the limit a chip stays available when it only swaps its opposite.
          input.disabled = !input.checked && selected >= max && !selectedOpposite(kind, input.value);
        });
        setText(group.count, t('profile.selected', { count: selected, max: max }));
        setFieldError(null, group.error, selected > max ? t('field.too_many', { max: max }) : state.fieldErrors[kind] || '');
      });
      renderCosts();
      renderSaveBar();
      renderCostCard();
      if (n['profile-editor']) n['profile-editor'].setAttribute('aria-busy', String(state.saving));
      if (n['recipients-editor']) n['recipients-editor'].setAttribute('aria-busy', String(state.saving));
    }
    function renderCosts() {
      if (!state.draft || !state.context) { setText(n['cost-line'], ''); setText(n['saved-at'], ''); return; }
      var enabled = state.draft.enabled !== false, unlimited = isUnlimited();
      setText(n['cost-line'], !enabled ? t('profile.cost_disabled') : unlimited ? t('profile.cost_unlimited')
        : t('profile.cost_line', { count: costFor(state.draft, 'general') }));
      if (n['cost-line'] && n['cost-line'].classList) n['cost-line'].classList.toggle('is-extra', enabled && !unlimited && costFor(state.draft, 'general') > 1);
      fields.forEach(function (field) {
        if (!field.recipient || !field.cost) return;
        field.cost.hidden = !enabled;
        setText(field.cost, unlimited ? t('recipients.cost_unlimited') : t('recipients.cost', { count: costFor(state.draft, field.recipient) }));
      });
      var saved = formatDate(state.context.updatedAt, true);
      setText(n['saved-at'], saved ? t('profile.saved_at', { date: saved }) : '');
    }
    function renderSaveBar() {
      if (!n.savebar) return;
      var dirty = isDirty();
      var invalid = hasLocalErrors() || Object.keys(state.fieldErrors).length > 0;
      var visible = state.contextStatus === 'ready' && (dirty || state.saving || state.conflict || state.unauthorized || state.accountChanged || !!state.saveError);
      show(n.savebar, visible);
      var blocked = state.conflict || state.unauthorized || state.accountChanged;
      var message = state.saving ? t('save.saving') : state.accountChanged ? t('account_changed')
        : state.conflict ? t('save.conflict') + ' ' + t('save.conflict_hint')
        : state.unauthorized ? t('save.session') : state.saveError ? state.saveError : invalid ? t('save.invalid') : dirty ? t('save.unsaved') : '';
      setText(n['save-message'], message);
      if (n.savebar.classList) n.savebar.classList.toggle('is-error', blocked || !!state.saveError || invalid);
      if (n.save) {
        n.save.hidden = blocked;
        n.save.disabled = state.saving || !dirty || invalid;
      }
      show(n.discard, dirty && !state.saving && !blocked);
      show(n['reload-context'], state.conflict && !state.accountChanged);
      show(n.relogin, state.unauthorized && !state.accountChanged);
      show(n['reload-page'], state.accountChanged);
    }
    function save() {
      if (!state.draft || state.saving || state.conflict || state.unauthorized || state.accountChanged || !isDirty() || hasLocalErrors()) return Promise.resolve();
      var user = state.userId, epoch = state.contextEpoch;
      var body = normalizeDocument(state.draft);
      state.saving = true;
      state.saveError = '';
      state.fieldErrors = {};
      refreshEditor();
      return api('/ai-context', { json: { context: body, expected_revision: state.context.revision, expected_user_id: user } }).then(function (response) {
        if (user !== state.userId || epoch !== state.contextEpoch) return;
        if (!acceptContext(response, body)) throw httpError('INVALID_RESPONSE', 0);
        say(t('save.saved'));
      }).catch(function (error) {
        if (user !== state.userId || epoch !== state.contextEpoch) return;
        if (isAccountChanged(error)) {
          // This page shows another account than the browser's session now.
          accountChanged();
        } else if (error.status === 409) {
          // Another device saved first: nothing was saved here.
          state.conflict = true;
          say(t('save.conflict'));
        } else if (error.status === 400 && serverFieldError(error.details)) {
          say(t('save.invalid'));
        } else if (error.status === 400) {
          state.saveError = t('save.rejected');
          say(state.saveError);
        } else if (error.status === 401) {
          state.unauthorized = true;
          say(t('save.session'));
        } else {
          state.saveError = error.message === 'RATE_LIMITED' ? t('error.RATE_LIMITED') : t('save.error');
          say(state.saveError);
        }
      }).then(function () {
        if (user !== state.userId || epoch !== state.contextEpoch) return;
        state.saving = false;
        refreshEditor();
      });
    }
    // details: {"field", "reason", "max"} (§5.2) → the message next to that field.
    function serverFieldError(details) {
      details = details || {};
      var field = String(details.field || '').replace(/^context\./, '');
      var recipient = /^recipients(?:\.|\[)?(friend|client|colleague|partner)/.exec(field);
      var name = recipient ? 'recipients.' + recipient[1] + '.notes' : field;
      var known = name === 'roles' || name === 'traits' || fields.some(function (item) { return item.name === name; });
      if (!known) return false;
      var max = wholeNumber(details.max);
      var message = details.reason === 'too_long' ? t('field.too_long', { max: formatNumber(max || limit(recipient ? 'notes' : name)) })
        : details.reason === 'too_many' ? t('field.too_many', { max: max || limit(name) })
        : details.reason === 'unknown' ? t('field.unknown')
        : details.reason === 'invalid_text' ? t('field.invalid_text')
        : details.reason === 'conflict' ? t('field.conflict') : t('field.invalid');
      state.fieldErrors[name] = message;
      var target = fields.filter(function (item) { return item.name === name; })[0];
      var section = target ? target.section : 'profile';
      if (state.section !== section) {
        // Show the field now; the hashchange that follows finds it in place.
        browser.location.hash = section;
        route(false);
      }
      var focusNode = target ? target.node : chips[name] && chips[name].inputs[0];
      if (focusNode && focusNode.focus) focusNode.focus();
      return true;
    }
    function discard() {
      if (!state.saved || state.saving) return;
      state.draft = cloneDocument(state.saved);
      state.fieldErrors = {};
      state.saveError = '';
      fillForm();
      refreshEditor();
    }

    /* ---------------------------------------------------------- dictation */

    function voiceSupported() {
      var media = browser.navigator && browser.navigator.mediaDevices;
      if (!media || typeof media.getUserMedia !== 'function' || typeof browser.AudioContext !== 'function') return false;
      // A Permissions-Policy that blocks the microphone would only fail later.
      var policy = document.permissionsPolicy || document.featurePolicy;
      if (policy && typeof policy.allowsFeature === 'function') {
        try { if (!policy.allowsFeature('microphone')) return false; } catch (_) { /* unknown feature name */ }
      }
      return true;
    }
    function renderMics() {
      var available = !!state.session && state.features.voice && !state.voiceUnavailable && state.contextStatus === 'ready' && voiceSupported();
      // Where speech goes, next to the microphones (people who accepted the
      // terms in an app never see the consent dialog).
      Array.prototype.forEach.call(document.querySelectorAll('[data-voice-note]'), function (note) { note.hidden = !available; });
      mics.forEach(function (mic) {
        var active = voice.mic === mic;
        var recording = active && voice.phase === 'recording';
        mic.button.hidden = !available;
        // One recording at a time, and none for a page that shows another account.
        mic.button.disabled = !!voice.mic && !recording || state.accountChanged;
        if (mic.button.classList) mic.button.classList.toggle('is-recording', recording);
        setText(mic.label, t(recording ? 'voice.stop' : 'voice.start'));
        show(mic.timer, recording);
      });
    }
    function clock(ms) {
      var seconds = Math.max(0, Math.floor(ms / 1000));
      return Math.floor(seconds / 60) + ':' + String(seconds % 60).padStart(2, '0');
    }
    function tick() {
      if (voice.phase !== 'recording') return;
      var elapsed = Math.min(MAX_RECORDING_MS, browser.now() - voice.startedAt);
      setText(voice.mic.timer, t('voice.timer', { elapsed: clock(elapsed), limit: clock(MAX_RECORDING_MS) }));
      voice.tick = browser.setTimeout(tick, 250);
    }
    // The text under the microphone, announced once through the page's live
    // region (which stays in place when a section is hidden).
    function voiceStatus(mic, message) {
      if (mic) setText(mic.status, message);
      if (message) say(message);
    }
    function resetVoice() {
      if (voice.tick) browser.clearTimeout(voice.tick);
      if (voice.limit) browser.clearTimeout(voice.limit);
      voice.phase = 'idle'; voice.mic = null; voice.recorder = null; voice.job = null; voice.tick = null; voice.limit = null;
    }
    function finishVoice(mic, message) {
      resetVoice();
      voiceStatus(mic, message);
      renderMics();
    }
    // Stops a recording without sending anything (tab hidden, section changed, sign-out).
    function cancelVoice(messageKey) {
      if (voice.phase !== 'recording' && voice.phase !== 'starting') return;
      var mic = voice.mic, recorder = voice.recorder;
      if (recorder) recorder.cancel();
      finishVoice(mic, messageKey ? t(messageKey) : '');
    }
    function toggleVoice(mic) {
      if (voice.mic === mic && voice.phase === 'recording') { stopVoice(false); return; }
      if (voice.mic || !state.session || state.accountChanged) return;
      var legal = state.session.legal || {};
      if (legal.consent_required === true && !state.consentGiven) {
        // The recording starts on the next press: browsers open the microphone
        // and audio only from a direct gesture, not after a network round trip.
        askConsent(mic).then(function (accepted) { if (accepted) voiceStatus(mic, t('voice.ready')); });
        return;
      }
      startVoice(mic);
    }
    function startVoice(mic) {
      var recorder = browser.createRecorder ? browser.createRecorder() : createRecorder(browser, captureURL);
      voice.mic = mic; voice.phase = 'starting'; voice.recorder = recorder;
      voiceStatus(mic, '');
      renderMics();
      recorder.start().then(function () {
        if (voice.recorder !== recorder) { recorder.cancel(); return; }
        voice.phase = 'recording';
        voice.startedAt = browser.now();
        voice.limit = browser.setTimeout(function () { stopVoice(true); }, MAX_RECORDING_MS);
        voiceStatus(mic, t('voice.recording'));
        renderMics();
        tick();
      }, function (error) {
        if (voice.recorder !== recorder) return;
        var name = error && error.name;
        finishVoice(mic, t(name === 'NotAllowedError' || name === 'SecurityError' ? 'voice.denied' : name === 'NotFoundError' || name === 'OverconstrainedError' ? 'voice.no_device' : 'voice.failed'));
      });
    }
    function stopVoice(limitReached) {
      if (voice.phase !== 'recording') return Promise.resolve();
      var mic = voice.mic, recorder = voice.recorder, user = state.userId;
      if (voice.tick) browser.clearTimeout(voice.tick);
      if (voice.limit) browser.clearTimeout(voice.limit);
      voice.tick = null; voice.limit = null;
      voice.phase = 'processing';
      voiceStatus(mic, t(limitReached ? 'voice.limit' : 'voice.processing'));
      renderMics();
      return recorder.stop().then(function (capture) {
        var pcm = downsample(capture.samples, capture.sampleRate, VOICE_RATE);
        var maxSamples = VOICE_RATE * MAX_RECORDING_MS / 1000;
        if (pcm.length > maxSamples) pcm = pcm.subarray(0, maxSamples);
        var duration = Math.round(pcm.length * 1000 / VOICE_RATE);
        if (duration < MIN_RECORDING_MS) { finishVoice(mic, t('voice.too_short')); return; }
        voice.job = { wav: encodeWAV(pcm, VOICE_RATE), durationMs: duration, requestId: requestID(browser.crypto), mic: mic, user: user };
        return transcribe(voice.job, true);
      }).catch(function () { if (voice.mic === mic) finishVoice(mic, t('voice.failed')); });
    }
    // A result still matters only for the recording that is waiting for it.
    function current(job) { return voice.job === job && job.user === state.userId; }
    function transcribe(job, mayAskConsent) {
      var form = new browser.FormData();
      form.append('audio', new browser.Blob([job.wav], { type: 'audio/wav' }), 'voice.wav');
      form.append('duration_ms', String(job.durationMs));
      form.append('request_id', job.requestId);
      form.append('languages', voiceLanguages(locale));
      form.append('purpose', 'dictation');
      form.append('expected_user_id', job.user);
      return api('/transcribe', { form: form, timeout: VOICE_TIMEOUT }).then(function (payload) {
        if (!current(job)) return;
        var text = typeof payload.text === 'string' ? payload.text.trim() : '';
        if (!text) { finishVoice(job.mic, t('voice.empty')); return; }
        insertText(job.mic.field, text);
        finishVoice(job.mic, t('voice.done'));
      }, function (error) {
        if (!current(job)) return;
        if (error.status === 403 && error.message === 'CONSENT_REQUIRED' && mayAskConsent) {
          // The same recording and request_id, sent once more after consent.
          return askConsent(job.mic).then(function (accepted) {
            if (!current(job)) return;
            if (!accepted) { finishVoice(job.mic, ''); return; }
            voiceStatus(job.mic, t('voice.processing'));
            return transcribe(job, false);
          });
        }
        if (error.status === 404) {
          state.voiceUnavailable = true;
          finishVoice(job.mic, t('voice.unavailable'));
          return;
        }
        if (error.status === 401) { finishVoice(job.mic, ''); unauthorized(); return; }
        if (isAccountChanged(error)) { finishVoice(job.mic, t('account_changed')); accountChanged(false); return; }
        finishVoice(job.mic, t(voiceErrorKey(error)));
      });
    }
    // 429 VOICE_LIMIT_REACHED is the day's voice cap, RATE_LIMITED the
    // per-minute bucket; a 400 about the audio says why it was refused.
    function voiceErrorKey(error) {
      var details = error.details || {};
      if (error.status === 429) return error.message === 'VOICE_LIMIT_REACHED' ? 'voice.daily_limit' : 'error.RATE_LIMITED';
      if (error.status === 403 && error.message === 'CONSENT_REQUIRED') return 'voice.consent_needed';
      if (error.status === 400 && details.field === 'audio' && details.reason === 'too_short') return 'voice.too_short';
      return 'voice.failed';
    }
    // Inserts at the caret (or replaces the selection), with a space between words.
    function insertText(field, text) {
      if (!field) return;
      var value = field.value || '';
      var start = typeof field.selectionStart === 'number' ? field.selectionStart : value.length;
      var end = typeof field.selectionEnd === 'number' ? field.selectionEnd : start;
      var before = value.slice(0, start), after = value.slice(end);
      var insert = (before && !/\s$/.test(before) ? ' ' : '') + text + (after && !/^\s/.test(after) ? ' ' : '');
      field.value = before + insert + after;
      var caret = before.length + insert.length;
      if (typeof field.setSelectionRange === 'function') { try { field.setSelectionRange(caret, caret); } catch (_) { /* not a text field */ } }
      if (field.focus) field.focus();
      var descriptor = fields.filter(function (item) { return item.node === field; })[0];
      if (descriptor && state.draft) {
        descriptor.write(state.draft, field.value);
        delete state.fieldErrors[descriptor.name];
        refreshEditor();
      }
    }

    /* ---------------------------------------------------------- consent */

    function safeLink(raw, fallback) {
      try {
        var url = new URL(String(raw || ''), browser.location.href);
        if ((url.protocol === 'https:' || url.protocol === 'http:' && url.origin === browser.location.origin) && !url.username && !url.password) return url.href;
      } catch (_) { /* fall back */ }
      return fallback;
    }
    // The legal pages in the page's language (the server's URLs carry none).
    function withLanguage(raw) {
      try {
        var url = new URL(String(raw || ''), browser.location.href);
        if (!url.searchParams.has('lang')) url.searchParams.set('lang', locale);
        return url.href;
      } catch (_) { return raw; }
    }
    function askConsent(mic) {
      if (consent.pending) return consent.pending;
      var legal = state.session && state.session.legal || {};
      if (n['consent-terms']) n['consent-terms'].href = safeLink(withLanguage(legal.terms_url), n['consent-terms'].href);
      if (n['consent-privacy']) n['consent-privacy'].href = safeLink(withLanguage(legal.privacy_url), n['consent-privacy'].href);
      setText(n['consent-error'], '');
      show(n['consent-error'], false);
      consent.opener = mic ? mic.button : null;
      consent.pending = new Promise(function (resolve) { consent.resolve = resolve; });
      var dialog = n.consent;
      if (dialog) {
        if (typeof dialog.showModal === 'function') {
          try { dialog.showModal(); } catch (_) { dialog.setAttribute('open', ''); }
        } else {
          dialog.setAttribute('open', '');
        }
      }
      return consent.pending;
    }
    function closeConsent(accepted) {
      var resolve = consent.resolve, opener = consent.opener;
      consent.pending = null; consent.resolve = null; consent.opener = null; consent.busy = false;
      if (n['consent-accept']) n['consent-accept'].disabled = false;
      var dialog = n.consent;
      if (dialog) {
        if (typeof dialog.close === 'function' && dialog.open) dialog.close();
        else dialog.removeAttribute('open');
      }
      if (opener && opener.focus && !opener.hidden) opener.focus();
      if (resolve) resolve(accepted);
    }
    function acceptConsent() {
      if (!consent.pending || consent.busy) return Promise.resolve();
      var legal = state.session && state.session.legal || {};
      if (typeof legal.terms_version !== 'string' || !legal.terms_version || typeof legal.privacy_version !== 'string' || !legal.privacy_version) {
        setText(n['consent-error'], t('consent.error'));
        show(n['consent-error'], true);
        return Promise.resolve();
      }
      consent.busy = true;
      if (n['consent-accept']) n['consent-accept'].disabled = true;
      var user = state.userId;
      return api('/consent', { json: { terms_version: legal.terms_version, privacy_version: legal.privacy_version, locale: locale, expected_user_id: user } }).then(function () {
        if (user !== state.userId) return;
        state.consentGiven = true;
        closeConsent(true);
      }, function (error) {
        if (user !== state.userId) return;
        if (error.status === 401) { closeConsent(false); unauthorized(); return; }
        if (isAccountChanged(error)) { closeConsent(false); accountChanged(); return; }
        consent.busy = false;
        if (n['consent-accept']) n['consent-accept'].disabled = false;
        setText(n['consent-error'], t('consent.error'));
        show(n['consent-error'], true);
      });
    }

    /* ---------------------------------------------------------- start */

    function guardLogout(event) {
      var logout = document.getElementById('account-logout');
      if (!logout || !isDirty() || event.target !== logout && !(logout.contains && logout.contains(event.target))) return;
      if (typeof browser.confirm === 'function' && browser.confirm(t('save.leave_confirm'))) return;
      event.preventDefault();
      event.stopPropagation();
      if (event.stopImmediatePropagation) event.stopImmediatePropagation();
    }
    function init() {
      document.addEventListener('aireply:session', function (event) { onSession(event.detail); });
      document.addEventListener('aireply:signedout', onSignedOut);
      document.addEventListener('visibilitychange', function () { if (document.hidden) cancelVoice('voice.cancelled'); });
      document.addEventListener('click', guardLogout, true);
      browser.addEventListener('hashchange', function () { route(true); });
      browser.addEventListener('pagehide', function () { cancelVoice(''); });
      browser.addEventListener('beforeunload', function (event) {
        if (!isDirty()) return undefined;
        event.preventDefault();
        event.returnValue = '';
        return '';
      });
      var resizeTimer = null;
      browser.addEventListener('resize', function () {
        if (resizeTimer) browser.clearTimeout(resizeTimer);
        resizeTimer = browser.setTimeout(function () { resizeTimer = null; renderChart(false); }, 150);
      });
      fields.forEach(function (field) {
        if (!field.node) return;
        field.node.addEventListener('input', function () {
          if (!state.draft) return;
          field.write(state.draft, field.node.value);
          delete state.fieldErrors[field.name];
          refreshEditor();
        });
      });
      if (n['ai-enabled']) n['ai-enabled'].addEventListener('change', function () {
        if (!state.draft) return;
        state.draft.enabled = !!n['ai-enabled'].checked;
        refreshEditor();
      });
      toArray(document.querySelectorAll('[data-mic-for]')).forEach(function (button) {
        var id = button.getAttribute('data-mic-for');
        var mic = { button: button, field: document.getElementById(id), status: document.getElementById(id + '-voice'),
          label: button.querySelector('.cabinet-mic-label'), timer: button.querySelector('.cabinet-mic-timer') };
        if (!mic.field) return;
        button.addEventListener('click', function () { toggleVoice(mic); });
        mics.push(mic);
      });
      if (n.save) n.save.addEventListener('click', save);
      if (n.discard) n.discard.addEventListener('click', discard);
      if (n['reload-context']) n['reload-context'].addEventListener('click', loadContext);
      if (n.relogin) n.relogin.addEventListener('click', signalSignOut);
      // After a reload the page shows the account this browser is signed in to
      // (the browser asks first while unsaved text is on screen).
      if (n['reload-page']) n['reload-page'].addEventListener('click', function () { browser.location.reload(); });
      ['profile-retry', 'recipients-retry'].forEach(function (id) { if (n[id]) n[id].addEventListener('click', loadContext); });
      if (n['chart-retry']) n['chart-retry'].addEventListener('click', loadUsage);
      if (n['consent-accept']) n['consent-accept'].addEventListener('click', acceptConsent);
      if (n['consent-cancel']) n['consent-cancel'].addEventListener('click', function () { closeConsent(false); });
      if (n.consent) {
        n.consent.addEventListener('cancel', function (event) { event.preventDefault(); closeConsent(false); });
        n.consent.addEventListener('close', function () { if (consent.pending) closeConsent(false); });
      }
      route(false);
      renderAll();
    }

    return {
      init: init, route: route, onSession: onSession, onSignedOut: onSignedOut, loadContext: loadContext, loadUsage: loadUsage,
      save: save, discard: discard, acceptConsent: acceptConsent, stopVoice: stopVoice, isDirty: isDirty, state: state
    };
  }

  if (typeof module !== 'undefined' && module.exports) {
    module.exports = {
      createCabinet: createCabinet, createRecorder: createRecorder, contextUnits: contextUnits, contextCost: contextCost,
      normalizeText: normalizeText, normalizeDocument: normalizeDocument, chars: chars, downsample: downsample, encodeWAV: encodeWAV,
      voiceLanguages: voiceLanguages, sectionFromHash: sectionFromHash, niceTicks: niceTicks, requestID: requestID
    };
  }
  if (typeof document !== 'undefined' && typeof window !== 'undefined' && document.getElementById('account-app') && document.getElementById('cabinet-overview')) {
    var boot;
    try { boot = JSON.parse(document.getElementById('account-data').textContent); } catch (_) { return; }
    createCabinet(document, boot, {
      fetch: window.fetch.bind(window), location: window.location, navigator: window.navigator,
      AudioContext: window.AudioContext || window.webkitAudioContext, AudioWorkletNode: window.AudioWorkletNode,
      FormData: window.FormData, Blob: window.Blob, CustomEvent: window.CustomEvent, crypto: window.crypto,
      AbortController: window.AbortController, now: Date.now, setTimeout: window.setTimeout.bind(window),
      clearTimeout: window.clearTimeout.bind(window), addEventListener: window.addEventListener.bind(window),
      matchMedia: window.matchMedia ? window.matchMedia.bind(window) : null, confirm: window.confirm.bind(window),
      getComputedStyle: window.getComputedStyle.bind(window)
    }).init();
  }
})();
