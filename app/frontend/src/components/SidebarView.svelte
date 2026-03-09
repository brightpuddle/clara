<script lang="ts">
  import Icon from '@iconify/svelte/dist/Icon.svelte'
  import { activeSection, artifacts, focusPane } from '../lib/store'
  import type { Section } from '../lib/store'

  const sections: { id: Section; label: string; icon: string }[] = [
    { id: 'all',       label: 'All Artifacts', icon: 'mdi:view-dashboard-outline' },
    { id: 'notes',     label: 'Notes',         icon: 'mdi:note-text-outline' },
    { id: 'tasks',     label: 'Tasks',         icon: 'mdi:checkbox-marked-circle-outline' },
    { id: 'files',     label: 'Files',         icon: 'mdi:file-document-outline' },
    { id: 'reminders', label: 'Reminders',     icon: 'mdi:bell-outline' },
  ]

  function countFor(section: Section): number {
    if (section === 'all') return $artifacts.length
    return $artifacts.filter((a) => a.kind === section).length
  }

  function select(s: Section) {
    activeSection.set(s)
    focusPane.set('list')
  }

  function openSettings() {
    ;(window as any).showSettings?.()
  }
</script>

<nav class="flex flex-col h-full select-none" data-pane="sidebar">
  <!-- Sidebar title (inside traffic light safe zone) -->
  <div class="titlebar-drag px-4 pt-8 pb-3 text-xs font-semibold text-base-content/50 uppercase tracking-widest">
    Clara
  </div>

  <ul class="menu menu-sm flex-1 px-2 gap-0.5">
    {#each sections as s}
      <li>
        <button
          class="flex items-center gap-2 w-full text-left rounded-lg px-3 py-2 text-sm transition-colors
            {$activeSection === s.id
              ? 'bg-primary/20 text-primary font-medium'
              : 'text-base-content/70 hover:bg-base-300/50'}"
          on:click={() => select(s.id)}
        >
          <Icon icon={s.icon} class="w-4 h-4 shrink-0" />
          <span class="flex-1">{s.label}</span>
          {#if countFor(s.id) > 0}
            <span class="badge badge-sm badge-ghost text-base-content/40">
              {countFor(s.id)}
            </span>
          {/if}
        </button>
      </li>
    {/each}
  </ul>

  <!-- Bottom actions -->
  <div class="px-4 pb-4">
    <button
      class="btn btn-ghost btn-sm w-full justify-start gap-2 text-base-content/50"
      on:click={openSettings}
    >
      <Icon icon="mdi:cog-outline" class="w-4 h-4" />
      Settings
    </button>
  </div>
</nav>
