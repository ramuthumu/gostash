// Shared early-theme bootstrap. Loaded render-blocking in <head> of both
// list.html and reader.html. Applies the theme class to <html> synchronously
// (documentElement exists during head parsing) and to <body> as soon as it
// parses, so there is no flash of the wrong theme. Uses the SAME localStorage
// key ("readlater.reader.prefs") and body classes as reader.js, so the two
// scripts never clobber each other.
(function () {
  "use strict";
  var KEY = "readlater.reader.prefs";
  var THEMES = ["theme-light", "theme-sepia", "theme-dark"];

  function readPrefs() {
    try { return JSON.parse(localStorage.getItem(KEY)) || {}; }
    catch (e) { return {}; }
  }
  function writePrefs(p) {
    try { localStorage.setItem(KEY, JSON.stringify(p)); } catch (e) {}
  }

  // Remove-all-then-add (matches reader.js setTheme) so there is never a
  // duplicate theme class on the same element.
  function applyThemeClass(el, theme) {
    if (!el) return;
    for (var i = 0; i < THEMES.length; i++) el.classList.remove(THEMES[i]);
    el.classList.add("theme-" + (theme || "light"));
  }

  function currentTheme() { return readPrefs().theme || "light"; }

  // 2-state homepage toggle (sepia collapses to light; the reader's 3-way
  // .theme-btn buttons remain the only way to pick sepia). Merges with the
  // existing prefs object so font/size/spacing/width are never wiped.
  function toggleTheme() {
    var p = readPrefs();
    p.theme = (p.theme === "dark") ? "light" : "dark";
    writePrefs(p);
    applyThemeClass(document.documentElement, p.theme);
    if (document.body) applyThemeClass(document.body, p.theme);
    updateToggleButton(p.theme);
  }

  // Only the homepage has #theme-toggle; guard with a null check. reader.js
  // owns the .theme-btn .active state on the reader page.
  function updateToggleButton(theme) {
    var btn = document.getElementById("theme-toggle");
    if (!btn) return;
    var dark = theme === "dark";
    btn.textContent = dark ? "☀️" : "🌙";
    btn.setAttribute("aria-label", dark ? "Switch to light theme" : "Switch to dark theme");
    btn.title = dark ? "Switch to light theme" : "Switch to dark theme";
  }

  // ---- FOUC-free early apply on <html> (synchronous, head-time) ----
  var theme = currentTheme();
  applyThemeClass(document.documentElement, theme);

  // ---- Apply on <body> as soon as it exists, then sync the toggle icon ----
  function onBody() {
    applyThemeClass(document.body, theme);
    updateToggleButton(theme);
  }
  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", onBody);
  else onBody();

  // ---- Wire the homepage toggle via delegation (button may not exist on the
  // reader page, and may not be parsed yet when this head script runs) ----
  document.addEventListener("click", function (e) {
    var btn = e.target && e.target.closest && e.target.closest("#theme-toggle");
    if (btn) { e.preventDefault(); toggleTheme(); }
  });

  window.gostashTheme = { currentTheme: currentTheme, toggleTheme: toggleTheme };
})();