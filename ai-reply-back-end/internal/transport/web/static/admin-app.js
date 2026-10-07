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
      throw error;
    }
    return payload;
  }

  /* ----------------------------------------------------------- errors */
  // Сервердің details.reason мәтіндері (notifications/validate.go) → аударма.
  var REASON_KEYS = {
    "1-80 characters, one line": "admin.error.reason.title_length",
    "1-400 characters": "admin.error.reason.body_length",
    "the notification is too large": "admin.error.reason.too_large",
    "the fallback language must be filled": "admin.error.reason.fallback_required",
    "unknown language": "admin.error.reason.unknown_language",
    "unknown": "admin.error.reason.unknown_value",
    "unknown value": "admin.error.reason.unknown_value",
    "authenticated or anonymous": "admin.error.reason.unknown_value",
    "too long": "admin.error.reason.link_too_long",
    "not a URL": "admin.error.reason.link_not_url",
    "unknown screen": "admin.error.reason.link_screen",
    "host is not allowed": "admin.error.reason.link_host",
    "only aireply:// screens and https links": "admin.error.reason.link_scheme",
    "at most 10 keys": "admin.error.reason.data_keys",
    "key must be lowercase snake_case and not reserved": "admin.error.reason.data_key",
    "at most 256 characters, one line": "admin.error.reason.data_value",
    "at most 1 KB in total": "admin.error.reason.data_size",
    "too many plans": "admin.error.reason.plans_count",
    "unknown plan": "admin.error.reason.unknown_plan",
    "not an id": "admin.error.reason.id",
    "at most 500 people": "admin.error.reason.recipients_count",
    "not an e-mail address": "admin.error.reason.email",
    "send an Idempotency-Key header": "admin.error.reason.idempotency"
  };

  var FIELD_KEYS = {
    name: "admin.push.form.name", title: "admin.push.form.title", body: "admin.push.form.body",
    category: "admin.push.form.category", link: "admin.push.form.link", data: "admin.push.form.data",
    fallback_locale: "admin.push.form.fallback", idempotency_key: "admin.push.form.request",
    "audience.segment": "admin.push.audience.segment", "audience.plan_ids": "admin.push.audience.plans",
    "audience.subscription": "admin.push.audience.subscription", "audience.platforms": "admin.push.audience.platforms",
    "audience.languages": "admin.push.audience.languages", "audience.quota": "admin.push.audience.quota",
    "audience.user_ids": "admin.push.audience.people", "audience.emails": "admin.push.audience.people",
    status: "admin.users.col_status", platform: "admin.users.col_platform", push_status: "admin.push.device.push",
    auth: "admin.push.device.account", channel: "admin.push.delivery.channel", source: "admin.push.delivery.source",
    type: "admin.push.delivery.type", locale: "common.language"
  };

  function fieldLabel(field) {
    var text = /^(title|body)\.([a-z]{2})$/.exec(field);
    if (text) return t(FIELD_KEYS[text[1]]) + " (" + text[2].toUpperCase() + ")";
    if (field.indexOf("data.") === 0) return t("admin.push.form.data") + " «" + field.slice(5) + "»";
    return FIELD_KEYS[field] ? t(FIELD_KEYS[field]) : field;
  }

  // errorText — API қатесі → әкімшіге түсінікті, аударылған мәтін.
  function errorText(e) {
    var code = e && e.code;
    var details = (e && e.details) || {};
    if (!code) return t("admin.error.network");
    switch (code) {
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
        return details.field === "idempotency_key" ? t("admin.error.idempotency_conflict") : t("admin.error.conflict");
      case "NOT_FOUND":
        return t("admin.error.not_found");
      default:
        return t("common.error");
    }
  }

  /* ----------------------------------------------------------- formatting */
  function nf(value) {
    if (value === null || value === undefined) return "—";
    return new Intl.NumberFormat(state.locale === "kk" ? "kk-KZ" : state.locale).format(value);
  }
  function money(value) { return "$" + (Math.round(value * 100) / 100).toFixed(2); }

  function shortID(id) { return id ? String(id).slice(0, 8) : ""; }

  function clock(date) {
    function two(n) { return (n < 10 ? "0" : "") + n; }
    return two(date.getHours()) + ":" + two(date.getMinutes()) + ":" + two(date.getSeconds());
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

  // flatten — метадеректі "key: value" жолдарына жаю ("[object Object]" орнына).
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
  var Dashboard = {
    components: { LineChart: LineChart, BarChart: BarChart, DonutChart: DonutChart },
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

  var UserDetail = {
    props: { id: String },
    data: function () { return { loading: true, data: null, plans: [], planID: "", expires: "" }; },
    mounted: function () { this.load(); },
    methods: {
      t: t, nf: nf, money: money,
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
                  <dt>{{ t('admin.user.preferred_language') }}</dt>
                  <dd>{{ data.user.preferred_language ? data.user.preferred_language.toUpperCase() : t('admin.user.not_set') }}</dd>
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
                    <th>{{ t('admin.users.col_last') }}</th></tr></thead>
                  <tbody>
                    <tr v-for="d in data.devices" :key="d.id">
                      <td class="mono">{{ d.id.slice(0, 8) }}</td><td>{{ d.platform }}</td><td>{{ d.app_version }}</td>
                      <td class="mono">{{ d.last_seen }}</td>
                    </tr>
                    <tr v-if="!data.devices.length"><td colspan="4" class="empty">{{ t('common.empty') }}</td></tr>
                  </tbody>
                </table>
              </div>
            </div>
          </div>

          <div>
            <div class="card">
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
              <div class="card-head"><h2>{{ t('admin.nav.notifications') }}</h2></div>
              <div class="card-body">
                <a v-if="data.user.status === 'active'" class="btn btn-primary btn-block" style="margin-bottom:10px"
                   :href="'/admin/notifications/new?user_ids=' + encodeURIComponent(data.user.id)"
                   @click.prevent="$root.go('/admin/notifications/new?user_ids=' + encodeURIComponent(data.user.id))">
                  {{ t('admin.user.send_push') }}</a>
                <a class="btn btn-block" style="margin-bottom:10px"
                   :href="'/admin/notifications/deliveries?user_id=' + encodeURIComponent(data.user.id)"
                   @click.prevent="$root.go('/admin/notifications/deliveries?user_id=' + encodeURIComponent(data.user.id))">
                  {{ t('admin.user.push_history') }}</a>
                <a class="btn btn-block" :href="'/admin/notifications/devices?user_id=' + encodeURIComponent(data.user.id)"
                   @click.prevent="$root.go('/admin/notifications/devices?user_id=' + encodeURIComponent(data.user.id))">
                  {{ t('admin.user.push_devices') }}</a>
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
      </div>
      <div v-else class="empty">…</div>`
  };

  var Plans = {
    data: function () {
      return { loading: true, plans: [], editing: null, tab: "kk", saving: false };
    },
    mounted: function () { this.load(); },
    methods: {
      t: t, nf: nf,
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
            <div class="right"><button class="btn btn-sm btn-primary" @click="create">+ {{ t('admin.plans.new') }}</button></div>
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
                    <button class="btn btn-sm" @click="edit(p)">{{ t('common.edit') }}</button>
                    <button class="btn btn-sm btn-danger" @click="archive(p)">{{ t('common.archive') }}</button>
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

  var Audit = {
    data: function () { return { rows: [], total: 0, page: 1, limit: 50 }; },
    mounted: function () { this.load(); },
    methods: {
      t: t, nf: nf,
      async load() {
        try {
          var data = await api("/audit?page=" + this.page);
          this.rows = data.entries; this.total = data.total; this.limit = data.limit;
        } catch (e) { toast("danger", t("common.error")); }
      },
      move(delta) { this.page = Math.max(1, this.page + delta); this.load(); },
      meta(row) {
        if (!row.metadata) return "";
        return flatten(row.metadata, "").map(function (p) { return p.key + "=" + p.value; }).join(" ");
      }
    },
    template: `
      <div class="card">
        <div class="card-head"><h2>{{ t('admin.audit.title') }}</h2>
          <div class="right"><span class="badge badge-muted">{{ t('common.total') }}: {{ nf(total) }}</span></div></div>
        <div class="table-wrap">
          <table>
            <thead><tr><th>{{ t('admin.audit.when') }}</th><th>{{ t('admin.audit.admin') }}</th>
              <th>{{ t('admin.audit.action') }}</th><th>{{ t('admin.audit.entity') }}</th><th>IP</th><th>Meta</th></tr></thead>
            <tbody>
              <tr v-for="(row, i) in rows" :key="i">
                <td class="mono">{{ row.at }}</td><td>{{ row.admin }}</td>
                <td><span class="badge badge-brand">{{ row.action }}</span></td>
                <td class="mono">{{ row.entity_type }} {{ row.entity_id ? row.entity_id.slice(0, 8) : '' }}</td>
                <td class="mono">{{ row.ip }}</td><td class="mono">{{ meta(row) }}</td>
              </tr>
              <tr v-if="!rows.length"><td colspan="6" class="empty">{{ t('common.empty') }}</td></tr>
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
      t: t,
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
                  v-model.number="limits[f.key]" :aria-invalid="outOfRange(f.key)">
                <small class="muted">{{ t('admin.settings.limit_default') }}: {{ data.ai_limits.defaults[f.key] }} ·
                  {{ t('admin.settings.limit_range') }}: {{ range(f.key).min }}–{{ range(f.key).max }}</small>
              </label>
            </div>
            <p class="muted">{{ t('admin.settings.limits_plans_note') }}</p>
            <button class="btn btn-primary" :disabled="savingLimits" @click="saveLimits">{{ t('common.save') }}</button>
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
          <div class="card-body">
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
  var CAMPAIGN_STATUSES = ["draft", "queued", "processing", "completed", "partially_failed", "failed", "cancelled"];
  var DELIVERY_STATUSES = ["queued", "sending", "retrying", "provider_accepted", "provider_failed",
    "invalid_token", "skipped", "cancelled"];
  var PUSH_STATUSES = ["none", "active", "invalid", "replaced"];

  // Used only until GET /notifications answers (or if it fails). Plans are never
  // listed here: the audience form always loads them from GET /plans.
  var DEFAULT_PUSH_META = {
    status: { enabled: false, worker: false, fcm: false, email: false, link_hosts: [] },
    categories: ["account", "subscription", "security", "system", "marketing"],
    screens: ["home", "subscription", "settings", "notifications", "templates", "profile", "keyboard", "compose"],
    content_locales: ["kk", "ru", "en", "uz"], required_locales: ["kk", "ru", "en"], fallback_locale: "ru",
    languages: ["kk", "ru", "en", "uz"], segments: ["all", "free", "paid", "demo"],
    subscription: ["active", "expired"], quota: ["has_remaining", "near_exhaustion", "exhausted"],
    platforms: ["android", "ios"], channels: ["push", "email"],
    types: ["subscription_activated", "subscription_expiring", "subscription_expired", "quota_low", "quota_exhausted"],
    limits: { title: 80, body: 400, data_keys: 10, recipients: 500 }
  };
  var DATA_KEY_PATTERN = /^[a-z][a-z0-9_]{0,31}$/;
  var dataRowSeq = 0;

  function pushMeta(meta) { return Object.assign({}, DEFAULT_PUSH_META, meta || {}); }

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

  // pendingOf — әлі аяқталмаған жеткізулер (queued + sending + retrying).
  function pendingOf(stats) { return stats ? (stats.queued || 0) + (stats.sending || 0) + (stats.retrying || 0) : 0; }

  function adminName(id, email) {
    if (email) return email;
    if (!id) return "—";
    return state.admin && id === state.admin.id ? state.admin.email : tf("admin.push.admin_id", { id: shortID(id) });
  }

  function platformName(p) { return p === "ios" ? "iOS" : p === "android" ? "Android" : (p || "—"); }
  function langName(l) { return l ? String(l).toUpperCase() : "—"; }
  function runeLength(s) { return Array.from(s || "").length; }

  function planName(p) { return (p.name && (p.name[state.locale] || p.name.en)) || p.code; }

  // planLabel — аудиториядағы тариф коды; тариф өшірілген болса, қысқа ID.
  function planLabel(id, plans) {
    var plan = (plans || []).filter(function (p) { return p.id === id; })[0];
    return plan ? plan.code : shortID(id);
  }

  // textSource — l тілінің алушысы қай тілдің мәтінін алады (сервердегі Campaign.TextFor сияқты):
  // өз тілі толық болса — сол, әйтпесе қор тіл, ол да бос болса — ретімен алғашқы толтырылғаны.
  function textSource(content, l, locales) {
    var titles = content.title || {}, bodies = content.body || {};
    function filled(x) { return !!(titles[x] && bodies[x]); }
    if (filled(l)) return l;
    if (filled(content.fallback_locale)) return content.fallback_locale;
    for (var i = 0; i < locales.length; i++) {
      if (filled(locales[i])) return locales[i];
    }
    return "";
  }

  function blankAudience() {
    return { segment: "", plan_ids: [], subscription: "", platforms: [], languages: [], quota: "", people: "" };
  }

  // splitPeople — нақты адамдар өрісі: ID мен пошта бос орынмен, үтірмен не жаңа жолмен бөлінеді.
  function splitPeople(raw) {
    var ids = [], emails = [], seen = {};
    String(raw || "").split(/[\s,;]+/).forEach(function (item) {
      item = item.trim();
      if (!item) return;
      var email = item.indexOf("@") !== -1;
      if (email) item = item.toLowerCase();
      if (seen[item]) return;
      seen[item] = true;
      (email ? emails : ids).push(item);
    });
    return { user_ids: ids.sort(), emails: emails.sort() };
  }

  // buildAudience — domain.AudienceFilter JSON: бос өріс жіберілмейді (omitempty). Алушыларды тек сервер таңдайды.
  function buildAudience(a) {
    var out = {};
    if (a.segment) out.segment = a.segment;
    if (a.plan_ids.length) out.plan_ids = a.plan_ids.slice().sort();
    if (a.subscription) out.subscription = a.subscription;
    if (a.platforms.length) out.platforms = a.platforms.slice().sort();
    if (a.languages.length) out.languages = a.languages.slice().sort();
    if (a.quota) out.quota = a.quota;
    var people = splitPeople(a.people);
    if (people.user_ids.length) out.user_ids = people.user_ids;
    if (people.emails.length) out.emails = people.emails;
    return out;
  }

  // audienceSummary — сүзгінің адам оқитын сипаттамасы (науқан беті мен растау терезесі).
  function audienceSummary(a, plans) {
    a = a || {};
    var out = [];
    if (a.segment && a.segment !== "all") out.push(t("admin.push.audience.segment") + ": " + t("admin.push.segment." + a.segment));
    if (a.plan_ids && a.plan_ids.length) {
      out.push(t("admin.push.audience.plans") + ": " + a.plan_ids.map(function (id) { return planLabel(id, plans); }).join(", "));
    }
    if (a.subscription) out.push(t("admin.push.audience.subscription") + ": " + t("admin.push.subscription." + a.subscription));
    if (a.platforms && a.platforms.length) out.push(t("admin.push.audience.platforms") + ": " + a.platforms.map(platformName).join(", "));
    if (a.languages && a.languages.length) out.push(t("admin.push.audience.languages") + ": " + a.languages.map(langName).join(", "));
    if (a.quota) out.push(t("admin.push.audience.quota") + ": " + t("admin.push.quota." + a.quota));
    var people = (a.user_ids || []).length + (a.emails || []).length;
    if (people) out.push(tf("admin.push.summary.people", { n: people }));
    if (!out.length) out.push(t("admin.push.summary.everyone"));
    return out;
  }

  // helpers — хабарлама беттерінің ортақ әдістері.
  var helpers = {
    t: t, tf: tf, nf: nf, go: navigate, shortID: shortID, langName: langName, platformName: platformName,
    campaignBadge: campaignBadge, deliveryBadge: deliveryBadge, pushBadge: pushBadge,
    permissionBadge: permissionBadge, pendingOf: pendingOf, adminName: adminName,
    pairs: function (value) { return flatten(value, ""); },
    userLink: function (id) { return "/admin/users/" + encodeURIComponent(id); },
    // target — жеткізу қайда кетті: пошта не құрылғы.
    target: function (d) {
      if (d.channel === "email") return t("admin.push.channel.email");
      return d.device || platformName(d.platform);
    }
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

  // listView — сүзгі, бет және мекенжай жолымен синхрондау (сілтемемен бөлісуге болады).
  function listView(options) {
    return {
      components: { Pager: Pager },
      data: function () {
        var query = new URLSearchParams(location.search);
        return {
          loading: true, rows: [], total: 0, limit: options.limit || 50, error: "",
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
          } catch (e) {
            // A failed load must never look like an empty list.
            this.rows = [];
            this.total = 0;
            this.error = errorText(e);
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

  // RecipientsPreview — алдын ала санау: алушылар, тіл бойынша бөлініс және табылмаған пошталар.
  var RecipientsPreview = {
    props: { preview: { type: Object, required: true }, content: { type: Object, required: true }, meta: { type: Object, default: null } },
    computed: {
      m: function () { return pushMeta(this.meta); },
      unresolved: function () { return this.preview.unresolved || []; }
    },
    methods: Object.assign({}, helpers, {
      reach: function (l) { return (this.preview.by_language || {})[l] || { users: 0, devices: 0 }; },
      source: function (l) { return textSource(this.content, l, this.m.content_locales); }
    }),
    template: `
      <div>
        <div class="stat-grid" aria-live="polite">
          <div class="stat stat-brand"><div class="label">{{ t('admin.push.preview.users') }}</div><div class="value">{{ nf(preview.users) }}</div></div>
          <div class="stat stat-brand"><div class="label">{{ t('admin.push.preview.devices') }}</div><div class="value">{{ nf(preview.devices) }}</div></div>
          <div class="stat"><div class="label">Android</div><div class="value">{{ nf(preview.android) }}</div></div>
          <div class="stat"><div class="label">iOS</div><div class="value">{{ nf(preview.ios) }}</div></div>
        </div>
        <p class="hint">{{ tf('admin.push.preview.matched', { n: nf(preview.matched_devices) }) }}</p>
        <h3 class="subhead">{{ t('admin.push.preview.by_language') }}</h3>
        <div class="table-wrap">
          <table class="mini-table">
            <thead><tr><th>{{ t('common.language') }}</th><th>{{ t('admin.push.preview.users') }}</th>
              <th>{{ t('admin.push.preview.devices') }}</th><th>{{ t('admin.push.preview.text') }}</th></tr></thead>
            <tbody>
              <tr v-for="l in m.languages" :key="l" :class="{ 'is-zero': !reach(l).devices }">
                <td><b>{{ langName(l) }}</b></td>
                <td>{{ nf(reach(l).users) }}</td>
                <td>{{ nf(reach(l).devices) }}</td>
                <td><span v-if="source(l) === l" class="badge badge-ok">{{ t('admin.push.lang.own') }}</span>
                  <span v-else-if="source(l)" class="badge badge-warn">→ {{ langName(source(l)) }}</span>
                  <span v-else class="muted">—</span></td>
              </tr>
            </tbody>
          </table>
        </div>
        <p class="hint">{{ t('admin.push.preview.language_hint') }}</p>
        <div v-if="unresolved.length" class="notice notice-warn unresolved" role="status">
          <b>{{ tf('admin.push.preview.unresolved', { n: unresolved.length }) }}</b>
          <ul class="plain-list mono"><li v-for="e in unresolved" :key="e">{{ e }}</li></ul>
        </div>
      </div>`
  };

  // SendConfirm — жіберер алдында: әр тілдің мазмұны, аудитория және серверде жаңа есептелген алушылар.
  var SendConfirm = {
    components: { RecipientsPreview: RecipientsPreview },
    props: {
      content: { type: Object, required: true }, audience: { type: Object, default: function () { return {}; } },
      plans: { type: Array, default: function () { return []; } }, meta: { type: Object, default: null },
      busy: { type: Boolean, default: false }, blocked: { type: Boolean, default: false }
    },
    emits: ["confirm", "close"],
    data: function () { return { preview: null, loading: true, error: "" }; },
    computed: {
      m: function () { return pushMeta(this.meta); },
      summary: function () { return audienceSummary(this.audience, this.plans); },
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
      source: function (l) { return textSource(this.content, l, this.m.content_locales); },
      close: function () { if (!this.busy) this.$emit("close"); },
      confirm: function () { if (!this.busy && !this.loading) this.$emit("confirm"); }
    }),
    template: `
      <div class="modal-backdrop" @click.self="close" @keydown.esc="close">
        <div class="modal modal-wide" ref="dialog" tabindex="-1" role="dialog" aria-modal="true" aria-labelledby="send-confirm-title">
          <div class="modal-head">
            <h2 id="send-confirm-title">{{ t('admin.push.confirm.title') }}</h2>
            <button type="button" class="btn btn-sm" :aria-label="t('common.cancel')" :disabled="busy" @click="close">✕</button>
          </div>
          <div class="modal-body">
            <div v-if="blocked" class="notice notice-danger">{{ t('admin.push.confirm.blocked') }}</div>
            <ul class="lang-list" :aria-label="t('admin.push.form.content')">
              <li v-for="l in m.content_locales" :key="l">
                <span class="lang-code">{{ langName(l) }}</span>
                <div v-if="source(l) === l" class="push-preview"><b>{{ content.title[l] }}</b><p>{{ content.body[l] }}</p></div>
                <span v-else-if="source(l)" class="muted">{{ tf('admin.push.lang.uses', { lang: langName(source(l)) }) }}</span>
                <span v-else class="muted">—</span>
              </li>
            </ul>
            <dl class="kv">
              <dt>{{ t('admin.push.form.category') }}</dt><dd>{{ t('admin.push.category.' + content.category) }}</dd>
              <dt>{{ t('admin.push.form.fallback') }}</dt><dd>{{ langName(content.fallback_locale) }}</dd>
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
            <recipients-preview v-else-if="preview" :preview="preview" :content="content" :meta="meta"/>
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
    props: { meta: { type: Object, default: null } },
    data: function () { return { statuses: CAMPAIGN_STATUSES }; },
    computed: { m: function () { return pushMeta(this.meta); } },
    methods: Object.assign({}, helpers, {
      open: function (c) { navigate("/admin/notifications/campaigns/" + c.id); },
      headline: function (c) { return (c.title || {})[c.fallback_locale] || ""; },
      languages: function (c) {
        var titles = c.title || {}, bodies = c.body || {};
        return this.m.content_locales.filter(function (l) { return titles[l] && bodies[l]; }).map(langName).join(" · ");
      }
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
            <a class="btn btn-sm btn-primary" href="/admin/notifications/new"
               @click.prevent="go('/admin/notifications/new')">+ {{ t('admin.push.new') }}</a>
          </div>
        </div>
        <div class="table-wrap">
          <table>
            <thead><tr><th>{{ t('admin.push.col.campaign') }}</th><th>{{ t('admin.users.col_status') }}</th>
              <th>{{ t('admin.push.col.created') }}</th><th>{{ t('admin.push.col.recipients') }}</th>
              <th>{{ t('admin.push.delivery_status.provider_accepted') }}</th><th>{{ t('admin.push.stat.failed') }}</th>
              <th>{{ t('admin.push.stat.opened') }}</th></tr></thead>
            <tbody>
              <tr v-if="loading" v-for="n in 4" :key="'s' + n"><td colspan="7"><div class="skeleton-row"></div></td></tr>
              <tr v-else v-for="c in rows" :key="c.id" class="clickable" tabindex="0" @click="open(c)" @keydown.enter="open(c)">
                <td><b>{{ c.name }}</b><div class="muted small">{{ headline(c) }}</div>
                  <div class="mono">{{ languages(c) }}</div></td>
                <td><span :class="'badge ' + campaignBadge(c.status)">{{ t('admin.push.campaign_status.' + c.status) }}</span></td>
                <td class="nowrap"><span class="mono">{{ c.created_at }}</span><div class="muted small">{{ adminName(c.created_by, c.created_by_email) }}</div></td>
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
    components: { SendConfirm: SendConfirm, RecipientsPreview: RecipientsPreview },
    props: { meta: { type: Object, default: null }, plans: { type: Array, default: function () { return []; } } },
    data: function () {
      var m = pushMeta(this.meta), title = {}, body = {};
      m.content_locales.forEach(function (l) { title[l] = ""; body[l] = ""; });
      var audience = blankAudience();
      // "Send push" on a user's page opens the form with that person already chosen.
      audience.people = fromQuery({ user_ids: "" }).user_ids.split(",")
        .map(function (s) { return s.trim(); }).filter(Boolean).join("\n");
      return {
        form: {
          name: "", category: "marketing", fallback: m.fallback_locale || m.content_locales[0],
          title: title, body: body, linkType: "", screen: m.screens[0], url: "", data: []
        },
        tab: m.content_locales[0],
        audience: audience,
        // One key per content: a double click, a retry after a timeout or a lost
        // response repeat the same command and the server returns the first campaign.
        idemKey: uuid(),
        preview: null, previewedFor: "", previewing: false, previewError: "",
        busy: "", confirming: false, fieldError: null, submitError: ""
      };
    },
    computed: {
      m: function () { return pushMeta(this.meta); },
      limits: function () { return this.m.limits; },
      locales: function () { return this.m.content_locales; },
      linkHosts: function () { return ((this.m.status && this.m.status.link_hosts) || []).join(", ") || "—"; },
      // Unknown status (not loaded) is not "blocked": the server decides and answers PUSH_DISABLED if so.
      ready: function () {
        var s = this.meta && this.meta.status;
        return !s || !!(s.enabled && s.fcm);
      },
      titleLength: function () { return runeLength(this.form.title[this.tab]); },
      bodyLength: function () { return runeLength(this.form.body[this.tab]); },
      peopleCount: function () {
        var people = splitPeople(this.audience.people);
        return people.user_ids.length + people.emails.length;
      },
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
      texts: function () {
        var form = this.form, title = {}, body = {};
        this.locales.forEach(function (l) { title[l] = form.title[l].trim(); body[l] = form.body[l].trim(); });
        return { title: title, body: body };
      },
      audienceJSON: function () { return buildAudience(this.audience); },
      payload: function () {
        return {
          name: this.form.name.trim(), category: this.form.category, fallback_locale: this.form.fallback,
          title: this.texts.title, body: this.texts.body, link: this.link, data: this.dataObject,
          audience: this.audienceJSON
        };
      },
      content: function () {
        var p = this.payload;
        return {
          title: p.title, body: p.body, fallback_locale: p.fallback_locale, category: p.category,
          link: p.link, data: p.data
        };
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
    mounted: function () { this.runPreview(); },
    methods: Object.assign({}, helpers, {
      planName: planName,
      errorFor: function (field) {
        var e = this.fieldError;
        if (!e) return "";
        return e.field === field || e.field.indexOf(field + ".") === 0 ? e.text : "";
      },
      filled: function (l) { return !!(this.texts.title[l] && this.texts.body[l]); },
      partial: function (l) { return !this.filled(l) && !!(this.texts.title[l] || this.texts.body[l]); },
      empty: function (l) { return !this.texts.title[l] && !this.texts.body[l]; },
      langError: function (l) { return !!(this.errorFor("title." + l) || this.errorFor("body." + l)); },
      optional: function (l) { return this.m.required_locales.indexOf(l) === -1; },
      sourceFor: function (l) { return textSource(this.content, l, this.locales); },
      shown: function (l) {
        var source = this.sourceFor(l);
        return { title: source ? this.texts.title[source] : "", body: source ? this.texts.body[source] : "" };
      },
      copyFrom: function (l) {
        this.form.title[this.tab] = this.form.title[l];
        this.form.body[this.tab] = this.form.body[l];
      },
      addData: function () {
        if (this.form.data.length < this.limits.data_keys) this.form.data.push({ id: ++dataRowSeq, key: "", value: "" });
      },
      removeData: function (i) { this.form.data.splice(i, 1); },
      resetAudience: function () { this.audience = blankAudience(); },
      // focusField — қате тілдің қойындысын ашады.
      focusField: function (field) {
        var text = /^(title|body)\.([a-z]{2})$/.exec(field || "");
        if (text && this.locales.indexOf(text[2]) !== -1) this.tab = text[2];
      },
      // localProblem — серверге дейінгі тексеру (сервер бәрібір қайта тексереді).
      localProblem: function () {
        var p = this.payload, limits = this.limits, seen = {}, problem = null;
        this.locales.forEach(function (l) {
          var title = p.title[l], body = p.body[l];
          if (problem || (!title && !body)) return;
          if (!title) problem = { field: "title." + l, reason: "admin.error.reason.pair" };
          else if (runeLength(title) > limits.title) problem = { field: "title." + l, reason: "admin.error.reason.title_length" };
          else if (!body) problem = { field: "body." + l, reason: "admin.error.reason.pair" };
          else if (runeLength(body) > limits.body) problem = { field: "body." + l, reason: "admin.error.reason.body_length" };
        });
        if (problem) return problem;
        if (!p.title[p.fallback_locale] || !p.body[p.fallback_locale]) {
          return { field: "title." + p.fallback_locale, reason: "admin.error.reason.fallback_required" };
        }
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
        if (this.peopleCount > limits.recipients) return { field: "audience.user_ids", reason: "admin.error.reason.recipients_count" };
        return null;
      },
      validate: function () {
        var problem = this.localProblem();
        if (!problem) return true;
        var text = fieldLabel(problem.field) + ": " + t(problem.reason);
        this.fieldError = { field: problem.field, text: t(problem.reason) };
        this.submitError = text;
        this.focusField(problem.field);
        toast("danger", text);
        return false;
      },
      showError: function (e) {
        var text = errorText(e);
        this.submitError = e.code ? text : text + " " + t("admin.push.retry_safe");
        if (e.code === "INVALID_REQUEST" && e.details && e.details.field) {
          this.fieldError = { field: e.details.field, text: text };
          this.focusField(e.details.field);
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

              <div class="form-grid">
                <label class="field"><span>{{ t('admin.push.form.category') }}</span>
                  <select v-model="form.category" :aria-invalid="!!errorFor('category')">
                    <option v-for="c in m.categories" :key="c" :value="c">{{ t('admin.push.category.' + c) }}</option>
                  </select>
                  <small class="hint">{{ form.category === 'security' ? t('admin.push.form.security_hint') : t('admin.push.form.category_hint') }}</small>
                  <small v-if="errorFor('category')" class="field-error" role="alert">{{ errorFor('category') }}</small></label>
                <label class="field"><span>{{ t('admin.push.form.fallback') }}</span>
                  <select v-model="form.fallback" :aria-invalid="!!errorFor('fallback_locale')">
                    <option v-for="l in locales" :key="l" :value="l">{{ langName(l) }}</option>
                  </select>
                  <small class="hint">{{ t('admin.push.form.fallback_hint') }}</small>
                  <small v-if="errorFor('fallback_locale')" class="field-error" role="alert">{{ errorFor('fallback_locale') }}</small></label>
              </div>

              <div class="tabs lang-tabs" role="tablist" :aria-label="t('admin.push.form.languages')">
                <button v-for="l in locales" :key="l" type="button" role="tab" :aria-selected="tab === l ? 'true' : 'false'"
                        :class="{ 'is-active': tab === l }" @click="tab = l">
                  {{ langName(l) }}
                  <span v-if="langError(l) || partial(l)" class="tab-mark tab-mark-danger" :title="t('admin.push.lang.incomplete')">!</span>
                  <span v-else-if="filled(l)" class="tab-mark tab-mark-ok" :title="t('admin.push.lang.filled')">✓</span>
                  <span v-else-if="sourceFor(l)" class="tab-mark" :title="tf('admin.push.lang.uses', { lang: langName(sourceFor(l)) })">→ {{ langName(sourceFor(l)) }}</span>
                  <small v-if="optional(l)" class="tab-note">{{ t('admin.push.lang.optional') }}</small>
                </button>
              </div>

              <div role="tabpanel">
                <p v-if="tab === form.fallback" class="hint lang-note">{{ t('admin.push.lang.fallback_note') }}</p>
                <div v-else-if="empty(tab) && !optional(tab)" class="notice notice-warn lang-note" role="status">
                  {{ tf('admin.push.lang.missing_note', { lang: langName(sourceFor(tab) || form.fallback) }) }}</div>
                <p v-else-if="empty(tab)" class="hint lang-note">{{ tf('admin.push.lang.empty_note', { lang: langName(sourceFor(tab) || form.fallback) }) }}</p>

                <label class="field"><span>{{ t('admin.push.form.title') }} · {{ langName(tab) }}
                    <small class="counter" :class="{ 'is-over': titleLength > limits.title }" aria-live="polite">{{ titleLength }} / {{ limits.title }}</small></span>
                  <input type="text" v-model="form.title[tab]" :lang="tab"
                         :aria-invalid="!!errorFor('title.' + tab) || titleLength > limits.title" aria-describedby="push-title-error">
                  <small v-if="errorFor('title.' + tab)" id="push-title-error" class="field-error" role="alert">{{ errorFor('title.' + tab) }}</small></label>

                <label class="field"><span>{{ t('admin.push.form.body') }} · {{ langName(tab) }}
                    <small class="counter" :class="{ 'is-over': bodyLength > limits.body }" aria-live="polite">{{ bodyLength }} / {{ limits.body }}</small></span>
                  <textarea rows="4" v-model="form.body[tab]" :lang="tab"
                            :aria-invalid="!!errorFor('body.' + tab) || bodyLength > limits.body" aria-describedby="push-body-error"></textarea>
                  <small v-if="errorFor('body.' + tab)" id="push-body-error" class="field-error" role="alert">{{ errorFor('body.' + tab) }}</small></label>

                <div class="lang-tools" v-if="tab !== form.fallback && empty(tab) && filled(form.fallback)">
                  <button type="button" class="btn btn-sm" @click="copyFrom(form.fallback)">
                    {{ tf('admin.push.form.copy_from', { lang: langName(form.fallback) }) }}</button>
                </div>

                <div class="push-preview push-phone" :aria-label="t('admin.push.form.phone_preview')">
                  <div class="push-phone-head"><span class="push-app">AI Reply</span>
                    <span v-if="sourceFor(tab) && sourceFor(tab) !== tab" class="badge badge-warn">{{ tf('admin.push.lang.uses', { lang: langName(sourceFor(tab)) }) }}</span></div>
                  <template v-if="sourceFor(tab)"><b>{{ shown(tab).title }}</b><p>{{ shown(tab).body }}</p></template>
                  <p v-else class="muted">{{ t('admin.push.form.preview_empty') }}</p>
                </div>
              </div>

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

              <fieldset class="field">
                <legend>{{ t('admin.push.audience.segment') }}</legend>
                <div class="segmented segmented-wrap" role="radiogroup" :aria-label="t('admin.push.audience.segment')">
                  <button v-for="s in m.segments" :key="s" type="button" role="radio"
                          :aria-checked="(audience.segment || 'all') === s ? 'true' : 'false'"
                          :class="{ 'is-active': (audience.segment || 'all') === s }"
                          @click="audience.segment = s === 'all' ? '' : s">{{ t('admin.push.segment.' + s) }}</button>
                </div>
                <small class="hint">{{ t('admin.push.audience.segment_hint') }}</small>
                <small v-if="errorFor('audience.segment')" class="field-error" role="alert">{{ errorFor('audience.segment') }}</small>
              </fieldset>

              <fieldset class="field">
                <legend>{{ t('admin.push.audience.plans') }}</legend>
                <div class="chips" v-if="plans.length">
                  <label v-for="p in plans" :key="p.id" class="chip"
                         :class="{ 'is-on': audience.plan_ids.indexOf(p.id) !== -1, 'is-dim': p.archived || !p.is_active }">
                    <input type="checkbox" :value="p.id" v-model="audience.plan_ids"> {{ planName(p) }}
                    <span class="mono">{{ p.code }}</span>
                    <span v-if="p.archived" class="badge badge-muted">{{ t('admin.push.audience.plan_archived') }}</span></label>
                </div>
                <small v-else class="hint">{{ t('admin.push.audience.no_plans') }}</small>
                <small class="hint">{{ t('admin.push.audience.plans_hint') }}</small>
                <small v-if="errorFor('audience.plan_ids')" class="field-error" role="alert">{{ errorFor('audience.plan_ids') }}</small>
              </fieldset>

              <div class="form-grid">
                <label class="field"><span>{{ t('admin.push.audience.subscription') }}</span>
                  <select v-model="audience.subscription" :aria-invalid="!!errorFor('audience.subscription')">
                    <option value="">{{ t('admin.push.audience.any') }}</option>
                    <option v-for="s in m.subscription" :key="s" :value="s">{{ t('admin.push.subscription.' + s) }}</option>
                  </select>
                  <small v-if="errorFor('audience.subscription')" class="field-error" role="alert">{{ errorFor('audience.subscription') }}</small></label>
                <label class="field"><span>{{ t('admin.push.audience.quota') }}</span>
                  <select v-model="audience.quota" :aria-invalid="!!errorFor('audience.quota')">
                    <option value="">{{ t('admin.push.audience.any') }}</option>
                    <option v-for="q in m.quota" :key="q" :value="q">{{ t('admin.push.quota.' + q) }}</option>
                  </select>
                  <small class="hint">{{ t('admin.push.audience.quota_hint') }}</small>
                  <small v-if="errorFor('audience.quota')" class="field-error" role="alert">{{ errorFor('audience.quota') }}</small></label>
              </div>

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
                  <legend>{{ t('admin.push.audience.languages') }}</legend>
                  <div class="chips">
                    <label v-for="l in m.languages" :key="l" class="chip" :class="{ 'is-on': audience.languages.indexOf(l) !== -1 }">
                      <input type="checkbox" :value="l" v-model="audience.languages"> {{ langName(l) }}</label>
                  </div>
                  <small class="hint">{{ t('admin.push.audience.languages_hint') }}</small>
                  <small v-if="errorFor('audience.languages')" class="field-error" role="alert">{{ errorFor('audience.languages') }}</small>
                </fieldset>
              </div>

              <label class="field" style="margin-bottom:0"><span>{{ t('admin.push.audience.people') }}</span>
                <textarea rows="3" v-model="audience.people" spellcheck="false" autocomplete="off"
                          :placeholder="t('admin.push.audience.people_placeholder')"
                          :aria-invalid="!!(errorFor('audience.user_ids') || errorFor('audience.emails')) || peopleCount > limits.recipients"></textarea>
                <small class="hint">{{ tf('admin.push.audience.people_count', { n: peopleCount, max: limits.recipients }) }}
                  · {{ t('admin.push.audience.people_hint') }}</small>
                <small v-if="errorFor('audience.user_ids')" class="field-error" role="alert">{{ errorFor('audience.user_ids') }}</small>
                <small v-if="errorFor('audience.emails')" class="field-error" role="alert">{{ errorFor('audience.emails') }}</small></label>
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
                <recipients-preview :preview="preview" :content="content" :meta="meta"/>
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

        <send-confirm v-if="confirming" :content="content" :audience="audienceJSON" :plans="plans" :meta="meta"
                      :busy="busy === 'send'" :blocked="!ready" @close="confirming = false" @confirm="submit(true)"/>
      </div>`
  };

  var CampaignDetail = {
    components: { SendConfirm: SendConfirm, Pager: Pager },
    props: {
      id: String, meta: { type: Object, default: null }, plans: { type: Array, default: function () { return []; } }
    },
    data: function () {
      return {
        data: null, loading: true, missing: false, error: "", deliveries: [], dTotal: 0, dPage: 1,
        dLimit: 20, confirming: false, busy: "", timer: null, updatedAt: "", tab: ""
      };
    },
    computed: {
      m: function () { return pushMeta(this.meta); },
      live: function () { return !!this.data && (this.data.status === "queued" || this.data.status === "processing"); },
      stats: function () { return (this.data && this.data.stats) || {}; },
      ready: function () {
        var s = this.meta && this.meta.status;
        return !s || !!(s.enabled && s.fcm);
      },
      summary: function () { return audienceSummary(this.data ? this.data.audience : {}, this.plans); },
      content: function () {
        var d = this.data;
        return {
          title: d.title || {}, body: d.body || {}, fallback_locale: d.fallback_locale, category: d.category,
          link: d.link, data: d.data
        };
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
          if (!this.tab) this.tab = this.data.fallback_locale || this.m.content_locales[0];
          this.error = "";
          await this.loadDeliveries();
          this.updatedAt = clock(new Date());
        } catch (e) {
          if (e.code === "NOT_FOUND") this.missing = true;
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
      source: function (l) { return textSource(this.content, l, this.m.content_locales); },
      shown: function (l) {
        var source = this.source(l);
        return { title: source ? this.content.title[source] : "", body: source ? this.content.body[source] : "" };
      },
      language: function (l) {
        return (this.stats.by_language || {})[l] || { total: 0, pending: 0, provider_accepted: 0, failed: 0, skipped: 0, opened: 0 };
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
        <div v-if="missing" class="card"><div class="empty">{{ t('admin.error.not_found') }}</div></div>
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
                  <div class="tabs lang-tabs" role="tablist" :aria-label="t('admin.push.form.languages')">
                    <button v-for="l in m.content_locales" :key="l" type="button" role="tab" :aria-selected="tab === l ? 'true' : 'false'"
                            :class="{ 'is-active': tab === l }" @click="tab = l">
                      {{ langName(l) }}
                      <span v-if="source(l) === l" class="tab-mark tab-mark-ok">✓</span>
                      <span v-else-if="source(l)" class="tab-mark">→ {{ langName(source(l)) }}</span>
                    </button>
                  </div>
                  <div class="push-preview push-phone" role="tabpanel">
                    <div class="push-phone-head"><span class="push-app">AI Reply</span>
                      <span v-if="source(tab) && source(tab) !== tab" class="badge badge-warn">{{ tf('admin.push.lang.uses', { lang: langName(source(tab)) }) }}</span></div>
                    <b>{{ shown(tab).title }}</b><p>{{ shown(tab).body }}</p>
                  </div>
                  <dl class="kv">
                    <dt>{{ t('admin.push.form.fallback') }}</dt><dd>{{ langName(data.fallback_locale) }}</dd>
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
                  <h3 class="subhead">{{ t('admin.push.stats.by_language') }}</h3>
                  <div class="table-wrap">
                    <table class="mini-table">
                      <thead><tr><th>{{ t('common.language') }}</th><th>{{ t('admin.push.stat.total') }}</th>
                        <th>{{ t('admin.push.stat.accepted') }}</th><th>{{ t('admin.push.stat.pending') }}</th>
                        <th>{{ t('admin.push.stat.failed') }}</th><th>{{ t('admin.push.delivery_status.skipped') }}</th>
                        <th>{{ t('admin.push.stat.opened') }}</th></tr></thead>
                      <tbody>
                        <tr v-for="l in m.languages" :key="l" :class="{ 'is-zero': !language(l).total }">
                          <td><b>{{ langName(l) }}</b>
                            <span v-if="source(l) && source(l) !== l" class="muted small"> → {{ langName(source(l)) }}</span></td>
                          <td>{{ nf(language(l).total) }}</td>
                          <td>{{ nf(language(l).provider_accepted) }}</td>
                          <td>{{ nf(language(l).pending) }}</td>
                          <td>{{ nf(language(l).failed) }}</td>
                          <td>{{ nf(language(l).skipped) }}</td>
                          <td>{{ nf(language(l).opened) }}</td>
                        </tr>
                      </tbody>
                    </table>
                  </div>
                </div>
              </div>

              <div class="card">
                <div class="card-head"><h2>{{ t('admin.push.errors.title') }}</h2></div>
                <div class="table-wrap">
                  <table>
                    <thead><tr><th>{{ t('admin.push.errors.code') }}</th><th>{{ t('admin.push.errors.count') }}</th></tr></thead>
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
                    <thead><tr><th>{{ t('admin.push.device.device') }}</th><th>{{ t('common.language') }}</th>
                      <th>{{ t('admin.push.delivery.user') }}</th><th>{{ t('admin.users.col_status') }}</th>
                      <th>{{ t('admin.push.delivery.error') }}</th><th>{{ t('admin.push.delivery.sent') }}</th>
                      <th>{{ t('admin.push.delivery.opened') }}</th></tr></thead>
                    <tbody>
                      <tr v-for="d in deliveries" :key="d.id">
                        <td class="nowrap">{{ target(d) }}
                          <div class="mono" v-if="d.platform">{{ platformName(d.platform) }} {{ d.app_version }} · {{ d.push || '—' }}</div></td>
                        <td>{{ langName(d.locale) }}</td>
                        <td><a v-if="d.user_id" class="link mono" :href="userLink(d.user_id)" @click.prevent="go(userLink(d.user_id))">{{ shortID(d.user_id) }}</a>
                          <span v-else class="muted">—</span></td>
                        <td class="nowrap"><span :class="'badge ' + deliveryBadge(d.status)">{{ t('admin.push.delivery_status.' + d.status) }}</span>
                          <div class="muted small">{{ t('admin.push.delivery.attempts') }}: {{ d.attempts }}</div></td>
                        <td class="mono">{{ d.error_code || '—' }}<div class="muted small" v-if="d.error_detail">{{ d.error_detail }}</div></td>
                        <td class="mono nowrap">{{ d.sent_at || d.failed_at || '—' }}</td>
                        <td class="mono nowrap">{{ d.opened_at || '—' }}</td>
                      </tr>
                      <tr v-if="!deliveries.length"><td colspan="7" class="empty">{{ t('admin.push.no_deliveries') }}</td></tr>
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
                    <dt>{{ t('admin.push.col.created') }}</dt><dd><span class="mono">{{ data.created_at }}</span><div class="muted small">{{ adminName(data.created_by, data.created_by_email) }}</div></dd>
                    <dt>{{ t('admin.push.time.queued') }}</dt><dd class="mono">{{ data.queued_at || '—' }}</dd>
                    <dt>{{ t('admin.push.time.started') }}</dt><dd class="mono">{{ data.started_at || '—' }}</dd>
                    <dt>{{ t('admin.push.time.completed') }}</dt><dd class="mono">{{ data.completed_at || '—' }}</dd>
                    <dt>{{ t('admin.push.time.cancelled') }}</dt><dd class="mono">{{ data.cancelled_at || '—' }}</dd>
                    <dt>{{ t('admin.push.col.recipients') }}</dt>
                    <dd><template v-if="data.started_at">{{ nf(data.recipient_count) }} / {{ nf(data.device_count) }}</template><span v-else>—</span></dd>
                  </dl>
                  <p class="hint">{{ t('admin.push.col.recipients_hint') }}</p>
                </div>
                <div class="card-foot" v-if="['draft', 'queued', 'processing'].indexOf(data.status) !== -1">
                  <button v-if="data.status === 'draft'" type="button" class="btn btn-primary" :disabled="!!busy" @click="askSend">
                    {{ t('admin.push.send') }}…</button>
                  <button type="button" class="btn btn-danger" :disabled="!!busy" @click="cancel">
                    {{ busy === 'cancel' ? t('admin.push.cancelling') : t('admin.push.cancel') }}</button>
                </div>
              </div>
            </div>
          </div>
          <send-confirm v-if="confirming" :content="content" :audience="data.audience || {}" :plans="plans" :meta="meta"
                        :busy="busy === 'send'" :blocked="!ready" @close="confirming = false" @confirm="send"/>
        </template>
      </div>`
  };

  var DeliveryList = {
    mixins: [listView({ endpoint: "/notifications/deliveries", rowsKey: "deliveries", limit: 50,
      defaults: { campaign_id: "", user_id: "", status: "", platform: "", channel: "", type: "", source: "", locale: "" } })],
    props: { meta: { type: Object, default: null } },
    data: function () { return { campaigns: [], statuses: DELIVERY_STATUSES, sources: ["campaign", "automatic"] }; },
    mounted: async function () {
      try { this.campaigns = (await api("/notifications/campaigns?limit=100")).campaigns || []; }
      catch (e) { this.campaigns = []; }
    },
    computed: {
      m: function () { return pushMeta(this.meta); },
      types: function () { return ["campaign"].concat(this.m.types); },
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
              <option value="">{{ t('common.all') }}</option>
              <option v-for="p in m.platforms" :key="p" :value="p">{{ platformName(p) }}</option>
            </select></label>
          <label><span>{{ t('admin.push.delivery.channel') }}</span>
            <select v-model="filters.channel" @change="search">
              <option value="">{{ t('common.all') }}</option>
              <option v-for="c in m.channels" :key="c" :value="c">{{ t('admin.push.channel.' + c) }}</option>
            </select></label>
          <label><span>{{ t('admin.push.delivery.source') }}</span>
            <select v-model="filters.source" @change="search">
              <option value="">{{ t('common.all') }}</option>
              <option v-for="s in sources" :key="s" :value="s">{{ t('admin.push.source.' + s) }}</option>
            </select></label>
          <label><span>{{ t('admin.push.delivery.type') }}</span>
            <select v-model="filters.type" @change="search">
              <option value="">{{ t('common.all') }}</option>
              <option v-for="k in types" :key="k" :value="k">{{ t('admin.push.type.' + k) }}</option>
            </select></label>
          <label><span>{{ t('common.language') }}</span>
            <select v-model="filters.locale" @change="search">
              <option value="">{{ t('common.all') }}</option>
              <option v-for="l in m.languages" :key="l" :value="l">{{ langName(l) }}</option>
            </select></label>
          <div class="filter-actions">
            <button type="submit" class="btn btn-sm btn-primary">{{ t('common.search') }}</button>
            <button type="button" class="btn btn-sm" @click="reset">{{ t('common.reset') }}</button>
          </div>
        </form>
        <div class="table-wrap">
          <table>
            <thead><tr><th>{{ t('admin.push.delivery.created') }}</th><th>{{ t('admin.push.delivery.notification') }}</th>
              <th>{{ t('common.language') }}</th><th>{{ t('admin.push.delivery.channel') }}</th>
              <th>{{ t('admin.push.delivery.user') }}</th><th>{{ t('admin.users.col_status') }}</th>
              <th>{{ t('admin.push.delivery.error') }}</th><th>{{ t('admin.push.delivery.sent') }}</th>
              <th>{{ t('admin.push.delivery.opened') }}</th></tr></thead>
            <tbody>
              <tr v-if="loading" v-for="n in 5" :key="'s' + n"><td colspan="9"><div class="skeleton-row"></div></td></tr>
              <tr v-else v-for="d in rows" :key="d.id">
                <td class="mono nowrap">{{ d.created_at }}</td>
                <td class="cell-wide"><a v-if="d.campaign_id" class="link" :href="'/admin/notifications/campaigns/' + d.campaign_id"
                       @click.prevent="go('/admin/notifications/campaigns/' + d.campaign_id)">{{ d.campaign_name || d.title }}</a>
                  <span v-else>{{ d.title }}</span>
                  <div class="muted small">{{ t('admin.push.category.' + d.category) }} · {{ t('admin.push.type.' + d.type) }}</div></td>
                <td>{{ langName(d.locale) }}</td>
                <td class="nowrap"><span class="badge badge-muted">{{ t('admin.push.channel.' + d.channel) }}</span>
                  <div class="small" v-if="d.channel !== 'email'">{{ target(d) }}</div>
                  <div class="mono" v-if="d.platform">{{ platformName(d.platform) }} {{ d.app_version }} · {{ d.push || '—' }}</div></td>
                <td><a v-if="d.user_id" class="link mono" :href="userLink(d.user_id)" @click.prevent="go(userLink(d.user_id))">{{ shortID(d.user_id) }}</a>
                  <span v-else class="muted">—</span></td>
                <td class="nowrap"><span :class="'badge ' + deliveryBadge(d.status)">{{ t('admin.push.delivery_status.' + d.status) }}</span>
                  <div class="muted small">{{ t('admin.push.delivery.attempts') }}: {{ d.attempts }}</div></td>
                <td class="mono">{{ d.error_code || '—' }}<div class="muted small" v-if="d.error_detail">{{ d.error_detail }}</div></td>
                <td class="mono nowrap">{{ d.sent_at || d.failed_at || '—' }}</td>
                <td class="mono nowrap">{{ d.opened_at || '—' }}</td>
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

  var DeviceList = {
    mixins: [listView({ endpoint: "/notifications/devices", rowsKey: "devices", limit: 50,
      defaults: { q: "", user_id: "", platform: "", push_status: "", auth: "", app_version: "" } })],
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
          <label><span>{{ t('admin.push.device.account') }}</span>
            <select v-model="filters.auth" @change="search">
              <option value="">{{ t('common.all') }}</option>
              <option value="authenticated">{{ t('admin.push.device.attached') }}</option>
              <option value="anonymous">{{ t('admin.push.device.anonymous') }}</option>
            </select></label>
          <label><span>{{ t('admin.push.device.app_version') }}</span>
            <input type="text" v-model.trim="filters.app_version" placeholder="1.3.2"></label>
          <div class="filter-actions">
            <button type="submit" class="btn btn-sm btn-primary">{{ t('common.search') }}</button>
            <button type="button" class="btn btn-sm" @click="reset">{{ t('common.reset') }}</button>
          </div>
        </form>
        <div class="table-wrap">
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
                <td><b>{{ d.device || '—' }}</b><div class="mono">{{ [d.device_model, d.installation_id].filter(Boolean).join(' · ') }}</div></td>
                <td>{{ platformName(d.platform) }}<div class="mono">{{ d.os || '—' }}</div></td>
                <td class="mono nowrap">{{ d.app_version || '—' }} ({{ d.app_build || '—' }})</td>
                <td>{{ d.locale ? langName(d.locale) : '—' }}</td>
                <td><span :class="'badge ' + permissionBadge(d.push.permission)">{{ t('admin.push.permission.' + d.push.permission) }}</span></td>
                <td><span :class="'badge ' + (d.push.enabled ? 'badge-ok' : 'badge-muted')">
                  {{ d.push.enabled ? t('admin.push.status.on') : t('admin.push.status.off') }}</span></td>
                <td><span :class="'badge ' + pushBadge(d.push.status)">{{ t('admin.push.push_status.' + d.push.status) }}</span>
                  <div class="mono" v-if="d.push.reason">{{ d.push.reason }}</div></td>
                <td class="mono">{{ d.push.token || '—' }}</td>
                <td class="mono nowrap">{{ d.first_seen }}</td>
                <td class="mono nowrap">{{ d.last_seen }}</td>
                <td><a v-if="d.user_id" class="link" :href="userLink(d.user_id)" @click.prevent="go(userLink(d.user_id))">{{ d.user || shortID(d.user_id) }}</a>
                  <span v-else class="muted">{{ t('admin.push.device.anonymous') }}</span></td>
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

  // Notifications — бөлім: ішкі мәзір, жіберу күйі және ішкі беттер.
  var Notifications = {
    components: {
      CampaignList: CampaignList, CampaignForm: CampaignForm, CampaignDetail: CampaignDetail,
      DeliveryList: DeliveryList, DeviceList: DeviceList
    },
    props: { route: { type: Object, required: true }, seq: { type: Number, default: 0 } },
    data: function () { return { meta: null, plans: [], loaded: false }; },
    mounted: async function () {
      var results = await Promise.allSettled([api("/notifications"), api("/plans")]);
      if (results[0].status === "fulfilled") this.meta = results[0].value;
      else toast("danger", errorText(results[0].reason));
      if (results[1].status === "fulfilled") this.plans = results[1].value.plans || [];
      this.loaded = true;
    },
    computed: {
      status: function () { return (this.meta && this.meta.status) || null; },
      // banner — push жіберу мүмкін емес кезде (сервер бәрібір PUSH_DISABLED қайтарады).
      banner: function () {
        var s = this.status;
        if (!s) return "";
        if (!s.enabled) return t("admin.push.banner.disabled");
        if (!s.fcm) return t("admin.push.banner.no_fcm");
        return "";
      },
      notes: function () {
        var s = this.status, out = [];
        if (!s) return out;
        // Only when this instance has work it would do: without FCM the banner already says why.
        if (!s.worker && ((s.enabled && s.fcm) || s.email)) out.push(t("admin.push.banner.no_worker"));
        if (!s.email) out.push(t("admin.push.banner.no_email"));
        return out;
      },
      tabs: function () {
        return [
          { key: "campaigns", path: "/admin/notifications", label: t("admin.push.tab.campaigns") },
          { key: "create", path: "/admin/notifications/new", label: t("admin.push.tab.create") },
          { key: "deliveries", path: "/admin/notifications/deliveries", label: t("admin.push.tab.deliveries") },
          { key: "devices", path: "/admin/notifications/devices", label: t("admin.push.tab.devices") }
        ];
      },
      activeTab: function () { return this.route.tab === "campaign" ? "campaigns" : this.route.tab; }
    },
    methods: Object.assign({}, helpers),
    template: `
      <div>
        <div class="subnav-bar">
          <nav class="subnav" :aria-label="t('admin.nav.notifications')">
            <a v-for="item in tabs" :key="item.key" :href="item.path" :class="{ 'is-active': activeTab === item.key }"
               :aria-current="activeTab === item.key ? 'page' : null" @click.prevent="go(item.path)">{{ item.label }}</a>
          </nav>
          <div v-if="status" class="push-status" :aria-label="t('admin.push.status.title')">
            <span :class="'badge ' + (status.enabled && status.fcm ? 'badge-ok' : 'badge-muted')">
              {{ t('admin.push.status.push') }}: {{ status.enabled && status.fcm ? t('admin.push.status.ready') : t('admin.push.status.off') }}</span>
            <span :class="'badge ' + (status.email ? 'badge-ok' : 'badge-muted')">
              {{ t('admin.push.status.email') }}: {{ status.email ? t('admin.push.status.on') : t('admin.push.status.off') }}</span>
          </div>
        </div>
        <div v-if="banner" class="notice notice-warn push-banner" role="status">
          <b>{{ banner }}</b>
          <div class="push-status">
            <span :class="'badge ' + (status.enabled ? 'badge-ok' : 'badge-muted')">{{ t('admin.push.status.sending') }}:
              {{ status.enabled ? t('admin.push.status.on') : t('admin.push.status.off') }}</span>
            <span :class="'badge ' + (status.fcm ? 'badge-ok' : 'badge-muted')">FCM (Android + iOS):
              {{ status.fcm ? t('admin.push.status.ready') : t('admin.push.status.not_configured') }}</span>
          </div>
        </div>
        <div v-for="note in notes" :key="note" class="notice notice-info" role="status">{{ note }}</div>

        <div v-if="!loaded" class="card"><div class="card-body"><div class="skeleton-row"></div></div></div>
        <campaign-form v-else-if="route.tab === 'create'" :meta="meta" :plans="plans" :key="'create' + seq"/>
        <campaign-detail v-else-if="route.tab === 'campaign'" :id="route.id" :meta="meta" :plans="plans" :key="'campaign' + route.id + seq"/>
        <delivery-list v-else-if="route.tab === 'deliveries'" :meta="meta" :key="'deliveries' + seq"/>
        <device-list v-else-if="route.tab === 'devices'" :key="'devices' + seq"/>
        <campaign-list v-else :meta="meta" :key="'campaigns' + seq"/>
      </div>`
  };

  /* --------------------------------------------------------------- shell */
  var App = {
    components: { Dashboard: Dashboard, Users: Users, UserDetail: UserDetail, Plans: Plans,
                  Audit: Audit, Settings: Settings, Notifications: Notifications },
    data: function () { return { state: state }; },
    computed: {
      title: function () {
        return {
          dashboard: t("admin.nav.dashboard"), users: t("admin.users.title"), user: t("admin.user.detail"),
          plans: t("admin.plans.title"), audit: t("admin.audit.title"),
          settings: t("admin.settings.title"), notifications: t("admin.notifications.title")
        }[state.route.name];
      }
    },
    methods: {
      t: t,
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
          <a class="item" href="/admin" :class="{ 'is-active': isActive('dashboard') }"
             :aria-current="isActive('dashboard') ? 'page' : null" @click.prevent="go('/admin')">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><rect x="3" y="3" width="7" height="7" rx="2"/><rect x="14" y="3" width="7" height="7" rx="2"/><rect x="3" y="14" width="7" height="7" rx="2"/><rect x="14" y="14" width="7" height="7" rx="2"/></svg>
            {{ t('admin.nav.dashboard') }}</a>
          <a class="item" href="/admin/users" :class="{ 'is-active': isActive('users') }"
             :aria-current="isActive('users') ? 'page' : null" @click.prevent="go('/admin/users')">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><circle cx="9" cy="8" r="3.2"/><path d="M3.5 20a5.5 5.5 0 0 1 11 0M16 11a3 3 0 1 0 0-6M17.5 20a5.5 5.5 0 0 0-2.2-4.4"/></svg>
            {{ t('admin.nav.users') }}</a>
          <a class="item" href="/admin/plans" :class="{ 'is-active': isActive('plans') }"
             :aria-current="isActive('plans') ? 'page' : null" @click.prevent="go('/admin/plans')">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><path d="M3 12V5a2 2 0 0 1 2-2h7l9 9-9 9z"/><circle cx="7.5" cy="7.5" r="1.4"/></svg>
            {{ t('admin.nav.plans') }}</a>
          <a class="item" href="/admin/notifications" :class="{ 'is-active': isActive('notifications') }"
             :aria-current="isActive('notifications') ? 'page' : null" @click.prevent="go('/admin/notifications')">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><path d="M6 9a6 6 0 1 1 12 0c0 5 2 6 2 6H4s2-1 2-6M10 20a2 2 0 0 0 4 0"/></svg>
            {{ t('admin.nav.notifications') }}</a>
          <a class="item" href="/admin/audit" :class="{ 'is-active': isActive('audit') }"
             :aria-current="isActive('audit') ? 'page' : null" @click.prevent="go('/admin/audit')">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><path d="M8 6h13M8 12h13M8 18h13M3.5 6h.01M3.5 12h.01M3.5 18h.01"/></svg>
            {{ t('admin.nav.audit') }}</a>
          <a class="item" href="/admin/settings" :class="{ 'is-active': isActive('settings') }"
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
            <dashboard v-if="state.route.name === 'dashboard'"/>
            <users v-else-if="state.route.name === 'users'"/>
            <user-detail v-else-if="state.route.name === 'user'" :id="state.route.id" :key="state.route.id"/>
            <plans v-else-if="state.route.name === 'plans'"/>
            <audit v-else-if="state.route.name === 'audit'"/>
            <settings v-else-if="state.route.name === 'settings'"/>
            <notifications v-else-if="state.route.name === 'notifications'" :route="state.route" :seq="state.routeSeq"/>
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
  app.mount("#app");
  document.getElementById("app").classList.remove("app-loading");
})();
