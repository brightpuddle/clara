/**
 * scripts/assistant/platform/types.ts
 *
 * Internal chat & session multiplexer interfaces for Clara.
 * Abstracts Discord and Webex so channels/spaces can be managed interchangeably.
 */

export type PlatformType = "discord" | "webex";

export interface BoundSessionInfo {
  host: "eve" | "sophia";
  sessionName: string;
}

export interface ManagedChannel {
  id: string;
  name: string;
  topic?: string;
  boundSession?: BoundSessionInfo;
}

export interface ManagedAgentChannel {
  id: string;
  name: string;
  host: string;
  session: string;
}

export interface ChatPlatformAdapter {
  readonly platform: PlatformType;

  /**
   * List all channels or spaces currently managed for agent development.
   */
  listManagedChannels(): Promise<ManagedChannel[]>;

  /**
   * Find a managed channel by host and session name.
   */
  findChannelForSession(host: "eve" | "sophia", sessionName: string): Promise<ManagedChannel | null>;

  /**
   * Create or ensure a managed channel exists for a given host + tmux session.
   */
  ensureSessionChannel(host: "eve" | "sophia", sessionName: string): Promise<ManagedChannel>;

  /**
   * List channels/spaces bound to managed agent sessions (not tmux sessions).
   */
  listAgentChannels(): Promise<ManagedAgentChannel[]>;

  /**
   * Create a channel/space for a managed agent session (idempotent per session id).
   * `dir` is the session's working directory; Discord groups sessions into one
   * channel per project (git repo) and makes each session a thread in it.
   */
  ensureAgentChannel(
    host: string,
    session: string,
    name: string,
    dir?: string
  ): Promise<ManagedAgentChannel>;

  /**
   * Remove channels left over from older layouts, once their sessions have been
   * recreated in the current layout. Returns the removed channel names.
   */
  purgeLegacyAgentChannels?(): Promise<string[]>;

  /**
   * Delete a managed channel by its ID.
   */
  deleteChannel(channelId: string): Promise<void>;

  /**
   * Clean up all managed agent channels/spaces on this platform.
   * Returns the list of deleted channel names.
   */
  cleanupAllManagedChannels(): Promise<string[]>;

  /**
   * Send a message to a channel/space (handling chunking if necessary).
   */
  sendMessage(channelId: string, content: string): Promise<string>;

  /**
   * Add a pending or status reaction/indicator.
   * Returns an optional tracking ID (e.g. for platforms where the indicator is a temporary message).
   */
  addReaction(channelId: string, messageId: string, emoji: string): Promise<string | void>;

  /**
   * Remove a reaction/indicator.
   */
  removeReaction(channelId: string, messageId: string, emoji: string): Promise<void>;

  /**
   * Send a typing indicator.
   */
  sendTyping(channelId: string): Promise<void>;
}
