// Runs before first paint (no inline script: CSP is script-src 'self'). Mirrors ThemeProvider's storage key.
try {
  var t = localStorage.getItem("theme")
  var dark = t === "dark" || ((t === "system" || !t) && matchMedia("(prefers-color-scheme: dark)").matches)
  document.documentElement.classList.add(dark ? "dark" : "light")
} catch (e) {}
