import "@/style.css"
import Alpine from "alpinejs"
import htmx from "htmx.org"

declare global {
  interface Window {
    Alpine: typeof Alpine
    htmx: typeof htmx
    __claraSetTheme: (theme: string) => void
  }
}

// Attach htmx and Alpine to window
window.htmx = htmx
window.Alpine = Alpine

// Theme management
;(function () {
  function applyTheme(pref: string) {
    let theme: string
    if (pref === "system" || !pref) {
      theme = window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light"
    } else {
      theme = pref
    }
    document.documentElement.setAttribute("data-theme", theme)
    document.documentElement.setAttribute("data-pref", pref || "system")
  }

  const saved = localStorage.getItem("clara-theme") || "system"
  applyTheme(saved)

  window.__claraSetTheme = function (pref: string) {
    localStorage.setItem("clara-theme", pref)
    applyTheme(pref)
    document.querySelectorAll("[data-theme-btn]").forEach((btn) => {
      const el = btn as HTMLElement
      el.classList.toggle("btn-active", el.dataset.themeBtn === pref)
    })
  }

  window.matchMedia("(prefers-color-scheme: dark)").addEventListener("change", () => {
    if ((localStorage.getItem("clara-theme") || "system") === "system") {
      applyTheme("system")
    }
  })
})()

Alpine.start()

document.addEventListener("DOMContentLoaded", () => {
  console.log("Clara Web UI client bundle initialized")
})
