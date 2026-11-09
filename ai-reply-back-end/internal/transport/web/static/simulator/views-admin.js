/*
 * Бизнес жағы: әкімші панелінің демонстрациясы және тарифтер/лимиттер.
 *
 * Тізім де, графиктер де — ойдан шығарылған дерек (сервер оларды "source":
 * "demo" деп белгілейді). Нақты қолданушылар ешқашан көрсетілмейді.
 * Тариф каталогы нақты, бірақ ондағы өзгеріс тек демо қабатқа жазылады.
 */
(function () {
  "use strict";

  var SIM = window.SIM;
  var t = SIM.t;

  function loadOverview(self) {
    self.loading = true;
    return SIM.api("/admin/overview", { silent: true }).then(function (payload) {
      self.data = payload;
      self.loading = false;
      if (payload.plans) SIM.state.plans = payload.plans;
      return payload;
    }, function (err) {
      self.loading = false;
      self.error = err.message;
      throw err;
    });
  }

  /* ------------------------------------------------------- әкімші панелі */
  SIM.register("view-admin", {
    data: function () { return { data: null, loading: true, error: "", selected: null }; },
    created: function () { loadOverview(this).catch(function () { /* қате көрсетілді */ }); },
    computed: {
      metrics: function () { return (this.data && this.data.metrics) || {}; },
      users: function () { return (this.data && this.data.users) || []; },
      health: function () { return (this.data && this.data.health) || SIM.state.health || {}; },
      demoAccount: function () { return this.data && this.data.demo_account; }
    },
    methods: {
      nf: SIM.nf,
      pick: function (user) { this.selected = this.selected && this.selected.id === user.id ? null : user; }
    },
    template: `
<div class="content-wide">
  <div class="section-head">
    <h2>{{ t('sim.admin.title') }}</h2>
    <p>{{ t('sim.admin.lede') }}</p>
  </div>

  <div class="scope-note" style="margin-bottom:16px">
    <span class="pill pill-demo" style="flex:none">{{ t('sim.badge.demo') }}</span>
    <span>{{ t('sim.admin.demo_note') }}</span>
  </div>

  <div v-if="loading" class="card">{{ t('sim.common.loading') }}</div>
  <div v-else-if="error" class="card">{{ error }}</div>

  <template v-else>
    <div class="grid g4" style="margin-bottom:16px">
      <sim-stat :label="t('sim.admin.total_users')" :value="nf(metrics.total_users)" :foot="t('sim.admin.new_today') + ': ' + nf(metrics.new_today)"/>
      <sim-stat :label="t('sim.admin.requests_today')" :value="nf(metrics.requests_today)" :foot="metrics.success_rate + '% ' + t('sim.admin.success')"/>
      <sim-stat :label="t('sim.admin.tokens_month')" :value="nf(metrics.tokens_month)" :foot="'$' + metrics.estimated_cost"/>
      <sim-stat :label="t('sim.admin.latency')" :value="nf(metrics.average_latency) + ' ms'" :foot="t('sim.admin.latency_foot')"/>
    </div>

    <div class="grid g2" style="margin-bottom:16px">
      <div class="card">
        <h3 style="margin-bottom:10px">{{ t('sim.admin.generations') }}</h3>
        <sim-spark :points="metrics.generations"/>
      </div>
      <div class="card">
        <h3 style="margin-bottom:10px">{{ t('sim.admin.registrations') }}</h3>
        <sim-spark :points="metrics.registrations"/>
      </div>
    </div>

    <div class="grid g3" style="margin-bottom:16px">
      <div class="card"><h3 style="margin-bottom:12px">{{ t('sim.admin.plan_mix') }}</h3><sim-bars :points="metrics.plan_mix"/></div>
      <div class="card"><h3 style="margin-bottom:12px">{{ t('sim.admin.platform_mix') }}</h3><sim-bars :points="metrics.platform_mix"/></div>
      <div class="card"><h3 style="margin-bottom:12px">{{ t('sim.admin.language_mix') }}</h3><sim-bars :points="metrics.language_mix"/></div>
    </div>

    <h3 style="margin:22px 0 10px">{{ t('sim.admin.users') }}</h3>
    <div class="table-wrap" style="margin-bottom:16px">
      <table class="t">
        <thead><tr>
          <th>{{ t('sim.admin.user') }}</th><th>{{ t('sim.admin.plan') }}</th><th>{{ t('sim.admin.usage') }}</th>
          <th>{{ t('sim.admin.platform') }}</th><th>{{ t('sim.admin.status') }}</th><th>{{ t('sim.admin.registered') }}</th>
        </tr></thead>
        <tbody>
          <tr v-for="u in users" :key="u.id" :class="{ 'is-on': selected && selected.id === u.id }"
              @click="pick(u)" tabindex="0" @keydown.enter="pick(u)">
            <td><b>{{ u.label }}</b><br><span class="muted mono">{{ u.identifier }}</span></td>
            <td><span class="pill">{{ u.plan_code }}</span></td>
            <td style="min-width:150px">
              <div class="num" style="font-size:12.5px;margin-bottom:4px">{{ u.used_today }} / {{ u.daily_limit }}</div>
              <sim-meter :used="u.used_today" :limit="u.daily_limit"/>
            </td>
            <td class="mono">{{ u.platform }} · {{ u.app_version }}</td>
            <td><span class="pill" :class="u.sub_status === 'active' ? 'pill-ok' : (u.sub_status === 'expired' ? 'pill-bad' : 'pill-warn')">{{ u.sub_status }}</span></td>
            <td class="mono">{{ u.registered }}</td>
          </tr>
        </tbody>
      </table>
    </div>

    <div class="card" v-if="selected" style="margin-bottom:16px">
      <div class="step-head"><b>{{ selected.label }}</b>
        <span class="pill pill-demo" style="margin-left:auto">{{ t('sim.badge.demo') }}</span></div>
      <div class="grid g2">
        <div class="kv">
          <div class="row"><span>{{ t('sim.prefs.language') }}</span><b>{{ selected.locale }}</b></div>
          <div class="row"><span>{{ t('sim.prefs.tone') }}</span><b>{{ t('sim.tone.' + selected.tone) }}</b></div>
          <div class="row"><span>{{ t('sim.personalization.business') }}</span><b style="max-width:60%">{{ selected.business }}</b></div>
          <div class="row"><span>{{ t('sim.admin.tokens_month') }}</span><b class="num">{{ nf(selected.tokens_month) }}</b></div>
          <div class="row"><span>{{ t('sim.admin.last_active') }}</span><b class="mono">{{ selected.last_active }}</b></div>
        </div>
        <div>
          <div class="muted" style="font-size:12.5px;margin-bottom:8px">{{ t('sim.admin.recent') }}</div>
          <div class="kv">
            <div class="row" v-for="(e, i) in selected.events" :key="i">
              <span class="mono">{{ e.at }} · {{ e.platform }}</span>
              <b><span class="pill" :class="e.status === 'success' ? 'pill-ok' : 'pill-bad'">{{ e.error_code || e.status }}</span></b>
            </div>
          </div>
        </div>
      </div>
    </div>

    <div class="grid g2">
      <div class="card">
        <div class="step-head"><b>{{ t('sim.admin.system') }}</b>
          <span class="pill pill-real" style="margin-left:auto">{{ t('sim.badge.real') }}</span></div>
        <div class="kv">
          <div class="row"><span>{{ t('sim.health.database') }}</span>
            <b><span class="dot" :class="health.database ? 'is-ok' : 'is-bad'"></span>
               {{ health.database ? t('sim.health.connected') : t('sim.health.down') }}</b></div>
          <div class="row"><span>{{ t('sim.health.provider') }}</span>
            <b><span class="dot" :class="health.provider_configured ? 'is-ok' : 'is-bad'"></span>
               {{ health.provider_configured ? t('sim.health.configured') : t('sim.health.missing') }}</b></div>
          <div class="row"><span>{{ t('sim.health.model') }}</span><b class="mono">{{ health.model }}</b></div>
          <div class="row"><span>{{ t('sim.health.env') }}</span><b class="mono">{{ health.env }}</b></div>
          <div class="row"><span>{{ t('sim.health.payments') }}</span><b class="mono">{{ health.payment_mode }}</b></div>
        </div>
      </div>

      <div class="card" v-if="demoAccount">
        <div class="step-head"><b>{{ t('sim.admin.demo_account') }}</b>
          <span class="pill pill-real" style="margin-left:auto">{{ t('sim.badge.real') }}</span></div>
        <div class="kv" style="margin-bottom:10px">
          <div class="row"><span>{{ t('sim.usage.plan') }}</span><b>{{ demoAccount.subscription.plan.code }}</b></div>
          <div class="row"><span>{{ t('sim.usage.today') }}</span>
            <b class="num">{{ demoAccount.usage.used_today }} / {{ demoAccount.usage.daily_limit }}</b></div>
        </div>
        <sim-meter :used="demoAccount.usage.used_today" :limit="demoAccount.usage.daily_limit"/>
        <p class="muted" style="font-size:12.5px;margin-top:10px">{{ t('sim.admin.demo_account_note') }}</p>
      </div>
    </div>
  </template>
</div>`
  });

  /* ----------------------------------------------------- тариф және лимит */
  SIM.register("view-pricing", {
    data: function () { return { data: null, loading: true, error: "", edits: {}, saving: "" }; },
    created: function () {
      // Деректер мен өңдеу картасы бір қадамда тұруы керек: әйтпесе Vue
      // тарифтерді өңдеу өрістері әлі жоқ кезде салып үлгереді.
      var self = this;
      this.loading = true;
      SIM.api("/admin/overview", { silent: true }).then(function (payload) {
        self.data = payload;
        if (payload.plans) SIM.state.plans = payload.plans;
        self.seedEdits();
        self.loading = false;
      }, function (err) {
        self.loading = false;
        self.error = err.message;
      });
    },
    computed: {
      plans: function () { return (this.data && this.data.plans) || []; },
      account: function () { return SIM.state.account; },
      hasDrafts: function () {
        return this.plans.some(function (p) { return !!p.draft; });
      }
    },
    methods: {
      nf: SIM.nf,
      money: SIM.money,
      planName: SIM.planName,
      // Тегін тариф «0» емес, сөзбен оқылуы керек.
      priceLabel: function (price, plan) {
        if (!Number(price)) return t('sim.pricing.free');
        return SIM.money(price, plan.currency);
      },
      seedEdits: function () {
        var edits = {};
        this.plans.forEach(function (plan) {
          var source = plan.draft || plan;
          edits[plan.id] = {
            price: Math.round(Number(source.price) / 100),
            daily: source.daily_message_limit,
            monthly: source.monthly_message_limit,
            period: source.period_days
          };
        });
        this.edits = edits;
      },
      save: function (plan) {
        var self = this;
        var edit = this.edits[plan.id];
        this.saving = plan.id;
        SIM.api("/admin/plan-draft", {
          method: "POST",
          body: {
            plan_id: plan.id,
            price: Math.round(Number(edit.price) * 100),
            daily_message_limit: Number(edit.daily),
            monthly_message_limit: Number(edit.monthly),
            period_days: Number(edit.period)
          }
        }).then(function (payload) {
          self.data.plans = payload.plans;
          SIM.state.plans = payload.plans;
          self.seedEdits();
          self.saving = "";
          SIM.toast("ok", t('sim.pricing.saved'));
        }, function (err) {
          self.saving = "";
          SIM.toast("error", err.message);
        });
      },
      resetAll: function () {
        var self = this;
        SIM.api("/admin/plan-draft/reset", { method: "POST", body: {} }).then(function (payload) {
          self.data.plans = payload.plans;
          SIM.state.plans = payload.plans;
          self.seedEdits();
          SIM.toast("ok", t('sim.pricing.reset_done'));
        }, function (err) { SIM.toast("error", err.message); });
      },
      applyToDemoAccount: function (plan) {
        SIM.api("/account/plan", { method: "POST", body: { plan_id: plan.id } })
          .then(function (account) { SIM.state.account = account; SIM.toast("ok", t('sim.usage.plan_changed')); },
            function (err) { SIM.toast("error", err.message); });
      }
    },
    template: `
<div class="content-wide">
  <div class="section-head">
    <h2>{{ t('sim.pricing.title') }}</h2>
    <p>{{ t('sim.pricing.lede') }}</p>
  </div>

  <div class="scope-note" style="margin-bottom:16px">
    <span class="pill pill-demo" style="flex:none">{{ t('sim.pricing.scope_badge') }}</span>
    <span>{{ t('sim.pricing.scope_note') }}</span>
  </div>

  <div v-if="loading" class="card">{{ t('sim.common.loading') }}</div>
  <div v-else-if="error" class="card">{{ error }}</div>

  <template v-else>
    <div style="display:flex;justify-content:flex-end;margin-bottom:12px" v-if="hasDrafts">
      <button class="btn btn-sm" @click="resetAll">{{ t('sim.pricing.reset') }}</button>
    </div>

    <div class="grid g3" style="margin-bottom:20px">
      <div class="plan-card" v-for="plan in plans" :key="plan.id" :class="{ 'is-draft': !!plan.draft }">
        <div class="head">
          <b style="font-size:16px">{{ planName(plan) }}</b>
          <span class="pill" v-if="plan.is_free">free</span>
          <span class="pill pill-demo" style="margin-left:auto" v-if="plan.draft">{{ t('sim.badge.demo') }}</span>
        </div>
        <div class="price">
          {{ priceLabel(plan.draft ? plan.draft.price : plan.price, plan) }}
          <s v-if="plan.draft">{{ priceLabel(plan.price, plan) }}</s>
        </div>
        <div class="muted" style="font-size:13px;margin-top:-6px" v-if="(plan.draft ? plan.draft.daily_message_limit : plan.daily_message_limit) > 0">
          {{ (plan.draft ? plan.draft.daily_message_limit : plan.daily_message_limit) }} {{ t('sim.pricing.per_day') }}
        </div>

        <div class="grid g2" style="gap:9px" v-if="edits[plan.id]">
          <div class="mini-field"><label>{{ t('sim.pricing.price') }}</label>
            <input type="number" min="0" v-model.number="edits[plan.id].price"></div>
          <div class="mini-field"><label>{{ t('sim.pricing.daily') }}</label>
            <input type="number" min="0" v-model.number="edits[plan.id].daily"></div>
          <div class="mini-field"><label>{{ t('sim.pricing.monthly') }}</label>
            <input type="number" min="0" v-model.number="edits[plan.id].monthly"></div>
          <div class="mini-field"><label>{{ t('sim.pricing.period') }}</label>
            <input type="number" min="0" v-model.number="edits[plan.id].period"></div>
        </div>

        <div style="display:flex;gap:7px;margin-top:auto">
          <button class="btn btn-sm btn-brand" style="flex:1" :disabled="saving === plan.id || !edits[plan.id]" @click="save(plan)">
            {{ saving === plan.id ? t('sim.action.saving') : t('sim.pricing.save') }}</button>
          <button class="btn btn-sm" @click="applyToDemoAccount(plan)">{{ t('sim.pricing.apply') }}</button>
        </div>
      </div>
    </div>

    <div class="grid g2">
      <div class="card">
        <div class="step-head"><b>{{ t('sim.pricing.limits_title') }}</b>
          <span class="pill pill-real" style="margin-left:auto">{{ t('sim.badge.real') }}</span></div>
        <div class="step-body"><p style="margin:0 0 12px">{{ t('sim.pricing.limits_body') }}</p></div>
        <template v-if="account">
          <div class="kv" style="margin-bottom:9px">
            <div class="row"><span>{{ t('sim.usage.today') }}</span>
              <b class="num">{{ account.usage.used_today }} / {{ account.usage.daily_limit }}</b></div>
          </div>
          <sim-meter :used="account.usage.used_today" :limit="account.usage.daily_limit"/>
          <p class="muted" style="font-size:12.5px;margin-top:10px">
            {{ t('sim.usage.resets') }} {{ account.usage.timezone }}</p>
        </template>
      </div>

      <div class="card">
        <div class="step-head"><b>{{ t('sim.pricing.enforce_title') }}</b></div>
        <div class="step-body">
          <p style="margin:0 0 10px">{{ t('sim.pricing.enforce_body') }}</p>
          <div class="kbd-error">{{ t('sim.error.daily_limit') }}</div>
        </div>
      </div>
    </div>
  </template>
</div>`
  });
})();
