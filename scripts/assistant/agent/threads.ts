/**
 * scripts/assistant/agent/threads.ts
 *
 * Maps Discord threads to managed agent sessions. Threads carry no topic, so the
 * binding lives in the assistant state database, which every script shares.
 */

import { Database } from "bun:sqlite";
import { mkdirSync } from "node:fs";
import { homedir } from "node:os";
import { dirname, join } from "node:path";

export interface ThreadBinding {
  threadId: string;
  parentId: string;
  host: string;
  session: string;
  project: string;
  name: string;
}

export class ThreadStore {
  constructor(private db: Database) {
    db.exec(`
      CREATE TABLE IF NOT EXISTS agent_threads (
        thread_id TEXT PRIMARY KEY,
        parent_id TEXT NOT NULL,
        host TEXT NOT NULL,
        session TEXT NOT NULL,
        project TEXT NOT NULL,
        name TEXT NOT NULL
      );
      CREATE INDEX IF NOT EXISTS agent_threads_session ON agent_threads (host, session);
    `);
  }

  put(b: ThreadBinding): void {
    this.db
      .query(
        "INSERT OR REPLACE INTO agent_threads (thread_id, parent_id, host, session, project, name) VALUES (?, ?, ?, ?, ?, ?)"
      )
      .run(b.threadId, b.parentId, b.host, b.session, b.project, b.name);
  }

  byThread(threadId: string): ThreadBinding | null {
    const r = this.db.query("SELECT * FROM agent_threads WHERE thread_id = ?").get(threadId);
    return r ? rowToBinding(r) : null;
  }

  list(): ThreadBinding[] {
    return this.db.query("SELECT * FROM agent_threads").all().map(rowToBinding);
  }

  remove(threadId: string): void {
    this.db.query("DELETE FROM agent_threads WHERE thread_id = ?").run(threadId);
  }
}

function rowToBinding(r: any): ThreadBinding {
  return {
    threadId: r.thread_id,
    parentId: r.parent_id,
    host: r.host,
    session: r.session,
    project: r.project,
    name: r.name,
  };
}

let shared: ThreadStore | null = null;

/** The process-wide store backed by the shared assistant state database. */
export function threadStore(): ThreadStore {
  if (!shared) {
    const path = join(homedir(), ".local/share/clara/assistant_state.db");
    mkdirSync(dirname(path), { recursive: true });
    const db = new Database(path);
    db.exec("PRAGMA busy_timeout = 5000");
    shared = new ThreadStore(db);
  }
  return shared;
}
