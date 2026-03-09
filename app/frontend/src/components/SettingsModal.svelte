<script lang="ts">
  import Icon from '@iconify/svelte/dist/Icon.svelte'
  import { showSettings } from "../lib/store"

  type Tab = 'general' | 'integrations' | 'ai'
  let activeTab: Tab = 'general'

  // Form state (non-reactive, saved on submit)
  let dataDir = ''
  let theme = 'system'
  let ollamaUrl = 'http://localhost:11434'
  let embedModel = 'nomic-embed-text'
  let watchDirs = ''
  let remindersEnabled = true
  let taskwarriorEnabled = false

  function handleKey(e: KeyboardEvent) {
    if (e.key === "Escape") showSettings.set(false)
  }

  const tabs: { id: Tab; label: string; icon: string }[] = [
    { id: 'general',      label: 'General',      icon: 'mdi:tune-vertical' },
    { id: 'integrations', label: 'Integrations', icon: 'mdi:puzzle-outline' },
    { id: 'ai',           label: 'AI',           icon: 'mdi:brain' },
  ]
</script>

<svelte:window on:keydown={handleKey} />

{#if $showSettings}
  <!-- Backdrop -->
  <!-- svelte-ignore a11y-click-events-have-key-events -->
  <div
    class="fixed inset-0 bg-black/50 z-40 flex items-center justify-center"
    on:click|self={() => showSettings.set(false)}
  >
    <!-- Modal -->
    <div class="bg-base-200 rounded-xl shadow-2xl w-[520px] max-h-[80vh] flex flex-col z-50">
      <!-- Header -->
      <div class="flex items-center justify-between px-6 py-4 border-b border-base-300/50 shrink-0">
        <h2 class="text-base font-semibold">Settings</h2>
        <button class="btn btn-sm btn-ghost btn-circle" on:click={() => showSettings.set(false)}>
          <Icon icon="mdi:close" class="w-4 h-4" />
        </button>
      </div>

      <!-- Tabs -->
      <div class="flex border-b border-base-300/50 shrink-0 px-2">
        {#each tabs as t}
          <button
            class="flex items-center gap-2 px-4 py-3 text-sm transition-colors border-b-2
              {activeTab === t.id
                ? 'border-primary text-primary'
                : 'border-transparent text-base-content/60 hover:text-base-content'}"
            on:click={() => (activeTab = t.id)}
          >
            <Icon icon={t.icon} class="w-4 h-4" />
            {t.label}
          </button>
        {/each}
      </div>

      <!-- Body (scrollable) -->
      <div class="flex-1 overflow-y-auto px-6 py-5 space-y-5">

        {#if activeTab === 'general'}
          <div class="form-control">
            <label class="label" for="data-dir">
              <span class="label-text text-sm">Data directory</span>
            </label>
            <input
              id="data-dir"
              type="text"
              class="input input-bordered input-sm bg-base-100"
              placeholder="~/.local/share/clara"
              bind:value={dataDir}
            />
            <!-- svelte-ignore a11y-label-has-associated-control -->
            <label class="label">
              <span class="label-text-alt text-base-content/40">
                Location for the SQLite database and Unix sockets
              </span>
            </label>
          </div>

          <div class="form-control">
            <label class="label" for="theme-sel">
              <span class="label-text text-sm">Theme</span>
            </label>
            <select id="theme-sel" class="select select-bordered select-sm bg-base-100" bind:value={theme}>
              <option value="system">System</option>
              <option value="dark">Dark</option>
              <option value="light">Light</option>
            </select>
          </div>

        {:else if activeTab === 'integrations'}
          <p class="text-xs text-base-content/40 uppercase tracking-wide font-semibold">Filesystem</p>

          <div class="form-control">
            <label class="label" for="watch-dirs">
              <span class="label-text text-sm">Watch directories</span>
            </label>
            <textarea
              id="watch-dirs"
              class="textarea textarea-bordered textarea-sm bg-base-100 font-mono text-xs"
              rows="4"
              placeholder="~/Documents/notes&#10;~/Projects"
              bind:value={watchDirs}
            />
            <!-- svelte-ignore a11y-label-has-associated-control -->
            <label class="label">
              <span class="label-text-alt text-base-content/40">One path per line</span>
            </label>
          </div>

          <div class="divider my-1 text-xs text-base-content/40">System Integrations</div>

          <div class="flex items-center justify-between">
            <div>
              <p class="text-sm font-medium">Reminders</p>
              <p class="text-xs text-base-content/50">Sync Apple Reminders</p>
            </div>
            <input type="checkbox" class="toggle toggle-sm toggle-primary" bind:checked={remindersEnabled} />
          </div>

          <div class="flex items-center justify-between">
            <div>
              <p class="text-sm font-medium">TaskWarrior</p>
              <p class="text-xs text-base-content/50">Sync TaskWarrior tasks from ~/.task</p>
            </div>
            <input type="checkbox" class="toggle toggle-sm toggle-primary" bind:checked={taskwarriorEnabled} />
          </div>

        {:else if activeTab === 'ai'}
          <div class="form-control">
            <label class="label" for="ollama-url">
              <span class="label-text text-sm">Ollama URL</span>
            </label>
            <input
              id="ollama-url"
              type="text"
              class="input input-bordered input-sm bg-base-100"
              placeholder="http://localhost:11434"
              bind:value={ollamaUrl}
            />
            <!-- svelte-ignore a11y-label-has-associated-control -->
            <label class="label">
              <span class="label-text-alt text-base-content/40">Ollama API endpoint for embeddings</span>
            </label>
          </div>

          <div class="form-control">
            <label class="label" for="embed-model">
              <span class="label-text text-sm">Embedding model</span>
            </label>
            <input
              id="embed-model"
              type="text"
              class="input input-bordered input-sm bg-base-100"
              placeholder="nomic-embed-text"
              bind:value={embedModel}
            />
            <!-- svelte-ignore a11y-label-has-associated-control -->
            <label class="label">
              <span class="label-text-alt text-base-content/40">
                Must be pulled via <code class="font-mono">ollama pull nomic-embed-text</code>
              </span>
            </label>
          </div>
        {/if}
      </div>

      <!-- Footer -->
      <div class="flex justify-end gap-2 px-6 pb-5 shrink-0 border-t border-base-300/30 pt-4">
        <button class="btn btn-sm btn-ghost" on:click={() => showSettings.set(false)}>
          Cancel
        </button>
        <button class="btn btn-sm btn-primary" on:click={() => showSettings.set(false)}>
          <Icon icon="mdi:content-save-outline" class="w-3.5 h-3.5" />
          Save
        </button>
      </div>
    </div>
  </div>
{/if}
