/*
 * AI Reply — әкімші панелі (Vue 3, құрастырусыз: браузерде тікелей жұмыс істейді).
 * Деректер тек /api/v1/admin/* арқылы келеді; хабарлама мазмұны API-де жоқ.
 */
(function () {
  "use strict";

  var Vue = window.Vue;
  var boot = JSON.parse(document.getElementById("boot").textContent);

  /* ------------------------------------------------------------------ state */
  var state = Vue.reactive({
    locale: boot.locale,
    admin: boot.admin,
    csrf: boot.csrf,
    env: boot.env,
    timezone: boot.timezone,
    demoMode: boot.demo_mode,
    paymentMode: boot.payment_mode,
    route: parseRoute(location.pathname),
    // Every navigation gets a new number: sub-views are keyed by it, so they
    // re-read their filters from the address bar after back/forward too.
    routeSeq: 0,
    toasts: [],
    sidebarOpen: false
  });

  function t(key) {
    var bucket = boot.i18n[state.locale] || boot.i18n.en || {};
    return bucket[key] || (boot.i18n.en || {})[key] || key;
  }

  // tf — аударма + {name} орнына мән.
  function tf(key, vars) {
    return t(key).replace(/\{(\w+)\}/g, function (match, name) {
      return vars && vars[name] !== undefined && vars[name] !== null ? vars[name] : match;
    });
  }

  // can — әкімшінің рұқсаты бар ма. Тек көрсетуге арналған: әр маршрутты сервер өзі тексереді.
  function can(permission) {
    var list = (state.admin && state.admin.permissions) || [];
    return list.indexOf(permission) !== -1;
  }

  // Бетті ашуға қажет рұқсат (GET /plans — dashboard.read).
  var ROUTE_PERMISSION = {
    dashboard: "dashboard.read", users: "users.read", user: "users.read", plans: "dashboard.read",
    notifications: "notifications.read", logs: "logs.read", audit: "audit_logs.read", settings: "settings.read"
  };

  function parseRoute(path) {
    var clean = path.split("?")[0].split("#")[0].replace(/\/+$/, "") || "/admin";
    var parts = clean.split("/").filter(Boolean); // ["admin", ...]
    if (parts.length <= 1) return { name: "dashboard" };
    if (parts[1] === "users") return parts[2] ? { name: "user", id: parts[2] } : { name: "users" };
    if (parts[1] === "plans") return { name: "plans" };
    if (parts[1] === "audit") return { name: "audit" };
    if (parts[1] === "settings") return { name: "settings" };
    if (parts[1] === "notifications") {
      if (parts[2] === "new") return { name: "notifications", tab: "create" };
      if (parts[2] === "campaigns" && parts[3]) return { name: "notifications", tab: "campaign", id: parts[3] };
      if (parts[2] === "deliveries" || parts[2] === "devices") return { name: "notifications", tab: parts[2] };
      return { name: "notifications", tab: "campaigns" };
    }
    if (parts[1] === "logs") {
      var tab = ["auth", "errors", "versions"].indexOf(parts[2]) !== -1 ? parts[2] : "events";
      return { name: "logs", tab: tab };
    }
    return { name: "dashboard" };
  }

  function navigate(path) {
    history.pushState({}, "", path);
    state.route = parseRoute(path);
    state.routeSeq++;
    state.sidebarOpen = false;
    window.scrollTo({ top: 0 });
  }
  window.addEventListener("popstate", function () {
    state.route = parseRoute(location.pathname);
    state.routeSeq++;
  });

  function toast(kind, message) {
    var item = { id: Date.now() + Math.random(), kind: kind, message: message };
    state.toasts.push(item);
    setTimeout(function () {
      state.toasts = state.toasts.filter(function (x) { return x.id !== item.id; });
    }, 3200);
  }

  /* -------------------------------------------------------------------- api */
  async function api(path, options) {
    options = options || {};
    var init = {
      method: options.method || "GET",
      headers: { "Accept": "application/json" },
      credentials: "same-origin"
    };
    if (options.body !== undefined) {
      init.headers["Content-Type"] = "application/json";
      init.headers["X-CSRF-Token"] = state.csrf;
      init.body = JSON.stringify(options.body);
    }
    if (options.headers) {
      for (var name in options.headers) init.headers[name] = options.headers[name];
    }
    var response = await fetch("/api/v1/admin" + path, init);
    if (response.status === 401) { location.href = "/admin/login"; throw new Error("unauthorized"); }
    var payload = null;
    try { payload = await response.json(); } catch (e) { payload = null; }
    if (!response.ok) {
      // message stays the bare code (older callers compare it); the rest is for errorText().
      var body = payload && payload.error ? payload.error : {};
      var error = new Error(body.code || "ERROR");
      error.code = body.code || "ERROR";
      error.status = response.status;
      error.details = body.details || {};
      error.requestID = body.request_id || response.headers.get("X-Request-ID") || "";
      throw error;
    }
    return payload;
  }

  /* ----------------------------------------------------------- errors */
  // Сервердің details.reason мәтіндері (validate.go) → аударма.
  var REASON_KEYS = {
    "1-80 characters, one line": "admin.error.reason.title_length",
    "1-400 characters": "admin.error.reason.body_length",
    "the notification is too large": "admin.error.reason.too_large",
    "unknown": "admin.error.reason.unknown_value",
    "unknown value": "admin.error.reason.unknown_value",
    "too long": "admin.error.reason.link_too_long",
    "not a URL": "admin.error.reason.link_not_url",
    "unknown screen": "admin.error.reason.link_screen",
    "host is not allowed": "admin.error.reason.link_host",
    "only aireply:// screens and https links": "admin.error.reason.link_scheme",
    "at most 10 keys": "admin.error.reason.data_keys",
    "key must be lowercase snake_case and not reserved": "admin.error.reason.data_key",
    "at most 256 characters, one line": "admin.error.reason.data_value",
    "at most 1 KB in total": "admin.error.reason.data_size",
    "use a version like 1.3.0": "admin.error.reason.version",
    "lower than the minimum": "admin.error.reason.version_range",
    "0-3650": "admin.error.reason.days_range",
    "no device can match both": "admin.error.reason.days_conflict",
    "use YYYY-MM-DD": "admin.error.reason.date",
    "before the start date": "admin.error.reason.date_range",
    "at most 500 users": "admin.error.reason.user_ids_count",
    "not a user id": "admin.error.reason.user_id",
    "anonymous devices have no account, plan or registration date": "admin.error.reason.anonymous",
    "send an Idempotency-Key header": "admin.error.reason.idempotency",
    "success or failure": "admin.error.reason.unknown_value"
  };

  var FIELD_KEYS = {
    name: "admin.push.form.name", title: "admin.push.form.title", body: "admin.push.form.body",
    category: "admin.push.form.category", link: "admin.push.form.link", data: "admin.push.form.data",
    idempotency_key: "admin.push.form.request",
    "audience.platforms": "admin.push.audience.platforms", "audience.auth": "admin.push.audience.auth",
    "audience.payment": "admin.push.audience.payment", "audience.subscription": "admin.push.audience.subscription",
    "audience.locales": "admin.push.audience.locales",
    "audience.app_version_min": "admin.push.audience.app_version_min",
    "audience.app_version_max": "admin.push.audience.app_version_max",
    "audience.os_version_min": "admin.push.audience.os_version_min",
    "audience.active_within_days": "admin.push.audience.active_within_days",
    "audience.inactive_for_days": "admin.push.audience.inactive_for_days",
    "audience.registered_from": "admin.push.audience.registered_from",
    "audience.registered_to": "admin.push.audience.registered_to",
    "audience.user_ids": "admin.push.audience.user_ids",
    status: "admin.users.col_status", platform: "admin.users.col_platform",
    push_status: "admin.push.device.push", outcome: "admin.logs.outcome",
    from: "common.from", to: "common.to"
  };

  function fieldLabel(field) {
    if (field.indexOf("data.") === 0) return t("admin.push.form.data") + " «" + field.slice(5) + "»";
    return FIELD_KEYS[field] ? t(FIELD_KEYS[field]) : field;
  }

  // errorText — API қатесі → әкімшіге түсінікті, аударылған мәтін.
  function errorText(e) {
    var code = e && e.code;
    var details = (e && e.details) || {};
    if (!code) return t("admin.error.network");
    switch (code) {
      case "FORBIDDEN":
        return tf("admin.error.forbidden", { permission: details.permission || "—" });
      case "CSRF_MISMATCH":
        return t("admin.error.csrf");
      case "PUSH_DISABLED":
        return t("admin.error.push_disabled");
      case "RATE_LIMITED":
        return details.retry_after_seconds
          ? tf("admin.error.rate_limited_retry", { minutes: Math.max(1, Math.ceil(details.retry_after_seconds / 60)) })
          : t("admin.error.rate_limited");
      case "INVALID_REQUEST":
        if (!details.field) return t("admin.error.invalid");
        return fieldLabel(details.field) + ": " +
          (REASON_KEYS[details.reason] ? t(REASON_KEYS[details.reason]) : t("admin.error.reason.unknown_value"));
      case "CONFLICT":
        return t("admin.error.conflict");
      case "NOT_FOUND":
        return t("admin.error.not_found");
      default:
        return t("common.error") + (e.requestID ? " · " + e.requestID : "");
    }
  }

  /* ----------------------------------------------------------- formatting */
  function nf(value) {
    if (value === null || value === undefined) return "—";
    return new Intl.NumberFormat(state.locale === "kk" ? "kk-KZ" : state.locale).format(value);
  }
  function money(value) { return "$" + (Math.round(value * 100) / 100).toFixed(2); }

  function shortID(id) { return id ? String(id).slice(0, 8) : ""; }

  // maskIP — толық IP ешқашан көрсетілмейді (сервердің redact.IP пішімі: 203.0.113.x).
  function maskIP(ip) {
    ip = String(ip || "").trim();
    if (!ip || /\.x$|::x$/.test(ip)) return ip;
    var v4 = ip.match(/^(\d+)\.(\d+)\.(\d+)\.\d+$/);
    if (v4) return v4[1] + "." + v4[2] + "." + v4[3] + ".x";
    if (ip.indexOf(":") !== -1) {
      return ip.split("::")[0].split(":").filter(Boolean).slice(0, 4).join(":") + "::x";
    }
    return "";
  }

  function clock(date) {
    function two(n) { return (n < 10 ? "0" : "") + n; }
    return two(date.getHours()) + ":" + two(date.getMinutes()) + ":" + two(date.getSeconds());
  }

  function duration(seconds) {
    seconds = Math.max(0, Math.round(seconds || 0));
    if (seconds < 60) return tf("admin.unit.seconds", { n: seconds });
    if (seconds < 3600) return tf("admin.unit.minutes", { n: Math.floor(seconds / 60) });
    return tf("admin.unit.hours", { h: Math.floor(seconds / 3600), m: Math.floor((seconds % 3600) / 60) });
  }

  // uuid — Idempotency-Key үшін (crypto.getRandomValues, v4).
  function uuid() {
    var bytes = new Uint8Array(16);
    window.crypto.getRandomValues(bytes);
    bytes[6] = (bytes[6] & 0x0f) | 0x40;
    bytes[8] = (bytes[8] & 0x3f) | 0x80;
    var hex = Array.prototype.map.call(bytes, function (b) { return (b + 256).toString(16).slice(1); }).join("");
    return hex.slice(0, 8) + "-" + hex.slice(8, 12) + "-" + hex.slice(12, 16) + "-" + hex.slice(16, 20) + "-" + hex.slice(20);
  }

  // flatten — метадеректі "audience.platforms: android, ios" жолдарына жаю ("[object Object]" орнына).
  function flatten(value, prefix, out) {
    out = out || [];
    if (value === null || value === undefined || value === "") return out;
    if (Array.isArray(value)) {
      var simple = value.every(function (v) { return v === null || typeof v !== "object"; });
      if (simple) {
        if (value.length) out.push({ key: prefix, value: value.join(", ") });
      } else {
        value.forEach(function (v, i) { flatten(v, prefix + "[" + i + "]", out); });
      }
      return out;
    }
    if (typeof value === "object") {
      var keys = Object.keys(value).sort();
      if (!keys.length && prefix) out.push({ key: prefix, value: "—" });
      keys.forEach(function (k) { flatten(value[k], prefix ? prefix + "." + k : k, out); });
      return out;
    }
    out.push({ key: prefix, value: String(value) });
    return out;
  }

  function toQuery(params) {
    var query = new URLSearchParams();
    Object.keys(params).forEach(function (key) {
      var v = params[key];
      if (v !== "" && v !== null && v !== undefined) query.set(key, v);
    });
    var s = query.toString();
    return s ? "?" + s : "";
  }

  function fromQuery(defaults) {
    var query = new URLSearchParams(location.search), out = {};
    Object.keys(defaults).forEach(function (key) { out[key] = query.has(key) ? query.get(key) : defaults[key]; });
    return out;
  }

  /* ------------------------------------------------------- status badges */
  var CAMPAIGN_STATUSES = ["draft", "queued", "processing", "completed", "partially_failed", "failed", "cancelled"];
  var DELIVERY_STATUSES = ["queued", "sending", "retrying", "provider_accepted", "provider_failed",
    "invalid_token", "skipped", "cancelled"];
  var PUSH_STATUSES = ["none", "active", "invalid", "replaced"];
  var AUTH_METHODS = ["email", "google", "apple", "phone", "refresh"];

  function campaignBadge(status) {
    return {
      draft: "badge-muted", queued: "badge-brand", processing: "badge-brand", completed: "badge-ok",
      partially_failed: "badge-warn", failed: "badge-danger", cancelled: "badge-muted"
    }[status] || "badge-muted";
  }
  function deliveryBadge(status) {
    return {
      queued: "badge-brand", sending: "badge-brand", retrying: "badge-brand", provider_accepted: "badge-ok",
      provider_failed: "badge-danger", invalid_token: "badge-warn", skipped: "badge-muted", cancelled: "badge-muted"
    }[status] || "badge-muted";
  }
  function pushBadge(status) {
    return { active: "badge-ok", none: "badge-muted", invalid: "badge-danger", replaced: "badge-warn" }[status] || "badge-muted";
  }
  function permissionBadge(p) {
    if (p === "denied") return "badge-danger";
    return p === "authorized" || p === "provisional" || p === "ephemeral" ? "badge-ok" : "badge-muted";
  }
  function httpBadge(status) { return status >= 500 ? "badge-danger" : "badge-warn"; }

  // pendingOf — әлі аяқталмаған жеткізулер (queued + sending + retrying).
  function pendingOf(stats) { return stats ? (stats.queued || 0) + (stats.sending || 0) + (stats.retrying || 0) : 0; }

  function adminName(id) {
    if (!id) return "—";
    return state.admin && id === state.admin.id ? state.admin.email : tf("admin.push.admin_id", { id: shortID(id) });
  }

  // audienceSummary — сүзгінің адам оқитын сипаттамасы (науқан беті мен растау терезесі).
  function audienceSummary(a) {
    a = a || {};
    var out = [];
    if (a.platforms && a.platforms.length) {
      out.push(t("admin.push.audience.platforms") + ": " + a.platforms.map(function (p) {
        return p === "ios" ? "iOS" : "Android";
      }).join(", "));
    }
    if (a.auth) out.push(t("admin.push.auth." + a.auth));
    if (a.payment) out.push(t("admin.push.payment." + a.payment));
    if (a.subscription) out.push(t("admin.push.audience.subscription") + ": " + t("admin.push.subscription." + a.subscription));
    if (a.locales && a.locales.length) {
      out.push(t("admin.push.audience.locales") + ": " + a.locales.map(function (l) { return l.toUpperCase(); }).join(", "));
    }
    if (a.app_version_min) out.push(tf("admin.push.summary.app_min", { v: a.app_version_min }));
    if (a.app_version_max) out.push(tf("admin.push.summary.app_max", { v: a.app_version_max }));
    if (a.os_version_min) out.push(tf("admin.push.summary.os_min", { v: a.os_version_min }));
    if (a.active_within_days) out.push(tf("admin.push.summary.active", { n: a.active_within_days }));
    if (a.inactive_for_days) out.push(tf("admin.push.summary.inactive", { n: a.inactive_for_days }));
    if (a.registered_from || a.registered_to) {
      out.push(tf("admin.push.summary.registered", { from: a.registered_from || "…", to: a.registered_to || "…" }));
    }
    if (a.user_ids && a.user_ids.length) out.push(tf("admin.push.summary.users", { n: a.user_ids.length }));
    if (!out.length) out.push(t("admin.push.summary.everyone"));
    return out;
  }

  // helpers — жаңа компоненттердің ортақ әдістері.
  var helpers = {
    t: t, tf: tf, nf: nf, can: can, go: navigate, shortID: shortID, maskIP: maskIP, duration: duration,
    campaignBadge: campaignBadge, deliveryBadge: deliveryBadge, pushBadge: pushBadge,
    permissionBadge: permissionBadge, httpBadge: httpBadge, pendingOf: pendingOf, adminName: adminName,
    platformName: function (p) { return p === "ios" ? "iOS" : p === "android" ? "Android" : (p || "—"); },
    pairs: function (value) { return flatten(value, ""); },
    kept: function (days) { return days > 0 ? tf("admin.logs.kept", { days: days }) : t("admin.logs.kept_forever"); },
    userLink: function (id) { return "/admin/users/" + id; }
  };

  /* ------------------------------------------------------- shared parts */
  // NoAccess — рұқсат жоқ бет не 403 жауабы (қате терезесінің орнына сабырлы ескерту).
  var NoAccess = {
    props: { permission: { type: String, default: "" } },
    methods: { t: t, tf: tf },
    template: `
      <div class="card">
        <div class="card-body">
          <div class="notice notice-warn" role="alert" style="margin-bottom:0">
            <b>{{ t('admin.access.denied') }}</b><br>
            {{ tf('admin.access.needs', { permission: permission || '—' }) }}
          </div>
        </div>
      </div>`
  };

  var Pager = {
    props: {
      page: { type: Number, default: 1 }, limit: { type: Number, default: 50 }, total: { type: Number, default: 0 }
    },
    emits: ["move"],
    computed: { pages: function () { return Math.max(1, Math.ceil(this.total / (this.limit || 1))); } },
    methods: { t: t, nf: nf },
    template: `
      <nav class="pagination" :aria-label="t('common.page')">
        <span class="pagination-total">{{ t('common.total') }}: {{ nf(total) }}</span>
        <button type="button" class="btn btn-sm" :disabled="page <= 1" @click="$emit('move', -1)">{{ t('common.prev') }}</button>
        <span>{{ t('common.page') }} {{ page }} / {{ pages }}</span>
        <button type="button" class="btn btn-sm" :disabled="page >= pages" @click="$emit('move', 1)">{{ t('common.next') }}</button>
      </nav>`
  };

  // listView — сүзгі, бет, 403 және мекенжай жолымен синхрондау (сілтемемен бөлісуге болады).
  function listView(options) {
    return {
      components: { Pager: Pager, NoAccess: NoAccess },
      data: function () {
        var query = new URLSearchParams(location.search);
        return {
          loading: true, rows: [], total: 0, limit: options.limit || 50, denied: "", error: "",
          page: Math.max(1, parseInt(query.get("page"), 10) || 1),
          filters: fromQuery(options.defaults)
        };
      },
      mounted: function () { this.load(); },
      methods: {
        async load() {
          this.loading = true;
          this.error = "";
          try {
            var params = Object.assign({}, this.filters, { page: this.page, limit: this.limit });
            var data = await api(options.endpoint + toQuery(params));
            this.rows = data[options.rowsKey] || [];
            this.total = data.total || 0;
            if (data.limit) this.limit = data.limit;
            this.denied = "";
          } catch (e) {
            // A failed load must never look like an empty list.
            this.rows = [];
            this.total = 0;
            if (e.code === "FORBIDDEN") this.denied = e.details.permission || "";
            else this.error = errorText(e);
          }
          this.loading = false;
        },
        syncAddress() {
          var params = Object.assign({}, this.filters, { page: this.page > 1 ? this.page : "" });
          history.replaceState(history.state, "", location.pathname + toQuery(params));
        },
        search() { this.page = 1; this.syncAddress(); this.load(); },
        reset() { this.filters = Object.assign({}, options.defaults); this.search(); },
        move(delta) { this.page = Math.max(1, this.page + delta); this.syncAddress(); this.load(); }
      }
    };
  }

  /* ------------------------------------------------------------- charts */
  var uid = 0;

  var chartMixin = {
    data: function () { return { width: 560, hover: -1, gradientID: "grad" + (++uid) }; },
    mounted: function () {
      this.onResize();
      window.addEventListener("resize", this.onResize);
    },
    unmounted: function () { window.removeEventListener("resize", this.onResize); },
    methods: {
      onResize: function () {
        if (this.$el && this.$el.parentElement) this.width = this.$el.parentElement.clientWidth || 560;
      },
      niceMax: function (value) {
        if (value <= 0) return 1;
        var magnitude = Math.pow(10, Math.floor(Math.log10(value)));
        var scaled = value / magnitude;
        var step = scaled <= 1 ? 1 : scaled <= 2 ? 2 : scaled <= 5 ? 5 : 10;
        return step * magnitude;
      },
      shortLabel: function (label) {
        return /^\d{4}-\d{2}-\d{2}$/.test(label) ? label.slice(5) : label;
      },
      fmt: function (v) {
        if (v >= 1000000) return (v / 1000000).toFixed(1) + "M";
        if (v >= 1000) return (v / 1000).toFixed(1) + "k";
        return Math.round(v * 100) / 100;
      }
    }
  };

  var LineChart = {
    mixins: [chartMixin],
    props: { points: { type: Array, default: function () { return []; } }, unit: { type: String, default: "" } },
    computed: {
      layout: function () {
        var w = this.width, h = 210, pad = { l: 44, r: 12, t: 14, b: 26 };
        var innerW = Math.max(w - pad.l - pad.r, 10), innerH = h - pad.t - pad.b;
        var values = this.points.map(function (p) { return p.value; });
        var max = this.niceMax(Math.max.apply(null, values.concat([0])));
        var step = this.points.length > 1 ? innerW / (this.points.length - 1) : 0;
        var coords = this.points.map(function (p, i) {
          return { x: pad.l + i * step, y: pad.t + innerH - (p.value / max) * innerH, p: p };
        });
        var line = coords.map(function (c, i) { return (i ? "L" : "M") + c.x.toFixed(1) + " " + c.y.toFixed(1); }).join(" ");
        var area = coords.length
          ? line + " L" + coords[coords.length - 1].x.toFixed(1) + " " + (pad.t + innerH) +
            " L" + coords[0].x.toFixed(1) + " " + (pad.t + innerH) + " Z"
          : "";
        return { w: w, h: h, pad: pad, innerW: innerW, innerH: innerH, max: max, coords: coords, line: line, area: area };
      }
    },
    methods: {
      pick: function (event) {
        if (!this.layout.coords.length) return;
        var box = this.$el.getBoundingClientRect();
        var x = (event.clientX - box.left) * (this.layout.w / box.width);
        var best = 0, bestDistance = Infinity;
        this.layout.coords.forEach(function (c, i) {
          var d = Math.abs(c.x - x);
          if (d < bestDistance) { bestDistance = d; best = i; }
        });
        this.hover = best;
      }
    },
    template: `
      <div class="chart-holder">
        <svg v-if="points.length" class="chart" :viewBox="'0 0 ' + layout.w + ' ' + layout.h"
             @mousemove="pick" @mouseleave="hover = -1">
          <defs>
            <linearGradient :id="gradientID" x1="0" y1="0" x2="0" y2="1">
              <stop offset="0%" stop-color="#3b5bfd" stop-opacity=".22"/>
              <stop offset="100%" stop-color="#3b5bfd" stop-opacity="0"/>
            </linearGradient>
          </defs>
          <g>
            <template v-for="r in [0, 0.25, 0.5, 0.75, 1]" :key="r">
              <line class="grid-line" :x1="layout.pad.l" :x2="layout.w - layout.pad.r"
                    :y1="layout.pad.t + layout.innerH * r" :y2="layout.pad.t + layout.innerH * r"/>
              <text class="tick" x="6" :y="layout.pad.t + layout.innerH * r + 3.5">{{ fmt(layout.max * (1 - r)) }}</text>
            </template>
          </g>
          <path class="area" :d="layout.area" :fill="'url(#' + gradientID + ')'"/>
          <path class="line" :d="layout.line"/>
          <g v-if="hover >= 0 && layout.coords[hover]">
            <line class="hover-line" :x1="layout.coords[hover].x" :x2="layout.coords[hover].x"
                  :y1="layout.pad.t" :y2="layout.pad.t + layout.innerH"/>
            <circle class="dot-active" :cx="layout.coords[hover].x" :cy="layout.coords[hover].y" r="5"/>
          </g>
          <text v-for="(c, i) in layout.coords" :key="'l' + i" class="tick" :x="c.x" :y="layout.h - 8"
                text-anchor="middle" v-show="i === 0 || i === layout.coords.length - 1 || i % Math.ceil(layout.coords.length / 6) === 0">
            {{ shortLabel(c.p.label) }}
          </text>
        </svg>
        <div v-else class="chart-empty">{{ t('common.empty') }}</div>
        <div v-if="hover >= 0 && layout.coords[hover]" class="chart-tip"
             :style="{ left: (layout.coords[hover].x / layout.w * 100) + '%' }">
          <b>{{ layout.coords[hover].p.label }}</b><span>{{ unit }}{{ fmt(layout.coords[hover].p.value) }}</span>
        </div>
      </div>`
  };
  LineChart.methods.t = t;

  var BarChart = {
    mixins: [chartMixin],
    props: { points: { type: Array, default: function () { return []; } }, unit: { type: String, default: "" } },
    computed: {
      layout: function () {
        var w = this.width, h = 210, pad = { l: 44, r: 12, t: 14, b: 26 };
        var innerW = Math.max(w - pad.l - pad.r, 10), innerH = h - pad.t - pad.b;
        var max = this.niceMax(Math.max.apply(null, this.points.map(function (p) { return p.value; }).concat([0])));
        var slot = this.points.length ? innerW / this.points.length : innerW;
        var barW = Math.max(6, Math.min(34, slot - 8));
        var bars = this.points.map(function (p, i) {
          var height = (p.value / max) * innerH;
          return { x: pad.l + i * slot + (slot - barW) / 2, y: pad.t + innerH - height,
                   w: barW, h: Math.max(height, 2), p: p, cx: pad.l + i * slot + slot / 2 };
        });
        return { w: w, h: h, pad: pad, innerH: innerH, max: max, bars: bars };
      }
    },
    template: `
      <div class="chart-holder">
        <svg v-if="points.length" class="chart" :viewBox="'0 0 ' + layout.w + ' ' + layout.h">
          <template v-for="r in [0, 0.5, 1]" :key="r">
            <line class="grid-line" :x1="layout.pad.l" :x2="layout.w - layout.pad.r"
                  :y1="layout.pad.t + layout.innerH * r" :y2="layout.pad.t + layout.innerH * r"/>
            <text class="tick" x="6" :y="layout.pad.t + layout.innerH * r + 3.5">{{ fmt(layout.max * (1 - r)) }}</text>
          </template>
          <g v-for="(b, i) in layout.bars" :key="i" @mouseenter="hover = i" @mouseleave="hover = -1">
            <rect class="bar" :class="{ 'is-hot': hover === i }" :x="b.x" :y="b.y" :width="b.w" :height="b.h" rx="5"/>
            <text class="tick" :x="b.cx" :y="layout.h - 8" text-anchor="middle"
                  v-show="layout.bars.length <= 8 || i % Math.ceil(layout.bars.length / 6) === 0">
              {{ shortLabel(b.p.label) }}
            </text>
          </g>
        </svg>
        <div v-else class="chart-empty">{{ t('common.empty') }}</div>
        <div v-if="hover >= 0 && layout.bars[hover]" class="chart-tip"
             :style="{ left: (layout.bars[hover].cx / layout.w * 100) + '%' }">
          <b>{{ layout.bars[hover].p.label }}</b><span>{{ unit }}{{ fmt(layout.bars[hover].p.value) }}</span>
        </div>
      </div>`
  };
  BarChart.methods = Object.assign({}, BarChart.methods, { t: t });

  var DonutChart = {
    props: { points: { type: Array, default: function () { return []; } } },
    data: function () { return { hover: -1 }; },
    computed: {
      slices: function () {
        var total = this.points.reduce(function (sum, p) { return sum + p.value; }, 0) || 1;
        var palette = ["#3b5bfd", "#00b8d9", "#7048e8", "#0ca678", "#f59f00", "#e8590c", "#e64980"];
        var angle = -Math.PI / 2;
        return this.points.map(function (p, i) {
          var share = p.value / total;
          var start = angle;
          angle += share * Math.PI * 2;
          var end = angle;
          var large = end - start > Math.PI ? 1 : 0;
          var r = 62, ir = 40, cx = 80, cy = 80;
          function pt(radius, a) { return [cx + radius * Math.cos(a), cy + radius * Math.sin(a)]; }
          var p1 = pt(r, start), p2 = pt(r, end), p3 = pt(ir, end), p4 = pt(ir, start);
          return {
            d: "M" + p1 + " A" + r + " " + r + " 0 " + large + " 1 " + p2 +
               " L" + p3 + " A" + ir + " " + ir + " 0 " + large + " 0 " + p4 + " Z",
            color: palette[i % palette.length], label: p.label, value: p.value,
            percent: Math.round(share * 100)
          };
        });
      }
    },
    template: `
      <div class="donut-wrap">
        <svg viewBox="0 0 160 160" class="donut" v-if="points.length">
          <path v-for="(s, i) in slices" :key="i" :d="s.d" :fill="s.color"
                :opacity="hover === -1 || hover === i ? 1 : .35"
                @mouseenter="hover = i" @mouseleave="hover = -1"/>
        </svg>
        <div v-else class="chart-empty">{{ t('common.empty') }}</div>
        <ul class="legend">
          <li v-for="(s, i) in slices" :key="i" @mouseenter="hover = i" @mouseleave="hover = -1">
            <span class="swatch" :style="{ background: s.color }"></span>
            <span class="legend-label">{{ s.label }}</span>
            <b>{{ s.value }}</b><small>{{ s.percent }}%</small>
          </li>
        </ul>
      </div>`,
    methods: { t: t }
  };

  /* -------------------------------------------------------------- views */
  // OpsPanel — құрылғылар, push, кіру және қателер (GET /ops). Өз қатесі тақтаның қалғанын бұзбайды.
  var OpsPanel = {
    components: { DonutChart: DonutChart },
    data: function () { return { loading: true, data: null, error: "" }; },
    mounted: function () { this.load(); },
    methods: Object.assign({}, helpers, {
      async load() {
        this.loading = true;
        this.error = "";
        try { this.data = await api("/ops"); }
        catch (e) { this.error = errorText(e); }
        this.loading = false;
      },
      openError: function (row) {
        if (can("logs.read") && row.request_id) navigate("/admin/logs/errors?request_id=" + encodeURIComponent(row.request_id));
      }
    }),
    template: `
      <section class="ops" aria-labelledby="ops-title">
        <div class="section-head">
          <h2 id="ops-title">{{ t('admin.ops.title') }}</h2>
          <div class="right" v-if="data">
            <span :class="'badge ' + (data.push.enabled ? 'badge-ok' : 'badge-muted')">
              {{ t('admin.push.status.sending') }}: {{ data.push.enabled ? t('admin.push.status.on') : t('admin.push.status.off') }}</span>
            <span :class="'badge ' + (data.push.fcm ? 'badge-ok' : 'badge-muted')">
              FCM: {{ data.push.fcm ? t('admin.push.status.ready') : t('admin.push.status.not_configured') }}</span>
            <span :class="'badge ' + (data.push.apns ? 'badge-ok' : 'badge-muted')">
              APNs: {{ data.push.apns ? t('admin.push.status.ready') : t('admin.push.status.not_configured') }}</span>
          </div>
        </div>
        <div v-if="loading" class="kpis"><div class="kpi skeleton" v-for="n in 4" :key="n"></div></div>
        <div v-else-if="error" class="notice notice-danger" role="alert">{{ error }}
          <button type="button" class="btn btn-sm" @click="load">{{ t('common.refresh') }}</button></div>
        <template v-else-if="data">
          <div class="kpis">
            <div class="kpi"><div class="label">{{ t('admin.ops.installations') }}</div>
              <div class="value">{{ nf(data.summary.installations) }}</div>
              <div class="sub">{{ t('admin.ops.active_30d') }}: {{ nf(data.summary.active_30d) }}</div></div>
            <div class="kpi"><div class="label">Android · iOS</div>
              <div class="value">{{ nf(data.summary.android) }} · {{ nf(data.summary.ios) }}</div>
              <div class="sub">{{ t('admin.ops.anonymous') }}: {{ nf(data.summary.anonymous) }}</div></div>
            <div class="kpi"><div class="label">{{ t('admin.ops.reachable') }}</div>
              <div class="value">{{ nf(data.summary.push_reachable) }}</div>
              <div class="sub">{{ t('admin.ops.active_tokens') }}: FCM {{ nf(data.summary.fcm_active) }} · APNs {{ nf(data.summary.apns_active) }}</div></div>
            <div class="kpi"><div class="label">{{ t('admin.ops.invalid_tokens') }}</div>
              <div class="value">{{ nf(data.summary.invalid_tokens) }}</div>
              <div class="sub">{{ t('admin.ops.permission_denied') }}: {{ nf(data.summary.permission_denied) }}</div></div>
            <div class="kpi"><div class="label">{{ t('admin.ops.accepted_7d') }}</div>
              <div class="value">{{ nf(data.summary.deliveries_7d.provider_accepted) }}</div>
              <div class="sub">{{ t('admin.ops.deliveries_7d') }}: {{ nf(data.summary.deliveries_7d.total) }} ·
                {{ t('admin.push.stat.failed_short') }} {{ nf(data.summary.deliveries_7d.provider_failed + data.summary.deliveries_7d.invalid_token) }} ·
                {{ t('admin.push.stat.pending_short') }} {{ nf(pendingOf(data.summary.deliveries_7d)) }}</div></div>
            <div class="kpi"><div class="label">{{ t('admin.ops.opened_7d') }}</div>
              <div class="value">{{ nf(data.summary.deliveries_7d.opened) }}</div>
              <div class="sub">{{ t('admin.ops.opened_hint') }}</div></div>
            <div class="kpi"><div class="label">{{ t('admin.ops.logins_7d') }}</div>
              <div class="value">{{ nf(data.summary.login_success_7d) }}</div>
              <div class="sub">{{ t('admin.ops.login_failures') }}: {{ nf(data.summary.login_failure_7d) }}</div></div>
            <div class="kpi"><div class="label">{{ t('admin.ops.api_errors_24h') }}</div>
              <div class="value">{{ nf(data.summary.api_errors_24h) }}</div>
              <div class="sub">5xx: {{ nf(data.summary.server_errors_24h) }}</div></div>
          </div>
          <div class="charts">
            <div class="chart-card"><h3>{{ t('admin.ops.app_versions') }}</h3>
              <donut-chart :points="data.app_versions"/></div>
            <div class="chart-card"><h3>{{ t('admin.ops.os_versions') }}</h3>
              <donut-chart :points="data.os_versions"/></div>
          </div>
          <div class="card" style="margin-top:18px">
            <div class="card-head"><h2>{{ t('admin.ops.recent_errors') }}</h2>
              <div class="right" v-if="can('logs.read')">
                <a class="btn btn-sm" href="/admin/logs/errors" @click.prevent="go('/admin/logs/errors')">{{ t('admin.ops.all_errors') }}</a></div></div>
            <div class="table-wrap">
              <table>
                <thead><tr><th>{{ t('admin.audit.when') }}</th><th>{{ t('admin.logs.request') }}</th>
                  <th>HTTP</th><th>{{ t('admin.logs.error_code') }}</th><th>{{ t('admin.logs.client') }}</th>
                  <th>{{ t('admin.logs.request_id') }}</th></tr></thead>
                <tbody>
                  <tr v-for="row in data.recent_errors" :key="row.id" :class="{ clickable: can('logs.read') && row.request_id }"
                      :tabindex="can('logs.read') && row.request_id ? 0 : null" @click="openError(row)" @keydown.enter="openError(row)">
                    <td class="mono nowrap">{{ row.at }}</td>
                    <td class="mono">{{ row.method }} {{ row.route }}</td>
                    <td><span :class="'badge ' + httpBadge(row.status)">{{ row.status }}</span></td>
                    <td class="mono">{{ row.error_code || '—' }}</td>
                    <td>{{ platformName(row.platform) }} <span class="mono">{{ row.app_version }}</span></td>
                    <td class="mono">{{ row.request_id || '—' }}</td>
                  </tr>
                  <tr v-if="!data.recent_errors.length"><td colspan="6" class="empty">{{ t('common.empty') }}</td></tr>
                </tbody>
              </table>
            </div>
          </div>
        </template>
      </section>`
  };

  var Dashboard = {
    components: { LineChart: LineChart, BarChart: BarChart, DonutChart: DonutChart, OpsPanel: OpsPanel },
    data: function () {
      return { loading: true, data: null, range: "30d", from: "", to: "", error: "" };
    },
    mounted: function () { this.load(); },
    methods: {
      t: t, nf: nf, money: money,
      async load() {
        this.loading = true;
        try {
          var query = "?range=" + encodeURIComponent(this.range);
          if (this.range === "custom") query += "&from=" + this.from + "&to=" + this.to;
          this.data = await api("/dashboard" + query);
          this.from = this.data.range.from;
          this.to = this.data.range.to;
        } catch (e) { this.error = e.message; toast("danger", t("common.error")); }
        this.loading = false;
      },
      setRange(key) { this.range = key; this.load(); },
      applyCustom() { this.range = "custom"; this.load(); }
    },
    template: `
      <div>
        <div class="toolbar">
          <div class="segmented">
            <button v-for="key in ['today','7d','30d','month','prev_month']" :key="key"
                    :class="{ 'is-active': range === key }" @click="setRange(key)">{{ t('common.' + key) }}</button>
          </div>
          <div class="toolbar-right">
            <input type="date" v-model="from"><span class="dash">—</span><input type="date" v-model="to">
            <button class="btn btn-sm" @click="applyCustom">{{ t('common.apply') }}</button>
          </div>
        </div>

        <div v-if="loading" class="kpis">
          <div class="kpi skeleton" v-for="n in 8" :key="n"></div>
        </div>

        <template v-else-if="data">
          <div class="kpis">
            <div class="kpi"><div class="label">{{ t('admin.metric.total_users') }}</div>
              <div class="value">{{ nf(data.stats.total_users) }}</div>
              <div class="sub">{{ t('admin.metric.new_today') }}: {{ nf(data.stats.new_today) }}</div></div>
            <div class="kpi"><div class="label">{{ t('admin.metric.active') }}</div>
              <div class="value">{{ nf(data.stats.active_30d) }}</div>
              <div class="sub">{{ t('admin.metric.new_month') }}: {{ nf(data.stats.new_month) }}</div></div>
            <div class="kpi"><div class="label">{{ t('admin.metric.paid') }}</div>
              <div class="value">{{ nf(data.stats.paid_users) }}</div>
              <div class="sub">{{ t('admin.metric.free') }}: {{ nf(data.stats.free_users) }}</div></div>
            <div class="kpi"><div class="label">{{ t('admin.metric.requests_today') }}</div>
              <div class="value">{{ nf(data.stats.requests_today) }}</div>
              <div class="sub">{{ t('admin.metric.requests_range') }}: {{ nf(data.stats.requests_range) }}</div></div>
            <div class="kpi"><div class="label">{{ t('admin.metric.total_tokens') }}</div>
              <div class="value">{{ nf(data.stats.total_tokens) }}</div>
              <div class="sub">{{ nf(data.stats.input_tokens) }} / {{ nf(data.stats.output_tokens) }}</div></div>
            <div class="kpi"><div class="label">{{ t('admin.metric.cost') }}</div>
              <div class="value">{{ money(data.stats.cost_usd) }}</div>
              <div class="sub">{{ t('admin.settings.pricing') }}</div></div>
            <div class="kpi"><div class="label">{{ t('admin.metric.success') }}</div>
              <div class="value">{{ nf(data.stats.succeeded) }}</div>
              <div class="sub">{{ t('admin.metric.failed') }}: {{ nf(data.stats.failed) }}</div></div>
            <div class="kpi"><div class="label">{{ t('admin.metric.latency') }}</div>
              <div class="value">{{ nf(data.stats.avg_latency_ms) }} ms</div>
              <div class="sub">iOS {{ nf(data.stats.ios_users) }} · Android {{ nf(data.stats.android_users) }}</div></div>
          </div>

          <div class="charts">
            <div class="chart-card"><h3>{{ t('admin.chart.generations') }}</h3>
              <line-chart :points="data.series.generations"/></div>
            <div class="chart-card"><h3>{{ t('admin.chart.registrations') }}</h3>
              <bar-chart :points="data.series.registrations"/></div>
            <div class="chart-card"><h3>{{ t('admin.chart.active') }}</h3>
              <line-chart :points="data.series.active_users"/></div>
            <div class="chart-card"><h3>{{ t('admin.chart.tokens') }}</h3>
              <bar-chart :points="data.series.tokens"/></div>
            <div class="chart-card"><h3>{{ t('admin.chart.cost') }}</h3>
              <line-chart :points="data.series.cost" unit="$"/></div>
            <div class="chart-card"><h3>{{ t('admin.chart.errors') }}</h3>
              <bar-chart :points="data.series.errors"/></div>
            <div class="chart-card"><h3>{{ t('admin.chart.plans') }}</h3>
              <donut-chart :points="data.series.plan_mix"/></div>
            <div class="chart-card"><h3>{{ t('admin.chart.platforms') }}</h3>
              <donut-chart :points="data.series.platform_mix"/></div>
          </div>

          <div class="card" style="margin-top:18px">
            <div class="card-head"><h2>{{ t('admin.chart.topcost') }}</h2></div>
            <div class="table-wrap">
              <table>
                <thead><tr><th>{{ t('admin.users.col_id') }}</th><th>{{ t('admin.metric.cost') }}</th></tr></thead>
                <tbody>
                  <tr v-for="row in data.series.top_cost" :key="row.label" class="clickable"
                      @click="$root.go('/admin/users/' + row.label)">
                    <td class="mono">{{ row.label.slice(0, 8) }}</td><td>{{ money(row.value) }}</td>
                  </tr>
                  <tr v-if="!data.series.top_cost.length"><td colspan="2" class="empty">{{ t('common.empty') }}</td></tr>
                </tbody>
              </table>
            </div>
          </div>
        </template>

        <ops-panel/>
      </div>`
  };

  var Users = {
    data: function () {
      return { loading: true, rows: [], total: 0, page: 1, limit: 25,
               filters: { q: "", status: "", platform: "", plan: "" }, plans: [] };
    },
    mounted: function () { this.load(); this.loadPlans(); },
    methods: {
      t: t, nf: nf,
      async load() {
        this.loading = true;
        try {
          var params = new URLSearchParams({ page: this.page, limit: this.limit });
          for (var key in this.filters) { if (this.filters[key]) params.set(key, this.filters[key]); }
          var data = await api("/users?" + params.toString());
          this.rows = data.users; this.total = data.total;
        } catch (e) { toast("danger", t("common.error")); }
        this.loading = false;
      },
      async loadPlans() {
        try { this.plans = (await api("/plans")).plans; } catch (e) { this.plans = []; }
      },
      search() { this.page = 1; this.load(); },
      reset() { this.filters = { q: "", status: "", platform: "", plan: "" }; this.search(); },
      move(delta) { this.page = Math.max(1, this.page + delta); this.load(); },
      usageWidth(row) { return row.daily_limit ? Math.min(100, Math.round(row.used_today / row.daily_limit * 100)) : 0; }
    },
    template: `
      <div class="card">
        <div class="card-head">
          <h2>{{ t('admin.users.title') }}</h2>
          <div class="right"><span class="badge badge-muted">{{ t('common.total') }}: {{ nf(total) }}</span></div>
        </div>
        <div class="card-body filters-row">
          <input type="text" v-model="filters.q" :placeholder="t('admin.users.search')"
                 @keyup.enter="search" style="min-width:260px; flex:1">
          <select v-model="filters.status" @change="search">
            <option value="">{{ t('common.all') }}</option>
            <option value="active">{{ t('common.active') }}</option>
            <option value="disabled">{{ t('common.disabled') }}</option>
          </select>
          <select v-model="filters.platform" @change="search">
            <option value="">{{ t('common.all') }}</option>
            <option value="ios">iOS</option><option value="android">Android</option><option value="legacy">Legacy</option>
          </select>
          <select v-model="filters.plan" @change="search">
            <option value="">{{ t('common.all') }}</option>
            <option v-for="p in plans" :key="p.id" :value="p.id">{{ p.code }}</option>
          </select>
          <button class="btn btn-sm btn-primary" @click="search">{{ t('common.search') }}</button>
          <button class="btn btn-sm" @click="reset">{{ t('common.reset') }}</button>
        </div>
        <div class="table-wrap">
          <table>
            <thead><tr>
              <th>{{ t('admin.users.col_identifier') }}</th><th>{{ t('admin.users.col_plan') }}</th>
              <th>{{ t('admin.users.col_today') }}</th><th>{{ t('admin.users.col_tokens') }}</th>
              <th>{{ t('admin.users.col_platform') }}</th><th>{{ t('admin.users.col_registered') }}</th>
              <th>{{ t('admin.users.col_last') }}</th><th>{{ t('admin.users.col_status') }}</th>
            </tr></thead>
            <tbody>
              <tr v-if="loading" v-for="n in 6" :key="'s' + n"><td colspan="8"><div class="skeleton-row"></div></td></tr>
              <tr v-else v-for="row in rows" :key="row.id" class="clickable" @click="$root.go('/admin/users/' + row.id)">
                <td><b>{{ row.identifier }}</b><div class="mono">{{ row.id.slice(0, 8) }}</div></td>
                <td><span class="badge badge-brand" v-if="row.plan_code">{{ row.plan_code }}</span>
                    <span v-else class="badge badge-muted">—</span></td>
                <td>
                  <div class="usage"><b>{{ row.used_today }}</b> / {{ row.daily_limit }}</div>
                  <div class="meter"><span :style="{ width: usageWidth(row) + '%' }"></span></div>
                </td>
                <td>{{ nf(row.tokens_month) }}</td>
                <td>{{ row.platform || '—' }} <span class="mono" v-if="row.app_version">{{ row.app_version }}</span></td>
                <td class="mono">{{ row.created_at }}</td>
                <td class="mono">{{ row.last_active || '—' }}</td>
                <td><span :class="'badge ' + (row.status === 'active' ? 'badge-ok' : 'badge-danger')">
                  {{ row.status === 'active' ? t('common.active') : t('common.disabled') }}</span></td>
              </tr>
              <tr v-if="!loading && !rows.length"><td colspan="8" class="empty">{{ t('common.empty') }}</td></tr>
            </tbody>
          </table>
        </div>
        <div class="pagination">
          <button class="btn btn-sm" :disabled="page === 1" @click="move(-1)">{{ t('common.prev') }}</button>
          <span>{{ t('common.page') }} {{ page }}</span>
          <button class="btn btn-sm" :disabled="page * limit >= total" @click="move(1)">{{ t('common.next') }}</button>
        </div>
      </div>`
  };

  // UserDiagnostics — қолдау қызметіне арналған толық көрініс. Ашу аудитке жазылады
  // және толық пошта/телефонды көрсетеді, сондықтан тек батырмамен жүктеледі.
  var UserDiagnostics = {
    components: { NoAccess: NoAccess },
    props: { id: String },
    data: function () { return { loading: false, data: null, tab: "account", denied: "", error: "" }; },
    computed: {
      tabs: function () {
        var d = this.data;
        return [
          { key: "account", label: t("admin.diag.account") },
          { key: "devices", label: t("admin.diag.devices"), count: d.installations.length },
          { key: "sessions", label: t("admin.diag.sessions"), count: d.sessions.length },
          { key: "auth", label: t("admin.diag.auth_events"), count: d.auth_events.length },
          { key: "api", label: t("admin.diag.api_errors"), count: d.api_errors.length },
          { key: "app", label: t("admin.diag.app_errors"), count: d.app_errors.length },
          { key: "push", label: t("admin.diag.notifications"), count: d.notifications.length }
        ];
      }
    },
    methods: Object.assign({}, helpers, {
      async load() {
        if (this.loading) return;
        this.loading = true;
        this.error = "";
        try { this.data = await api("/users/" + this.id + "/diagnostics"); this.denied = ""; }
        catch (e) {
          if (e.code === "FORBIDDEN") this.denied = e.details.permission || "users.diagnostics.read";
          else this.error = errorText(e);
        }
        this.loading = false;
      }
    }),
    template: `
      <div class="card diag" style="margin-top:18px">
        <div class="card-head">
          <h2>{{ t('admin.diag.title') }}</h2>
          <div class="right">
            <button type="button" class="btn btn-sm" :class="{ 'btn-primary': !data }" :disabled="loading" @click="load">
              {{ data ? t('common.refresh') : t('admin.diag.open') }}</button>
          </div>
        </div>
        <div class="card-body" v-if="denied"><no-access :permission="denied"/></div>
        <div class="card-body" v-else-if="!data">
          <p class="muted" style="margin:0">{{ t('admin.diag.hint') }}</p>
          <div v-if="loading" class="skeleton-row" style="margin-top:14px"></div>
          <div v-if="error" class="notice notice-danger" role="alert" style="margin:14px 0 0">{{ error }}</div>
        </div>
        <template v-else>
          <div class="subnav subnav-inner" role="tablist">
            <button v-for="item in tabs" :key="item.key" type="button" role="tab" :aria-selected="tab === item.key"
                    :class="{ 'is-active': tab === item.key }" @click="tab = item.key">
              {{ item.label }}<span v-if="item.count !== undefined" class="count">{{ item.count }}</span></button>
          </div>

          <div class="card-body" v-if="tab === 'account'">
            <div class="diag-grid">
              <dl class="kv">
                <dt>{{ t('admin.users.col_id') }}</dt><dd class="mono">{{ data.user.id }}</dd>
                <dt>{{ t('admin.login.email') }}</dt><dd>{{ data.user.email || '—' }}</dd>
                <dt>{{ t('admin.diag.phone') }}</dt><dd>{{ data.user.phone || '—' }}</dd>
                <dt>{{ t('admin.users.col_status') }}</dt>
                <dd><span :class="'badge ' + (data.user.status === 'active' ? 'badge-ok' : 'badge-danger')">
                  {{ data.user.status === 'active' ? t('common.active') : t('common.disabled') }}</span></dd>
                <dt>{{ t('admin.diag.sign_in') }}</dt>
                <dd><span v-for="m in data.user.sign_in_methods" :key="m" class="badge badge-brand" style="margin-right:4px">{{ m }}</span>
                  <span v-if="!data.user.sign_in_methods.length">—</span></dd>
                <dt>{{ t('common.language') }}</dt><dd>{{ data.user.locale || '—' }}</dd>
                <dt>{{ t('admin.settings.timezone') }}</dt><dd>{{ data.user.timezone || '—' }}</dd>
                <dt>{{ t('admin.users.col_registered') }}</dt><dd>{{ data.user.created_at }}</dd>
                <dt>{{ t('admin.users.col_last') }}</dt><dd>{{ data.user.last_active || '—' }}</dd>
              </dl>
              <dl class="kv">
                <dt>{{ t('admin.users.col_plan') }}</dt>
                <dd>{{ data.subscription.plan_name }} <span class="mono">{{ data.subscription.plan_code }}</span>
                  <span v-if="data.subscription.paid" class="badge badge-brand">{{ t('admin.diag.paid_plan') }}</span></dd>
                <dt>{{ t('admin.users.col_sub') }}</dt><dd>{{ data.subscription.status || '—' }}</dd>
                <dt>{{ t('admin.diag.expires') }}</dt><dd>{{ data.subscription.expires_at || '—' }}</dd>
              </dl>
            </div>
          </div>

          <div class="table-wrap" v-else-if="tab === 'devices'">
            <table>
              <thead><tr><th>{{ t('admin.push.device.device') }}</th><th>{{ t('admin.push.device.os') }}</th>
                <th>{{ t('admin.push.device.app') }}</th><th>{{ t('common.language') }}</th>
                <th>{{ t('admin.push.device.permission') }}</th><th>{{ t('admin.push.device.switch') }}</th>
                <th>{{ t('admin.push.device.push') }}</th><th>{{ t('admin.push.device.token') }}</th>
                <th>{{ t('admin.push.device.first_seen') }}</th><th>{{ t('admin.push.device.last_seen') }}</th></tr></thead>
              <tbody>
                <tr v-for="d in data.installations" :key="d.id">
                  <td><b>{{ d.device || '—' }}</b><div class="mono">{{ d.device_model }}</div></td>
                  <td>{{ platformName(d.platform) }}<div class="mono">{{ d.os }}</div></td>
                  <td class="mono">{{ d.app_version }} ({{ d.app_build || '—' }})</td>
                  <td>{{ d.locale || '—' }}<div class="mono">{{ d.timezone }}</div></td>
                  <td><span :class="'badge ' + permissionBadge(d.push.permission)">{{ t('admin.push.permission.' + d.push.permission) }}</span></td>
                  <td>{{ d.push.enabled ? t('admin.push.status.on') : t('admin.push.status.off') }}</td>
                  <td><span :class="'badge ' + pushBadge(d.push.status)">{{ t('admin.push.push_status.' + d.push.status) }}</span>
                    <div class="mono" v-if="d.push.reason">{{ d.push.reason }}</div></td>
                  <td class="mono">{{ d.push.token || '—' }}<div v-if="d.push.environment">{{ d.push.environment }}</div></td>
                  <td class="mono nowrap">{{ d.first_seen }}</td><td class="mono nowrap">{{ d.last_seen }}</td>
                </tr>
                <tr v-if="!data.installations.length"><td colspan="10" class="empty">{{ t('common.empty') }}</td></tr>
              </tbody>
            </table>
          </div>

          <div class="table-wrap" v-else-if="tab === 'sessions'">
            <table>
              <thead><tr><th>{{ t('admin.diag.started') }}</th><th>{{ t('admin.diag.last_activity') }}</th>
                <th>{{ t('admin.diag.duration') }}</th><th>{{ t('admin.diag.events') }}</th>
                <th>{{ t('admin.push.device.device') }}</th><th>{{ t('admin.push.device.app') }}</th>
                <th>{{ t('admin.diag.session') }}</th></tr></thead>
              <tbody>
                <tr v-for="s in data.sessions" :key="s.session_id + s.started_at">
                  <td class="mono nowrap">{{ s.started_at }}</td>
                  <td class="mono nowrap">{{ s.last_activity_at }}<div v-if="s.ended_at">{{ t('admin.diag.ended') }} {{ s.ended_at }}</div></td>
                  <td>{{ duration(s.duration_s) }}</td><td>{{ nf(s.events) }}</td>
                  <td>{{ s.device || '—' }}<div class="mono">{{ platformName(s.platform) }} {{ s.os_version }}</div></td>
                  <td class="mono">{{ s.app_version }} ({{ s.app_build || '—' }})</td>
                  <td class="mono">{{ s.session_id }}</td>
                </tr>
                <tr v-if="!data.sessions.length"><td colspan="7" class="empty">{{ t('common.empty') }}</td></tr>
              </tbody>
            </table>
          </div>

          <div class="table-wrap" v-else-if="tab === 'auth'">
            <table>
              <thead><tr><th>{{ t('admin.audit.when') }}</th><th>{{ t('admin.logs.event') }}</th>
                <th>{{ t('admin.logs.method') }}</th><th>{{ t('admin.logs.outcome') }}</th>
                <th>{{ t('admin.logs.error_code') }}</th><th>{{ t('admin.logs.client') }}</th><th>IP</th>
                <th>{{ t('admin.logs.request_id') }}</th></tr></thead>
              <tbody>
                <tr v-for="e in data.auth_events" :key="e.id">
                  <td class="mono nowrap">{{ e.at }}</td><td class="mono">{{ e.name }}</td><td>{{ e.method || '—' }}</td>
                  <td><span :class="'badge ' + (e.outcome === 'success' ? 'badge-ok' : 'badge-danger')">{{ t('admin.logs.outcome.' + e.outcome) }}</span></td>
                  <td class="mono">{{ e.error_code || '—' }}</td>
                  <td>{{ platformName(e.platform) }} <span class="mono">{{ e.app_version }}</span></td>
                  <td class="mono">{{ maskIP(e.ip) || '—' }}</td><td class="mono">{{ e.request_id || '—' }}</td>
                </tr>
                <tr v-if="!data.auth_events.length"><td colspan="8" class="empty">{{ t('common.empty') }}</td></tr>
              </tbody>
            </table>
          </div>

          <div class="table-wrap" v-else-if="tab === 'api'">
            <table>
              <thead><tr><th>{{ t('admin.audit.when') }}</th><th>{{ t('admin.logs.request') }}</th><th>HTTP</th>
                <th>{{ t('admin.logs.error_code') }}</th><th>{{ t('admin.logs.client') }}</th>
                <th>{{ t('admin.logs.duration') }}</th><th>{{ t('admin.logs.request_id') }}</th></tr></thead>
              <tbody>
                <tr v-for="e in data.api_errors" :key="e.id">
                  <td class="mono nowrap">{{ e.at }}</td><td class="mono">{{ e.method }} {{ e.route }}</td>
                  <td><span :class="'badge ' + httpBadge(e.status)">{{ e.status }}</span></td>
                  <td class="mono">{{ e.error_code || '—' }}</td>
                  <td>{{ platformName(e.platform) }} <span class="mono">{{ e.app_version }} {{ e.app_build ? '(' + e.app_build + ')' : '' }}</span></td>
                  <td>{{ nf(e.duration_ms) }} ms</td><td class="mono">{{ e.request_id || '—' }}</td>
                </tr>
                <tr v-if="!data.api_errors.length"><td colspan="7" class="empty">{{ t('common.empty') }}</td></tr>
              </tbody>
            </table>
          </div>

          <div class="table-wrap" v-else-if="tab === 'app'">
            <table>
              <thead><tr><th>{{ t('admin.audit.when') }}</th><th>{{ t('admin.logs.event') }}</th>
                <th>{{ t('admin.logs.error_code') }}</th><th>{{ t('admin.logs.client') }}</th>
                <th>{{ t('admin.push.device.device') }}</th><th>{{ t('admin.logs.properties') }}</th></tr></thead>
              <tbody>
                <tr v-for="e in data.app_errors" :key="e.id">
                  <td class="mono nowrap">{{ e.occurred_at }}</td><td class="mono">{{ e.name }}</td>
                  <td class="mono">{{ e.error_code || '—' }}</td>
                  <td>{{ platformName(e.platform) }} <span class="mono">{{ e.app_version }} {{ e.app_build ? '(' + e.app_build + ')' : '' }}</span></td>
                  <td>{{ e.device || '—' }}</td>
                  <td><ul class="meta-list"><li v-for="p in pairs(e.properties)" :key="p.key">
                    <span class="k">{{ p.key }}:</span> <span class="v">{{ p.value }}</span></li></ul></td>
                </tr>
                <tr v-if="!data.app_errors.length"><td colspan="6" class="empty">{{ t('common.empty') }}</td></tr>
              </tbody>
            </table>
          </div>

          <div class="table-wrap" v-else-if="tab === 'push'">
            <table>
              <thead><tr><th>{{ t('admin.push.delivery.created') }}</th><th>{{ t('admin.push.delivery.notification') }}</th>
                <th>{{ t('admin.push.device.device') }}</th><th>{{ t('admin.users.col_status') }}</th>
                <th>{{ t('admin.push.delivery.error') }}</th><th>{{ t('admin.push.delivery.sent') }}</th>
                <th>{{ t('admin.push.delivery.opened') }}</th></tr></thead>
              <tbody>
                <tr v-for="d in data.notifications" :key="d.id">
                  <td class="mono nowrap">{{ d.created_at }}</td>
                  <td><a v-if="d.campaign_id && can('notifications.read')" class="link" :href="'/admin/notifications/campaigns/' + d.campaign_id"
                         @click.prevent="go('/admin/notifications/campaigns/' + d.campaign_id)">{{ d.campaign_name || d.title }}</a>
                    <span v-else>{{ d.title }}</span>
                    <div class="muted small">{{ t('admin.push.category.' + d.category) }}<span v-if="!d.campaign_id" class="mono"> · {{ d.type }}</span></div></td>
                  <td class="nowrap">{{ d.device || platformName(d.platform) }}<div class="mono">{{ d.push || '—' }}</div></td>
                  <td class="nowrap"><span :class="'badge ' + deliveryBadge(d.status)">{{ t('admin.push.delivery_status.' + d.status) }}</span>
                    <div class="muted small">{{ t('admin.push.delivery.attempts') }}: {{ d.attempts }}</div></td>
                  <td class="mono">{{ d.error_code || '—' }}</td>
                  <td class="mono nowrap">{{ d.sent_at || d.failed_at || '—' }}</td><td class="mono nowrap">{{ d.opened_at || '—' }}</td>
                </tr>
                <tr v-if="!data.notifications.length"><td colspan="7" class="empty">{{ t('common.empty') }}</td></tr>
              </tbody>
            </table>
          </div>
        </template>
      </div>`
  };

  var UserDetail = {
    components: { UserDiagnostics: UserDiagnostics },
    props: { id: String },
    data: function () { return { loading: true, data: null, plans: [], planID: "", expires: "" }; },
    mounted: function () { this.load(); },
    methods: {
      t: t, nf: nf, money: money, can: can,
      async load() {
        this.loading = true;
        try {
          this.data = await api("/users/" + this.id);
          this.plans = (await api("/plans")).plans;
          this.planID = this.data.entitlement.plan_id;
          this.expires = this.data.subscription.expires_at || "";
        } catch (e) { toast("danger", t("common.error")); }
        this.loading = false;
      },
      async act(path, body, confirmKey) {
        if (confirmKey && !window.confirm(t(confirmKey) + "?")) return;
        try {
          await api("/users/" + this.id + path, { method: "POST", body: body || {} });
          toast("ok", t("common.saved"));
          this.load();
        } catch (e) { toast("danger", t("common.error")); }
      },
      toggleStatus() {
        var next = this.data.user.status === "active" ? "disabled" : "active";
        this.act("/status", { status: next }, "common.confirm");
      },
      savePlan() { this.act("/plan", { plan_id: this.planID, expires_at: this.expires }); },
      usagePercent() {
        var e = this.data.entitlement;
        return e.daily_limit ? Math.min(100, Math.round(e.used_today / e.daily_limit * 100)) : 0;
      }
    },
    template: `
      <div v-if="data">
        <div class="toolbar">
          <button class="btn btn-sm" @click="$root.go('/admin/users')">← {{ t('common.back') }}</button>
        </div>
        <div class="notice notice-info">{{ t('admin.user.no_messages') }}</div>
        <div class="detail-grid">
          <div>
            <div class="card">
              <div class="card-head">
                <h2>{{ data.user.identifier }}</h2>
                <div class="right">
                  <span :class="'badge ' + (data.user.status === 'active' ? 'badge-ok' : 'badge-danger')">
                    {{ data.user.status === 'active' ? t('common.active') : t('common.disabled') }}</span>
                </div>
              </div>
              <div class="card-body">
                <div class="quota-box">
                  <div class="quota-head">
                    <span>{{ t('admin.users.col_today') }}</span>
                    <b>{{ data.entitlement.used_today }} / {{ data.entitlement.daily_limit }}</b>
                  </div>
                  <div class="meter meter-lg"><span :style="{ width: usagePercent() + '%' }"></span></div>
                  <small>{{ t('common.today') }} · {{ data.entitlement.resets_at }}</small>
                </div>
                <dl class="kv">
                  <dt>{{ t('admin.users.col_id') }}</dt><dd class="mono">{{ data.user.id }}</dd>
                  <dt>{{ t('admin.users.col_registered') }}</dt><dd>{{ data.user.created_at }}</dd>
                  <dt>{{ t('admin.users.col_last') }}</dt><dd>{{ data.user.last_active || '—' }}</dd>
                  <dt>{{ t('admin.users.col_platform') }}</dt><dd>{{ data.user.platform || '—' }} {{ data.user.app_version }}</dd>
                  <dt>{{ t('common.language') }}</dt><dd>{{ data.user.locale }}</dd>
                  <dt>{{ t('admin.users.col_plan') }}</dt><dd>{{ data.entitlement.plan_name }}
                    <span class="mono">{{ data.entitlement.plan_code }}</span></dd>
                  <dt>{{ t('admin.users.col_sub') }}</dt><dd>{{ data.subscription.status }}
                    <span v-if="data.subscription.expires_at">· {{ data.subscription.expires_at }}</span></dd>
                </dl>
              </div>
            </div>

            <div class="card">
              <div class="card-head"><h2>{{ t('admin.user.events') }}</h2></div>
              <div class="table-wrap">
                <table>
                  <thead><tr><th>{{ t('admin.audit.when') }}</th><th>{{ t('admin.users.col_status') }}</th>
                    <th>{{ t('admin.settings.model') }}</th><th>Tokens</th><th>{{ t('admin.metric.cost') }}</th><th>ms</th></tr></thead>
                  <tbody>
                    <tr v-for="(e, i) in data.events" :key="i">
                      <td class="mono">{{ e.at }}</td>
                      <td><span v-if="e.status === 'success'" class="badge badge-ok">ok</span>
                          <span v-else class="badge badge-danger">{{ e.error_code }}</span></td>
                      <td class="mono">{{ e.model }}</td>
                      <td>{{ e.input_tokens }} / {{ e.output_tokens }}</td>
                      <td>{{ money(e.cost_usd) }}</td><td>{{ e.latency_ms }}</td>
                    </tr>
                    <tr v-if="!data.events.length"><td colspan="6" class="empty">{{ t('common.empty') }}</td></tr>
                  </tbody>
                </table>
              </div>
            </div>

            <div class="card">
              <div class="card-head"><h2>{{ t('admin.user.devices') }}</h2></div>
              <div class="table-wrap">
                <table>
                  <thead><tr><th>ID</th><th>{{ t('admin.users.col_platform') }}</th><th>{{ t('admin.users.col_version') }}</th>
                    <th>Push</th><th>{{ t('admin.users.col_last') }}</th></tr></thead>
                  <tbody>
                    <tr v-for="d in data.devices" :key="d.id">
                      <td class="mono">{{ d.id.slice(0, 8) }}</td><td>{{ d.platform }}</td><td>{{ d.app_version }}</td>
                      <td><span :class="'badge ' + (d.push_enabled ? 'badge-ok' : 'badge-muted')">
                        {{ d.push_enabled ? 'on' : 'off' }}</span></td>
                      <td class="mono">{{ d.last_seen }}</td>
                    </tr>
                    <tr v-if="!data.devices.length"><td colspan="5" class="empty">{{ t('common.empty') }}</td></tr>
                  </tbody>
                </table>
              </div>
            </div>
          </div>

          <div>
            <div class="card" v-if="can('users.write')">
              <div class="card-head"><h2>{{ t('admin.user.actions') }}</h2></div>
              <div class="card-body">
                <button class="btn" :class="data.user.status === 'active' ? 'btn-danger' : 'btn-primary'"
                        style="width:100%; margin-bottom:14px" @click="toggleStatus">
                  {{ data.user.status === 'active' ? t('admin.user.deactivate') : t('admin.user.activate') }}
                </button>
                <label class="field"><span>{{ t('admin.user.change_plan') }}</span>
                  <select v-model="planID">
                    <option v-for="p in plans" :key="p.id" :value="p.id">
                      {{ p.code }} · {{ p.daily_message_limit }}/{{ t('pricing.per_day') }}</option>
                  </select></label>
                <label class="field"><span>{{ t('admin.user.set_expiry') }}</span>
                  <input type="date" v-model="expires"></label>
                <button class="btn btn-primary" style="width:100%; margin-bottom:14px" @click="savePlan">
                  {{ t('common.save') }}</button>
                <button class="btn" style="width:100%; margin-bottom:10px"
                        @click="act('/reset-quota', {}, 'admin.user.reset_quota')">{{ t('admin.user.reset_quota') }}</button>
                <button class="btn btn-danger" style="width:100%"
                        @click="act('/revoke-sessions', {}, 'admin.user.revoke')">
                  {{ t('admin.user.revoke') }} ({{ data.sessions.length }})</button>
              </div>
            </div>

            <div class="card">
              <div class="card-head"><h2>{{ t('admin.user.payments') }}</h2></div>
              <div class="table-wrap">
                <table>
                  <thead><tr><th>{{ t('admin.audit.when') }}</th><th>{{ t('admin.plans.price') }}</th>
                    <th>{{ t('admin.users.col_status') }}</th></tr></thead>
                  <tbody>
                    <tr v-for="(p, i) in data.payments" :key="i">
                      <td class="mono">{{ p.at }}</td><td>{{ p.amount }}</td><td>{{ p.status }}</td></tr>
                    <tr v-if="!data.payments.length"><td colspan="3" class="empty">{{ t('common.empty') }}</td></tr>
                  </tbody>
                </table>
              </div>
            </div>
          </div>
        </div>

        <user-diagnostics v-if="can('users.diagnostics.read')" :id="id"/>
      </div>
      <div v-else class="empty">…</div>`
  };

  var Plans = {
    data: function () {
      return { loading: true, plans: [], editing: null, tab: "kk", saving: false };
    },
    mounted: function () { this.load(); },
    methods: {
      t: t, nf: nf, can: can,
      async load() {
        this.loading = true;
        try { this.plans = (await api("/plans")).plans; }
        catch (e) { toast("danger", t("common.error")); }
        this.loading = false;
      },
      blank() {
        return { id: "", code: "", name: { kk: "", ru: "", en: "", uz: "" },
                 description: { kk: "", ru: "", en: "", uz: "" }, price: 0, currency: "KZT",
                 daily_message_limit: 30, monthly_message_limit: 0, period_days: 30,
                 is_free: false, is_active: true, sort_order: 50 };
      },
      create() { this.editing = this.blank(); this.tab = "kk"; },
      edit(plan) { this.editing = JSON.parse(JSON.stringify(plan)); this.tab = "kk"; },
      close() { this.editing = null; },
      async save() {
        this.saving = true;
        var body = this.editing;
        try {
          if (body.id) await api("/plans/" + body.id, { method: "PATCH", body: body });
          else await api("/plans", { method: "POST", body: body });
          toast("ok", t("common.saved"));
          this.close(); this.load();
        } catch (e) { toast("danger", e.message === "CONFLICT" ? t("common.error") : t("common.error")); }
        this.saving = false;
      },
      async archive(plan) {
        if (!window.confirm(t("admin.plans.archive_confirm"))) return;
        try { await api("/plans/" + plan.id + "/archive", { method: "POST", body: {} });
              toast("ok", t("common.saved")); this.load(); }
        catch (e) { toast("danger", t("common.error")); }
      }
    },
    template: `
      <div>
        <div class="card">
          <div class="card-head">
            <h2>{{ t('admin.plans.title') }}</h2>
            <div class="right" v-if="can('plans.write')"><button class="btn btn-sm btn-primary" @click="create">+ {{ t('admin.plans.new') }}</button></div>
          </div>
          <div class="table-wrap">
            <table>
              <thead><tr><th>{{ t('admin.plans.code') }}</th><th>{{ t('admin.plans.name') }}</th>
                <th>{{ t('admin.plans.daily') }}</th><th>{{ t('admin.plans.monthly') }}</th>
                <th>{{ t('admin.plans.price') }}</th><th>{{ t('admin.plans.subscribers') }}</th>
                <th>{{ t('admin.users.col_status') }}</th><th></th></tr></thead>
              <tbody>
                <tr v-for="p in plans" :key="p.id">
                  <td><b>{{ p.code }}</b> <span v-if="p.is_free" class="badge badge-muted">free</span></td>
                  <td>{{ p.name[$root.state.locale] || p.name.en }}</td>
                  <td><b>{{ p.daily_message_limit }}</b></td>
                  <td>{{ p.monthly_message_limit || '∞' }}</td>
                  <td>{{ p.price_text }}</td>
                  <td>{{ p.subscribers }}</td>
                  <td><span :class="'badge ' + (p.is_active ? 'badge-ok' : 'badge-muted')">
                    {{ p.is_active ? t('common.active') : t('common.disabled') }}</span></td>
                  <td style="text-align:right; white-space:nowrap">
                    <template v-if="can('plans.write')">
                      <button class="btn btn-sm" @click="edit(p)">{{ t('common.edit') }}</button>
                      <button class="btn btn-sm btn-danger" @click="archive(p)">{{ t('common.archive') }}</button>
                    </template>
                  </td>
                </tr>
                <tr v-if="!plans.length && !loading"><td colspan="8" class="empty">{{ t('common.empty') }}</td></tr>
              </tbody>
            </table>
          </div>
        </div>

        <div class="modal-backdrop" v-if="editing" @click.self="close">
          <div class="modal">
            <div class="modal-head">
              <h2>{{ editing.id ? editing.code : t('admin.plans.new') }}</h2>
              <button class="btn btn-sm" @click="close">✕</button>
            </div>
            <div class="modal-body">
              <div class="form-grid">
                <label class="field"><span>{{ t('admin.plans.code') }}</span>
                  <input type="text" v-model="editing.code" placeholder="pro"></label>
                <label class="field"><span>{{ t('admin.plans.sort') }}</span>
                  <input type="number" v-model.number="editing.sort_order"></label>
              </div>
              <div class="tabs">
                <button v-for="l in $root.state.locales" :key="l" :class="{ 'is-active': tab === l }"
                        @click="tab = l">{{ l.toUpperCase() }}</button>
              </div>
              <label class="field"><span>{{ t('admin.plans.name') }}</span>
                <input type="text" v-model="editing.name[tab]"></label>
              <label class="field"><span>{{ t('admin.plans.description') }}</span>
                <textarea rows="2" v-model="editing.description[tab]"></textarea></label>
              <div class="form-grid">
                <label class="field"><span>{{ t('admin.plans.daily') }}</span>
                  <input type="number" v-model.number="editing.daily_message_limit" min="0"></label>
                <label class="field"><span>{{ t('admin.plans.monthly') }}</span>
                  <input type="number" v-model.number="editing.monthly_message_limit" min="0"></label>
                <label class="field"><span>{{ t('admin.plans.price') }}</span>
                  <input type="number" v-model.number="editing.price" min="0"></label>
                <label class="field"><span>{{ t('admin.plans.currency') }}</span>
                  <input type="text" v-model="editing.currency" maxlength="3"></label>
                <label class="field"><span>{{ t('admin.plans.period') }}</span>
                  <input type="number" v-model.number="editing.period_days" min="0"></label>
              </div>
              <label class="checkline"><input type="checkbox" v-model="editing.is_free"> {{ t('admin.plans.is_free') }}</label>
              <label class="checkline"><input type="checkbox" v-model="editing.is_active"> {{ t('admin.plans.is_active') }}</label>
            </div>
            <div class="modal-foot">
              <button class="btn" @click="close">{{ t('common.cancel') }}</button>
              <button class="btn btn-primary" :disabled="saving" @click="save">{{ t('common.save') }}</button>
            </div>
          </div>
        </div>
      </div>`
  };

  // Аудит нысандарының түрлері (сүзгіге ұсыныс; еркін мән де жарайды).
  var AUDIT_ENTITY_TYPES = ["user", "plan", "notification_campaign", "system_settings", "model_pricing", "admin_user"];

  var Audit = {
    mixins: [listView({ endpoint: "/audit", rowsKey: "entries", limit: 50,
      defaults: { action: "", admin: "", entity_type: "", entity_id: "" } })],
    data: function () { return { entityTypes: AUDIT_ENTITY_TYPES }; },
    methods: Object.assign({}, helpers, {
      // Нысан сілтемесі: қолданушы не науқан беті (рұқсат болса).
      entityPath: function (row) {
        if (!row.entity_id) return "";
        if (row.entity_type === "user" && can("users.read")) return "/admin/users/" + row.entity_id;
        if (row.entity_type === "notification_campaign" && can("notifications.read")) {
          return "/admin/notifications/campaigns/" + row.entity_id;
        }
        return "";
      }
    }),
    template: `
      <div class="card">
        <div class="card-head"><h2>{{ t('admin.audit.title') }}</h2>
          <div class="right"><span class="badge badge-muted">{{ t('common.total') }}: {{ nf(total) }}</span></div></div>
        <form class="filter-grid" @submit.prevent="search">
          <label><span>{{ t('admin.audit.action') }}</span>
            <input type="text" v-model.trim="filters.action" placeholder="notification." aria-describedby="audit-action-hint"></label>
          <label><span>{{ t('admin.audit.admin') }}</span>
            <input type="text" v-model.trim="filters.admin" :placeholder="t('admin.audit.admin_hint')"></label>
          <label><span>{{ t('admin.audit.entity_type') }}</span>
            <input type="text" v-model.trim="filters.entity_type" list="audit-entity-types">
            <datalist id="audit-entity-types"><option v-for="e in entityTypes" :key="e" :value="e"></option></datalist></label>
          <label><span>{{ t('admin.audit.entity_id') }}</span>
            <input type="text" v-model.trim="filters.entity_id"></label>
          <div class="filter-actions">
            <button type="submit" class="btn btn-sm btn-primary">{{ t('common.search') }}</button>
            <button type="button" class="btn btn-sm" @click="reset">{{ t('common.reset') }}</button>
          </div>
        </form>
        <p id="audit-action-hint" class="hint card-note">{{ t('admin.audit.action_hint') }}</p>
        <div class="card-body" v-if="denied"><no-access :permission="denied"/></div>
        <div class="table-wrap" v-else>
          <table>
            <thead><tr><th>{{ t('admin.audit.when') }}</th><th>{{ t('admin.audit.admin') }}</th>
              <th>{{ t('admin.audit.action') }}</th><th>{{ t('admin.audit.entity') }}</th>
              <th>{{ t('admin.audit.details') }}</th><th>{{ t('admin.logs.request_id') }}</th><th>IP</th></tr></thead>
            <tbody>
              <tr v-if="loading" v-for="n in 5" :key="'s' + n"><td colspan="7"><div class="skeleton-row"></div></td></tr>
              <tr v-else v-for="(row, i) in rows" :key="i">
                <td class="mono nowrap">{{ row.at }}</td><td>{{ row.admin }}</td>
                <td><span class="badge badge-brand">{{ row.action }}</span></td>
                <td class="mono">{{ row.entity_type }}
                  <a v-if="entityPath(row)" class="link" :href="entityPath(row)" @click.prevent="go(entityPath(row))">{{ shortID(row.entity_id) }}</a>
                  <span v-else>{{ row.entity_id && row.entity_id.length > 12 ? shortID(row.entity_id) : row.entity_id }}</span></td>
                <td><ul class="meta-list"><li v-for="p in pairs(row.metadata)" :key="p.key">
                  <span class="k">{{ p.key }}:</span> <span class="v">{{ p.value }}</span></li></ul>
                  <span v-if="!pairs(row.metadata).length" class="muted">—</span></td>
                <td class="mono">{{ row.request_id || '—' }}</td>
                <td class="mono">{{ maskIP(row.ip) || '—' }}</td>
              </tr>
              <tr v-if="!loading && !rows.length"><td colspan="7" class="empty">
                <span v-if="error" class="load-error" role="alert">{{ error }}
                  <button type="button" class="btn btn-sm" @click="load">{{ t('common.refresh') }}</button></span>
                <span v-else>{{ t('common.empty') }}</span></td></tr>
            </tbody>
          </table>
        </div>
        <pager :page="page" :limit="limit" :total="total" @move="move"/>
      </div>`
  };

  var LIMIT_FIELDS = [
    { key: "max_source_characters", label: "admin.settings.source_chars" },
    { key: "max_instruction_length", label: "admin.settings.instruction_chars" },
    { key: "max_output_tokens", label: "admin.settings.output_tokens" }
  ];

  var Settings = {
    data: function () {
      return {
        data: null,
        form: { model: "", input_per_1m: 0.15, output_per_1m: 0.6, effective_from: "" },
        limits: {}, limitFields: LIMIT_FIELDS, savingLimits: false
      };
    },
    mounted: function () { this.load(); },
    methods: {
      t: t, can: can,
      async load() {
        try {
          this.data = await api("/settings");
          this.form.model = this.data.model;
          this.fillLimits(this.data.ai_limits);
        }
        catch (e) { toast("danger", t("common.error")); }
      },
      fillLimits(ai) {
        if (!ai) return;
        var form = {};
        LIMIT_FIELDS.forEach(function (f) { form[f.key] = ai.current[f.key]; });
        this.limits = form;
      },
      range(key) { return this.data.ai_limits.ranges[key]; },
      outOfRange(key) {
        var value = this.limits[key], r = this.range(key);
        return !(Number.isInteger(value) && value >= r.min && value <= r.max);
      },
      async save() {
        try {
          await api("/settings/pricing", { method: "POST", body: this.form });
          toast("ok", t("common.saved")); this.load();
        } catch (e) { toast("danger", t("common.error")); }
      },
      async saveLimits() {
        var self = this;
        if (LIMIT_FIELDS.some(function (f) { return self.outOfRange(f.key); })) {
          toast("danger", t("admin.settings.limit_invalid"));
          return;
        }
        this.savingLimits = true;
        try {
          var fresh = await api("/settings/limits", { method: "POST", body: this.limits });
          this.data.ai_limits = fresh;
          this.fillLimits(fresh);
          toast("ok", t("common.saved"));
        } catch (e) {
          toast("danger", e.message === "INVALID_REQUEST" ? t("admin.settings.limit_invalid") : t("common.error"));
        } finally {
          this.savingLimits = false;
        }
      }
    },
    template: `
      <div v-if="data">
        <div class="card">
          <div class="card-head"><h2>{{ t('admin.settings.title') }}</h2></div>
          <div class="card-body">
            <dl class="kv">
              <dt>{{ t('admin.settings.env') }}</dt><dd><b>{{ data.env }}</b></dd>
              <dt>{{ t('admin.settings.timezone') }}</dt><dd>{{ data.timezone }}</dd>
              <dt>{{ t('admin.settings.demo') }}</dt>
              <dd><span :class="'badge ' + (data.demo_mode ? 'badge-warn' : 'badge-ok')">
                {{ data.demo_mode ? 'on' : 'off' }}</span></dd>
              <dt>{{ t('admin.settings.payments') }}</dt><dd><span class="badge badge-muted">{{ data.payment_mode }}</span></dd>
              <dt>{{ t('admin.settings.model') }}</dt><dd class="mono">{{ data.model }}</dd>
              <dt>Legacy API</dt><dd><span :class="'badge ' + (data.legacy_api ? 'badge-ok' : 'badge-muted')">
                {{ data.legacy_api ? 'on' : 'off' }}</span></dd>
              <dt>Access / Refresh TTL</dt><dd class="mono">{{ data.access_ttl }} · {{ data.refresh_ttl }}</dd>
            </dl>
          </div>
        </div>
        <div class="card" v-if="data.ai_limits">
          <div class="card-head"><h2>{{ t('admin.settings.limits') }}</h2></div>
          <div class="card-body">
            <p class="muted" style="margin-top:0">{{ t('admin.settings.limits_hint') }}</p>
            <div class="form-grid">
              <label class="field" v-for="f in limitFields" :key="f.key">
                <span>{{ t(f.label) }}
                  <span :class="'badge ' + (data.ai_limits.overridden[f.key] ? 'badge-brand' : 'badge-muted')">
                    {{ data.ai_limits.overridden[f.key] ? t('admin.settings.limit_from_admin') : t('admin.settings.limit_from_default') }}</span>
                </span>
                <input type="number" step="1" :min="range(f.key).min" :max="range(f.key).max"
                  v-model.number="limits[f.key]" :aria-invalid="outOfRange(f.key)" :disabled="!can('settings.write')">
                <small class="muted">{{ t('admin.settings.limit_default') }}: {{ data.ai_limits.defaults[f.key] }} ·
                  {{ t('admin.settings.limit_range') }}: {{ range(f.key).min }}–{{ range(f.key).max }}</small>
              </label>
            </div>
            <p class="muted">{{ t('admin.settings.limits_plans_note') }}</p>
            <button v-if="can('settings.write')" class="btn btn-primary" :disabled="savingLimits" @click="saveLimits">{{ t('common.save') }}</button>
          </div>
        </div>
        <div class="card">
          <div class="card-head"><h2>{{ t('admin.settings.pricing') }}</h2></div>
          <div class="table-wrap">
            <table>
              <thead><tr><th>{{ t('admin.settings.model') }}</th><th>{{ t('admin.settings.input_price') }}</th>
                <th>{{ t('admin.settings.output_price') }}</th><th>{{ t('admin.settings.effective') }}</th></tr></thead>
              <tbody>
                <tr v-for="p in data.pricing" :key="p.id">
                  <td class="mono">{{ p.model }}</td><td>\${{ p.input_per_1m }}</td>
                  <td>\${{ p.output_per_1m }}</td><td class="mono">{{ p.effective_from }}</td></tr>
                <tr v-if="!data.pricing.length"><td colspan="4" class="empty">{{ t('common.empty') }}</td></tr>
              </tbody>
            </table>
          </div>
          <div class="card-body" v-if="can('settings.write')">
            <div class="form-grid">
              <label class="field"><span>{{ t('admin.settings.model') }}</span><input type="text" v-model="form.model"></label>
              <label class="field"><span>{{ t('admin.settings.effective') }}</span>
                <input type="date" v-model="form.effective_from"></label>
              <label class="field"><span>{{ t('admin.settings.input_price') }}</span>
                <input type="number" step="0.0001" v-model.number="form.input_per_1m"></label>
              <label class="field"><span>{{ t('admin.settings.output_price') }}</span>
                <input type="number" step="0.0001" v-model.number="form.output_per_1m"></label>
            </div>
            <button class="btn btn-primary" @click="save">{{ t('common.save') }}</button>
          </div>
        </div>
      </div>`
  };

  /* ------------------------------------------------------ notifications */
  var DEFAULT_PUSH_META = {
    categories: ["account", "subscription", "security", "system", "marketing"],
    screens: ["home", "subscription", "settings", "notifications", "templates", "profile", "keyboard", "compose"],
    locales: ["kk", "ru", "en", "uz"], platforms: ["android", "ios"],
    limits: { title: 80, body: 400, data_keys: 10, user_ids: 500 },
    status: { enabled: false, worker: false, fcm: false, apns: false, link_hosts: [] }
  };
  var DATA_KEY_PATTERN = /^[a-z][a-z0-9_]{0,31}$/;
  var dataRowSeq = 0;

  function runeLength(s) { return Array.from(s || "").length; }

  function splitIDs(raw) {
    return String(raw || "").split(/[\s,;]+/).map(function (s) { return s.trim(); }).filter(Boolean);
  }

  function blankAudience() {
    return {
      platforms: [], auth: "", payment: "", subscription: "", locales: [], app_version_min: "",
      app_version_max: "", os_version_min: "", active_within_days: "", inactive_for_days: "",
      registered_from: "", registered_to: "", user_ids: ""
    };
  }

  // buildAudience — domain.AudienceFilter JSON: бос өріс жіберілмейді (omitempty). Сүзгіні тек сервер қолданады.
  function buildAudience(a) {
    var out = {};
    if (a.platforms.length) out.platforms = a.platforms.slice().sort();
    if (a.auth) out.auth = a.auth;
    if (a.locales.length) out.locales = a.locales.slice().sort();
    ["app_version_min", "app_version_max", "os_version_min"].forEach(function (key) {
      var v = String(a[key] || "").trim();
      if (v) out[key] = v;
    });
    ["active_within_days", "inactive_for_days"].forEach(function (key) {
      var n = parseInt(a[key], 10);
      if (n > 0) out[key] = n;
    });
    // Anonymous devices have no account, plan or registration date: those filters are
    // disabled in the form and left out here (the server would refuse the mix).
    if (a.auth !== "anonymous") {
      if (a.payment) out.payment = a.payment;
      if (a.subscription) out.subscription = a.subscription;
      if (a.registered_from) out.registered_from = a.registered_from;
      if (a.registered_to) out.registered_to = a.registered_to;
      var ids = splitIDs(a.user_ids);
      if (ids.length) out.user_ids = ids;
    }
    return out;
  }

  // SendConfirm — жіберер алдында: мазмұн, аудитория және серверде жаңа есептелген алушылар саны.
  var SendConfirm = {
    props: {
      content: { type: Object, required: true }, audience: { type: Object, default: function () { return {}; } },
      busy: { type: Boolean, default: false }, blocked: { type: Boolean, default: false }
    },
    emits: ["confirm", "close"],
    data: function () { return { preview: null, loading: true, error: "" }; },
    computed: {
      summary: function () { return audienceSummary(this.audience); },
      dataPairs: function () { return flatten(this.content.data || {}, ""); }
    },
    mounted: function () {
      this.load();
      if (this.$refs.dialog) this.$refs.dialog.focus();
    },
    methods: Object.assign({}, helpers, {
      async load() {
        this.loading = true;
        this.error = "";
        try {
          var res = await api("/notifications/audience/preview", {
            method: "POST", body: { audience: this.audience, category: this.content.category }
          });
          this.preview = res.preview;
        } catch (e) { this.error = errorText(e); }
        this.loading = false;
      },
      close: function () { if (!this.busy) this.$emit("close"); },
      confirm: function () { if (!this.busy && !this.loading) this.$emit("confirm"); }
    }),
    template: `
      <div class="modal-backdrop" @click.self="close" @keydown.esc="close">
        <div class="modal" ref="dialog" tabindex="-1" role="dialog" aria-modal="true" aria-labelledby="send-confirm-title">
          <div class="modal-head">
            <h2 id="send-confirm-title">{{ t('admin.push.confirm.title') }}</h2>
            <button type="button" class="btn btn-sm" :aria-label="t('common.cancel')" :disabled="busy" @click="close">✕</button>
          </div>
          <div class="modal-body">
            <div v-if="blocked" class="notice notice-danger">{{ t('admin.push.confirm.blocked') }}</div>
            <div class="push-preview" :aria-label="t('admin.push.form.content')">
              <b>{{ content.title }}</b><p>{{ content.body }}</p>
            </div>
            <dl class="kv">
              <dt>{{ t('admin.push.form.category') }}</dt><dd>{{ t('admin.push.category.' + content.category) }}</dd>
              <dt>{{ t('admin.push.form.link') }}</dt><dd class="mono">{{ content.link || '—' }}</dd>
              <dt>{{ t('admin.push.form.data') }}</dt>
              <dd><ul class="meta-list" v-if="dataPairs.length"><li v-for="p in dataPairs" :key="p.key">
                <span class="k">{{ p.key }}:</span> <span class="v">{{ p.value }}</span></li></ul><span v-else>—</span></dd>
              <dt>{{ t('admin.push.form.audience') }}</dt>
              <dd><ul class="plain-list"><li v-for="line in summary" :key="line">{{ line }}</li></ul></dd>
            </dl>
            <h3 class="subhead">{{ t('admin.push.preview.title') }}</h3>
            <div v-if="loading" class="skeleton-row" style="height:64px"></div>
            <div v-else-if="error" class="notice notice-danger" role="alert">{{ error }}</div>
            <div v-else-if="preview" class="stat-grid" aria-live="polite">
              <div class="stat stat-brand"><div class="label">{{ t('admin.push.preview.users') }}</div><div class="value">{{ nf(preview.users) }}</div></div>
              <div class="stat stat-brand"><div class="label">{{ t('admin.push.preview.devices') }}</div><div class="value">{{ nf(preview.devices) }}</div></div>
              <div class="stat"><div class="label">Android</div><div class="value">{{ nf(preview.android) }}</div></div>
              <div class="stat"><div class="label">iOS</div><div class="value">{{ nf(preview.ios) }}</div></div>
              <div class="stat"><div class="label">{{ t('admin.push.preview.anonymous') }}</div><div class="value">{{ nf(preview.anonymous_devices) }}</div></div>
            </div>
            <div v-if="preview && !preview.devices" class="notice notice-warn" style="margin-top:12px">{{ t('admin.push.confirm.no_devices') }}</div>
            <p class="hint">{{ t('admin.push.confirm.note') }}</p>
          </div>
          <div class="modal-foot">
            <button type="button" class="btn" :disabled="busy" @click="close">{{ t('common.cancel') }}</button>
            <button type="button" class="btn btn-primary" :disabled="busy || loading" @click="confirm">
              {{ busy ? t('admin.push.sending') : (preview ? tf('admin.push.confirm.send_n', { n: nf(preview.devices) }) : t('admin.push.send')) }}</button>
          </div>
        </div>
      </div>`
  };

  var CampaignList = {
    mixins: [listView({ endpoint: "/notifications/campaigns", rowsKey: "campaigns", limit: 20, defaults: { status: "" } })],
    data: function () { return { statuses: CAMPAIGN_STATUSES }; },
    methods: Object.assign({}, helpers, {
      open: function (c) { navigate("/admin/notifications/campaigns/" + c.id); }
    }),
    template: `
      <div class="card">
        <div class="card-head">
          <h2>{{ t('admin.push.tab.campaigns') }}</h2>
          <div class="right">
            <select v-model="filters.status" @change="search" :aria-label="t('admin.users.col_status')">
              <option value="">{{ t('common.all') }}</option>
              <option v-for="s in statuses" :key="s" :value="s">{{ t('admin.push.campaign_status.' + s) }}</option>
            </select>
            <a v-if="can('notifications.send')" class="btn btn-sm btn-primary" href="/admin/notifications/new"
               @click.prevent="go('/admin/notifications/new')">+ {{ t('admin.push.new') }}</a>
          </div>
        </div>
        <div class="card-body" v-if="denied"><no-access :permission="denied"/></div>
        <div class="table-wrap" v-else>
          <table>
            <thead><tr><th>{{ t('admin.push.col.campaign') }}</th><th>{{ t('admin.users.col_status') }}</th>
              <th>{{ t('admin.push.col.created') }}</th><th>{{ t('admin.push.col.recipients') }}</th>
              <th>{{ t('admin.push.delivery_status.provider_accepted') }}</th><th>{{ t('admin.push.stat.failed') }}</th>
              <th>{{ t('admin.push.stat.opened') }}</th></tr></thead>
            <tbody>
              <tr v-if="loading" v-for="n in 4" :key="'s' + n"><td colspan="7"><div class="skeleton-row"></div></td></tr>
              <tr v-else v-for="c in rows" :key="c.id" class="clickable" tabindex="0" @click="open(c)" @keydown.enter="open(c)">
                <td><b>{{ c.name }}</b><div class="muted small">{{ c.title }}</div></td>
                <td><span :class="'badge ' + campaignBadge(c.status)">{{ t('admin.push.campaign_status.' + c.status) }}</span></td>
                <td class="nowrap"><span class="mono">{{ c.created_at }}</span><div class="muted small">{{ adminName(c.created_by) }}</div></td>
                <td class="nowrap">
                  <template v-if="c.started_at">{{ nf(c.recipient_count) }} / {{ nf(c.device_count) }}</template>
                  <span v-else class="muted">—</span></td>
                <td>{{ nf(c.stats.provider_accepted) }}</td>
                <td>{{ nf(c.stats.provider_failed + c.stats.invalid_token) }}</td>
                <td>{{ nf(c.stats.opened) }}</td>
              </tr>
              <tr v-if="!loading && !rows.length"><td colspan="7" class="empty">
                <span v-if="error" class="load-error" role="alert">{{ error }}
                  <button type="button" class="btn btn-sm" @click="load">{{ t('common.refresh') }}</button></span>
                <span v-else>{{ t('admin.push.empty_campaigns') }}</span></td></tr>
            </tbody>
          </table>
        </div>
        <p class="hint card-note">{{ t('admin.push.col.recipients_hint') }}</p>
        <pager :page="page" :limit="limit" :total="total" @move="move"/>
      </div>`
  };

  var CampaignForm = {
    components: { SendConfirm: SendConfirm },
    props: { meta: { type: Object, default: null } },
    data: function () {
      return {
        form: { name: "", title: "", body: "", category: "marketing", linkType: "", screen: "home", url: "", data: [] },
        audience: blankAudience(),
        // One key per form instance: a double click, a retry after a timeout or a lost
        // response repeat the same command and the server returns the first campaign.
        idemKey: uuid(),
        preview: null, previewedFor: "", previewing: false, previewError: "",
        busy: "", confirming: false, fieldError: null, submitError: ""
      };
    },
    computed: {
      m: function () { return this.meta || DEFAULT_PUSH_META; },
      limits: function () { return this.m.limits || DEFAULT_PUSH_META.limits; },
      linkHosts: function () { return ((this.m.status && this.m.status.link_hosts) || []).join(", ") || "—"; },
      // Unknown status (not loaded) is not "blocked": the server decides and answers PUSH_DISABLED if so.
      ready: function () {
        var s = this.meta && this.meta.status;
        return !s || !!(s.enabled && (s.fcm || s.apns));
      },
      anonymous: function () { return this.audience.auth === "anonymous"; },
      titleLength: function () { return runeLength(this.form.title); },
      bodyLength: function () { return runeLength(this.form.body); },
      userIDCount: function () { return splitIDs(this.audience.user_ids).length; },
      link: function () {
        if (this.form.linkType === "screen") return "aireply://" + this.form.screen;
        if (this.form.linkType === "url") return this.form.url.trim();
        return "";
      },
      dataObject: function () {
        var out = {};
        this.form.data.forEach(function (row) { if (row.key.trim()) out[row.key.trim()] = row.value.trim(); });
        return out;
      },
      audienceJSON: function () { return buildAudience(this.audience); },
      payload: function () {
        return {
          name: this.form.name.trim(), title: this.form.title.trim(), body: this.form.body.trim(),
          category: this.form.category, link: this.link, data: this.dataObject, audience: this.audienceJSON
        };
      },
      content: function () {
        var p = this.payload;
        return { title: p.title, body: p.body, category: p.category, link: p.link, data: p.data };
      },
      fingerprint: function () { return JSON.stringify(this.payload); },
      previewKey: function () { return JSON.stringify([this.audienceJSON, this.form.category]); },
      previewStale: function () { return !!this.preview && this.previewedFor !== this.previewKey; }
    },
    watch: {
      // Changed content or audience is a different command: it gets a new key.
      fingerprint: function () {
        this.idemKey = uuid();
        this.fieldError = null;
        this.submitError = "";
      }
    },
    methods: Object.assign({}, helpers, {
      errorFor: function (field) {
        var e = this.fieldError;
        if (!e) return "";
        return e.field === field || e.field.indexOf(field + ".") === 0 ? e.text : "";
      },
      addData: function () {
        if (this.form.data.length < this.limits.data_keys) this.form.data.push({ id: ++dataRowSeq, key: "", value: "" });
      },
      removeData: function (i) { this.form.data.splice(i, 1); },
      resetAudience: function () { this.audience = blankAudience(); },
      // localProblem — серверге дейінгі тексеру (сервер бәрібір қайта тексереді).
      localProblem: function () {
        var p = this.payload, limits = this.limits, seen = {}, problem = null;
        if (!p.title) return { field: "title", reason: "admin.error.reason.required" };
        if (runeLength(p.title) > limits.title) return { field: "title", reason: "admin.error.reason.title_length" };
        if (!p.body) return { field: "body", reason: "admin.error.reason.required" };
        if (runeLength(p.body) > limits.body) return { field: "body", reason: "admin.error.reason.body_length" };
        if (this.form.linkType === "url" && !/^https:\/\/[^\s]+$/i.test(p.link)) {
          return { field: "link", reason: "admin.error.reason.link_scheme" };
        }
        this.form.data.forEach(function (row) {
          var key = row.key.trim();
          if (problem || (!key && !row.value.trim())) return;
          if (!DATA_KEY_PATTERN.test(key)) problem = { field: "data." + key, reason: "admin.error.reason.data_key" };
          else if (seen[key]) problem = { field: "data." + key, reason: "admin.error.reason.duplicate_key" };
          seen[key] = true;
        });
        if (problem) return problem;
        if (this.userIDCount > limits.user_ids) return { field: "audience.user_ids", reason: "admin.error.reason.user_ids_count" };
        return null;
      },
      validate: function () {
        var problem = this.localProblem();
        if (!problem) return true;
        var text = fieldLabel(problem.field) + ": " + t(problem.reason);
        this.fieldError = { field: problem.field, text: t(problem.reason) };
        this.submitError = text;
        toast("danger", text);
        return false;
      },
      showError: function (e) {
        var text = errorText(e);
        this.submitError = e.code ? text : text + " " + t("admin.push.retry_safe");
        if (e.code === "INVALID_REQUEST" && e.details && e.details.field) {
          this.fieldError = { field: e.details.field, text: text };
        }
        toast("danger", text);
      },
      async runPreview() {
        if (this.previewing) return;
        this.previewing = true;
        this.previewError = "";
        var key = this.previewKey;
        try {
          var res = await api("/notifications/audience/preview", {
            method: "POST", body: { audience: this.audienceJSON, category: this.form.category }
          });
          this.preview = res.preview;
          this.previewedFor = key;
        } catch (e) {
          this.previewError = errorText(e);
          if (e.code === "INVALID_REQUEST" && e.details && e.details.field) this.fieldError = { field: e.details.field, text: this.previewError };
        }
        this.previewing = false;
      },
      openConfirm: function () {
        if (this.busy || !this.validate()) return;
        this.confirming = true;
      },
      async submit(send) {
        if (this.busy) return;
        if (!this.validate()) { this.confirming = false; return; }
        this.busy = send ? "send" : "draft";
        this.submitError = "";
        try {
          var res = await api("/notifications/campaigns", {
            method: "POST", headers: { "Idempotency-Key": this.idemKey },
            body: Object.assign({}, this.payload, { send: send })
          });
          this.idemKey = uuid(); // the command succeeded; anything after it is a new one
          this.confirming = false;
          toast("ok", send ? t("admin.push.toast_queued") : t("admin.push.toast_draft"));
          navigate("/admin/notifications/campaigns/" + res.id);
        } catch (e) {
          this.confirming = false;
          this.showError(e);
        } finally {
          this.busy = "";
        }
      }
    }),
    template: `
      <div class="detail-grid push-form">
        <div>
          <div class="card">
            <div class="card-head"><h2>{{ t('admin.push.form.content') }}</h2></div>
            <div class="card-body">
              <label class="field"><span>{{ t('admin.push.form.name') }}</span>
                <input type="text" v-model="form.name" maxlength="120" :placeholder="t('admin.push.form.name_hint')"></label>

              <label class="field"><span>{{ t('admin.push.form.title') }}
                  <small class="counter" :class="{ 'is-over': titleLength > limits.title }" aria-live="polite">{{ titleLength }} / {{ limits.title }}</small></span>
                <input type="text" v-model="form.title" required
                       :aria-invalid="!!errorFor('title') || titleLength > limits.title" aria-describedby="push-title-error">
                <small v-if="errorFor('title')" id="push-title-error" class="field-error" role="alert">{{ errorFor('title') }}</small></label>

              <label class="field"><span>{{ t('admin.push.form.body') }}
                  <small class="counter" :class="{ 'is-over': bodyLength > limits.body }" aria-live="polite">{{ bodyLength }} / {{ limits.body }}</small></span>
                <textarea rows="4" v-model="form.body" required
                          :aria-invalid="!!errorFor('body') || bodyLength > limits.body" aria-describedby="push-body-error"></textarea>
                <small v-if="errorFor('body')" id="push-body-error" class="field-error" role="alert">{{ errorFor('body') }}</small></label>

              <label class="field"><span>{{ t('admin.push.form.category') }}</span>
                <select v-model="form.category" :aria-invalid="!!errorFor('category')">
                  <option v-for="c in m.categories" :key="c" :value="c">{{ t('admin.push.category.' + c) }}</option>
                </select>
                <small class="hint">{{ form.category === 'security' ? t('admin.push.form.security_hint') : t('admin.push.form.category_hint') }}</small>
                <small v-if="errorFor('category')" class="field-error" role="alert">{{ errorFor('category') }}</small></label>

              <div class="form-grid">
                <label class="field"><span>{{ t('admin.push.form.link') }}</span>
                  <select v-model="form.linkType">
                    <option value="">{{ t('admin.push.link.none') }}</option>
                    <option value="screen">{{ t('admin.push.link.screen') }}</option>
                    <option value="url">{{ t('admin.push.link.url') }}</option>
                  </select></label>
                <label class="field" v-if="form.linkType === 'screen'"><span>{{ t('admin.push.link.screen') }}</span>
                  <select v-model="form.screen">
                    <option v-for="s in m.screens" :key="s" :value="s">{{ t('admin.push.screen.' + s) }} · aireply://{{ s }}</option>
                  </select></label>
                <label class="field" v-if="form.linkType === 'url'"><span>{{ t('admin.push.link.url') }}</span>
                  <input type="url" v-model="form.url" placeholder="https://" :aria-invalid="!!errorFor('link')">
                  <small class="hint">{{ tf('admin.push.link.hosts', { hosts: linkHosts }) }}</small></label>
              </div>
              <small v-if="errorFor('link')" class="field-error" role="alert" style="margin:-8px 0 14px">{{ errorFor('link') }}</small>

              <fieldset class="field">
                <legend>{{ t('admin.push.form.data') }} <small class="muted">({{ form.data.length }} / {{ limits.data_keys }})</small></legend>
                <div class="data-row" v-for="(row, i) in form.data" :key="row.id">
                  <input type="text" v-model="row.key" placeholder="promo_code" autocomplete="off" spellcheck="false"
                         :aria-label="t('admin.push.form.data_key')" :aria-invalid="!!errorFor('data.' + row.key.trim())">
                  <input type="text" v-model="row.value" maxlength="256" :aria-label="t('admin.push.form.data_value')">
                  <button type="button" class="btn btn-sm" :aria-label="t('admin.push.form.remove')" @click="removeData(i)">✕</button>
                </div>
                <button type="button" class="btn btn-sm" :disabled="form.data.length >= limits.data_keys" @click="addData">
                  + {{ t('admin.push.form.add_data') }}</button>
                <small class="hint">{{ t('admin.push.form.data_hint') }}</small>
                <small v-if="errorFor('data')" class="field-error" role="alert">{{ errorFor('data') }}</small>
              </fieldset>
            </div>
          </div>

          <div class="card">
            <div class="card-head"><h2>{{ t('admin.push.form.audience') }}</h2>
              <div class="right"><button type="button" class="btn btn-sm" @click="resetAudience">{{ t('common.reset') }}</button></div></div>
            <div class="card-body">
              <p class="muted" style="margin-top:0">{{ t('admin.push.audience.hint') }}</p>
              <div class="form-grid">
                <fieldset class="field">
                  <legend>{{ t('admin.push.audience.platforms') }}</legend>
                  <div class="chips">
                    <label v-for="p in m.platforms" :key="p" class="chip" :class="{ 'is-on': audience.platforms.indexOf(p) !== -1 }">
                      <input type="checkbox" :value="p" v-model="audience.platforms"> {{ platformName(p) }}</label>
                  </div>
                  <small class="hint">{{ t('admin.push.audience.none_means_all') }}</small>
                  <small v-if="errorFor('audience.platforms')" class="field-error" role="alert">{{ errorFor('audience.platforms') }}</small>
                </fieldset>
                <fieldset class="field">
                  <legend>{{ t('admin.push.audience.locales') }}</legend>
                  <div class="chips">
                    <label v-for="l in m.locales" :key="l" class="chip" :class="{ 'is-on': audience.locales.indexOf(l) !== -1 }">
                      <input type="checkbox" :value="l" v-model="audience.locales"> {{ l.toUpperCase() }}</label>
                  </div>
                  <small class="hint">{{ t('admin.push.audience.none_means_all') }}</small>
                  <small v-if="errorFor('audience.locales')" class="field-error" role="alert">{{ errorFor('audience.locales') }}</small>
                </fieldset>
              </div>
              <div class="form-grid form-grid-3">
                <label class="field"><span>{{ t('admin.push.audience.auth') }}</span>
                  <select v-model="audience.auth" :aria-invalid="!!errorFor('audience.auth')">
                    <option value="">{{ t('admin.push.audience.any') }}</option>
                    <option value="authenticated">{{ t('admin.push.auth.authenticated') }}</option>
                    <option value="anonymous">{{ t('admin.push.auth.anonymous') }}</option>
                  </select></label>
                <label class="field"><span>{{ t('admin.push.audience.payment') }}</span>
                  <select v-model="audience.payment" :disabled="anonymous">
                    <option value="">{{ t('admin.push.audience.any') }}</option>
                    <option value="paid">{{ t('admin.push.payment.paid') }}</option>
                    <option value="unpaid">{{ t('admin.push.payment.unpaid') }}</option>
                  </select></label>
                <label class="field"><span>{{ t('admin.push.audience.subscription') }}</span>
                  <select v-model="audience.subscription" :disabled="anonymous">
                    <option value="">{{ t('admin.push.audience.any') }}</option>
                    <option value="active">{{ t('admin.push.subscription.active') }}</option>
                    <option value="expired">{{ t('admin.push.subscription.expired') }}</option>
                    <option value="none">{{ t('admin.push.subscription.none') }}</option>
                  </select></label>
              </div>
              <p v-if="anonymous" class="hint" style="margin:-6px 0 14px">{{ t('admin.error.reason.anonymous') }}</p>
              <small v-if="errorFor('audience.auth')" class="field-error" role="alert" style="margin:-6px 0 14px">{{ errorFor('audience.auth') }}</small>
              <div class="form-grid form-grid-3">
                <label class="field"><span>{{ t('admin.push.audience.app_version_min') }}</span>
                  <input type="text" v-model="audience.app_version_min" placeholder="1.3.0" :aria-invalid="!!errorFor('audience.app_version_min')">
                  <small v-if="errorFor('audience.app_version_min')" class="field-error" role="alert">{{ errorFor('audience.app_version_min') }}</small></label>
                <label class="field"><span>{{ t('admin.push.audience.app_version_max') }}</span>
                  <input type="text" v-model="audience.app_version_max" placeholder="2.0.0" :aria-invalid="!!errorFor('audience.app_version_max')">
                  <small v-if="errorFor('audience.app_version_max')" class="field-error" role="alert">{{ errorFor('audience.app_version_max') }}</small></label>
                <label class="field"><span>{{ t('admin.push.audience.os_version_min') }}</span>
                  <input type="text" v-model="audience.os_version_min" placeholder="17.0" :aria-invalid="!!errorFor('audience.os_version_min')">
                  <small v-if="errorFor('audience.os_version_min')" class="field-error" role="alert">{{ errorFor('audience.os_version_min') }}</small></label>
                <label class="field"><span>{{ t('admin.push.audience.active_within_days') }}</span>
                  <input type="number" min="1" max="3650" step="1" v-model="audience.active_within_days" :aria-invalid="!!errorFor('audience.active_within_days')">
                  <small v-if="errorFor('audience.active_within_days')" class="field-error" role="alert">{{ errorFor('audience.active_within_days') }}</small></label>
                <label class="field"><span>{{ t('admin.push.audience.inactive_for_days') }}</span>
                  <input type="number" min="1" max="3650" step="1" v-model="audience.inactive_for_days" :aria-invalid="!!errorFor('audience.inactive_for_days')">
                  <small v-if="errorFor('audience.inactive_for_days')" class="field-error" role="alert">{{ errorFor('audience.inactive_for_days') }}</small></label>
                <label class="field"><span>{{ t('admin.push.audience.registered_from') }}</span>
                  <input type="date" v-model="audience.registered_from" :disabled="anonymous" :aria-invalid="!!errorFor('audience.registered_from')">
                  <small v-if="errorFor('audience.registered_from')" class="field-error" role="alert">{{ errorFor('audience.registered_from') }}</small></label>
                <label class="field"><span>{{ t('admin.push.audience.registered_to') }}</span>
                  <input type="date" v-model="audience.registered_to" :disabled="anonymous" :aria-invalid="!!errorFor('audience.registered_to')">
                  <small v-if="errorFor('audience.registered_to')" class="field-error" role="alert">{{ errorFor('audience.registered_to') }}</small></label>
              </div>
              <label class="field" style="margin-bottom:0"><span>{{ t('admin.push.audience.user_ids') }}</span>
                <textarea rows="3" v-model="audience.user_ids" :disabled="anonymous" spellcheck="false"
                          :placeholder="t('admin.push.audience.user_ids_hint')" :aria-invalid="!!errorFor('audience.user_ids')"></textarea>
                <small class="hint">{{ tf('admin.push.audience.user_ids_count', { n: userIDCount, max: limits.user_ids }) }}</small>
                <small v-if="errorFor('audience.user_ids')" class="field-error" role="alert">{{ errorFor('audience.user_ids') }}</small></label>
            </div>
          </div>
        </div>

        <div>
          <div class="card sticky">
            <div class="card-head"><h2>{{ t('admin.push.preview.title') }}</h2></div>
            <div class="card-body">
              <button type="button" class="btn btn-block" :disabled="previewing" @click="runPreview">
                {{ previewing ? t('admin.push.preview.counting') : t('admin.push.preview.run') }}</button>
              <p class="hint">{{ t('admin.push.preview.never_sends') }}</p>
              <div v-if="previewError" class="notice notice-danger" role="alert">{{ previewError }}</div>
              <template v-if="preview">
                <div v-if="previewStale" class="notice notice-warn" role="status">{{ t('admin.push.preview.stale') }}</div>
                <div class="stat-grid" aria-live="polite">
                  <div class="stat stat-brand"><div class="label">{{ t('admin.push.preview.users') }}</div><div class="value">{{ nf(preview.users) }}</div></div>
                  <div class="stat stat-brand"><div class="label">{{ t('admin.push.preview.devices') }}</div><div class="value">{{ nf(preview.devices) }}</div></div>
                  <div class="stat"><div class="label">Android</div><div class="value">{{ nf(preview.android) }}</div></div>
                  <div class="stat"><div class="label">iOS</div><div class="value">{{ nf(preview.ios) }}</div></div>
                  <div class="stat"><div class="label">{{ t('admin.push.preview.anonymous') }}</div><div class="value">{{ nf(preview.anonymous_devices) }}</div></div>
                </div>
                <p class="hint">{{ tf('admin.push.preview.matched', { n: nf(preview.matched_devices) }) }}</p>
              </template>
            </div>
            <div class="card-foot">
              <div v-if="submitError" class="notice notice-danger" role="alert" style="width:100%; margin:0">{{ submitError }}</div>
              <button type="button" class="btn" :disabled="!!busy" @click="submit(false)">
                {{ busy === 'draft' ? t('admin.push.saving') : t('admin.push.save_draft') }}</button>
              <button type="button" class="btn btn-primary" :disabled="!!busy" @click="openConfirm">{{ t('admin.push.send') }}…</button>
            </div>
          </div>
        </div>

        <send-confirm v-if="confirming" :content="content" :audience="audienceJSON" :busy="busy === 'send'"
                      :blocked="!ready" @close="confirming = false" @confirm="submit(true)"/>
      </div>`
  };

  var CampaignDetail = {
    components: { SendConfirm: SendConfirm, Pager: Pager, NoAccess: NoAccess },
    props: { id: String, meta: { type: Object, default: null } },
    data: function () {
      return {
        data: null, loading: true, denied: "", missing: false, error: "", deliveries: [], dTotal: 0, dPage: 1,
        dLimit: 20, confirming: false, busy: "", timer: null, updatedAt: ""
      };
    },
    computed: {
      live: function () { return !!this.data && (this.data.status === "queued" || this.data.status === "processing"); },
      stats: function () { return (this.data && this.data.stats) || {}; },
      ready: function () {
        var s = this.meta && this.meta.status;
        return !s || !!(s.enabled && (s.fcm || s.apns));
      },
      summary: function () { return audienceSummary(this.data ? this.data.audience : {}); },
      content: function () {
        var d = this.data;
        return { title: d.title, body: d.body, category: d.category, link: d.link, data: d.data };
      },
      segments: function () {
        var s = this.stats, total = s.total || 0;
        if (!total) return [];
        return [
          { cls: "seg-ok", n: s.provider_accepted, label: t("admin.push.delivery_status.provider_accepted") },
          { cls: "seg-pending", n: pendingOf(s), label: t("admin.push.stat.pending") },
          { cls: "seg-danger", n: s.provider_failed, label: t("admin.push.delivery_status.provider_failed") },
          { cls: "seg-warn", n: s.invalid_token, label: t("admin.push.delivery_status.invalid_token") },
          { cls: "seg-muted", n: (s.skipped || 0) + (s.cancelled || 0), label: t("admin.push.stat.not_sent") }
        ].filter(function (x) { return x.n > 0; }).map(function (x) {
          x.width = Math.max(1.5, x.n / total * 100);
          return x;
        });
      },
      segmentsLabel: function () {
        return this.segments.map(function (s) { return s.label + ": " + s.n; }).join(", ");
      }
    },
    mounted: function () {
      var self = this;
      this.load();
      // While the campaign is being sent the counters move: poll until it settles.
      this.timer = setInterval(function () { if (self.live && !document.hidden) self.load(true); }, 5000);
    },
    unmounted: function () { clearInterval(this.timer); },
    methods: Object.assign({}, helpers, {
      async load(quiet) {
        if (!quiet) this.loading = true;
        try {
          this.data = await api("/notifications/campaigns/" + encodeURIComponent(this.id));
          this.error = "";
          await this.loadDeliveries();
          this.updatedAt = clock(new Date());
        } catch (e) {
          if (e.code === "FORBIDDEN") this.denied = e.details.permission || "notifications.read";
          else if (e.code === "NOT_FOUND") this.missing = true;
          else if (!this.data) this.error = errorText(e);
          else if (!quiet) toast("danger", errorText(e));
        }
        this.loading = false;
      },
      async loadDeliveries() {
        var data = await api("/notifications/deliveries" + toQuery({ campaign_id: this.id, page: this.dPage, limit: this.dLimit }));
        this.deliveries = data.deliveries || [];
        this.dTotal = data.total || 0;
      },
      async moveDeliveries(delta) {
        this.dPage = Math.max(1, this.dPage + delta);
        try { await this.loadDeliveries(); } catch (e) { toast("danger", errorText(e)); }
      },
      askSend: function () { if (!this.busy) this.confirming = true; },
      async send() {
        if (this.busy) return;
        this.busy = "send";
        try {
          await api("/notifications/campaigns/" + encodeURIComponent(this.id) + "/send", { method: "POST", body: {} });
          toast("ok", t("admin.push.toast_queued"));
          await this.load(true);
        } catch (e) { toast("danger", errorText(e)); }
        this.confirming = false;
        this.busy = "";
      },
      async cancel() {
        if (this.busy || !window.confirm(t("admin.push.cancel_confirm"))) return;
        this.busy = "cancel";
        try {
          await api("/notifications/campaigns/" + encodeURIComponent(this.id) + "/cancel", { method: "POST", body: {} });
          toast("ok", t("admin.push.toast_cancelled"));
          await this.load(true);
        } catch (e) { toast("danger", errorText(e)); }
        this.busy = "";
      }
    }),
    template: `
      <div>
        <div class="toolbar">
          <a class="btn btn-sm" href="/admin/notifications" @click.prevent="go('/admin/notifications')">← {{ t('common.back') }}</a>
          <span v-if="live" class="badge badge-brand" role="status"><span class="pulse"></span>{{ t('admin.push.auto_refresh') }}</span>
        </div>
        <no-access v-if="denied" :permission="denied"/>
        <div v-else-if="missing" class="card"><div class="empty">{{ t('admin.error.not_found') }}</div></div>
        <div v-else-if="error && !data" class="notice notice-danger" role="alert">{{ error }}
          <button type="button" class="btn btn-sm" @click="load()">{{ t('common.refresh') }}</button></div>
        <div v-else-if="loading && !data" class="card"><div class="card-body"><div class="skeleton-row"></div></div></div>
        <template v-else-if="data">
          <div class="detail-grid">
            <div>
              <div class="card">
                <div class="card-head"><h2>{{ data.name }}</h2>
                  <div class="right"><span :class="'badge ' + campaignBadge(data.status)">{{ t('admin.push.campaign_status.' + data.status) }}</span></div></div>
                <div class="card-body">
                  <div class="push-preview"><b>{{ data.title }}</b><p>{{ data.body }}</p></div>
                  <dl class="kv">
                    <dt>{{ t('admin.push.form.category') }}</dt><dd>{{ t('admin.push.category.' + data.category) }}</dd>
                    <dt>{{ t('admin.push.form.link') }}</dt><dd class="mono">{{ data.link || '—' }}</dd>
                    <dt>{{ t('admin.push.form.data') }}</dt>
                    <dd><ul class="meta-list" v-if="pairs(data.data).length"><li v-for="p in pairs(data.data)" :key="p.key">
                      <span class="k">{{ p.key }}:</span> <span class="v">{{ p.value }}</span></li></ul><span v-else>—</span></dd>
                    <dt>{{ t('admin.push.form.audience') }}</dt>
                    <dd><ul class="plain-list"><li v-for="line in summary" :key="line">{{ line }}</li></ul></dd>
                  </dl>
                </div>
              </div>

              <div class="card">
                <div class="card-head"><h2>{{ t('admin.push.stats.title') }}</h2>
                  <div class="right muted small" v-if="updatedAt">{{ tf('admin.push.updated_at', { time: updatedAt }) }}</div></div>
                <div class="card-body">
                  <div class="stat-grid">
                    <div class="stat stat-ok"><div class="label">{{ t('admin.push.delivery_status.provider_accepted') }}</div>
                      <div class="value">{{ nf(stats.provider_accepted) }}</div></div>
                    <div class="stat stat-brand"><div class="label">{{ t('admin.push.stat.pending') }}</div>
                      <div class="value">{{ nf(pendingOf(stats)) }}</div>
                      <div class="sub">{{ nf(stats.queued) }} · {{ nf(stats.sending) }} · {{ nf(stats.retrying) }}</div></div>
                    <div class="stat stat-danger"><div class="label">{{ t('admin.push.delivery_status.provider_failed') }}</div>
                      <div class="value">{{ nf(stats.provider_failed) }}</div></div>
                    <div class="stat stat-warn"><div class="label">{{ t('admin.push.delivery_status.invalid_token') }}</div>
                      <div class="value">{{ nf(stats.invalid_token) }}</div></div>
                    <div class="stat"><div class="label">{{ t('admin.push.delivery_status.skipped') }}</div>
                      <div class="value">{{ nf(stats.skipped) }}</div></div>
                    <div class="stat"><div class="label">{{ t('admin.push.delivery_status.cancelled') }}</div>
                      <div class="value">{{ nf(stats.cancelled) }}</div></div>
                    <div class="stat stat-brand"><div class="label">{{ t('admin.push.stat.opened') }}</div>
                      <div class="value">{{ nf(stats.opened) }}</div><div class="sub">{{ t('admin.push.stat.opened_hint') }}</div></div>
                    <div class="stat"><div class="label">{{ t('admin.push.stat.total') }}</div>
                      <div class="value">{{ nf(stats.total) }}</div>
                      <div class="sub">Android {{ nf(stats.android) }} · iOS {{ nf(stats.ios) }}</div></div>
                  </div>
                  <div class="stacked" v-if="segments.length" role="img" :aria-label="segmentsLabel">
                    <span v-for="s in segments" :key="s.cls" :class="s.cls" :style="{ width: s.width + '%' }" :title="s.label + ': ' + s.n"></span>
                  </div>
                  <p class="hint">{{ t('admin.push.stats.note') }}</p>
                </div>
              </div>

              <div class="card">
                <div class="card-head"><h2>{{ t('admin.push.errors.title') }}</h2></div>
                <div class="table-wrap">
                  <table>
                    <thead><tr><th>{{ t('admin.logs.error_code') }}</th><th>{{ t('admin.push.errors.count') }}</th></tr></thead>
                    <tbody>
                      <tr v-for="e in (data.errors || [])" :key="e.code"><td class="mono">{{ e.code }}</td><td>{{ nf(e.count) }}</td></tr>
                      <tr v-if="!(data.errors || []).length"><td colspan="2" class="empty">{{ t('admin.push.errors.none') }}</td></tr>
                    </tbody>
                  </table>
                </div>
              </div>

              <div class="card">
                <div class="card-head"><h2>{{ t('admin.push.tab.deliveries') }}</h2>
                  <div class="right"><a class="btn btn-sm" :href="'/admin/notifications/deliveries?campaign_id=' + id"
                    @click.prevent="go('/admin/notifications/deliveries?campaign_id=' + id)">{{ t('admin.push.all_deliveries') }}</a></div></div>
                <div class="table-wrap">
                  <table>
                    <thead><tr><th>{{ t('admin.push.device.device') }}</th><th>{{ t('admin.push.delivery.user') }}</th>
                      <th>{{ t('admin.users.col_status') }}</th><th>{{ t('admin.push.delivery.error') }}</th>
                      <th>{{ t('admin.push.delivery.sent') }}</th><th>{{ t('admin.push.delivery.opened') }}</th></tr></thead>
                    <tbody>
                      <tr v-for="d in deliveries" :key="d.id">
                        <td class="nowrap">{{ d.device || platformName(d.platform) }}
                          <div class="mono">{{ platformName(d.platform) }} {{ d.app_version }} · {{ d.push || '—' }}</div></td>
                        <td><a v-if="d.user_id" class="link mono" :href="userLink(d.user_id)" @click.prevent="go(userLink(d.user_id))">{{ shortID(d.user_id) }}</a>
                          <span v-else class="muted">{{ t('admin.push.anonymous') }}</span></td>
                        <td class="nowrap"><span :class="'badge ' + deliveryBadge(d.status)">{{ t('admin.push.delivery_status.' + d.status) }}</span>
                          <div class="muted small">{{ t('admin.push.delivery.attempts') }}: {{ d.attempts }}</div></td>
                        <td class="mono">{{ d.error_code || '—' }}<div class="muted small" v-if="d.error_detail">{{ d.error_detail }}</div></td>
                        <td class="mono nowrap">{{ d.sent_at || d.failed_at || '—' }}</td>
                        <td class="mono nowrap">{{ d.opened_at || '—' }}</td>
                      </tr>
                      <tr v-if="!deliveries.length"><td colspan="6" class="empty">{{ t('admin.push.no_deliveries') }}</td></tr>
                    </tbody>
                  </table>
                </div>
                <pager v-if="dTotal > dLimit" :page="dPage" :limit="dLimit" :total="dTotal" @move="moveDeliveries"/>
              </div>
            </div>

            <div>
              <div class="card">
                <div class="card-head"><h2>{{ t('admin.push.timeline') }}</h2></div>
                <div class="card-body">
                  <dl class="kv kv-tight">
                    <dt>{{ t('admin.push.col.created') }}</dt><dd><span class="mono">{{ data.created_at }}</span><div class="muted small">{{ adminName(data.created_by) }}</div></dd>
                    <dt>{{ t('admin.push.time.queued') }}</dt><dd class="mono">{{ data.queued_at || '—' }}</dd>
                    <dt>{{ t('admin.push.time.started') }}</dt><dd class="mono">{{ data.started_at || '—' }}</dd>
                    <dt>{{ t('admin.push.time.completed') }}</dt><dd class="mono">{{ data.completed_at || '—' }}</dd>
                    <dt>{{ t('admin.push.time.cancelled') }}</dt><dd class="mono">{{ data.cancelled_at || '—' }}</dd>
                    <dt>{{ t('admin.push.col.recipients') }}</dt>
                    <dd><template v-if="data.started_at">{{ nf(data.recipient_count) }} / {{ nf(data.device_count) }}</template><span v-else>—</span></dd>
                  </dl>
                  <p class="hint">{{ t('admin.push.col.recipients_hint') }}</p>
                </div>
                <div class="card-foot" v-if="can('notifications.send') && ['draft', 'queued', 'processing'].indexOf(data.status) !== -1">
                  <button v-if="data.status === 'draft'" type="button" class="btn btn-primary" :disabled="!!busy" @click="askSend">
                    {{ t('admin.push.send') }}…</button>
                  <button type="button" class="btn btn-danger" :disabled="!!busy" @click="cancel">
                    {{ busy === 'cancel' ? t('admin.push.cancelling') : t('admin.push.cancel') }}</button>
                </div>
              </div>
            </div>
          </div>
          <send-confirm v-if="confirming" :content="content" :audience="data.audience || {}" :busy="busy === 'send'"
                        :blocked="!ready" @close="confirming = false" @confirm="send"/>
        </template>
      </div>`
  };

  var DeliveryList = {
    mixins: [listView({ endpoint: "/notifications/deliveries", rowsKey: "deliveries", limit: 50,
      defaults: { campaign_id: "", user_id: "", status: "", platform: "" } })],
    data: function () { return { campaigns: [], statuses: DELIVERY_STATUSES }; },
    mounted: async function () {
      try { this.campaigns = (await api("/notifications/campaigns?limit=100")).campaigns || []; }
      catch (e) { this.campaigns = []; }
    },
    computed: {
      campaignOptions: function () {
        var current = this.filters.campaign_id;
        var list = this.campaigns.map(function (c) { return { id: c.id, name: c.name }; });
        if (current && !list.some(function (c) { return c.id === current; })) list.unshift({ id: current, name: shortID(current) });
        return list;
      }
    },
    methods: Object.assign({}, helpers),
    template: `
      <div class="card">
        <div class="card-head"><h2>{{ t('admin.push.tab.deliveries') }}</h2></div>
        <form class="filter-grid" @submit.prevent="search">
          <label><span>{{ t('admin.push.col.campaign') }}</span>
            <select v-model="filters.campaign_id" @change="search">
              <option value="">{{ t('common.all') }}</option>
              <option v-for="c in campaignOptions" :key="c.id" :value="c.id">{{ c.name }}</option>
            </select></label>
          <label><span>{{ t('admin.push.filter.user_id') }}</span>
            <input type="text" v-model.trim="filters.user_id" spellcheck="false" placeholder="UUID"></label>
          <label><span>{{ t('admin.users.col_status') }}</span>
            <select v-model="filters.status" @change="search">
              <option value="">{{ t('common.all') }}</option>
              <option v-for="s in statuses" :key="s" :value="s">{{ t('admin.push.delivery_status.' + s) }}</option>
            </select></label>
          <label><span>{{ t('admin.users.col_platform') }}</span>
            <select v-model="filters.platform" @change="search">
              <option value="">{{ t('common.all') }}</option><option value="android">Android</option><option value="ios">iOS</option>
            </select></label>
          <div class="filter-actions">
            <button type="submit" class="btn btn-sm btn-primary">{{ t('common.search') }}</button>
            <button type="button" class="btn btn-sm" @click="reset">{{ t('common.reset') }}</button>
          </div>
        </form>
        <div class="card-body" v-if="denied"><no-access :permission="denied"/></div>
        <div class="table-wrap" v-else>
          <table>
            <thead><tr><th>{{ t('admin.push.delivery.created') }}</th><th>{{ t('admin.push.delivery.notification') }}</th>
              <th>{{ t('admin.push.delivery.user') }}</th><th>{{ t('admin.push.device.device') }}</th>
              <th>{{ t('admin.users.col_status') }}</th><th>{{ t('admin.push.delivery.error') }}</th>
              <th>{{ t('admin.push.delivery.sent') }}</th><th>{{ t('admin.push.delivery.opened') }}</th></tr></thead>
            <tbody>
              <tr v-if="loading" v-for="n in 5" :key="'s' + n"><td colspan="8"><div class="skeleton-row"></div></td></tr>
              <tr v-else v-for="d in rows" :key="d.id">
                <td class="mono nowrap">{{ d.created_at }}</td>
                <td><a v-if="d.campaign_id" class="link" :href="'/admin/notifications/campaigns/' + d.campaign_id"
                       @click.prevent="go('/admin/notifications/campaigns/' + d.campaign_id)">{{ d.campaign_name || d.title }}</a>
                  <span v-else>{{ d.title }}</span>
                  <div class="muted small">{{ t('admin.push.category.' + d.category) }}<span v-if="!d.campaign_id" class="mono"> · {{ d.type }}</span></div></td>
                <td><a v-if="d.user_id" class="link mono" :href="userLink(d.user_id)" @click.prevent="go(userLink(d.user_id))">{{ shortID(d.user_id) }}</a>
                  <span v-else class="muted">{{ t('admin.push.anonymous') }}</span></td>
                <td class="nowrap">{{ d.device || platformName(d.platform) }}
                  <div class="mono">{{ platformName(d.platform) }} {{ d.app_version }}</div><div class="mono">{{ d.push || '—' }}</div></td>
                <td class="nowrap"><span :class="'badge ' + deliveryBadge(d.status)">{{ t('admin.push.delivery_status.' + d.status) }}</span>
                  <div class="muted small">{{ t('admin.push.delivery.attempts') }}: {{ d.attempts }}</div></td>
                <td class="mono">{{ d.error_code || '—' }}<div class="muted small" v-if="d.error_detail">{{ d.error_detail }}</div></td>
                <td class="mono nowrap">{{ d.sent_at || d.failed_at || '—' }}</td>
                <td class="mono nowrap">{{ d.opened_at || '—' }}</td>
              </tr>
              <tr v-if="!loading && !rows.length"><td colspan="8" class="empty">
                <span v-if="error" class="load-error" role="alert">{{ error }}
                  <button type="button" class="btn btn-sm" @click="load">{{ t('common.refresh') }}</button></span>
                <span v-else>{{ t('common.empty') }}</span></td></tr>
            </tbody>
          </table>
        </div>
        <pager :page="page" :limit="limit" :total="total" @move="move"/>
      </div>`
  };

  var DeviceList = {
    mixins: [listView({ endpoint: "/notifications/devices", rowsKey: "devices", limit: 50,
      defaults: { q: "", user_id: "", platform: "", push_status: "", app_version: "" } })],
    data: function () { return { statuses: PUSH_STATUSES }; },
    methods: Object.assign({}, helpers),
    template: `
      <div class="card">
        <div class="card-head"><h2>{{ t('admin.push.tab.devices') }}</h2></div>
        <form class="filter-grid" @submit.prevent="search">
          <label><span>{{ t('common.search') }}</span>
            <input type="text" v-model.trim="filters.q" spellcheck="false" :placeholder="t('admin.push.filter.device_search')"></label>
          <label><span>{{ t('admin.push.filter.user_id') }}</span>
            <input type="text" v-model.trim="filters.user_id" spellcheck="false" placeholder="UUID"></label>
          <label><span>{{ t('admin.users.col_platform') }}</span>
            <select v-model="filters.platform" @change="search">
              <option value="">{{ t('common.all') }}</option><option value="android">Android</option><option value="ios">iOS</option>
            </select></label>
          <label><span>{{ t('admin.push.device.push') }}</span>
            <select v-model="filters.push_status" @change="search">
              <option value="">{{ t('common.all') }}</option>
              <option v-for="s in statuses" :key="s" :value="s">{{ t('admin.push.push_status.' + s) }}</option>
            </select></label>
          <label><span>{{ t('admin.push.device.app_version') }}</span>
            <input type="text" v-model.trim="filters.app_version" placeholder="1.3.2"></label>
          <div class="filter-actions">
            <button type="submit" class="btn btn-sm btn-primary">{{ t('common.search') }}</button>
            <button type="button" class="btn btn-sm" @click="reset">{{ t('common.reset') }}</button>
          </div>
        </form>
        <div class="card-body" v-if="denied"><no-access :permission="denied"/></div>
        <div class="table-wrap" v-else>
          <table>
            <thead><tr><th>{{ t('admin.push.device.device') }}</th><th>{{ t('admin.push.device.os') }}</th>
              <th>{{ t('admin.push.device.app') }}</th><th>{{ t('common.language') }}</th>
              <th>{{ t('admin.push.device.permission') }}</th><th>{{ t('admin.push.device.switch') }}</th>
              <th>{{ t('admin.push.device.push') }}</th><th>{{ t('admin.push.device.token') }}</th>
              <th>{{ t('admin.push.device.first_seen') }}</th><th>{{ t('admin.push.device.last_seen') }}</th>
              <th>{{ t('admin.push.delivery.user') }}</th></tr></thead>
            <tbody>
              <tr v-if="loading" v-for="n in 5" :key="'s' + n"><td colspan="11"><div class="skeleton-row"></div></td></tr>
              <tr v-else v-for="d in rows" :key="d.id">
                <td><b>{{ d.device || '—' }}</b><div class="mono">{{ d.device_model }} · {{ shortID(d.installation_id) }}</div></td>
                <td>{{ platformName(d.platform) }}<div class="mono">{{ d.os || '—' }}</div></td>
                <td class="mono nowrap">{{ d.app_version || '—' }} ({{ d.app_build || '—' }})</td>
                <td>{{ d.locale || '—' }}</td>
                <td><span :class="'badge ' + permissionBadge(d.push.permission)">{{ t('admin.push.permission.' + d.push.permission) }}</span></td>
                <td><span :class="'badge ' + (d.push.enabled ? 'badge-ok' : 'badge-muted')">
                  {{ d.push.enabled ? t('admin.push.status.on') : t('admin.push.status.off') }}</span></td>
                <td><span :class="'badge ' + pushBadge(d.push.status)">{{ t('admin.push.push_status.' + d.push.status) }}</span>
                  <div class="mono" v-if="d.push.reason">{{ d.push.reason }}</div></td>
                <td class="mono">{{ d.push.token || '—' }}<div v-if="d.push.environment">{{ d.push.environment }}</div></td>
                <td class="mono nowrap">{{ d.first_seen }}</td>
                <td class="mono nowrap">{{ d.last_seen }}</td>
                <td><a v-if="d.user_id" class="link" :href="userLink(d.user_id)" @click.prevent="go(userLink(d.user_id))">{{ d.user || shortID(d.user_id) }}</a>
                  <span v-else class="muted">{{ t('admin.push.anonymous') }}</span></td>
              </tr>
              <tr v-if="!loading && !rows.length"><td colspan="11" class="empty">
                <span v-if="error" class="load-error" role="alert">{{ error }}
                  <button type="button" class="btn btn-sm" @click="load">{{ t('common.refresh') }}</button></span>
                <span v-else>{{ t('common.empty') }}</span></td></tr>
            </tbody>
          </table>
        </div>
        <p class="hint card-note">{{ t('admin.push.device.token_hint') }}</p>
        <pager :page="page" :limit="limit" :total="total" @move="move"/>
      </div>`
  };

  // Notifications — бөлім: ішкі мәзір, push күйінің ескертуі және ішкі беттер.
  var Notifications = {
    components: {
      NoAccess: NoAccess, CampaignList: CampaignList, CampaignForm: CampaignForm, CampaignDetail: CampaignDetail,
      DeliveryList: DeliveryList, DeviceList: DeviceList
    },
    props: { route: { type: Object, required: true }, seq: { type: Number, default: 0 } },
    data: function () { return { meta: null }; },
    mounted: async function () {
      try { this.meta = await api("/notifications"); }
      catch (e) { if (e.code !== "FORBIDDEN") toast("danger", errorText(e)); }
    },
    computed: {
      status: function () { return (this.meta && this.meta.status) || null; },
      // banner — жіберу мүмкін емес кезде (сервер бәрібір PUSH_DISABLED қайтарады).
      banner: function () {
        var s = this.status;
        if (!s) return "";
        if (!s.enabled) return t("admin.push.banner.disabled");
        if (!s.fcm && !s.apns) return t("admin.push.banner.no_provider");
        return "";
      },
      notes: function () {
        var s = this.status, out = [];
        if (!s || this.banner) return out;
        if (!s.fcm) out.push(t("admin.push.banner.no_fcm"));
        if (!s.apns) out.push(t("admin.push.banner.no_apns"));
        if (!s.worker) out.push(t("admin.push.banner.no_worker"));
        return out;
      },
      tabs: function () {
        var list = [{ key: "campaigns", path: "/admin/notifications", label: t("admin.push.tab.campaigns") }];
        if (can("notifications.send")) list.push({ key: "create", path: "/admin/notifications/new", label: t("admin.push.tab.create") });
        list.push({ key: "deliveries", path: "/admin/notifications/deliveries", label: t("admin.push.tab.deliveries") });
        list.push({ key: "devices", path: "/admin/notifications/devices", label: t("admin.push.tab.devices") });
        return list;
      },
      activeTab: function () { return this.route.tab === "campaign" ? "campaigns" : this.route.tab; }
    },
    methods: Object.assign({}, helpers),
    template: `
      <div>
        <nav class="subnav" :aria-label="t('admin.nav.notifications')">
          <a v-for="item in tabs" :key="item.key" :href="item.path" :class="{ 'is-active': activeTab === item.key }"
             :aria-current="activeTab === item.key ? 'page' : null" @click.prevent="go(item.path)">{{ item.label }}</a>
        </nav>
        <div v-if="banner" class="notice notice-warn push-banner" role="status">
          <b>{{ banner }}</b>
          <div class="push-status">
            <span :class="'badge ' + (status.enabled ? 'badge-ok' : 'badge-muted')">{{ t('admin.push.status.sending') }}:
              {{ status.enabled ? t('admin.push.status.on') : t('admin.push.status.off') }}</span>
            <span :class="'badge ' + (status.fcm ? 'badge-ok' : 'badge-muted')">FCM (Android):
              {{ status.fcm ? t('admin.push.status.ready') : t('admin.push.status.not_configured') }}</span>
            <span :class="'badge ' + (status.apns ? 'badge-ok' : 'badge-muted')">APNs (iOS):
              {{ status.apns ? t('admin.push.status.ready') : t('admin.push.status.not_configured') }}</span>
          </div>
        </div>
        <div v-for="note in notes" :key="note" class="notice notice-info" role="status">{{ note }}</div>

        <no-access v-if="route.tab === 'create' && !can('notifications.send')" permission="notifications.send"/>
        <campaign-form v-else-if="route.tab === 'create'" :meta="meta" :key="'create' + seq"/>
        <campaign-detail v-else-if="route.tab === 'campaign'" :id="route.id" :meta="meta" :key="'campaign' + route.id + seq"/>
        <delivery-list v-else-if="route.tab === 'deliveries'" :key="'deliveries' + seq"/>
        <device-list v-else-if="route.tab === 'devices'" :key="'devices' + seq"/>
        <campaign-list v-else :key="'campaigns' + seq"/>
      </div>`
  };

  /* ------------------------------------------------------------------ logs */
  var LogEvents = {
    mixins: [listView({ endpoint: "/logs/events", rowsKey: "events", limit: 50,
      defaults: { user: "", name: "", platform: "", app_version: "", app_build: "", os_version: "",
                  device_model: "", outcome: "", from: "", to: "" } })],
    props: { meta: { type: Object, default: null } },
    methods: Object.assign({}, helpers),
    template: `
      <div class="card">
        <div class="card-head"><h2>{{ t('admin.logs.events') }}</h2>
          <div class="right" v-if="meta"><span class="badge badge-muted">{{ kept(meta.retention.app_events_days) }}</span></div></div>
        <form class="filter-grid" @submit.prevent="search">
          <label><span>{{ t('admin.logs.user') }}</span>
            <input type="text" v-model.trim="filters.user" spellcheck="false" :placeholder="t('admin.logs.user_hint')"></label>
          <label><span>{{ t('admin.logs.event') }}</span>
            <select v-model="filters.name" @change="search">
              <option value="">{{ t('common.all') }}</option>
              <option v-for="n in (meta ? meta.app_events : [])" :key="n" :value="n">{{ n }}</option>
            </select></label>
          <label><span>{{ t('admin.users.col_platform') }}</span>
            <select v-model="filters.platform" @change="search">
              <option value="">{{ t('common.all') }}</option><option value="android">Android</option><option value="ios">iOS</option>
            </select></label>
          <label><span>{{ t('admin.push.device.app_version') }}</span><input type="text" v-model.trim="filters.app_version" placeholder="1.3.2"></label>
          <label><span>{{ t('admin.logs.build') }}</span><input type="text" v-model.trim="filters.app_build" placeholder="142"></label>
          <label><span>{{ t('admin.logs.os_version') }}</span><input type="text" v-model.trim="filters.os_version" placeholder="17.5"></label>
          <label><span>{{ t('admin.logs.device_model') }}</span><input type="text" v-model.trim="filters.device_model" placeholder="SM-S928B"></label>
          <label><span>{{ t('admin.logs.outcome') }}</span>
            <select v-model="filters.outcome" @change="search">
              <option value="">{{ t('common.all') }}</option>
              <option value="success">{{ t('admin.logs.outcome.success') }}</option>
              <option value="failure">{{ t('admin.logs.outcome.failure') }}</option>
            </select></label>
          <label><span>{{ t('common.from') }}</span><input type="date" v-model="filters.from"></label>
          <label><span>{{ t('common.to') }}</span><input type="date" v-model="filters.to"></label>
          <div class="filter-actions">
            <button type="submit" class="btn btn-sm btn-primary">{{ t('common.search') }}</button>
            <button type="button" class="btn btn-sm" @click="reset">{{ t('common.reset') }}</button>
          </div>
        </form>
        <div class="card-body" v-if="denied"><no-access :permission="denied"/></div>
        <div class="table-wrap" v-else>
          <table>
            <thead><tr><th>{{ t('admin.audit.when') }}</th><th>{{ t('admin.logs.event') }}</th><th>{{ t('admin.logs.outcome') }}</th>
              <th>{{ t('admin.logs.user') }}</th><th>{{ t('admin.logs.client') }}</th>
              <th>{{ t('admin.logs.error_code') }}</th><th>{{ t('admin.logs.properties') }}</th><th>{{ t('admin.logs.request_id') }}</th></tr></thead>
            <tbody>
              <tr v-if="loading" v-for="n in 5" :key="'s' + n"><td colspan="8"><div class="skeleton-row"></div></td></tr>
              <tr v-else v-for="e in rows" :key="e.id">
                <td class="mono nowrap">{{ e.occurred_at }}</td><td class="mono">{{ e.name }}</td>
                <td><span v-if="e.outcome" :class="'badge ' + (e.outcome === 'success' ? 'badge-ok' : 'badge-danger')">{{ t('admin.logs.outcome.' + e.outcome) }}</span>
                  <span v-else class="muted">—</span></td>
                <td><a v-if="e.user_id" class="link mono" :href="userLink(e.user_id)" @click.prevent="go(userLink(e.user_id))">{{ shortID(e.user_id) }}</a>
                  <span v-else class="muted">{{ t('admin.push.anonymous') }}</span></td>
                <td class="nowrap">{{ platformName(e.platform) }} <span class="mono">{{ e.app_version }} {{ e.app_build ? '(' + e.app_build + ')' : '' }}</span>
                  <div class="muted small">{{ e.device || '—' }} · OS {{ e.os_version || '—' }}</div>
                  <div class="mono">{{ e.installation_id }}</div></td>
                <td class="mono">{{ e.error_code || '—' }}</td>
                <td><ul class="meta-list"><li v-for="p in pairs(e.properties)" :key="p.key">
                  <span class="k">{{ p.key }}:</span> <span class="v">{{ p.value }}</span></li></ul></td>
                <td class="mono">{{ e.request_id || '—' }}</td>
              </tr>
              <tr v-if="!loading && !rows.length"><td colspan="8" class="empty">
                <span v-if="error" class="load-error" role="alert">{{ error }}
                  <button type="button" class="btn btn-sm" @click="load">{{ t('common.refresh') }}</button></span>
                <span v-else>{{ t('common.empty') }}</span></td></tr>
            </tbody>
          </table>
        </div>
        <pager :page="page" :limit="limit" :total="total" @move="move"/>
      </div>`
  };

  var LogAuth = {
    mixins: [listView({ endpoint: "/logs/auth", rowsKey: "events", limit: 50,
      defaults: { user: "", name: "", method: "", outcome: "", from: "", to: "" } })],
    props: { meta: { type: Object, default: null } },
    data: function () { return { methods: AUTH_METHODS }; },
    methods: Object.assign({}, helpers),
    template: `
      <div class="card">
        <div class="card-head"><h2>{{ t('admin.logs.auth') }}</h2>
          <div class="right" v-if="meta"><span class="badge badge-muted">{{ kept(meta.retention.auth_events_days) }}</span></div></div>
        <form class="filter-grid" @submit.prevent="search">
          <label><span>{{ t('admin.logs.user') }}</span>
            <input type="text" v-model.trim="filters.user" spellcheck="false" :placeholder="t('admin.logs.user_hint')"></label>
          <label><span>{{ t('admin.logs.event') }}</span>
            <select v-model="filters.name" @change="search">
              <option value="">{{ t('common.all') }}</option>
              <option v-for="n in (meta ? meta.auth_events : [])" :key="n" :value="n">{{ n }}</option>
            </select></label>
          <label><span>{{ t('admin.logs.method') }}</span>
            <select v-model="filters.method" @change="search">
              <option value="">{{ t('common.all') }}</option>
              <option v-for="m in methods" :key="m" :value="m">{{ m }}</option>
            </select></label>
          <label><span>{{ t('admin.logs.outcome') }}</span>
            <select v-model="filters.outcome" @change="search">
              <option value="">{{ t('common.all') }}</option>
              <option value="success">{{ t('admin.logs.outcome.success') }}</option>
              <option value="failure">{{ t('admin.logs.outcome.failure') }}</option>
            </select></label>
          <label><span>{{ t('common.from') }}</span><input type="date" v-model="filters.from"></label>
          <label><span>{{ t('common.to') }}</span><input type="date" v-model="filters.to"></label>
          <div class="filter-actions">
            <button type="submit" class="btn btn-sm btn-primary">{{ t('common.search') }}</button>
            <button type="button" class="btn btn-sm" @click="reset">{{ t('common.reset') }}</button>
          </div>
        </form>
        <p class="hint card-note" style="margin-top:12px">{{ t('admin.logs.user_search_note') }}</p>
        <div class="card-body" v-if="denied"><no-access :permission="denied"/></div>
        <div class="table-wrap" v-else>
          <table>
            <thead><tr><th>{{ t('admin.audit.when') }}</th><th>{{ t('admin.logs.event') }}</th><th>{{ t('admin.logs.method') }}</th>
              <th>{{ t('admin.logs.outcome') }}</th><th>{{ t('admin.logs.error_code') }}</th><th>{{ t('admin.logs.user') }}</th>
              <th>{{ t('admin.logs.client') }}</th><th>IP</th><th>{{ t('admin.logs.request_id') }}</th></tr></thead>
            <tbody>
              <tr v-if="loading" v-for="n in 5" :key="'s' + n"><td colspan="9"><div class="skeleton-row"></div></td></tr>
              <tr v-else v-for="e in rows" :key="e.id">
                <td class="mono nowrap">{{ e.at }}</td><td class="mono">{{ e.name }}</td><td>{{ e.method || '—' }}</td>
                <td><span :class="'badge ' + (e.outcome === 'success' ? 'badge-ok' : 'badge-danger')">{{ t('admin.logs.outcome.' + e.outcome) }}</span></td>
                <td class="mono">{{ e.error_code || '—' }}</td>
                <td><a v-if="e.user_id" class="link mono" :href="userLink(e.user_id)" @click.prevent="go(userLink(e.user_id))">{{ shortID(e.user_id) }}</a>
                  <span v-else class="muted">—</span></td>
                <td class="nowrap">{{ platformName(e.platform) }} <span class="mono">{{ e.app_version }} {{ e.app_build ? '(' + e.app_build + ')' : '' }}</span></td>
                <td class="mono">{{ maskIP(e.ip) || '—' }}</td>
                <td class="mono">{{ e.request_id || '—' }}</td>
              </tr>
              <tr v-if="!loading && !rows.length"><td colspan="9" class="empty">
                <span v-if="error" class="load-error" role="alert">{{ error }}
                  <button type="button" class="btn btn-sm" @click="load">{{ t('common.refresh') }}</button></span>
                <span v-else>{{ t('common.empty') }}</span></td></tr>
            </tbody>
          </table>
        </div>
        <pager :page="page" :limit="limit" :total="total" @move="move"/>
      </div>`
  };

  var LogErrors = {
    mixins: [listView({ endpoint: "/logs/errors", rowsKey: "errors", limit: 50,
      defaults: { user: "", platform: "", app_version: "", app_build: "", route: "", status: "", request_id: "", from: "", to: "" } })],
    props: { meta: { type: Object, default: null } },
    methods: Object.assign({}, helpers),
    template: `
      <div class="card">
        <div class="card-head"><h2>{{ t('admin.logs.errors') }}</h2>
          <div class="right" v-if="meta"><span class="badge badge-muted">{{ kept(meta.retention.api_errors_days) }}</span></div></div>
        <form class="filter-grid" @submit.prevent="search">
          <label><span>{{ t('admin.logs.user') }}</span>
            <input type="text" v-model.trim="filters.user" spellcheck="false" :placeholder="t('admin.logs.user_hint')"></label>
          <label><span>{{ t('admin.users.col_platform') }}</span>
            <select v-model="filters.platform" @change="search">
              <option value="">{{ t('common.all') }}</option><option value="android">Android</option>
              <option value="ios">iOS</option><option value="web">Web</option>
            </select></label>
          <label><span>{{ t('admin.push.device.app_version') }}</span><input type="text" v-model.trim="filters.app_version" placeholder="1.3.2"></label>
          <label><span>{{ t('admin.logs.build') }}</span><input type="text" v-model.trim="filters.app_build" placeholder="142"></label>
          <label><span>{{ t('admin.logs.route') }}</span><input type="text" v-model.trim="filters.route" spellcheck="false" placeholder="/api/v1/ai/reply"></label>
          <label><span>HTTP</span><input type="number" min="400" max="599" v-model.trim="filters.status" placeholder="500"></label>
          <label><span>{{ t('admin.logs.request_id') }}</span><input type="text" v-model.trim="filters.request_id" spellcheck="false" placeholder="req_…"></label>
          <label><span>{{ t('common.from') }}</span><input type="date" v-model="filters.from"></label>
          <label><span>{{ t('common.to') }}</span><input type="date" v-model="filters.to"></label>
          <div class="filter-actions">
            <button type="submit" class="btn btn-sm btn-primary">{{ t('common.search') }}</button>
            <button type="button" class="btn btn-sm" @click="reset">{{ t('common.reset') }}</button>
          </div>
        </form>
        <div class="card-body" v-if="denied"><no-access :permission="denied"/></div>
        <div class="table-wrap" v-else>
          <table>
            <thead><tr><th>{{ t('admin.audit.when') }}</th><th>{{ t('admin.logs.request') }}</th><th>HTTP</th>
              <th>{{ t('admin.logs.error_code') }}</th><th>{{ t('admin.logs.user') }}</th><th>{{ t('admin.logs.client') }}</th>
              <th>{{ t('admin.logs.duration') }}</th><th>{{ t('admin.logs.request_id') }}</th></tr></thead>
            <tbody>
              <tr v-if="loading" v-for="n in 5" :key="'s' + n"><td colspan="8"><div class="skeleton-row"></div></td></tr>
              <tr v-else v-for="e in rows" :key="e.id">
                <td class="mono nowrap">{{ e.at }}</td><td class="mono">{{ e.method }} {{ e.route }}</td>
                <td><span :class="'badge ' + httpBadge(e.status)">{{ e.status }}</span></td>
                <td class="mono">{{ e.error_code || '—' }}</td>
                <td><a v-if="e.user_id" class="link mono" :href="userLink(e.user_id)" @click.prevent="go(userLink(e.user_id))">{{ shortID(e.user_id) }}</a>
                  <span v-else class="muted">—</span></td>
                <td class="nowrap">{{ platformName(e.platform) }} <span class="mono">{{ e.app_version }} {{ e.app_build ? '(' + e.app_build + ')' : '' }}</span>
                  <div class="mono" v-if="e.os_version">OS {{ e.os_version }}</div></td>
                <td class="nowrap">{{ nf(e.duration_ms) }} ms</td>
                <td class="mono">{{ e.request_id || '—' }}<div v-if="e.trace_id" class="muted small">trace {{ shortID(e.trace_id) }}</div></td>
              </tr>
              <tr v-if="!loading && !rows.length"><td colspan="8" class="empty">
                <span v-if="error" class="load-error" role="alert">{{ error }}
                  <button type="button" class="btn btn-sm" @click="load">{{ t('common.refresh') }}</button></span>
                <span v-else>{{ t('common.empty') }}</span></td></tr>
            </tbody>
          </table>
        </div>
        <pager :page="page" :limit="limit" :total="total" @move="move"/>
      </div>`
  };

  var LogVersions = {
    components: { NoAccess: NoAccess },
    data: function () { return { loading: true, rows: [], denied: "", error: "" }; },
    mounted: function () { this.load(); },
    methods: Object.assign({}, helpers, {
      async load() {
        this.loading = true;
        this.error = "";
        try { this.rows = (await api("/logs/versions")).versions || []; }
        catch (e) {
          this.rows = [];
          if (e.code === "FORBIDDEN") this.denied = e.details.permission || "logs.read";
          else this.error = errorText(e);
        }
        this.loading = false;
      }
    }),
    template: `
      <div class="card">
        <div class="card-head"><h2>{{ t('admin.logs.versions') }}</h2></div>
        <p class="hint card-note" style="margin-top:12px">{{ t('admin.logs.versions_hint') }}</p>
        <div class="card-body" v-if="denied"><no-access :permission="denied"/></div>
        <div class="table-wrap" v-else>
          <table>
            <thead><tr><th>{{ t('admin.users.col_platform') }}</th><th>{{ t('admin.users.col_version') }}</th>
              <th>{{ t('admin.logs.build') }}</th><th>{{ t('admin.ops.installations') }}</th><th>{{ t('admin.ops.active_30d') }}</th>
              <th>{{ t('admin.ops.reachable') }}</th><th>{{ t('admin.logs.api_errors_7d') }}</th>
              <th>{{ t('admin.logs.push_failures_7d') }}</th><th>{{ t('admin.push.device.last_seen') }}</th></tr></thead>
            <tbody>
              <tr v-if="loading" v-for="n in 4" :key="'s' + n"><td colspan="9"><div class="skeleton-row"></div></td></tr>
              <tr v-else v-for="v in rows" :key="v.platform + v.app_version + v.app_build">
                <td>{{ platformName(v.platform) }}</td><td class="mono">{{ v.app_version || '—' }}</td><td class="mono">{{ v.app_build || '—' }}</td>
                <td>{{ nf(v.installations) }}</td><td>{{ nf(v.active_30d) }}</td><td>{{ nf(v.push_reachable) }}</td>
                <td><span :class="'badge ' + (v.api_errors_7d ? 'badge-warn' : 'badge-muted')">{{ nf(v.api_errors_7d) }}</span></td>
                <td><span :class="'badge ' + (v.push_registration_failures_7d ? 'badge-danger' : 'badge-muted')">{{ nf(v.push_registration_failures_7d) }}</span></td>
                <td class="mono nowrap">{{ v.last_seen || '—' }}</td>
              </tr>
              <tr v-if="!loading && !rows.length"><td colspan="9" class="empty">
                <span v-if="error" class="load-error" role="alert">{{ error }}
                  <button type="button" class="btn btn-sm" @click="load">{{ t('common.refresh') }}</button></span>
                <span v-else>{{ t('common.empty') }}</span></td></tr>
            </tbody>
          </table>
        </div>
      </div>`
  };

  var Logs = {
    components: { LogEvents: LogEvents, LogAuth: LogAuth, LogErrors: LogErrors, LogVersions: LogVersions },
    props: { route: { type: Object, required: true }, seq: { type: Number, default: 0 } },
    data: function () { return { meta: null }; },
    mounted: async function () {
      try { this.meta = await api("/logs/meta"); }
      catch (e) { if (e.code !== "FORBIDDEN") toast("danger", errorText(e)); }
    },
    computed: {
      tabs: function () {
        return [
          { key: "events", path: "/admin/logs", label: t("admin.logs.events") },
          { key: "auth", path: "/admin/logs/auth", label: t("admin.logs.auth") },
          { key: "errors", path: "/admin/logs/errors", label: t("admin.logs.errors") },
          { key: "versions", path: "/admin/logs/versions", label: t("admin.logs.versions") }
        ];
      }
    },
    methods: Object.assign({}, helpers),
    template: `
      <div>
        <nav class="subnav" :aria-label="t('admin.logs.title')">
          <a v-for="item in tabs" :key="item.key" :href="item.path" :class="{ 'is-active': route.tab === item.key }"
             :aria-current="route.tab === item.key ? 'page' : null" @click.prevent="go(item.path)">{{ item.label }}</a>
        </nav>
        <div class="notice notice-info">{{ t('admin.logs.privacy') }}</div>
        <log-auth v-if="route.tab === 'auth'" :meta="meta" :key="'auth' + seq"/>
        <log-errors v-else-if="route.tab === 'errors'" :meta="meta" :key="'errors' + seq"/>
        <log-versions v-else-if="route.tab === 'versions'" :key="'versions' + seq"/>
        <log-events v-else :meta="meta" :key="'events' + seq"/>
      </div>`
  };

  /* --------------------------------------------------------------- shell */
  var App = {
    components: { Dashboard: Dashboard, Users: Users, UserDetail: UserDetail, Plans: Plans,
                  Audit: Audit, Settings: Settings, Notifications: Notifications, Logs: Logs, NoAccess: NoAccess },
    data: function () { return { state: state }; },
    computed: {
      title: function () {
        return {
          dashboard: t("admin.nav.dashboard"), users: t("admin.users.title"), user: t("admin.user.detail"),
          plans: t("admin.plans.title"), audit: t("admin.audit.title"),
          settings: t("admin.settings.title"), notifications: t("admin.notifications.title"),
          logs: t("admin.logs.title")
        }[state.route.name];
      },
      // Бетті ашуға рұқсат жоқ болса, API шақырылмайды — сабырлы ескерту көрсетіледі.
      requiredPermission: function () { return ROUTE_PERMISSION[state.route.name] || ""; },
      allowed: function () { return !this.requiredPermission || can(this.requiredPermission); }
    },
    methods: {
      t: t,
      can: can,
      go: navigate,
      isActive: function (name) {
        return state.route.name === name || (name === "users" && state.route.name === "user");
      },
      async setLocale(locale) {
        state.locale = locale;
        try { await api("/locale", { method: "POST", body: { locale: locale } }); } catch (e) { /* көрнекі тіл бәрібір ауысты */ }
      },
      logout: function () { document.getElementById("logout-form").submit(); }
    },
    template: `
      <div class="shell">
        <aside class="sidebar" :class="{ 'is-open': state.sidebarOpen }">
          <div class="brand"><span class="mark">AI</span> AI&nbsp;Reply</div>
          <a v-if="can('dashboard.read')" class="item" href="/admin" :class="{ 'is-active': isActive('dashboard') }"
             :aria-current="isActive('dashboard') ? 'page' : null" @click.prevent="go('/admin')">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><rect x="3" y="3" width="7" height="7" rx="2"/><rect x="14" y="3" width="7" height="7" rx="2"/><rect x="3" y="14" width="7" height="7" rx="2"/><rect x="14" y="14" width="7" height="7" rx="2"/></svg>
            {{ t('admin.nav.dashboard') }}</a>
          <a v-if="can('users.read')" class="item" href="/admin/users" :class="{ 'is-active': isActive('users') }"
             :aria-current="isActive('users') ? 'page' : null" @click.prevent="go('/admin/users')">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><circle cx="9" cy="8" r="3.2"/><path d="M3.5 20a5.5 5.5 0 0 1 11 0M16 11a3 3 0 1 0 0-6M17.5 20a5.5 5.5 0 0 0-2.2-4.4"/></svg>
            {{ t('admin.nav.users') }}</a>
          <a v-if="can('dashboard.read')" class="item" href="/admin/plans" :class="{ 'is-active': isActive('plans') }"
             :aria-current="isActive('plans') ? 'page' : null" @click.prevent="go('/admin/plans')">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><path d="M3 12V5a2 2 0 0 1 2-2h7l9 9-9 9z"/><circle cx="7.5" cy="7.5" r="1.4"/></svg>
            {{ t('admin.nav.plans') }}</a>
          <a v-if="can('notifications.read')" class="item" href="/admin/notifications" :class="{ 'is-active': isActive('notifications') }"
             :aria-current="isActive('notifications') ? 'page' : null" @click.prevent="go('/admin/notifications')">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><path d="M6 9a6 6 0 1 1 12 0c0 5 2 6 2 6H4s2-1 2-6M10 20a2 2 0 0 0 4 0"/></svg>
            {{ t('admin.nav.notifications') }}</a>
          <a v-if="can('logs.read')" class="item" href="/admin/logs" :class="{ 'is-active': isActive('logs') }"
             :aria-current="isActive('logs') ? 'page' : null" @click.prevent="go('/admin/logs')">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><rect x="4" y="3" width="16" height="18" rx="2"/><path d="M8 8h8M8 12h8M8 16h5"/></svg>
            {{ t('admin.nav.logs') }}</a>
          <a v-if="can('audit_logs.read')" class="item" href="/admin/audit" :class="{ 'is-active': isActive('audit') }"
             :aria-current="isActive('audit') ? 'page' : null" @click.prevent="go('/admin/audit')">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><path d="M8 6h13M8 12h13M8 18h13M3.5 6h.01M3.5 12h.01M3.5 18h.01"/></svg>
            {{ t('admin.nav.audit') }}</a>
          <a v-if="can('settings.read')" class="item" href="/admin/settings" :class="{ 'is-active': isActive('settings') }"
             :aria-current="isActive('settings') ? 'page' : null" @click.prevent="go('/admin/settings')">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><circle cx="12" cy="12" r="3"/><path d="M12 2v3M12 19v3M2 12h3M19 12h3M5 5l2 2M17 17l2 2M19 5l-2 2M7 17l-2 2"/></svg>
            {{ t('admin.nav.settings') }}</a>
          <div class="spacer"></div>
          <div class="env">{{ t('admin.settings.env') }}: <b>{{ state.env }}</b><br>
            {{ t('admin.settings.timezone') }}: <b>{{ state.timezone }}</b></div>
        </aside>

        <div class="main">
          <header class="topbar">
            <button class="burger" :aria-label="t('admin.nav.menu')" :aria-expanded="state.sidebarOpen ? 'true' : 'false'"
                    @click="state.sidebarOpen = !state.sidebarOpen">☰</button>
            <h1>{{ title }}</h1>
            <div class="right">
              <div class="lang">
                <a v-for="l in state.locales" :key="l" :class="{ 'is-active': state.locale === l }"
                   @click="setLocale(l)">{{ l }}</a>
              </div>
              <span class="who">{{ state.admin.email }}</span>
              <form id="logout-form" method="post" action="/admin/logout">
                <input type="hidden" name="csrf" :value="state.csrf">
                <button class="btn btn-sm" type="submit">{{ t('admin.nav.logout') }}</button>
              </form>
            </div>
          </header>
          <main class="content">
            <no-access v-if="!allowed" :permission="requiredPermission"/>
            <dashboard v-else-if="state.route.name === 'dashboard'"/>
            <users v-else-if="state.route.name === 'users'"/>
            <user-detail v-else-if="state.route.name === 'user'" :id="state.route.id" :key="state.route.id"/>
            <plans v-else-if="state.route.name === 'plans'"/>
            <audit v-else-if="state.route.name === 'audit'" :key="'audit' + state.routeSeq"/>
            <settings v-else-if="state.route.name === 'settings'"/>
            <notifications v-else-if="state.route.name === 'notifications'" :route="state.route" :seq="state.routeSeq"/>
            <logs v-else-if="state.route.name === 'logs'" :route="state.route" :seq="state.routeSeq"/>
          </main>
        </div>

        <div class="toasts">
          <div v-for="item in state.toasts" :key="item.id" :class="'toast toast-' + item.kind">{{ item.message }}</div>
        </div>
      </div>`
  };

  state.locales = boot.locales;
  var app = Vue.createApp(App);
  app.config.globalProperties.t = t;
  app.config.globalProperties.tf = tf;
  app.config.globalProperties.can = can;
  app.mount("#app");
  document.getElementById("app").classList.remove("app-loading");
})();
