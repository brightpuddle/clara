/**
 * scripts/mail_triage/satellite.ts
 *
 * Utilities for probing satellite (Sophia) status and executing tools safely
 * with timeouts and disconnect recovery.
 */

import { callTool } from "../clara.ts";

export const DEFAULT_SATELLITE_TIMEOUT_MS = 8000;

/**
 * Check if the satellite (Sophia) Mail capability is currently responsive.
 */
export async function isSatelliteMailAvailable(timeoutMs = 3000): Promise<boolean> {
  try {
    const res = await Promise.race([
      callTool("laptop.macos.mail_list_inbox" as any, { limit: 1 }),
      new Promise((_, reject) =>
        setTimeout(() => reject(new Error("Satellite connection timed out")), timeoutMs)
      ),
    ]);
    if (res && (res as any).error) return false;
    return Array.isArray(res);
  } catch {
    return false;
  }
}

/**
 * Invoke a tool with a strict timeout to avoid hanging during sleep/disconnect.
 */
export async function callToolWithTimeout<T = any>(
  toolName: string,
  args: Record<string, any>,
  timeoutMs = DEFAULT_SATELLITE_TIMEOUT_MS
): Promise<T> {
  return Promise.race([
    callTool(toolName as any, args),
    new Promise<T>((_, reject) =>
      setTimeout(
        () => reject(new Error(`Tool call '${toolName}' timed out after ${timeoutMs}ms`)),
        timeoutMs
      )
    ),
  ]);
}
