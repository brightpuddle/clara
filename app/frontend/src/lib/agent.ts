/**
 * agent.ts — typed wrappers around Wails Go bindings + event listeners.
 *
 * The Wails runtime exposes bound Go methods at window.go.main.App.*
 * Real-time events are pushed via Wails EventsEmit and received with EventsOn.
 */

// Wails runtime imports (provided by wails dev server / production build)
import { EventsOn } from '../../wailsjs/runtime/runtime'
import {
  ListArtifacts,
  GetArtifact,
  MarkDone,
  Search,
  GetStatus,
  OpenNative,
  ShowWindow,
} from '../../wailsjs/go/main/App'

export interface Artifact {
  id: string
  kind: string
  title: string
  content: string
  source_path: string
  source_app: string
  heat_score: number
  tags: string[]
  due_at?: number // unix seconds
}

export interface ArtifactDetail {
  artifact: Artifact
  related: Artifact[]
}

export interface ComponentStatus {
  connected: boolean
  state: string
  uptime_seconds: number
  fault?: string
}

export interface Status {
  agent: ComponentStatus
  native: ComponentStatus
  artifact_counts: Record<string, number>
}

export type ArtifactEvent = {
  type: 'created' | 'updated' | 'deleted'
  artifact: Artifact
}

// Re-export Go bindings with proper types
export const listArtifacts = (kinds: string[]): Promise<Artifact[]> =>
  ListArtifacts(kinds) as Promise<Artifact[]>

export const getArtifact = (id: string): Promise<ArtifactDetail> =>
  GetArtifact(id) as Promise<ArtifactDetail>

export const markDone = (id: string): Promise<void> =>
  MarkDone(id) as Promise<void>

export const search = (query: string, limit = 50): Promise<Artifact[]> =>
  Search(query, limit) as Promise<Artifact[]>

export const getStatus = (): Promise<Status> =>
  GetStatus() as Promise<Status>

export const openNative = (artifact: Artifact): Promise<void> =>
  OpenNative(artifact) as Promise<void>

export const showWindow = (): Promise<void> =>
  ShowWindow() as Promise<void>

// Subscribe to real-time artifact events from the agent's Subscribe stream
export function onArtifactEvent(cb: (event: ArtifactEvent) => void): () => void {
  const offCreated = EventsOn('artifact:created', (a: Artifact) =>
    cb({ type: 'created', artifact: a })
  )
  const offUpdated = EventsOn('artifact:updated', (a: Artifact) =>
    cb({ type: 'updated', artifact: a })
  )
  const offDeleted = EventsOn('artifact:deleted', (a: Artifact) =>
    cb({ type: 'deleted', artifact: a })
  )
  return () => {
    offCreated()
    offUpdated()
    offDeleted()
  }
}

export function onConnectionChange(cb: (connected: boolean) => void): () => void {
  const offConn = EventsOn('agent:connected', () => cb(true))
  const offDisconn = EventsOn('agent:disconnected', () => cb(false))
  return () => {
    offConn()
    offDisconn()
  }
}

// Listen for system theme changes emitted by the Go backend (via native bridge)
// Payload: true = dark, false = light
export function onThemeChange(cb: (dark: boolean) => void): () => void {
  return EventsOn('theme:changed', (dark: boolean) => cb(dark))
}
