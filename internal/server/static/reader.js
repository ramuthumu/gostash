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
  if (!content) return;

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
    // Speed-read the rendered article's text (innerText respects layout).
    var art = document.getElementById("article-content");
    var text = art ? (art.innerText || art.textContent) : "";
    if (!text || text.length < 2) { alert("No readable text to speed-read."); return; }
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
    controlsEl.inert = !open;
    if (open && tocMenu) tocMenu.open = false;
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

  // ---------- Scroll progress & position persistence ----------
  var progressFill = document.getElementById("reading-progress-fill");
  var readerContainer = document.querySelector(".reader");
  var articleId = readerContainer ? readerContainer.dataset.articleId : null;
  var SCROLL_KEY_PREFIX = "readlater.scroll.";

  var scrollRange = 0;
  var scrollFrame = null;
  var saveTimer = null;
  var canSavePosition = false;
  var userInteracted = false;

  function measureReadingArea() {
    scrollRange = Math.max(0, document.documentElement.scrollHeight - window.innerHeight);
    var top = document.querySelector(".reader-top");
    if (top) content.style.setProperty("--reader-toolbar-height", top.offsetHeight + "px");
    queueReadingProgress();
  }

  function updateReadingProgress() {
    var fraction = scrollRange > 0 ? Math.min(1, Math.max(0, window.scrollY / scrollRange)) : 0;
    if (progressFill) progressFill.style.transform = "scaleX(" + fraction + ")";
  }

  function queueReadingProgress() {
    if (scrollFrame !== null) return;
    scrollFrame = requestAnimationFrame(function () {
      scrollFrame = null;
      updateReadingProgress();
    });
  }

  function saveScrollPosition() {
    clearTimeout(saveTimer);
    if (!canSavePosition || !articleId || scrollRange <= 0) return;
    try {
      localStorage.setItem(SCROLL_KEY_PREFIX + articleId, JSON.stringify({
        y: Math.round(window.scrollY),
        pct: Math.round(Math.min(100, Math.max(0, window.scrollY / scrollRange * 100))),
        updatedAt: Date.now()
      }));
    } catch (e) {}
  }

  // The scroll path only schedules a transform. Storage is synchronous, so
  // persist once scrolling has settled, or immediately when leaving the page.
  window.addEventListener("scroll", function () {
    queueReadingProgress();
    clearTimeout(saveTimer);
    saveTimer = setTimeout(saveScrollPosition, 250);
  }, { passive: true });
  window.addEventListener("pagehide", saveScrollPosition);
  document.addEventListener("visibilitychange", function () {
    if (document.hidden) saveScrollPosition();
  });
  window.addEventListener("resize", measureReadingArea, { passive: true });
  window.addEventListener("pageshow", measureReadingArea);
  if (window.ResizeObserver) {
    var sizeObserver = new ResizeObserver(measureReadingArea);
    sizeObserver.observe(document.body);
    sizeObserver.observe(content);
    sizeObserver.observe(document.querySelector(".reader-top"));
  }
  // Never pull the reader back to a saved location after they start interacting.
  ["wheel", "touchstart", "pointerdown", "keydown"].forEach(function (event) {
    window.addEventListener(event, function () { userInteracted = true; }, { passive: true, once: true });
  });

  function restoreScrollPosition() {
    var saved = null;
    try { saved = JSON.parse(localStorage.getItem(SCROLL_KEY_PREFIX + articleId)); } catch (e) {}
    var navigation = performance.getEntriesByType ? performance.getEntriesByType("navigation")[0] : null;
    var nativeRestore = location.hash || (navigation && navigation.type === "back_forward");
    // Font changes affect article height; wait for the initial font layout before
    // resuming. Browser history and explicit section links take precedence.
    var fontsReady = document.fonts ? document.fonts.ready : Promise.resolve();
    Promise.race([fontsReady, new Promise(function (resolve) { setTimeout(resolve, 1000); })]).then(function () {
      requestAnimationFrame(function () {
        measureReadingArea();
        if (!nativeRestore && !userInteracted && window.scrollY === 0 && saved && saved.y > 120 && saved.pct < 98) {
          window.scrollTo({ top: Math.min(saved.y, scrollRange), behavior: "instant" });
          showToast("Resumed from " + saved.pct + "%");
        }
        canSavePosition = true;
        queueReadingProgress();
      });
    });
  }

  function showToast(msg) {
    var toast = document.createElement("div");
    toast.className = "reader-toast";
    toast.textContent = msg;
    document.body.appendChild(toast);
    setTimeout(function () { toast.classList.add("visible"); }, 50);
    setTimeout(function () {
      toast.classList.remove("visible");
      setTimeout(function () { toast.remove(); }, 300);
    }, 2400);
  }

  // ---------- Server-rendered table of contents ----------
  var tocMenu = document.getElementById("toc-menu");
  if (tocMenu) {
    tocMenu.addEventListener("toggle", function () {
      if (tocMenu.open) setControlsOpen(false);
    });
    tocMenu.addEventListener("click", function (e) {
      if (e.target.closest("a[href^='#']")) {
        tocMenu.open = false;
        setControlsOpen(false);
        // Native fragment navigation updates the URL and honors reduced motion.
      }
    });
    document.addEventListener("click", function (e) {
      if (!tocMenu.contains(e.target)) tocMenu.open = false;
    });
    document.addEventListener("keydown", function (e) {
      if (e.key === "Escape") tocMenu.open = false;
    });
  }

  // ---------- Read aloud (TTS) ----------
  // Two modes: "local" (default) uses the device's own speech synthesis —
  // Siri voices on iPhone, macOS system voices (install an Enhanced voice
  // for best quality) — free and natural, needs the tab open. "server"
  // (READLATER_TTS_KEY configured) streams Azure Neural mp3 that keeps
  // playing in the background.
  var ttsBtn = document.getElementById("tts-btn");
  var ttsAudio = null; // server mode

  function ttsMode() { return ttsBtn ? (ttsBtn.dataset.mode || "local") : "local"; }

  function stopTTS() {
    if (ttsAudio) {
      ttsAudio.pause();
      ttsAudio.src = "";
      ttsAudio = null;
    }
    if (window.speechSynthesis) {
      speechSynthesis.cancel();
    }
    if (ttsBtn) {
      ttsBtn.textContent = "🔊 Listen";
      ttsBtn.classList.remove("tts-playing");
    }
  }

  // SpeechSynthesis voices load asynchronously; speaking before they arrive
  // leaves the queue stuck "pending" with no audio (Chromium/WebKit quirk —
  // audio can sit silent for minutes before recovering). Resolve as soon as
  // voices are ready, or after a short timeout so a voice-less environment
  // still speaks with the default voice.
  function whenVoicesReady() {
    return new Promise(function (resolve) {
      if (window.speechSynthesis.getVoices().length > 0) { resolve(); return; }
      var done = false;
      var finish = function () { if (!done) { done = true; resolve(); } };
      window.speechSynthesis.addEventListener("voiceschanged", finish);
      setTimeout(finish, 700);
    });
  }

  // Script evidence identical in spirit to the server's voiceForText: a
  // language wins only with enough non-Latin runes, so English articles
  // quoting a few Telugu words keep an English voice.
  function guessLang(text) {
    var te = 0, hi = 0;
    for (var i = 0; i < text.length && i < 4000; i++) {
      var c = text.charCodeAt(i);
      if (c >= 0x0C00 && c <= 0x0C7F) te++;
      else if (c >= 0x0900 && c <= 0x097F) hi++;
    }
    if (te >= 40) return "te-IN";
    if (hi >= 40) return "hi-IN";
    return "en";
  }

  function pickVoice(lang, voices) {
    if (lang !== "en") {
      var want = lang.toLowerCase();
      var match = voices.filter(function (v) { return v.lang && v.lang.toLowerCase().indexOf(want) === 0; });
      if (match.length) return match[0];
    }
    var preferred = ["Samantha", "Karen", "Moira", "Tessa", "Alex", "Google US English", "Daniel", "Zira"];
    for (var i = 0; i < preferred.length; i++) {
      for (var j = 0; j < voices.length; j++) {
        if (voices[j].name === preferred[i]) return voices[j];
      }
    }
    for (var k = 0; k < voices.length; k++) {
      if (voices[k].default) return voices[k];
    }
    return null;
  }

  // Sentence splitter without lookbehind (Safari < 16.4 rejects it).
  function ttsChunks(text) {
    var out = [];
    var cur = "";
    var push = function (s) { s = s.trim(); if (s) out.push(s); };
    for (var i = 0; i < text.length; i++) {
      cur += text[i];
      if (text[i] === "." || text[i] === "!" || text[i] === "?" || text[i] === "।" || text[i] === "\n") {
        if (i + 1 < text.length && text[i + 1] === " ") i++; // keep one space
        push(cur);
        cur = "";
      }
    }
    push(cur);
    return out;
  }

  function speakLocal(text) {
    if (!window.speechSynthesis) {
      showToast("This browser has no speech support.");
      stopTTS();
      return;
    }
    whenVoicesReady().then(function () {
      if (!ttsBtn || ttsBtn.textContent !== "⏹ Stop") return; // stopped while waiting
      var lang = guessLang(text);
      var voices = window.speechSynthesis.getVoices();
      var voice = pickVoice(lang, voices);
      var chunks = ttsChunks(text);
      if (!chunks.length) { showToast("No readable text to read aloud."); stopTTS(); return; }
      // Chain utterances onend -> next: iOS drops queued utterances after
      // the first, and chaining also survives the Chromium stall.
      var idx = 0;
      var sayNext = function () {
        if (!ttsBtn || ttsBtn.textContent !== "⏹ Stop") return; // stopped
        if (idx >= chunks.length) { stopTTS(); return; }
        var u = new SpeechSynthesisUtterance(chunks[idx++]);
        if (voice) u.voice = voice;
        u.lang = voice ? voice.lang : (lang === "en" ? "en-US" : lang);
        u.rate = 1.0;
        u.pitch = 1.0;
        u.onend = sayNext;
        u.onerror = function (e) {
          if (e.error && e.error !== "canceled" && e.error !== "interrupted") {
            showToast("Speech error: " + e.error);
            stopTTS();
          }
        };
        window.speechSynthesis.speak(u);
      };
      sayNext();
    });
  }

  function startTTS() {
    if (overlay && !overlay.hidden) close();
    ttsBtn.textContent = "⏹ Stop";
    ttsBtn.classList.add("tts-playing");
    if (ttsMode() === "server") {
      var id = readerContainer ? readerContainer.dataset.articleId : "";
      if (!id) { showToast("Missing article id."); stopTTS(); return; }
      ttsAudio = new Audio("/article/" + id + "/tts");
      ttsAudio.addEventListener("ended", stopTTS);
      ttsAudio.addEventListener("error", function () {
        showToast("Could not start audio.");
        stopTTS();
      });
      ttsAudio.play().catch(function () {
        showToast("Playback blocked — try again.");
        stopTTS();
      });
      return;
    }
    var art = document.getElementById("article-content");
    var text = art ? (art.innerText || art.textContent) : "";
    if (!text || text.length < 2) {
      showToast("No readable text to read aloud.");
      stopTTS();
      return;
    }
    speakLocal(text);
  }

  if (ttsBtn) {
    ttsBtn.addEventListener("click", function () {
      if (ttsBtn.textContent === "⏹ Stop") {
        stopTTS();
        return;
      }
      startTTS();
    });
  }

  // ---------- init ----------
  applyPrefs();
  restoreScrollPosition();
  updateReadingProgress();
  measureReadingArea();
})();