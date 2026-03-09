/**
 * store.ts — Svelte stores for global application state.
 */
import { writable, derived, get } from 'svelte/store'
import type { Artifact, ArtifactDetail, Status } from './agent'

// ── Connection ──────────────────────────────────────────────────────────────
export const connected = writable<boolean>(false)

// ── Sidebar section ─────────────────────────────────────────────────────────
export type Section = 'all' | 'notes' | 'tasks' | 'files' | 'reminders'

export const activeSection = writable<Section>('all')

// ── Artifacts ───────────────────────────────────────────────────────────────
export const artifacts = writable<Artifact[]>([])

export const selectedId = writable<string | null>(null)

export const selectedDetail = writable<ArtifactDetail | null>(null)

export const searchQuery = writable<string>('')

// Derived: filter artifacts by active section
export const visibleArtifacts = derived(
  [artifacts, activeSection, searchQuery],
  ([$artifacts, $section, $query]) => {
    let list = $artifacts
    if ($section !== 'all') {
      list = list.filter((a) => a.kind === $section)
    }
    if ($query) {
      const q = $query.toLowerCase()
      list = list.filter(
        (a) =>
          a.title.toLowerCase().includes(q) ||
          a.content.toLowerCase().includes(q)
      )
    }
    return list
  }
)

// ── Status ───────────────────────────────────────────────────────────────────
export const status = writable<Status | null>(null)

// ── Focus / keyboard navigation ─────────────────────────────────────────────
export type FocusPane = 'sidebar' | 'list' | 'detail'
export const focusPane = writable<FocusPane>('list')

// ── Settings modal ───────────────────────────────────────────────────────────
export const showSettings = writable<boolean>(false)

// ── Helpers ──────────────────────────────────────────────────────────────────

/** Apply a Subscribe stream event to the artifacts store. */
export function applyArtifactEvent(
  type: 'created' | 'updated' | 'deleted',
  artifact: Artifact
): void {
  artifacts.update((list) => {
    if (type === 'deleted') {
      return list.filter((a) => a.id !== artifact.id)
    }
    const idx = list.findIndex((a) => a.id === artifact.id)
    if (idx >= 0) {
      const next = [...list]
      next[idx] = artifact
      return next.sort((a, b) => b.heat_score - a.heat_score)
    }
    return [artifact, ...list].sort((a, b) => b.heat_score - a.heat_score)
  })
}

/** Move the list selection by delta (-1 or +1). Returns the new selected ID. */
export function moveSelection(delta: number): string | null {
  const list = get(visibleArtifacts)
  if (list.length === 0) return null
  const current = get(selectedId)
  const idx = list.findIndex((a) => a.id === current)
  const next = Math.max(0, Math.min(list.length - 1, idx + delta))
  const id = list[next]?.id ?? null
  selectedId.set(id)
  return id
}

/** Jump to top or bottom of the visible list. */
export function jumpTo(position: 'top' | 'bottom'): string | null {
  const list = get(visibleArtifacts)
  if (list.length === 0) return null
  const id = position === 'top' ? list[0].id : list[list.length - 1].id
  selectedId.set(id)
  return id
}
