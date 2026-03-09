<script lang="ts">
  import { selectedDetail, focusPane, selectedId } from '../lib/store'
  import { markDone, openNative } from '../lib/agent'
  import { tick } from 'svelte'

  let contentEl: HTMLElement
  let detailScrollY = 0

  function heatLabel(score: number): string {
    if (score >= 0.8) return 'Critical'
    if (score >= 0.5) return 'High'
    if (score >= 0.25) return 'Medium'
    return 'Low'
  }

  function heatClass(score: number): string {
    if (score >= 0.8) return 'heat-critical'
    if (score >= 0.5) return 'heat-high'
    if (score >= 0.25) return 'heat-medium'
    return 'heat-low'
  }

  function formatDate(unix?: number): string {
    if (!unix) return '—'
    return new Date(unix * 1000).toLocaleString()
  }

  async function handleMarkDone() {
    if (!$selectedDetail) return
    await markDone($selectedDetail.artifact.id)
    selectedId.set(null)
  }

  async function handleOpen() {
    if (!$selectedDetail) return
    await openNative($selectedDetail.artifact)
  }

  function handleKey(e: KeyboardEvent) {
    if ($focusPane !== 'detail' || !contentEl) return
    const step = 40
    switch (e.key) {
      case 'j': case 'ArrowDown':
        e.preventDefault()
        contentEl.scrollTop += step
        break
      case 'k': case 'ArrowUp':
        e.preventDefault()
        contentEl.scrollTop -= step
        break
      case 'g':
        e.preventDefault()
        contentEl.scrollTop = 0
        break
      case 'G':
        e.preventDefault()
        contentEl.scrollTop = contentEl.scrollHeight
        break
    }
  }

  $: artifact = $selectedDetail?.artifact
  $: related = $selectedDetail?.related ?? []
</script>

<svelte:window on:keydown={handleKey} />

<div
  class="flex flex-col h-full"
  data-pane="detail"
  on:click={() => focusPane.set('detail')}
>
  {#if artifact}
    <!-- Toolbar -->
    <div class="flex items-center gap-2 px-4 py-2 border-b border-base-300/30 shrink-0">
      <span class="text-xs font-medium uppercase tracking-wide text-base-content/40">
        {artifact.kind}
      </span>
      <span class="flex-1" />
      <button
        class="btn btn-xs btn-ghost"
        title="Mark Done (Space)"
        on:click={handleMarkDone}
      >
        ✓ Done
      </button>
      <button
        class="btn btn-xs btn-ghost"
        title="Open in App (o)"
        on:click={handleOpen}
      >
        ↗ Open
      </button>
    </div>

    <!-- Scrollable content -->
    <div bind:this={contentEl} class="flex-1 overflow-y-auto px-5 py-4 space-y-4">
      <!-- Title -->
      <h1 class="text-lg font-semibold text-base-content leading-snug">
        {artifact.title}
      </h1>

      <!-- Metadata row -->
      <div class="flex flex-wrap gap-3 text-xs text-base-content/50">
        <span class="{heatClass(artifact.heat_score)} font-medium">
          ● {heatLabel(artifact.heat_score)}
        </span>
        {#if artifact.source_path}
          <span title={artifact.source_path}>
            📄 {artifact.source_path.split('/').pop()}
          </span>
        {/if}
        {#if artifact.source_app}
          <span>🔗 {artifact.source_app}</span>
        {/if}
        {#if artifact.due_at}
          <span class="text-warning">📅 {formatDate(artifact.due_at)}</span>
        {/if}
      </div>

      <!-- Tags -->
      {#if artifact.tags?.length}
        <div class="flex flex-wrap gap-1">
          {#each artifact.tags as tag}
            <span class="badge badge-sm badge-ghost">{tag}</span>
          {/each}
        </div>
      {/if}

      <!-- Content -->
      {#if artifact.content}
        <div class="divider my-2" />
        <pre class="whitespace-pre-wrap text-sm text-base-content/80 font-sans leading-relaxed">
{artifact.content}</pre>
      {/if}

      <!-- Related items -->
      {#if related.length > 0}
        <div class="divider my-2">Related</div>
        <ul class="space-y-2">
          {#each related as rel}
            <li>
              <button
                class="w-full text-left p-3 rounded-lg bg-base-200/60 hover:bg-base-200 transition-colors"
                on:click={() => { selectedId.set(rel.id); focusPane.set('list') }}
              >
                <div class="text-sm font-medium text-base-content">{rel.title}</div>
                {#if rel.content}
                  <div class="text-xs text-base-content/50 truncate mt-0.5">
                    {rel.content.slice(0, 80)}
                  </div>
                {/if}
              </button>
            </li>
          {/each}
        </ul>
      {/if}
    </div>
  {:else}
    <div class="flex-1 flex items-center justify-center text-base-content/25 text-sm select-none">
      Select an item
    </div>
  {/if}
</div>
