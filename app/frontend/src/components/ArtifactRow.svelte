<script lang="ts">
  import type { Artifact } from '../lib/agent'

  export let artifact: Artifact
  export let selected: boolean = false

  function heatClass(score: number): string {
    if (score >= 0.8) return 'heat-critical'
    if (score >= 0.5) return 'heat-high'
    if (score >= 0.25) return 'heat-medium'
    return 'heat-low'
  }

  function kindIcon(kind: string): string {
    switch (kind) {
      case 'note':      return '📝'
      case 'task':      return '✓'
      case 'file':      return '📄'
      case 'reminder':  return '🔔'
      case 'email':     return '✉️'
      default:          return '◎'
    }
  }

  function formatDate(unix?: number): string {
    if (!unix) return ''
    const d = new Date(unix * 1000)
    return d.toLocaleDateString(undefined, { month: 'short', day: 'numeric' })
  }

  $: snippet = artifact.content?.slice(0, 100).replace(/\n/g, ' ') ?? ''
  $: heatCls = heatClass(artifact.heat_score)
  $: icon = kindIcon(artifact.kind)
  $: dueStr = formatDate(artifact.due_at)
</script>

<button
  class="w-full text-left px-4 py-3 border-b border-base-300/30 transition-colors cursor-pointer
    {selected ? 'bg-primary/15 border-l-2 border-l-primary' : 'hover:bg-base-200/50'}"
  on:click
>
  <div class="flex items-start gap-2">
    <!-- Heat indicator -->
    <span class="mt-0.5 text-xs {heatCls}" title="Heat: {artifact.heat_score.toFixed(2)}">●</span>

    <div class="flex-1 min-w-0">
      <div class="flex items-center gap-1.5">
        <span class="text-xs">{icon}</span>
        <span class="text-sm font-medium text-base-content truncate flex-1">{artifact.title}</span>
        {#if dueStr}
          <span class="text-xs text-warning shrink-0">{dueStr}</span>
        {/if}
      </div>
      {#if snippet}
        <p class="text-xs text-base-content/50 truncate mt-0.5">{snippet}</p>
      {/if}
    </div>
  </div>
</button>
