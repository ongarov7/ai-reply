/*
 * AI Reply пернетақтасы.
 *
 * Күй машинасы нақты қосымшамен бірдей (ReplyStage): idle → ready →
 * generating → result. Пернелер қатары әдейі «жансыз»: нұсқау мәтіндік өріске
 * жазылады, ал пернелер тек пернетақта шынымен де пернетақта екенін көрсетеді.
 */
(function () {
  "use strict";

  var SIM = window.SIM;
  var t = SIM.t;

  var ROWS = {
    ru: [
      ["й", "ц", "у", "к", "е", "н", "г", "ш", "щ", "з", "х"],
      ["ф", "ы", "в", "а", "п", "р", "о", "л", "д", "ж", "э"],
      ["я", "ч", "с", "м", "и", "т", "ь", "б", "ю"]
    ],
    kk: [
      ["й", "ц", "у", "к", "е", "н", "г", "ш", "щ", "з", "х"],
      ["ф", "ы", "в", "а", "п", "р", "о", "л", "д", "ж", "э"],
      ["ә", "і", "ң", "ғ", "ү", "ұ", "қ", "ө", "һ"]
    ],
    en: [
      ["q", "w", "e", "r", "t", "y", "u", "i", "o", "p"],
      ["a", "s", "d", "f", "g", "h", "j", "k", "l"],
      ["z", "x", "c", "v", "b", "n", "m"]
    ],
    uz: [
      ["q", "w", "e", "r", "t", "y", "u", "i", "o", "p"],
      ["a", "s", "d", "f", "g", "h", "j", "k", "l"],
      ["z", "x", "c", "v", "b", "n", "m"]
    ]
  };

  var TEMPLATES = ["friend", "client", "work", "business"];

  SIM.register("sim-keyboard", {
    props: {
      platform: { type: String, default: "ios" },
      model: { type: Object, required: true },
      voiceEnabled: { type: Boolean, default: false }
    },
    emits: ["generate", "regenerate", "insert", "clear", "mic", "open"],
    computed: {
      rows: function () { return ROWS[this.model.language] || ROWS.ru; },
      stages: function () {
        return [
          t("sim.kbd.phase_context"),
          t("sim.kbd.phase_request"),
          t("sim.kbd.phase_generating"),
          t("sim.kbd.phase_ready")
        ];
      },
      phaseLabel: function () {
        var index = Math.min(this.model.phase || 0, this.stages.length - 1);
        return this.stages[index];
      },
      phasePct: function () {
        var total = this.stages.length - 1;
        return Math.round((Math.min(this.model.phase || 0, total) / total) * 100);
      },
      templates: function () { return TEMPLATES; },
      canGenerate: function () {
        return !!this.model.copied && this.model.stage !== "generating";
      },
      voiceLabel: function () {
        var map = {
          idle: "sim.voice.idle", starting: "sim.voice.starting", listening: "sim.voice.listening",
          processing: "sim.voice.processing", done: "sim.voice.done", denied: "sim.voice.denied"
        };
        return t(map[this.model.voice.state] || "sim.voice.idle");
      }
    },
    methods: {
      templateLabel: function (id) { return t("sim.template." + id); },
      onDraftInput: function (event) { this.model.draft = event.target.innerText; }
    },
    template:
      '<div class="kbd">' +

      /* --- құралдар жолағы --- */
      '<div class="kbd-bar">' +
      '<button class="kbd-logo" @click="$emit(\'open\')" :aria-label="t(\'sim.kbd.open\')">' +
      '<span class="mark">AI</span>AI Reply</button>' +
      '<button v-for="id in templates" :key="id" class="kbd-chip"' +
      ' :class="{ \'is-on\': model.template === id }" :disabled="model.stage === \'generating\'"' +
      ' @click="model.template = id">{{ templateLabel(id) }}</button>' +
      '<span class="kbd-lang">{{ model.language.toUpperCase() }}</span>' +
      '</div>' +

      /* --- панель --- */
      '<div class="kbd-panel" v-if="model.open">' +

      '<div class="kbd-quote" v-if="model.copied">' +
      '<b>{{ t("sim.kbd.copied") }}</b>{{ model.copied }}</div>' +
      '<div class="kbd-quote" v-else><b>{{ t("sim.kbd.copied") }}</b>{{ t("sim.kbd.nothing_copied") }}</div>' +

      '<template v-if="model.stage !== \'result\'">' +
      '<textarea class="kbd-instruction" rows="2" v-model="model.instruction"' +
      ' :maxlength="model.instructionMax" :placeholder="t(\'sim.kbd.instruction_hint\')"' +
      ' :aria-label="t(\'sim.kbd.instruction_hint\')" :disabled="model.stage === \'generating\'"></textarea>' +

      '<div v-if="voiceEnabled && model.voice.state !== \'idle\'" class="kbd-stage" style="margin-top:7px">' +
      '<sim-wave :active="model.voice.state === \'listening\'"/>' +
      '<span>{{ voiceLabel }}</span></div>' +
      '<div v-if="model.voice.error" class="kbd-error" style="margin-top:7px">{{ model.voice.error }}</div>' +

      '<div class="kbd-actions">' +
      '<button v-if="voiceEnabled" class="kbd-mic" :class="{ \'is-live\': model.voice.state === \'listening\' }"' +
      ' @click="$emit(\'mic\')" :aria-label="t(\'sim.voice.button\')">' +
      '<svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2"' +
      ' stroke-linecap="round"><rect x="9" y="2.5" width="6" height="11" rx="3"/>' +
      '<path d="M5.5 11a6.5 6.5 0 0 0 13 0M12 17.5V21"/></svg></button>' +
      '<button class="app-btn" :disabled="!canGenerate" @click="$emit(\'generate\')">' +
      '<span v-if="model.stage !== \'generating\'">{{ t("sim.kbd.generate") }}</span>' +
      '<span v-else>{{ t("sim.kbd.generating") }}</span></button>' +
      '</div>' +

      '<div class="kbd-stage" v-if="model.stage === \'generating\'" style="margin-top:9px" aria-live="polite">' +
      '<span>{{ phaseLabel }}</span><span class="bar"><i :style="{ width: phasePct + \'%\' }"></i></span></div>' +
      '</template>' +

      /* --- нәтиже --- */
      '<template v-else>' +
      '<div class="kbd-draft" contenteditable="true" @input="onDraftInput"' +
      ' :aria-label="t(\'sim.kbd.draft\')">{{ model.draftInitial }}</div>' +
      '<div class="kbd-actions">' +
      '<button class="app-btn app-btn-quiet" style="flex:1" @click="$emit(\'regenerate\')">{{ t("sim.kbd.regenerate") }}</button>' +
      '<button class="app-btn" style="flex:1.4" @click="$emit(\'insert\')">{{ t("sim.kbd.insert") }}</button>' +
      '</div>' +
      '<button class="app-btn app-btn-quiet" style="margin-top:7px;min-height:34px;font-size:13px"' +
      ' @click="$emit(\'clear\')">{{ t("sim.kbd.clear") }}</button>' +
      '</template>' +

      '<div v-if="model.error" class="kbd-error" style="margin-top:9px" role="alert">{{ model.error }}</div>' +
      '</div>' +

      /* --- пернелер --- */
      '<div class="keys" aria-hidden="true">' +
      '<div class="row" v-for="(row, index) in rows" :key="index">' +
      '<span class="key" v-for="key in row" :key="key">{{ key }}</span></div>' +
      '<div class="row">' +
      '<span class="key key-dim" style="flex:1.4">123</span>' +
      '<span class="key key-dim">{{ model.language }}</span>' +
      '<span class="key key-space"> </span>' +
      '<span class="key key-dim">.</span>' +
      '<span class="key key-wide">⏎</span>' +
      '</div></div>' +

      '</div>'
  });
})();
