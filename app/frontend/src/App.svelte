<script lang="ts">
  import { onMount, onDestroy } from 'svelte'
  import SidebarView from './components/SidebarView.svelte'
  import ArtifactListView from './components/ArtifactListView.svelte'
  import ArtifactDetailView from './components/ArtifactDetailView.svelte'
  import SettingsModal from './components/SettingsModal.svelte'

  import {
    artifacts,
    connected,
    focusPane,
    showSettings,
    selectedId,
    activeSection,
    applyArtifactEvent,
    moveSelection,
  } from './lib/store'
  import {
    listArtifacts,
    getStatus,
    onArtifactEvent,
    onConnectionChange,
  } from './lib/agent'

  let sidebarVisible = true
  let cleanupEvents: (() => void) | null = null
  let statusPollInterval: ReturnType<typeof setInterval> | null = null

  // Load initial artifacts
  async function loadArtifacts() {
    try {
      const list = await listArtifacts([])
      artifacts.set(list ?? [])
      // Auto-select first item
      if (list?.length && !$selectedId) {
        selectedId.set(list[0].id)
      }
    } catch (e) {
      console.warn('Failed to load artifacts:', e)
    }
  }

  onMount(async () => {
    // Subscribe to real-time events
    cleanupEvents = onArtifactEvent(({ type, artifact }) => {
      applyArtifactEvent(type, artifact)
    })

    // Watch connection state
    onConnectionChange((isConnected) => {
      connected.set(isConnected)
      if (isConnected) loadArtifacts()
    })

    // Initial load (agent may already be connected)
    await loadArtifacts()

    // Reload artifacts when section changes
    const unsubSection = activeSection.subscribe(() => loadArtifacts())

    // Expose settings toggle for sidebar button
    ;(window as any).showSettings = () => showSettings.set(true)

    return () => {
      unsubSection()
    }
  })

  onDestroy(() => {
    cleanupEvents?.()
    if (statusPollInterval) clearInterval(statusPollInterval)
  })

  // Global keyboard shortcuts
  function handleGlobalKey(e: KeyboardEvent) {
    // Cmd+, → Settings
    if ((e.metaKey || e.ctrlKey) && e.key === ',') {
      e.preventDefault()
      showSettings.update((v) => !v)
      return
    }
    // h/l → move between panes
    if (e.key === 'h' && !e.metaKey && !e.ctrlKey && !e.altKey) {
      const order: typeof $focusPane[] = ['detail', 'list', 'sidebar']
      const idx = order.indexOf($focusPane)
      focusPane.set(order[Math.max(0, idx - 1)] ?? 'sidebar')
      return
    }
    if (e.key === 'l' && !e.metaKey && !e.ctrlKey && !e.altKey) {
      const order: typeof $focusPane[] = ['sidebar', 'list', 'detail']
      const idx = order.indexOf($focusPane)
      focusPane.set(order[Math.min(order.length - 1, idx + 1)] ?? 'detail')
      return
    }
    // Space → mark done (when list focused)
    if (e.key === ' ' && $focusPane === 'list') {
      e.preventDefault()
      // MarkDone handled in detail view via keyboard
    }
  }

  $: listCursor = $focusPane === 'list' ? 'ring-1 ring-primary/30' : ''
  $: detailCursor = $focusPane === 'detail' ? 'ring-1 ring-primary/30' : ''
  $: sidebarCursor = $focusPane === 'sidebar' ? 'ring-1 ring-primary/30' : ''
</script>

<svelte:window on:keydown={handleGlobalKey} />

<div class="flex h-screen bg-base-100 text-base-content" data-theme="clara">

  <!-- Sidebar -->
  {#if sidebarVisible}
    <div
      class="w-52 shrink-0 bg-base-200/70 border-r border-base-300/40 flex flex-col {sidebarCursor} transition-all"
      on:click={() => focusPane.set('sidebar')}
    >
      <SidebarView />
    </div>
  {/if}

  <!-- List -->
  <div
    class="w-72 shrink-0 border-r border-base-300/40 flex flex-col {listCursor} transition-all"
    on:click={() => focusPane.set('list')}
  >
    <ArtifactListView />
  </div>

  <!-- Detail -->
  <div
    class="flex-1 min-w-0 flex flex-col {detailCursor} transition-all"
    on:click={() => focusPane.set('detail')}
  >
    <ArtifactDetailView />
  </div>

  <!-- Status bar -->
  <div class="fixed bottom-0 left-0 right-0 h-5 bg-base-300/60 border-t border-base-300/40
    flex items-center px-3 gap-4 text-xs text-base-content/30 z-10">
    <span class="{$connected ? 'text-success' : 'text-error'}">
      {$connected ? '● connected' : '○ connecting…'}
    </span>
    <span class="flex-1" />
    <button
      class="hover:text-base-content/60 transition-colors"
      on:click={() => sidebarVisible = !sidebarVisible}
    >
      {sidebarVisible ? '⊞ hide sidebar' : '⊟ show sidebar'}
    </button>
  </div>
</div>

<SettingsModal />
