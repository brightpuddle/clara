/**
 * scripts/assistant/agent/naming.ts
 *
 * Thread/space naming for sessions. A name has a source: a generated fallback,
 * the first prompt, or an explicit title. A stronger source replaces a weaker one.
 */

export const NAME_FALLBACK = 0;
export const NAME_PROMPT = 1;
export const NAME_TITLE = 2;
export type NameSource = typeof NAME_FALLBACK | typeof NAME_PROMPT | typeof NAME_TITLE;

/** A prompt only names a session that still has its generated fallback name. */
export function shouldRename(current: number, incoming: NameSource, same: boolean): boolean {
  if (same) return false;
  if (incoming === NAME_PROMPT) return current === NAME_FALLBACK;
  return incoming === NAME_TITLE && current <= NAME_TITLE;
}

/** A short readable name from the first line of a prompt, or null if unusable. */
export function promptName(text: string, max = 40): string | null {
  const first = String(text || "")
    .split("\n")
    .map((l) => l.trim())
    .find(Boolean);
  if (!first || first.startsWith("/")) return null;
  const cleaned = first
    .replace(/https?:\/\/\S+/g, "")
    .replace(/[`*_>#~]/g, "")
    .replace(/\s+/g, " ")
    .trim();
  if (cleaned.length < 3) return null;
  if (cleaned.length <= max) return cleaned;
  const cut = cleaned.slice(0, max);
  const at = cut.lastIndexOf(" ");
  return (at > max / 2 ? cut.slice(0, at) : cut).replace(/[\s,.;:-]+$/, "") + "…";
}

export function titleName(title: string, max = 80): string | null {
  const t = String(title || "").replace(/\s+/g, " ").trim();
  return t ? t.slice(0, max) : null;
}
