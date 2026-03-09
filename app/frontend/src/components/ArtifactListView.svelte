<script lang="ts">
  import { onMount, tick } from "svelte";
  import ArtifactRow from "./ArtifactRow.svelte";
  import {
    visibleArtifacts,
    selectedId,
    focusPane,
    searchQuery,
    selectedDetail,
    moveSelection,
    jumpTo,
  } from "../lib/store";
  import { getArtifact, search } from "../lib/agent";

  let listEl: HTMLElement;
  let searchInput: HTMLInputElement;
  let searching = false;

  // Load selected artifact detail whenever selection changes
  $: if ($selectedId) {
    getArtifact($selectedId)
      .then((d) => selectedDetail.set(d))
      .catch(() => {});
  } else {
    selectedDetail.set(null);
  }

  // Scroll selected item into view
  $: if ($selectedId && listEl) {
    tick().then(() => {
      const el = listEl.querySelector('[data-selected="true"]') as HTMLElement;
      el?.scrollIntoView({ block: "nearest" });
    });
  }

  function handleKey(e: KeyboardEvent) {
    if ($focusPane !== "list") return;
    switch (e.key) {
      case "j":
      case "ArrowDown":
        e.preventDefault();
        moveSelection(1);
        break;
      case "k":
      case "ArrowUp":
        e.preventDefault();
        moveSelection(-1);
        break;
      case "g":
        if ((e as any)._gg) {
          e.preventDefault();
          jumpTo("top");
        } else {
          (e as any)._gg = true;
          setTimeout(() => {
            delete (e as any)._gg;
          }, 300);
        }
        break;
      case "G":
        e.preventDefault();
        jumpTo("bottom");
        break;
      case "/":
        e.preventDefault();
        searching = true;
        tick().then(() => searchInput?.focus());
        break;
    }
  }

  async function runSearch() {
    if (!$searchQuery.trim()) return;
    await search($searchQuery);
  }

  function clearSearch() {
    searchQuery.set("");
    searching = false;
  }
</script>

<svelte:window on:keydown={handleKey} />

<div class="flex flex-col h-full" data-pane="list">
  <!-- Search bar -->
  <div class="px-3 py-2 border-b border-base-300/30">
    {#if searching}
      <div class="flex gap-1">
        <input
          bind:this={searchInput}
          bind:value={$searchQuery}
          on:keydown={(e) => {
            if (e.key === "Escape") clearSearch();
            if (e.key === "Enter") runSearch();
          }}
          class="input input-sm input-bordered flex-1 bg-base-200 text-sm"
          placeholder="Search…"
          autocomplete="off"
        />
        <button class="btn btn-sm btn-ghost" on:click={clearSearch}>✕</button>
      </div>
    {:else}
      <button
        class="w-full text-left text-sm text-base-content/30 bg-base-200/50 rounded-lg px-3 py-1.5 hover:bg-base-200 transition-colors"
        on:click={() => {
          searching = true;
          tick().then(() => searchInput?.focus());
        }}
      >
        🔍 Search…
      </button>
    {/if}
  </div>

  <!-- List -->
  <!-- svelte-ignore a11y-click-events-have-key-events -->
  <div
    bind:this={listEl}
    class="flex-1 overflow-y-auto"
    on:click={() => focusPane.set("list")}
  >
    {#each $visibleArtifacts as artifact (artifact.id)}
      <div data-selected={artifact.id === $selectedId}>
        <ArtifactRow
          {artifact}
          selected={artifact.id === $selectedId}
          on:click={() => {
            selectedId.set(artifact.id);
            focusPane.set("list");
          }}
        />
      </div>
    {/each}
    {#if $visibleArtifacts.length === 0}
      <div class="p-8 text-center text-base-content/30 text-sm">
        {$searchQuery ? "No results" : "No artifacts"}
      </div>
    {/if}
  </div>
</div>
