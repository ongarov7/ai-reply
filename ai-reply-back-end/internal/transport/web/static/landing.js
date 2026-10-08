/*
 * Лендингтің және көпшілік беттердің интерактив бөлігі. Фреймворксіз: мазмұн
 * серверде рендерленген, бұл скрипт оны тек жандандырады.
 *
 * WHY NO FRAMEWORK HERE. The page is server-rendered for search engines and for
 * the first paint, and everything below is three small enhancements. Shipping a
 * runtime to a marketing page would cost every visitor a download, and its
 * template compiler would need 'unsafe-eval' in the Content-Security-Policy —
 * a real weakening of a public page in exchange for nothing. The admin panel,
 * which is behind a session and genuinely interactive, keeps Vue.
 */
(function () {
  "use strict";

  document.documentElement.classList.add("js");

  var data = readJSON("landing-data");

  function readJSON(id) {
    var node = document.getElementById(id);
    if (!node) return null;
    try { return JSON.parse(node.textContent); } catch (error) { return null; }
  }

  /* ------------------------------------------------- keyboard demo */
  var phone = document.getElementById("hero-demo");
  if (phone && data && data.scenes && data.scenes.length) {
    startDemo(phone, data.scenes);
  }

  function startDemo(root, scenes) {
    var fields = {
      incoming: root.querySelector('[data-field="incoming"]'),
      instruction: root.querySelector('[data-field="instruction"]'),
      reply: root.querySelector('[data-field="reply"]')
    };
    var rows = {
      incoming: root.querySelector('[data-stage="1"]'),
      instruction: root.querySelector('[data-stage="2"]'),
      reply: root.querySelector('[data-stage="3"]')
    };
    var dotsHost = root.querySelector("[data-dots]");
    var index = 0, timer = null, runId = 0;

    // Motion is an enhancement, never a requirement: a visitor who asked the
    // system not to animate still sees the whole conversation, just at rest.
    var reduced = window.matchMedia && window.matchMedia("(prefers-reduced-motion: reduce)").matches;

    var dots = scenes.map(function (_, i) {
      var button = document.createElement("button");
      button.type = "button";
      button.setAttribute("aria-label", "demo " + (i + 1));
      button.addEventListener("click", function () { play(i, true); });
      dotsHost.appendChild(button);
      return button;
    });

    function wait(ms) {
      return new Promise(function (resolve) { timer = setTimeout(resolve, ms); });
    }

    function type(node, text, speed, run) {
      node.textContent = "";
      node.classList.add("is-typing");
      return new Promise(function (resolve) {
        var i = 0;
        (function step() {
          if (run !== runId) return resolve();
          node.textContent = text.slice(0, i);
          i += 2;
          if (i <= text.length + 2) {
            timer = setTimeout(step, speed);
          } else {
            node.textContent = text;
            node.classList.remove("is-typing");
            resolve();
          }
        })();
      });
    }

    function markDots() {
      dots.forEach(function (dot, i) { dot.classList.toggle("is-active", i === index); });
    }

    function reset() {
      Object.keys(rows).forEach(function (key) { rows[key].classList.remove("is-on"); });
      fields.reply.classList.remove("is-thinking");
      fields.reply.textContent = "";
    }

    // Every run carries an id. A dot tapped mid-animation bumps it, and the
    // steps of the previous run return instead of writing into the DOM the new
    // one is already using — no overlapping typing, no stray timers.
    async function play(next, manual) {
      clearTimeout(timer);
      runId += 1;
      var run = runId;

      index = typeof next === "number" ? next : index;
      var scene = scenes[index];
      markDots();

      if (reduced) {
        // Still show the finished conversation, just without the typing.
        Object.keys(rows).forEach(function (key) { rows[key].classList.add("is-on"); });
        fields.incoming.textContent = scene.incoming;
        fields.instruction.textContent = scene.instruction;
        fields.reply.textContent = scene.reply;
        return;
      }

      reset();
      await wait(manual ? 150 : 400);
      if (run !== runId) return;

      rows.incoming.classList.add("is-on");
      await type(fields.incoming, scene.incoming, 16, run);
      if (run !== runId) return;
      await wait(600);

      rows.instruction.classList.add("is-on");
      await type(fields.instruction, scene.instruction, 18, run);
      if (run !== runId) return;
      await wait(450);

      rows.reply.classList.add("is-on");
      fields.reply.classList.add("is-thinking");
      fields.reply.innerHTML = '<span class="typing"><i></i><i></i><i></i></span>';
      await wait(900);
      if (run !== runId) return;
      fields.reply.classList.remove("is-thinking");
      await type(fields.reply, scene.reply, 14, run);
      if (run !== runId) return;

      await wait(2800);
      if (run !== runId) return;
      index = (index + 1) % scenes.length;
      play(index, false);
    }

    play(0, false);
  }

  /* ------------------------------------------------- counters */
  var counters = document.querySelectorAll("[data-count]");
  if (counters.length && "IntersectionObserver" in window) {
    var observer = new IntersectionObserver(function (entries) {
      entries.forEach(function (entry) {
        if (!entry.isIntersecting) return;
        observer.unobserve(entry.target);
        countUp(entry.target);
      });
    }, { threshold: 0.4 });
    counters.forEach(function (node) { observer.observe(node); });
  }

  function countUp(node) {
    var target = parseInt(node.getAttribute("data-count"), 10) || 0;
    var suffix = node.getAttribute("data-suffix") || "";
    if (target === 0) { node.textContent = "0" + suffix; return; }

    var started = performance.now(), duration = 1100;
    (function step(now) {
      var progress = Math.min((now - started) / duration, 1);
      node.textContent = Math.round(target * (1 - Math.pow(1 - progress, 3))) + suffix;
      if (progress < 1) requestAnimationFrame(step);
    })(started);
  }

  /* ------------------------------------------------- reveal on scroll */
  var sections = document.querySelectorAll(".reveal");
  if (sections.length && "IntersectionObserver" in window) {
    var reveal = new IntersectionObserver(function (entries) {
      entries.forEach(function (entry) {
        if (!entry.isIntersecting) return;
        entry.target.classList.add("is-visible");
        reveal.unobserve(entry.target);
      });
    }, { threshold: 0.12 });
    sections.forEach(function (node) { reveal.observe(node); });
  } else {
    sections.forEach(function (node) { node.classList.add("is-visible"); });
  }

  /* ------------------------------------------------- mobile menu */
  var toggle = document.querySelector(".nav-toggle");
  if (toggle) {
    var header = document.querySelector(".site-header");
    toggle.addEventListener("click", function () {
      var open = header.classList.toggle("is-open");
      toggle.setAttribute("aria-expanded", open ? "true" : "false");
    });
    document.querySelectorAll(".nav a").forEach(function (link) {
      link.addEventListener("click", function () {
        header.classList.remove("is-open");
        toggle.setAttribute("aria-expanded", "false");
      });
    });
  }

  /* ------------------------------------------------- account deletion (/account/delete) */
  // Екі қадам: пошта → код. Сервер тіркелгі бар-жоғын айтпайды, сондықтан бірінші
  // қадамнан кейін әрқашан код өрісі ашылады.
  var deletion = document.getElementById("delete-account");
  if (deletion) {
    startDeletion(deletion, (data && data.errors) || {});
  }

  function startDeletion(root, errors) {
    var emailForm = root.querySelector('[data-step="email"]');
    var codeForm = root.querySelector('[data-step="code"]');
    var done = root.querySelector('[data-step="done"]');
    var errorBox = root.querySelector(".form-error");
    var email = "";

    function showError(code) {
      errorBox.textContent = errors[code] || errors["default"] || "";
      errorBox.hidden = !errorBox.textContent;
    }

    function post(path, body) {
      return fetch(path, {
        method: "POST",
        headers: { "Content-Type": "application/json", "Accept": "application/json" },
        credentials: "same-origin",
        body: JSON.stringify(body)
      }).then(function (response) {
        return response.json().catch(function () { return {}; }).then(function (payload) {
          if (response.ok) return payload;
          var code = payload && payload.error ? payload.error.code : "default";
          throw new Error(code || "default");
        });
      });
    }

    function busy(form, on) {
      form.querySelectorAll("button, input").forEach(function (node) { node.disabled = on; });
    }

    emailForm.addEventListener("submit", function (event) {
      event.preventDefault();
      errorBox.hidden = true;
      email = emailForm.elements.email.value.trim();
      if (!email) { showError("INVALID_EMAIL"); return; }
      busy(emailForm, true);
      post("/api/v1/account/delete/request", { email: email, locale: root.getAttribute("data-locale") || "en" })
        .then(function () {
          emailForm.hidden = true;
          codeForm.hidden = false;
        })
        .catch(function (error) { showError(error.message); return true; })
        .then(function (failed) {
          busy(emailForm, false);
          // Disabled fields lose focus: put it back where the person continues.
          (failed ? emailForm.elements.email : codeForm.elements.code).focus();
        });
    });

    codeForm.addEventListener("submit", function (event) {
      event.preventDefault();
      errorBox.hidden = true;
      var code = codeForm.elements.code.value.trim();
      if (!code) { showError("INVALID_OTP"); return; }
      busy(codeForm, true);
      post("/api/v1/account/delete/confirm", { email: email, code: code })
        .then(function () {
          codeForm.hidden = true;
          done.hidden = false;
        })
        .catch(function (error) { showError(error.message); return true; })
        .then(function (failed) {
          busy(codeForm, false);
          if (failed) codeForm.elements.code.select();
        });
    });

    codeForm.querySelector('[data-action="restart"]').addEventListener("click", function () {
      errorBox.hidden = true;
      codeForm.reset();
      codeForm.hidden = true;
      emailForm.hidden = false;
      emailForm.elements.email.focus();
    });
  }

  var languageSelect = document.querySelector(".lang-select");
  if (languageSelect) {
    languageSelect.addEventListener("change", function () {
      var url = new URL(window.location.href);
      url.searchParams.set("lang", languageSelect.value);
      window.location.assign(url.toString());
    });
  }
})();
