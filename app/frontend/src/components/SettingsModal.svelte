<script lang="ts">
  import { showSettings } from '../lib/store'

  // Close on Escape
  function handleKey(e: KeyboardEvent) {
    if (e.key === 'Escape') showSettings.set(false)
  }
</script>

<svelte:window on:keydown={handleKey} />

{#if $showSettings}
  <!-- Backdrop -->
  <div
    class="fixed inset-0 bg-black/50 z-40 flex items-center justify-center"
    on:click|self={() => showSettings.set(false)}
  >
    <!-- Modal -->
    <div class="bg-base-200 rounded-xl shadow-2xl w-[520px] max-h-[80vh] overflow-y-auto z-50">
      <!-- Header -->
      <div class="flex items-center justify-between px-6 py-4 border-b border-base-300/50">
        <h2 class="text-base font-semibold">Settings</h2>
        <button class="btn btn-sm btn-ghost btn-circle" on:click={() => showSettings.set(false)}>✕</button>
      </div>

      <!-- Tabs -->
      <div class="tabs tabs-bordered px-6 pt-4">
        <button class="tab tab-active">General</button>
        <button class="tab">Integrations</button>
        <button class="tab">AI</button>
      </div>

      <!-- Body -->
      <div class="px-6 py-5 space-y-5">
        <!-- General tab -->
        <div class="form-control">
          <label class="label" for="data-dir">
            <span class="label-text text-sm">Data directory</span>
          </label>
          <input
            id="data-dir"
            type="text"
            class="input input-bordered input-sm bg-base-100"
            placeholder="~/.local/share/clara"
          />
          <label class="label">
            <span class="label-text-alt text-base-content/40">
              Location for the SQLite database and Unix sockets
            </span>
          </label>
        </div>

        <div class="form-control">
          <label class="label" for="theme">
            <span class="label-text text-sm">Theme</span>
          </label>
          <select id="theme" class="select select-bordered select-sm bg-base-100">
            <option value="system">System</option>
            <option value="dark">Dark</option>
            <option value="light">Light</option>
          </select>
        </div>

        <div class="divider text-xs text-base-content/40">AI</div>

        <div class="form-control">
          <label class="label" for="ollama-url">
            <span class="label-text text-sm">Ollama URL</span>
          </label>
          <input
            id="ollama-url"
            type="text"
            class="input input-bordered input-sm bg-base-100"
            placeholder="http://localhost:11434"
          />
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
          />
        </div>
      </div>

      <!-- Footer -->
      <div class="flex justify-end gap-2 px-6 pb-5">
        <button class="btn btn-sm btn-ghost" on:click={() => showSettings.set(false)}>
          Cancel
        </button>
        <button class="btn btn-sm btn-primary" on:click={() => showSettings.set(false)}>
          Save
        </button>
      </div>
    </div>
  </div>
{/if}
