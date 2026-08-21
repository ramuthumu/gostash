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
    vollkorn:     '"Vollkorn", Georgia, serif',
    newsreader:   '"Newsreader", Georgia, serif',
    ebgaramond:   '"EB Garamond", Georgia, serif',
    inter:        '"Inter", -apple-system, Arial, sans-serif',
    mono:         'ui-monospace, SFMono-Regular, Menlo, Consolas, monospace'
  };

  // Base body size in rem. Some fonts (Vollkorn, EB Garamond) have a small
  // x-height and read tiny at the same size, so we scale them up per font.
  var BASE_REM = 1.25;
  var FONT_SCALE = {
    vollkorn: 1.18,
    ebgaramond: 1.14,
    newsreader: 1.0,
    inter: 0.98,
    mono: 0.92
  };
  var WIDTHS = [540, 760, 1000, 1200];

  var content = document.getElementById("article-content");
  var sizeLabel = document.getElementById("font-size-label");

  function applyPrefs() {
    var fam = prefs.font || "vollkorn";
    document.getElementById("font-family").value = fam;
    content.style.fontFamily = FONTS[fam] || FONTS.vollkorn;

    var size = prefs.size || 100;
    var scale = FONT_SCALE[fam] != null ? FONT_SCALE[fam] : 1;
    content.style.fontSize = (BASE_REM * size / 100 * scale).toFixed(3) + "rem";
    sizeLabel.textContent = size + "%";

    var spacing = prefs.spacing || 1.75;
    content.style.lineHeight = String(spacing);

    setTheme(prefs.theme || "light");
    setWidth(prefs.width || 760);
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
    document.documentElement.classList.remove("theme-light", "theme-sepia", "theme-dark");
    document.documentElement.classList.add("theme-" + theme);
    document.body.classList.remove("theme-light", "theme-sepia", "theme-dark");
    document.body.classList.add("theme-" + theme);
    themeBtns.forEach(function (b) {
      b.classList.toggle("active", b.dataset.theme === theme);
    });
  }

  // ---------- Width (column width) ----------
  var readerEl = document.querySelector(".reader");
  var widthBtns = document.querySelectorAll(".width-btn");

  function setWidth(px) {
    readerEl.style.maxWidth = px + "px";
    widthBtns.forEach(function (b) {
      b.classList.toggle("active", parseInt(b.dataset.width, 10) === px);
    });
  }
  widthBtns.forEach(function (btn) {
    btn.addEventListener("click", function () {
      prefs.width = parseInt(btn.dataset.width, 10);
      savePrefs();
      setWidth(prefs.width);
    });
  });

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
    // Collapse all whitespace (incl. unicode NBSP and zero-width) to single spaces.
    text = text.replace(/[\s\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000\ufeff]+/g, " ");
    // Word segmentation: Intl.Segmenter splits CJK (Chinese/Japanese/Korean),
    // which have no inter-word spaces, on per-word boundaries while keeping
    // Latin words intact. Without this, a whole CJK paragraph would collapse
    // into one giant token and speed-reading would be unusable for those scripts.
    if (window.Intl && Intl.Segmenter) {
      try {
        var wordSeg = new Intl.Segmenter(undefined, { granularity: "word" });
        var out = [];
        // for..of over the segment iterable (Segmenter is iterable).
        var iter = wordSeg[Symbol.iterator]();
        var s;
        while (!(s = iter.next()).done) {
          if (s.value.isWordLike) out.push(s.value.segment);
        }
        if (out.length) return out;
      } catch (e) {}
    }
    return text.split(" ").map(function (w) { return w.trim(); }).filter(Boolean);
  }

  // Grapheme segmentation so complex scripts (Telugu, Hindi, etc.) are split on
  // user-perceived characters, not UTF-16 code units — prevents vowel signs/matras
  // detaching from their consonant and rendering as dotted circles.
  var segmenter = null;
  try { segmenter = new Intl.Segmenter(undefined, { granularity: "grapheme" }); } catch (e) {}
  function graphemes(word) {
    if (segmenter) return Array.from(segmenter.segment(word), function (s) { return s.segment; });
    return Array.from(word); // fallback: code points (handles surrogate pairs)
  }

  function focalIndex(glyphs) {
    // Optimal Recognition Point: ~1/3 into the word (clamped), in graphemes
    var n = glyphs.length;
    if (n <= 1) return 0;
    var p = Math.floor(n / 3);
    return Math.min(p, 4);
  }

  function renderWord(word) {
    var g = graphemes(word);
    var p = Math.min(focalIndex(g), g.length);
    var left = g.slice(0, p).join("");
    var mid = g.slice(p, p + 1).join("") || "";
    var right = g.slice(p + 1).join("");
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
    // index here is the count of words shown so far (incremented before this
    // call), so progress reaches 100% on the last word instead of stalling ~97%.
    var pct = words.length ? (index / words.length) * 100 : 0;
    barEl.style.width = pct + "%";
  }

  function step() {
    if (index >= words.length) { pause(); playBtn.textContent = "▶ Play"; return; }
    renderWord(words[index]);
    index++;
    updateProgress();
    timer = setTimeout(step, delayFor(words[index - 1]));
  }

  function delayFor(word) {
    // Longer pause on sentence-ending punctuation for comprehension
    if (/[.!?;:]$/.test(word)) return baseDelay() * 2.4;
    if (/,$/.test(word)) return baseDelay() * 1.6;
    if (word.length > 8) return baseDelay() * 1.3;
    return baseDelay();
  }
  function baseDelay() { return 60000 / wpm; }

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
    else if (e.key === "ArrowLeft") { e.preventDefault(); setWpm(wpm - 50); }
    else if (e.key === "ArrowRight") { e.preventDefault(); setWpm(wpm + 50); }
  });

  // ---------- Font settings: reveal controls via the Aa button ----------
  // The controls bar is hidden by default and toggled by the Aa (Font settings)
  // button in the top-right — Instapaper-style, no scroll guessing (the prior
  // scroll auto-hide flickered when stopping mid-scroll). Close with Aa again,
  // Escape, or a click outside the bar.
  var controlsEl = document.getElementById("controls");
  var fsBtn = document.getElementById("font-settings-btn");
  function setControlsOpen(open) {
    controlsEl.classList.toggle("collapsed", !open);
    if (fsBtn) fsBtn.setAttribute("aria-expanded", open ? "true" : "false");
  }
  if (fsBtn) {
    fsBtn.addEventListener("click", function (e) {
      e.preventDefault();
      e.stopPropagation();
      setControlsOpen(controlsEl.classList.contains("collapsed"));
    });
  }
  // close on Escape
  document.addEventListener("keydown", function (e) {
    if (e.key === "Escape" && !controlsEl.classList.contains("collapsed")) setControlsOpen(false);
  });
  // close when clicking outside the controls and the button
  document.addEventListener("click", function (e) {
    if (controlsEl.classList.contains("collapsed")) return;
    if (controlsEl.contains(e.target) || (fsBtn && fsBtn.contains(e.target))) return;
    setControlsOpen(false);
  });

  // ---------- init ----------
  applyPrefs();
})();