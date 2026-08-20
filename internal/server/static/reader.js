(function () {
  "use strict";

  // ---------- Persistence ----------
  var KEY = "readlater.reader.prefs";
  var prefs = loadPrefs();

  function loadPrefs() {
    try { return JSON.parse(localStorage.getItem(KEY)) || {}; }
    catch (e) { return {}; }
  }
  function savePrefs() {
    try { localStorage.setItem(KEY, JSON.stringify(prefs)); } catch (e) {}
  }

  // ---------- Font families ----------
  var FONTS = {
    serif:   'Georgia, "Times New Roman", serif',
    athelas: '"Iowan Old Style", "Palatino Linotype", Palatino, "Book Antiqua", Georgia, serif',
    sans:    '-apple-system, BlinkMacSystemFont, "Helvetica Neue", Arial, sans-serif',
    mono:    'ui-monospace, SFMono-Regular, Menlo, Consolas, monospace'
  };

  var content = document.getElementById("article-content");
  var sizeLabel = document.getElementById("font-size-label");

  function applyPrefs() {
    var fam = prefs.font || "serif";
    document.getElementById("font-family").value = fam;
    content.style.fontFamily = FONTS[fam] || FONTS.serif;

    var size = prefs.size || 100;
    sizeLabel.textContent = size + "%";
    content.style.fontSize = size + "%";

    var spacing = prefs.spacing || 1.75;
    content.style.lineHeight = String(spacing);

    setTheme(prefs.theme || "light");
  }

  // ---------- Controls ----------
  document.getElementById("font-family").addEventListener("change", function (e) {
    prefs.font = e.target.value; savePrefs(); applyPrefs();
  });

  function bumpSize(delta) {
    prefs.size = Math.min(200, Math.max(70, (prefs.size || 100) + delta));
    savePrefs(); applyPrefs();
  }
  document.getElementById("font-larger").addEventListener("click", function () { bumpSize(10); });
  document.getElementById("font-smaller").addEventListener("click", function () { bumpSize(-10); });

  document.getElementById("spacing-tight").addEventListener("click", function () {
    prefs.spacing = 1.4; savePrefs(); applyPrefs();
  });
  document.getElementById("spacing-loose").addEventListener("click", function () {
    prefs.spacing = 2.1; savePrefs(); applyPrefs();
  });

  var themeBtns = document.querySelectorAll(".theme-btn");
  themeBtns.forEach(function (btn) {
    btn.addEventListener("click", function () {
      setTheme(btn.dataset.theme);
      prefs.theme = btn.dataset.theme; savePrefs();
    });
  });

  function setTheme(theme) {
    document.body.classList.remove("theme-light", "theme-sepia", "theme-dark");
    document.body.classList.add("theme-" + theme);
    themeBtns.forEach(function (b) {
      b.classList.toggle("active", b.dataset.theme === theme);
    });
  }

  // ---------- Speed reader (RSVP) ----------
  var overlay = document.getElementById("rsvp-overlay");
  var wordEl = document.getElementById("rsvp-word");
  var barEl = document.getElementById("rsvp-bar");
  var playBtn = document.getElementById("rsvp-play");
  var wpmEl = document.getElementById("rsvp-wpm");
  var slider = document.getElementById("rsvp-slider");

  var words = [];
  var index = 0;
  var timer = null;
  var wpm = 350;

  function tokenize(text) {
    // Collapse all whitespace (incl. unicode NBSP and zero-width) to single spaces,
    // then split and drop empty/whitespace-only tokens.
    text = text.replace(/[\s\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000\ufeff]+/g, " ");
    return text.split(" ").map(function (w) { return w.trim(); }).filter(Boolean);
  }

  function focalIndex(word) {
    // Optimal Recognition Point: ~1/3 into the word (clamped)
    var n = word.length;
    if (n <= 1) return 0;
    var p = Math.floor(n / 3);
    return Math.min(p, 4);
  }

  function renderWord(word) {
    var p = focalIndex(word);
    var left = word.slice(0, p);
    var mid = word.slice(p, p + 1);
    var right = word.slice(p + 1);
    // Use an actual non-breaking space char so esc() won't mangle it into "&nbsp;".
    var pad = "\u00a0".repeat(Math.max(0, 5 - p));
    wordEl.innerHTML =
      '<span class="rsvp-left">' + esc(pad + left) + "</span>" +
      '<span class="rsvp-focus">' + esc(mid) + "</span>" +
      '<span class="rsvp-right">' + esc(right) + "</span>";
  }
  function esc(s) {
    return s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
  }

  function updateProgress() {
    var pct = words.length ? (index / words.length) * 100 : 0;
    barEl.style.width = pct + "%";
  }

  function delayFor(word) {
    // Longer pause on sentence-ending punctuation for comprehension
    if (/[.!?;:]$/.test(word)) return baseDelay() * 2.4;
    if (/,$/.test(word)) return baseDelay() * 1.6;
    if (word.length > 8) return baseDelay() * 1.3;
    return baseDelay();
  }
  function baseDelay() { return 60000 / wpm; }

  function step() {
    if (index >= words.length) { pause(); index = 0; renderWord(words[0] || ""); updateProgress(); playBtn.textContent = "▶ Play"; return; }
    renderWord(words[index]);
    updateProgress();
    index++;
    timer = setTimeout(step, delayFor(words[index - 1]));
  }

  function play() {
    if (timer) return;
    if (index >= words.length) index = 0;
    playBtn.textContent = "⏸ Pause";
    step();
  }
  function pause() {
    if (timer) { clearTimeout(timer); timer = null; }
    playBtn.textContent = "▶ Play";
  }
  function toggle() { if (timer) pause(); else play(); }

  function setWpm(v) {
    wpm = Math.max(150, Math.min(900, v));
    wpmEl.textContent = wpm;
    slider.value = wpm;
  }

  function open() {
    // Prefer the rendered article's innerText (respects layout spacing);
    // fall back to the hidden extracted text source.
    var art = document.getElementById("article-content");
    var text = art ? (art.innerText || art.textContent) : "";
    if (!text || text.length < 2) {
      var src = document.getElementById("rsvp-source");
      text = src ? src.textContent : "";
    }
    if (!text) { alert("No readable text to speed-read."); return; }
    words = tokenize(text);
    index = 0;
    setWpm(parseInt(slider.value, 10) || 350);
    overlay.hidden = false;
    renderWord(words[0] || "");
    updateProgress();
    play();
  }
  function close() { pause(); overlay.hidden = true; }

  document.getElementById("rsvp-open").addEventListener("click", open);
  document.getElementById("rsvp-close").addEventListener("click", close);
  playBtn.addEventListener("click", toggle);
  document.getElementById("rsvp-restart").addEventListener("click", function () {
    pause(); index = 0; renderWord(words[0] || ""); updateProgress(); play();
  });
  document.getElementById("rsvp-slower").addEventListener("click", function () { setWpm(wpm - 50); });
  document.getElementById("rsvp-faster").addEventListener("click", function () { setWpm(wpm + 50); });
  slider.addEventListener("input", function (e) { setWpm(parseInt(e.target.value, 10)); });

  // keyboard shortcuts in speed reader
  document.addEventListener("keydown", function (e) {
    if (overlay.hidden) return;
    if (e.code === "Space") { e.preventDefault(); toggle(); }
    else if (e.key === "Escape") { close(); }
    else if (e.key === "ArrowLeft") { setWpm(wpm - 50); }
    else if (e.key === "ArrowRight") { setWpm(wpm + 50); }
  });

  // ---------- init ----------
  applyPrefs();
})();