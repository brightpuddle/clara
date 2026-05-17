// Clara Web UI — theme management
// Reads/persists user's theme preference to localStorage.
// Values: "light" | "dark" | "system"

(function () {
  function applyTheme(pref) {
    var theme;
    if (pref === "system" || !pref) {
      theme = window.matchMedia("(prefers-color-scheme: dark)").matches
        ? "dark"
        : "light";
    } else {
      theme = pref;
    }
    document.documentElement.setAttribute("data-theme", theme);
    document.documentElement.setAttribute("data-pref", pref || "system");
  }

  var saved = localStorage.getItem("clara-theme") || "system";
  applyTheme(saved);

  window.__claraSetTheme = function (pref) {
    localStorage.setItem("clara-theme", pref);
    applyTheme(pref);
    // Update button states
    document.querySelectorAll("[data-theme-btn]").forEach(function (btn) {
      btn.classList.toggle("btn-active", btn.dataset.themeBtn === pref);
    });
  };

  // Respond to system theme changes when set to "system"
  window
    .matchMedia("(prefers-color-scheme: dark)")
    .addEventListener("change", function () {
      if ((localStorage.getItem("clara-theme") || "system") === "system") {
        applyTheme("system");
      }
    });
})();
