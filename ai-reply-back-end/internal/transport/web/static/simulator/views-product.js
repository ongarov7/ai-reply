/*
 * Өнім экрандары: шолу, iOS, Android, пернетақта, дербестендіру.
 */
(function () {
  "use strict";

  var SIM = window.SIM;
  var t = SIM.t;

  /* ------------------------------------------------- қадам жанындағы панель */
  SIM.register("sim-step-panel", {
    props: { flow: { type: Object, default: function () { return { steps: [], index: 0 }; } }, platform: String },
    emits: ["go", "next", "back", "restart"],
    computed: {
      state: function () { return SIM.state; },
      usage: function () {
        return (SIM.state.account && SIM.state.account.usage) || { used_today: 0, daily_limit: 0, remaining_today: 0 };
      },
      plan: function () {
        var sub = SIM.state.account && SIM.state.account.subscription;
        return sub ? sub.plan : null;
      },
      name: function () { return this.flow.name || "welcome"; },
      isReal: function () { return this.name === "preferences" || this.name === "keyboard" || this.name === "voice"; }
    },
    methods: {
      planName: SIM.planName,
      nf: SIM.nf,
      label: function (step) { return t("sim.step." + step + "_nav"); },
      setPlan: function (event) {
        SIM.api("/account/plan", { method: "POST", body: { plan_id: event.target.value } })
          .then(function (account) { SIM.state.account = account; SIM.toast("ok", t("sim.usage.plan_changed")); },
            function (err) { SIM.toast("error", err.message); });
      },
      resetQuota: function () {
        SIM.api("/account/reset-quota", { method: "POST", body: {} })
          .then(function (account) { SIM.state.account = account; SIM.toast("ok", t("sim.usage.quota_reset")); },
            function (err) { SIM.toast("error", err.message); });
      }
    },
    template: `
<div class="panel-stack">
  <div class="card">
    <div class="step-head">
      <span class="num">{{ (flow.index || 0) + 1 }}</span>
      <b>{{ t('sim.step.' + name + '_nav') }}</b>
    </div>
    <div class="step-body">
      <p style="margin:0 0 10px">{{ t('sim.step.' + name + '_panel') }}</p>
      <div style="display:flex;gap:6px;flex-wrap:wrap">
        <span class="pill" :class="isReal ? 'pill-real' : ''">{{ isReal ? t('sim.badge.real_api') : t('sim.badge.ui_only') }}</span>
        <span class="pill mono" v-if="t('sim.step.' + name + '_endpoint') !== 'sim.step.' + name + '_endpoint'">
          {{ t('sim.step.' + name + '_endpoint') }}</span>
      </div>
    </div>
    <div class="step-nav">
      <button class="btn btn-sm" @click="$emit('back')" :disabled="(flow.index || 0) === 0">{{ t('sim.action.back') }}</button>
      <button class="btn btn-sm btn-brand" @click="$emit('next')"
              :disabled="(flow.index || 0) >= (flow.total || 1) - 1">{{ t('sim.action.next') }}</button>
      <button class="btn btn-sm btn-ghost" @click="$emit('restart')">{{ t('sim.action.restart') }}</button>
      <span class="count">{{ (flow.index || 0) + 1 }} / {{ flow.total || 1 }}</span>
    </div>
    <div class="steps-rail">
      <button v-for="(step, i) in flow.steps" :key="step" class="rail-item"
              :class="{ 'is-on': i === flow.index, 'is-done': i < flow.index }" @click="$emit('go', i)">
        <span class="tick">{{ i < flow.index ? '✓' : '' }}</span>{{ label(step) }}
      </button>
    </div>
  </div>

  <div class="card">
    <div class="step-head"><b>{{ t('sim.usage.title') }}</b></div>
    <div class="kv" style="margin-bottom:10px">
      <div class="row"><span>{{ t('sim.usage.today') }}</span>
        <b>{{ nf(usage.used_today) }} / {{ nf(usage.daily_limit) }}</b></div>
    </div>
    <sim-meter :used="usage.used_today" :limit="usage.daily_limit"/>
    <p class="muted" style="font-size:12.5px;margin:9px 0 12px">{{ t('sim.usage.explain') }}</p>
    <div class="kv">
      <div class="row"><span>{{ t('sim.usage.plan') }}</span>
        <select class="sel" style="max-width:158px" :value="plan ? plan.id : ''" @change="setPlan"
                :aria-label="t('sim.usage.plan')">
          <option v-for="p in state.plans" :key="p.id" :value="p.id">{{ planName(p) }}{{ p.daily_message_limit > 0 ? ' · ' + p.daily_message_limit + '/' + t('sim.usage.day') : '' }}</option>
        </select></div>
    </div>
    <button class="btn btn-sm" style="margin-top:10px;width:100%" @click="resetQuota">{{ t('sim.usage.reset') }}</button>
  </div>

  <sim-inspector/>
</div>`
  });

  /* --------------------------------------------------------- құрылғы беті */
  var deviceView = {
    props: { platform: String, startAt: { type: String, default: "" } },
    data: function () { return { flow: { name: "welcome", index: 0, total: 8, steps: [] } }; },
    computed: { state: function () { return SIM.state; } },
    methods: {
      onStep: function (flow) { this.flow = flow; },
      go: function (index) { this.$refs.phone.go(index); },
      next: function () { this.$refs.phone.next(); },
      back: function () { this.$refs.phone.back(); },
      restart: function () { this.$refs.phone.restart(); }
    },
    template: `
<div class="content-wide">
  <div class="section-head">
    <h2>{{ platform === 'ios' ? t('sim.ios.title') : t('sim.android.title') }}</h2>
    <p>{{ platform === 'ios' ? t('sim.ios.lede') : t('sim.android.lede') }}</p>
  </div>
  <div class="stage-layout">
    <sim-stage :platform="platform" :label="t('sim.a11y.device')" :badge="t('sim.step.' + (flow.name || 'welcome') + '_nav')">
      <template #tools>
        <button class="btn btn-sm btn-ghost" style="color:#c9d2ec" @click="restart">{{ t('sim.action.restart') }}</button>
      </template>
      <sim-phone ref="phone" :platform="platform" :start-at="startAt" @step="onStep"/>
    </sim-stage>
    <sim-step-panel :flow="flow" :platform="platform" @go="go" @next="next" @back="back" @restart="restart"/>
  </div>
</div>`
  };

  // Бір ғана компонент, екі бет: платформа маршруттан келеді.
  SIM.register("sim-device-view", deviceView);

  /* ------------------------------------------------------------- шолу */
  SIM.register("view-overview", {
    computed: {
      state: function () { return SIM.state; },
      health: function () { return SIM.state.health || {}; }
    },
    methods: {
      go: function (path) { SIM.go(path); },
      planName: SIM.planName,
      money: SIM.money
    },
    template: `
<div class="content-wide">
  <div class="card card-pad-lg" style="background:var(--stage);border:none;color:var(--stage-ink);margin-bottom:18px">
    <span class="pill" style="background:rgba(255,255,255,.08);border-color:rgba(255,255,255,.14);color:#e8ecf8">
      {{ t('sim.overview.badge') }}</span>
    <h2 style="font-size:30px;margin:14px 0 10px;max-width:22ch;font-weight:660">{{ t('sim.overview.title') }}</h2>
    <p style="color:var(--stage-muted);max-width:62ch;margin:0 0 20px;font-size:15.5px">{{ t('sim.overview.lede') }}</p>
    <div style="display:flex;gap:9px;flex-wrap:wrap">
      <button class="btn btn-brand" @click="go('/simulator/ios')">{{ t('sim.overview.cta_ios') }}</button>
      <button class="btn" style="background:rgba(255,255,255,.08);border-color:rgba(255,255,255,.16);color:#e8ecf8"
              @click="go('/simulator/android')">{{ t('sim.overview.cta_android') }}</button>
      <button class="btn btn-ghost" style="color:#98a2bf" @click="go('/simulator/workflow')">{{ t('sim.overview.cta_flow') }}</button>
    </div>
    <div class="flow-strip" style="margin-top:22px">
      <template v-for="(key, i) in ['message','keyboard','instruction','generate','insert','send']" :key="key">
        <span class="flow-chip" style="background:rgba(255,255,255,.07);border-color:rgba(255,255,255,.14);color:#e8ecf8;box-shadow:none">
          {{ t('sim.flow.' + key) }}</span>
        <span class="flow-arrow" v-if="i < 5" style="color:#5f6b92">→</span>
      </template>
    </div>
  </div>

  <div class="grid g3" style="margin-bottom:18px">
    <div class="card"><h3>{{ t('sim.overview.p1_title') }}</h3><p>{{ t('sim.overview.p1_body') }}</p></div>
    <div class="card"><h3>{{ t('sim.overview.p2_title') }}</h3><p>{{ t('sim.overview.p2_body') }}</p></div>
    <div class="card"><h3>{{ t('sim.overview.p3_title') }}</h3><p>{{ t('sim.overview.p3_body') }}</p></div>
  </div>

  <div class="grid g2">
    <div class="card">
      <h3 style="margin-bottom:12px">{{ t('sim.overview.real_title') }}</h3>
      <div class="kv">
        <div class="row"><span>{{ t('sim.overview.real_generate') }}</span><b><span class="pill pill-real">{{ t('sim.badge.real') }}</span></b></div>
        <div class="row"><span>{{ t('sim.overview.real_profile') }}</span><b><span class="pill pill-real">{{ t('sim.badge.real') }}</span></b></div>
        <div class="row"><span>{{ t('sim.overview.real_limits') }}</span><b><span class="pill pill-real">{{ t('sim.badge.real') }}</span></b></div>
        <div class="row"><span>{{ t('sim.overview.real_plans') }}</span><b><span class="pill pill-real">{{ t('sim.badge.real') }}</span></b></div>
        <div class="row"><span>{{ t('sim.overview.demo_users') }}</span><b><span class="pill pill-demo">{{ t('sim.badge.demo') }}</span></b></div>
        <div class="row"><span>{{ t('sim.overview.demo_metrics') }}</span><b><span class="pill pill-demo">{{ t('sim.badge.demo') }}</span></b></div>
        <div class="row"><span>{{ t('sim.overview.demo_pricing') }}</span><b><span class="pill pill-demo">{{ t('sim.badge.demo') }}</span></b></div>
      </div>
    </div>

    <div class="card">
      <h3 style="margin-bottom:12px">{{ t('sim.overview.backend_title') }}</h3>
      <div class="kv">
        <div class="row"><span>{{ t('sim.health.database') }}</span>
          <b><span class="dot" :class="health.database ? 'is-ok' : 'is-bad'"></span>
             {{ health.database ? t('sim.health.connected') : t('sim.health.down') }}</b></div>
        <div class="row"><span>{{ t('sim.health.provider') }}</span>
          <b><span class="dot" :class="health.provider_configured ? 'is-ok' : 'is-bad'"></span>
             {{ health.provider_configured ? t('sim.health.configured') : t('sim.health.missing') }}</b></div>
        <div class="row"><span>{{ t('sim.health.model') }}</span><b class="mono">{{ health.model }}</b></div>
        <div class="row"><span>{{ t('sim.health.env') }}</span><b class="mono">{{ health.env }}</b></div>
        <div class="row"><span>{{ t('sim.health.timezone') }}</span><b class="mono">{{ health.timezone }}</b></div>
        <div class="row"><span>{{ t('sim.health.rate') }}</span><b class="num">{{ health.limits && health.limits.ai_per_minute }}/min</b></div>
      </div>
      <p class="muted" style="font-size:12.5px;margin-top:12px">{{ t('sim.health.note') }}</p>
    </div>
  </div>
</div>`
  });

  /* ------------------------------------------------------ пернетақта беті */
  SIM.register("view-keyboard", {
    data: function () { return { platform: "ios", flow: { name: "keyboard", index: 6, total: 8, steps: [] } }; },
    methods: {
      onStep: function (flow) { this.flow = flow; },
      go: function (i) { this.$refs.phone.go(i); },
      next: function () { this.$refs.phone.next(); },
      back: function () { this.$refs.phone.back(); },
      restart: function () { this.$refs.phone.restart(); }
    },
    template: `
<div class="content-wide">
  <div class="section-head">
    <h2>{{ t('sim.keyboard.title') }}</h2>
    <p>{{ t('sim.keyboard.lede') }}</p>
  </div>
  <div class="stage-layout">
    <sim-stage :platform="platform" :label="t('sim.a11y.device')" :badge="t('sim.nav.keyboard')">
      <template #tools>
        <span style="display:inline-flex;gap:4px">
          <button class="btn btn-sm" :class="platform === 'ios' ? 'btn-brand' : 'btn-ghost'"
                  :style="platform === 'ios' ? '' : 'color:#c9d2ec'" @click="platform = 'ios'">iOS</button>
          <button class="btn btn-sm" :class="platform === 'android' ? 'btn-brand' : 'btn-ghost'"
                  :style="platform === 'android' ? '' : 'color:#c9d2ec'" @click="platform = 'android'">Android</button>
        </span>
      </template>
      <sim-phone ref="phone" :key="platform" :platform="platform" start-at="messenger" @step="onStep"/>
    </sim-stage>

    <div class="panel-stack">
      <div class="card">
        <div class="step-head"><b>{{ t('sim.keyboard.anatomy') }}</b></div>
        <div class="step-body">
          <ul style="padding-left:17px;margin:0">
            <li v-for="key in ['bar','templates','quote','instruction','mic','generate','draft','insert']" :key="key">
              <b style="color:var(--ink)">{{ t('sim.keyboard.a_' + key) }}</b> — {{ t('sim.keyboard.b_' + key) }}
            </li>
          </ul>
        </div>
      </div>
      <div class="card">
        <div class="step-head"><b>{{ t('sim.keyboard.limits_title') }}</b></div>
        <div class="step-body"><p style="margin:0">{{ t('sim.keyboard.limits_body') }}</p></div>
      </div>
      <sim-inspector/>
    </div>
  </div>
</div>`
  });

  /* ---------------------------------------------------- дербестендіру беті */
  var PROFILE_A = {
    key: "a",
    preferred_tone: "friendly",
    role: "retail",
    business_offering: "Premium cosmetics",
    business_summary: "",
    description: ""
  };
  var PROFILE_B = {
    key: "b",
    preferred_tone: "formal",
    role: "business",
    business_offering: "Legal consulting",
    business_summary: "",
    description: ""
  };

  SIM.register("view-personalization", {
    data: function () {
      return {
        question: "",
        instruction: "",
        language: "ru",
        busy: false,
        results: { a: "", b: "" },
        errors: { a: "", b: "" }
      };
    },
    created: function () {
      this.question = t("sim.demo.incoming");
      this.instruction = t("sim.personalization.instruction");
      this.language = SIM.state.locale === "kk" ? "kk" : (SIM.state.locale === "en" ? "en" : "ru");
    },
    computed: {
      profiles: function () {
        return [
          Object.assign({}, PROFILE_A, {
            title: t("sim.personalization.a_title"),
            business_summary: t("sim.personalization.a_business"),
            description: t("sim.personalization.a_business")
          }),
          Object.assign({}, PROFILE_B, {
            title: t("sim.personalization.b_title"),
            business_summary: t("sim.personalization.b_business"),
            description: t("sim.personalization.b_business")
          })
        ];
      }
    },
    methods: {
      run: function () {
        var self = this;
        this.busy = true;
        this.results = { a: "", b: "" };
        this.errors = { a: "", b: "" };
        var calls = this.profiles.map(function (profile) {
          return SIM.api("/generate", {
            method: "POST",
            body: {
              source_text: self.question,
              instruction: self.instruction,
              language: self.language,
              template_id: profile.role === "business" ? "business" : "client",
              platform: "web",
              override: {
                preferred_tone: profile.preferred_tone,
                role: profile.role,
                business_offering: profile.business_offering,
                business_summary: profile.business_summary,
                description: profile.description
              }
            }
          }).then(function (payload) {
            self.results[profile.key] = payload.reply;
            if (payload.usage && SIM.state.account) SIM.state.account.usage = payload.usage;
          }, function (err) {
            self.errors[profile.key] = err.message;
          });
        });
        Promise.all(calls).then(function () { self.busy = false; });
      }
    },
    template: `
<div class="content-wide">
  <div class="section-head">
    <h2>{{ t('sim.personalization.title') }}</h2>
    <p>{{ t('sim.personalization.lede') }}</p>
  </div>

  <div class="card" style="margin-bottom:16px">
    <div class="grid g2">
      <div>
        <div class="mini-field" style="margin-bottom:10px">
          <label>{{ t('sim.personalization.question') }}</label>
          <textarea class="app-input" rows="3" v-model="question" maxlength="300"></textarea>
        </div>
      </div>
      <div>
        <div class="mini-field" style="margin-bottom:10px">
          <label>{{ t('sim.personalization.instruction_label') }}</label>
          <textarea class="app-input" rows="3" v-model="instruction" maxlength="400"></textarea>
        </div>
      </div>
    </div>
    <button class="btn btn-brand" :disabled="busy || !question" @click="run">
      {{ busy ? t('sim.kbd.generating') : t('sim.personalization.run') }}
    </button>
    <span class="muted" style="margin-left:10px;font-size:13px">{{ t('sim.personalization.cost') }}</span>
  </div>

  <div class="grid g2">
    <div class="card" v-for="p in profiles" :key="p.key">
      <div class="step-head"><span class="num">{{ p.key.toUpperCase() }}</span><b>{{ p.title }}</b></div>
      <div class="kv" style="margin-bottom:12px">
        <div class="row"><span>{{ t('sim.personalization.business') }}</span><b>{{ p.business_offering }}</b></div>
        <div class="row"><span>{{ t('sim.prefs.tone') }}</span><b>{{ t('sim.tone.' + p.preferred_tone) }}</b></div>
        <div class="row"><span>{{ t('sim.prefs.audience') }}</span><b>{{ t('sim.prefs.audience_' + p.role) }}</b></div>
      </div>
      <div class="kbd-draft" v-if="results[p.key]">{{ results[p.key] }}</div>
      <div class="kbd-error" v-else-if="errors[p.key]">{{ errors[p.key] }}</div>
      <div class="muted" v-else style="font-size:13.5px">{{ t('sim.personalization.empty') }}</div>
    </div>
  </div>

  <div class="card" style="margin-top:16px">
    <p style="margin:0" class="muted">{{ t('sim.personalization.note') }}</p>
  </div>
</div>`
  });
})();
