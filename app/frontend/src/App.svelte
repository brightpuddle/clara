<script lang="ts">
  import { onMount, onDestroy } from "svelte";
  import Icon from '@iconify/svelte/dist/Icon.svelte'
  import SidebarView from "./components/SidebarView.svelte";
  import ArtifactListView from "./components/ArtifactListView.svelte";
  import ArtifactDetailView from "./components/ArtifactDetailView.svelte";
  import SettingsModal from "./components/SettingsModal.svelte";

  import {
    artifacts,
    connected,
    focusPane,
    showSettings,
    selectedId,
    activeSection,
    applyArtifactEvent,
  } from "./lib/store";
  import {
    listArtifacts,
    onArtifactEvent,
    onConnectionChange,
    onThemeChange,
  } from "./lib/agent";
  import { WindowMinimise, WindowHide, Quit } from "../wailsjs/runtime/runtime";

  let sidebarVisible = true;
  let cleanupEvents: (() => void) | null = null;

  // --- Theme tracking ---
  // Detect system theme and update daisyUI data-theme
  function applyTheme(dark: boolean) {
    document.documentElement.setAttribute("data-theme", dark ? "clara-dark" : "clara-light")
  }

  // Match system preference
  const mq = window.matchMedia("(prefers-color-scheme: dark)")
  applyTheme(mq.matches)
  const onMqChange = (e: MediaQueryListEvent) => applyTheme(e.matches)
  mq.addEventListener("change", onMqChange)

  // Load initial artifacts
  async function loadArtifacts() {
    try {
      const list = await listArtifacts([]);
      artifacts.set(list ?? []);
      if (list?.length && !$selectedId) {
        selectedId.set(list[0].id);
      }
    } catch (e) {
      console.warn("Failed to load artifacts:", e);
    }
  }

  onMount(async () => {
    cleanupEvents = onArtifactEvent(({ type, artifact }) => {
      applyArtifactEvent(type, artifact);
    });

    onConnectionChange((isConnected) => {
      connected.set(isConnected);
      if (isConnected) loadArtifacts();
    });

    // Listen for theme changes from the Go backend (via native bridge)
    const cleanupTheme = onThemeChange((dark: boolean) => applyTheme(dark))

    await loadArtifacts();

    const unsubSection = activeSection.subscribe(() => loadArtifacts());

    // Expose settings toggle for sidebar button
    ;(window as any).showSettings = () => showSettings.set(true)

    return () => {
      unsubSection()
      cleanupTheme()
    }
  });

  onDestroy(() => {
    cleanupEvents?.();
    mq.removeEventListener("change", onMqChange)
  });

  // Global keyboard shortcuts
  function handleGlobalKey(e: KeyboardEvent) {
    // macOS standard shortcuts
    if (e.metaKey) {
      switch (e.key) {
        case ",": e.preventDefault(); showSettings.update(v => !v); return
        case "q": e.preventDefault(); Quit(); return
        case "w": e.preventDefault(); WindowHide(); return
        case "h": e.preventDefault(); WindowHide(); return
        case "m": e.preventDefault(); WindowMinimise(); return
      }
    }

    // Skip if typing in an input
    if ((e.target as HTMLElement)?.closest('input, textarea, select')) return

    // h/l → move between panes
    if (e.key === "h" && !e.metaKey && !e.ctrlKey && !e.altKey) {
      const order: (typeof $focusPane)[] = ["detail", "list", "sidebar"]
      const idx = order.indexOf($focusPane)
      focusPane.set(order[Math.max(0, idx - 1)] ?? "sidebar")
      return
    }
    if (e.key === "l" && !e.metaKey && !e.ctrlKey && !e.altKey) {
      const order: (typeof $focusPane)[] = ["sidebar", "list", "detail"]
      const idx = order.indexOf($focusPane)
      focusPane.set(order[Math.min(order.length - 1, idx + 1)] ?? "detail")
      return
    }
  }

  $: listCursor   = $focusPane === "list"    ? "ring-1 ring-primary/30" : ""
  $: detailCursor = $focusPane === "detail"  ? "ring-1 ring-primary/30" : ""
  $: sidebarCursor = $focusPane === "sidebar" ? "ring-1 ring-primary/30" : ""
</script>

<svelte:window on:keydown={handleGlobalKey} />

<!-- Root container — no data-theme here; set dynamically on <html> -->
<div class="flex h-screen bg-base-100 text-base-content overflow-hidden">
  <!-- Sidebar -->
  {#if sidebarVisible}
    <!-- svelte-ignore a11y-click-events-have-key-events -->
    <div
      class="w-52 shrink-0 bg-base-200/70 border-r border-base-300/40 flex flex-col {sidebarCursor} transition-all"
      on:click={() => focusPane.set("sidebar")}
    >
      <SidebarView />
    </div>
  {/if}

  <!-- List -->
  <!-- svelte-ignore a11y-click-events-have-key-events -->
  <div
    class="w-72 shrink-0 border-r border-base-300/40 flex flex-col {listCursor} transition-all"
    on:click={() => focusPane.set("list")}
  >
    <ArtifactListView />
  </div>

  <!-- Detail -->
  <!-- svelte-ignore a11y-click-events-have-key-events -->
  <div
    class="flex-1 min-w-0 flex flex-col {detailCursor} transition-all"
    on:click={() => focusPane.set("detail")}
  >
    <ArtifactDetailView />
  </div>

  <!-- Status bar: taller, padded to avoid Tahoe curve -->
  <div
    class="fixed bottom-0 left-0 right-0 h-7 bg-base-300/60 border-t border-base-300/40
    flex items-center px-6 gap-4 text-xs text-base-content/30 z-10"
  >
    <span class={$connected ? "text-success" : "text-error"}>
      {$connected ? "● connected" : "○ connecting…"}
    </span>
    <span class="flex-1" />
    <button
      class="hover:text-base-content/60 transition-colors flex items-center gap-1"
      on:click={() => (sidebarVisible = !sidebarVisible)}
    >
      <Icon icon={sidebarVisible ? "mdi:page-layout-sidebar-left-close" : "mdi:page-layout-sidebar-left"} class="w-3.5 h-3.5" />
      {sidebarVisible ? "Hide sidebar" : "Show sidebar"}
    </button>
  </div>
</div>

<SettingsModal />
