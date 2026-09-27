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
    toasts: [],
    sidebarOpen: false
  });

  function t(key) {
    var bucket = boot.i18n[state.locale] || boot.i18n.en || {};
    return bucket[key] || (boot.i18n.en || {})[key] || key;
  }

  function parseRoute(path) {
    var clean = path.replace(/\/+$/, "") || "/admin";
    var parts = clean.split("/").filter(Boolean); // ["admin", ...]
    if (parts.length <= 1) return { name: "dashboard" };
    if (parts[1] === "users") return parts[2] ? { name: "user", id: parts[2] } : { name: "users" };
    if (parts[1] === "plans") return { name: "plans" };
    if (parts[1] === "audit") return { name: "audit" };
    if (parts[1] === "settings") return { name: "settings" };
    if (parts[1] === "notifications") return { name: "notifications" };
    return { name: "dashboard" };
  }

  function navigate(path) {
    history.pushState({}, "", path);
    state.route = parseRoute(path);
    state.sidebarOpen = false;
    window.scrollTo({ top: 0 });
  }
  window.addEventListener("popstate", function () { state.route = parseRoute(location.pathname); });

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
    var response = await fetch("/api/v1/admin" + path, init);
    if (response.status === 401) { location.href = "/admin/login"; throw new Error("unauthorized"); }
    var payload = null;
    try { payload = await response.json(); } catch (e) { payload = null; }
    if (!response.ok) {
      var code = payload && payload.error ? payload.error.code : "ERROR";
      throw new Error(code);
    }
    return payload;
  }

  /* ----------------------------------------------------------- formatting */
  function nf(value) {
    if (value === null || value === undefined) return "—";
    return new Intl.NumberFormat(state.locale === "kk" ? "kk-KZ" : state.locale).format(value);
  }
  function money(value) { return "$" + (Math.round(value * 100) / 100).toFixed(2); }

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
        return Object.keys(row.metadata).map(function (k) { return k + "=" + row.metadata[k]; }).join(" ");
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

  var Notifications = {
    data: function () { return { data: null }; },
    mounted: async function () {
      try { this.data = await api("/notifications"); } catch (e) { toast("danger", t("common.error")); }
    },
    methods: { t: t },
    template: `
      <div class="card" v-if="data">
        <div class="card-head"><h2>{{ t('admin.notifications.title') }}</h2></div>
        <div class="card-body">
          <div class="notice notice-warn">{{ t('admin.notifications.unavailable') }}</div>
          <dl class="kv">
            <dt>APNs (iOS)</dt><dd><span :class="'badge ' + (data.apns ? 'badge-ok' : 'badge-muted')">
              {{ data.apns ? 'ready' : 'not configured' }}</span></dd>
            <dt>FCM (Android)</dt><dd><span :class="'badge ' + (data.fcm ? 'badge-ok' : 'badge-muted')">
              {{ data.fcm ? 'ready' : 'not configured' }}</span></dd>
          </dl>
          <button class="btn" disabled>{{ t('admin.notifications.title') }}</button>
        </div>
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
          <a class="item" :class="{ 'is-active': isActive('dashboard') }" @click="go('/admin')">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="3" y="3" width="7" height="7" rx="2"/><rect x="14" y="3" width="7" height="7" rx="2"/><rect x="3" y="14" width="7" height="7" rx="2"/><rect x="14" y="14" width="7" height="7" rx="2"/></svg>
            {{ t('admin.nav.dashboard') }}</a>
          <a class="item" :class="{ 'is-active': isActive('users') }" @click="go('/admin/users')">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="9" cy="8" r="3.2"/><path d="M3.5 20a5.5 5.5 0 0 1 11 0M16 11a3 3 0 1 0 0-6M17.5 20a5.5 5.5 0 0 0-2.2-4.4"/></svg>
            {{ t('admin.nav.users') }}</a>
          <a class="item" :class="{ 'is-active': isActive('plans') }" @click="go('/admin/plans')">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M3 12V5a2 2 0 0 1 2-2h7l9 9-9 9z"/><circle cx="7.5" cy="7.5" r="1.4"/></svg>
            {{ t('admin.nav.plans') }}</a>
          <a class="item" :class="{ 'is-active': isActive('notifications') }" @click="go('/admin/notifications')">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M6 9a6 6 0 1 1 12 0c0 5 2 6 2 6H4s2-1 2-6M10 20a2 2 0 0 0 4 0"/></svg>
            {{ t('admin.nav.notifications') }}</a>
          <a class="item" :class="{ 'is-active': isActive('audit') }" @click="go('/admin/audit')">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M8 6h13M8 12h13M8 18h13M3.5 6h.01M3.5 12h.01M3.5 18h.01"/></svg>
            {{ t('admin.nav.audit') }}</a>
          <a class="item" :class="{ 'is-active': isActive('settings') }" @click="go('/admin/settings')">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="3"/><path d="M12 2v3M12 19v3M2 12h3M19 12h3M5 5l2 2M17 17l2 2M19 5l-2 2M7 17l-2 2"/></svg>
            {{ t('admin.nav.settings') }}</a>
          <div class="spacer"></div>
          <div class="env">{{ t('admin.settings.env') }}: <b>{{ state.env }}</b><br>
            {{ t('admin.settings.timezone') }}: <b>{{ state.timezone }}</b></div>
        </aside>

        <div class="main">
          <header class="topbar">
            <button class="burger" @click="state.sidebarOpen = !state.sidebarOpen">☰</button>
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
            <notifications v-else-if="state.route.name === 'notifications'"/>
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
  app.mount("#app");
  document.getElementById("app").classList.remove("app-loading");
})();
