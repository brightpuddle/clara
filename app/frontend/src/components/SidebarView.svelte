<script lang="ts">
  import { activeSection, artifacts, focusPane } from '../lib/store'
  import type { Section } from '../lib/store'

  const sections: { id: Section; label: string; icon: string }[] = [
    { id: 'all',       label: 'All Artifacts', icon: '◎' },
    { id: 'notes',     label: 'Notes',         icon: '📝' },
    { id: 'tasks',     label: 'Tasks',         icon: '✓' },
    { id: 'files',     label: 'Files',         icon: '📄' },
    { id: 'reminders', label: 'Reminders',     icon: '🔔' },
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
  <!-- Sidebar title -->
  <div class="titlebar-drag px-4 py-3 text-xs font-semibold text-base-content/50 uppercase tracking-widest">
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
          <span class="text-base w-5 text-center shrink-0">{s.icon}</span>
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
      <svg xmlns="http://www.w3.org/2000/svg" class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
          d="M10.325 4.317c.426-1.756 2.924-1.756 3.35 0a1.724 1.724 0 002.573 1.066c1.543-.94 3.31.826 2.37 2.37a1.724 1.724 0 001.065 2.572c1.756.426 1.756 2.924 0 3.35a1.724 1.724 0 00-1.066 2.573c.94 1.543-.826 3.31-2.37 2.37a1.724 1.724 0 00-2.572 1.065c-.426 1.756-2.924 1.756-3.35 0a1.724 1.724 0 00-2.573-1.066c-1.543.94-3.31-.826-2.37-2.37a1.724 1.724 0 00-1.065-2.572c-1.756-.426-1.756-2.924 0-3.35a1.724 1.724 0 001.066-2.573c-.94-1.543.826-3.31 2.37-2.37.996.608 2.296.07 2.572-1.065z" />
        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 12a3 3 0 11-6 0 3 3 0 016 0z" />
      </svg>
      Settings
    </button>
  </div>
</nav>
