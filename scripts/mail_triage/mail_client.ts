// scripts/mail_triage/mail_client.ts
// Mail Client Abstraction supporting both Clara MCP Bridge and direct osascript execution.

import type { MailSummary, MailDetail } from "./types.ts";
import { callTool } from "../clara.ts";

export class AppleMailClient {
  /**
   * List unread messages in the inbox across an account (or default inbox).
   */
  public async listInbox(limit: number = 50, accountName?: string, unreadOnly: boolean = true): Promise<MailSummary[]> {
    try {
      // Try Clara MCP tool first
      const res = await callTool("mail_list_inbox" as any, {
        limit,
        account_name: accountName,
        unread: unreadOnly,
      });
      if (Array.isArray(res) && res.length > 0) {
        return res.map(m => ({ ...m, account_name: accountName }));
      }
    } catch {
      // Standalone osascript fallback for local testing / direct execution
    }

    return this.execAppleScriptListInbox(limit, accountName, unreadOnly);
  }

  /**
   * Fetch full message details including body and headers.
   */
  public async getMessage(messageId: string, accountName?: string): Promise<MailDetail> {
    try {
      const res = await callTool("mail_get_message" as any, {
        message_id: messageId,
      });
      if (res && res.id && res.subject !== undefined) {
        return { ...res, account_name: accountName };
      }
    } catch {
      // Standalone osascript fallback
    }

    return this.execAppleScriptGetMessage(messageId, accountName);
  }

  /**
   * Move a message to a specific destination mailbox.
   */
  public async moveMessage(messageId: string, targetMailbox: string, accountName: string): Promise<boolean> {
    try {
      const res = await callTool("mail_move" as any, {
        message_id: messageId,
        target_mailbox: targetMailbox,
        account_name: accountName,
      });
      if (res && (res.moved || res.status === "moved")) {
        return true;
      }
    } catch {
      // Fallback
    }

    return this.execAppleScriptMove(messageId, targetMailbox, accountName);
  }

  /**
   * Delete / trash a message.
   */
  public async deleteMessage(messageId: string): Promise<boolean> {
    try {
      const res = await callTool("mail_delete" as any, {
        message_id: messageId,
      });
      if (res && (res.deleted || res.status === "deleted")) {
        return true;
      }
    } catch {
      // Fallback
    }

    return this.execAppleScriptDelete(messageId);
  }

  /**
   * List messages in any mailbox with optional limit and date filtering.
   */
  public async listMailbox(
    mailboxName: string,
    accountName?: string,
    limit: number = 250,
    unreadOnly: boolean = false
  ): Promise<MailSummary[]> {
    return this.execAppleScriptListMailbox(mailboxName, accountName, limit, unreadOnly);
  }

  /**
   * Batch move multiple messages to a target mailbox in a single AppleScript execution.
   */
  public async batchMoveMessages(
    messageIds: string[],
    targetMailbox: string,
    accountName: string
  ): Promise<{ moved: number; failed: number }> {
    if (messageIds.length === 0) return { moved: 0, failed: 0 };
    return this.execAppleScriptBatchMove(messageIds, targetMailbox, accountName);
  }

  /**
   * Batch delete multiple messages in a single AppleScript execution.
   */
  public async batchDeleteMessages(messageIds: string[]): Promise<{ deleted: number; failed: number }> {
    if (messageIds.length === 0) return { deleted: 0, failed: 0 };
    return this.execAppleScriptBatchDelete(messageIds);
  }

  /**
   * Get all mailboxes for an account.
   */
  public async getMailboxes(accountName?: string): Promise<{ name: string; account: string }[]> {
    try {
      const res = await callTool("mail_get_mailboxes" as any, {
        account_name: accountName,
      });
      if (Array.isArray(res) && res.length > 0) {
        return res;
      }
    } catch {
      // Fallback
    }

    return this.execAppleScriptGetMailboxes(accountName);
  }

  // --- AppleScript Standalone Direct Fallbacks ---

  private async execAppleScript(script: string): Promise<string> {
    const proc = Bun.spawn(["osascript", "-e", script], {
      stdout: "pipe",
      stderr: "pipe",
    });
    const stdout = await new Response(proc.stdout).text();
    const stderr = await new Response(proc.stderr).text();
    const exitCode = await proc.exited;

    if (exitCode !== 0) {
      throw new Error(`AppleScript error: ${stderr.trim() || stdout.trim()}`);
    }
    return stdout.trim();
  }

  private async execAppleScriptListInbox(limit: number, accountName?: string, unreadOnly: boolean = true): Promise<MailSummary[]> {
    const script = `
tell application "Mail"
  set targetInbox to missing value
  ${accountName ? `
    try
      set theAcc to account "${accountName}"
      try
        set targetInbox to mailbox "Inbox" of theAcc
      on error
        try
          set targetInbox to mailbox "INBOX" of theAcc
        on error
          set targetInbox to (first mailbox of theAcc whose name is "Inbox" or name is "INBOX")
        end try
      end try
    on error
      set targetInbox to inbox
    end try
  ` : `
    set targetInbox to inbox
  `}
  if targetInbox is missing value then return ""
  
  if ${unreadOnly} then
    set theMessages to (messages of targetInbox whose read status is false)
    set msgCount to count of theMessages
    set fetchCount to msgCount
    if fetchCount > ${limit} then set fetchCount to ${limit}
    set outList to ""
    if fetchCount > 0 then
      repeat with i from 1 to fetchCount
        set m to item i of theMessages
        set mId to id of m
        set mSub to subject of m
        set mSender to sender of m
        set mDate to date received of m as string
        set mRead to read status of m
        set outList to outList & mId & "|||" & mSub & "|||" & mSender & "|||" & mDate & "|||" & mRead & "---MSG---"
      end repeat
    end if
    return outList
  else
    set msgCount to count of messages of targetInbox
    set startIndex to msgCount - ${limit - 1}
    if startIndex < 1 then set startIndex to 1
    set outList to ""
    if msgCount > 0 then
      repeat with i from msgCount to startIndex by -1
        set m to item i of (messages of targetInbox)
        set mId to id of m
        set mSub to subject of m
        set mSender to sender of m
        set mDate to date received of m as string
        set mRead to read status of m
        set outList to outList & mId & "|||" & mSub & "|||" & mSender & "|||" & mDate & "|||" & mRead & "---MSG---"
      end repeat
    end if
    return outList
  end if
end tell`;

    try {
      const raw = await this.execAppleScript(script);
      if (!raw) return [];
      const parts = raw.split("---MSG---").filter(p => p.trim().length > 0);
      return parts.map(part => {
        const [id, subject, sender, date_received, read_status] = part.split("|||");
        return {
          id: id?.trim() || "",
          subject: subject?.trim() || "",
          sender: sender?.trim() || "",
          date_received: date_received?.trim() || "",
          read_status: read_status?.trim() === "true",
          account_name: accountName,
        };
      });
    } catch (err: any) {
      console.warn(`[AppleMailClient] Error listing inbox: ${err.message}`);
      return [];
    }
  }

  private async execAppleScriptGetMessage(messageId: string, accountName?: string): Promise<MailDetail> {
    const script = `
tell application "Mail"
  set theMsg to missing value
  ${accountName ? `
    try
      set theAcc to account "${accountName}"
      repeat with mb in (every mailbox of theAcc)
        try
          set theMsg to (first message of mb whose id is ${messageId})
          if theMsg is not missing value then exit repeat
        end try
      end repeat
    end try
  ` : `
    repeat with acc in accounts
      repeat with mb in (every mailbox of acc)
        try
          set theMsg to (first message of mb whose id is ${messageId})
          if theMsg is not missing value then exit repeat
        end try
      end repeat
      if theMsg is not missing value then exit repeat
    end repeat
  `}
  if theMsg is missing value then
    try
      set theMsg to (first message of inbox whose id is ${messageId})
    end try
  end if
  if theMsg is missing value then error "Message ${messageId} not found"
  
  set mId to id of theMsg
  set mSub to subject of theMsg
  set mSender to sender of theMsg
  set mContent to content of theMsg
  set mDate to date received of m as string
  set mRead to read status of theMsg
  set mHeaders to ""
  try
    set mHeaders to all headers of theMsg
  end try
  return mId & "|||" & mSub & "|||" & mSender & "|||" & mDate & "|||" & mRead & "|||" & mHeaders & "|||" & mContent
end tell`;

    try {
      const raw = await this.execAppleScript(script);
      const [id, subject, sender, date_received, read_status, headers, ...contentParts] = raw.split("|||");
      const content = contentParts.join("|||");
      
      // Parse basic headers into key-value map
      const headersMap: Record<string, string> = {};
      if (headers) {
        const lines = headers.split("\n");
        for (const line of lines) {
          const colonIdx = line.indexOf(":");
          if (colonIdx > 0) {
            const k = line.substring(0, colonIdx).trim().toLowerCase();
            const v = line.substring(colonIdx + 1).trim();
            headersMap[k] = v;
          }
        }
      }

      return {
        id: id?.trim() || messageId,
        subject: subject?.trim() || "",
        sender: sender?.trim() || "",
        date_received: date_received?.trim() || "",
        read_status: read_status?.trim() === "true",
        content: content || "",
        headers: headersMap,
        raw_headers: headers,
        to: headersMap["to"],
        cc: headersMap["cc"],
        account_name: accountName,
      };
    } catch (err: any) {
      console.warn(`[AppleMailClient] Error getting message ${messageId}: ${err.message}`);
      return {
        id: messageId,
        subject: "",
        sender: "",
        date_received: "",
        read_status: false,
        content: "",
        account_name: accountName,
      };
    }
  }

  private async execAppleScriptMove(messageId: string, targetMailbox: string, accountName: string): Promise<boolean> {
    const script = `
tell application "Mail"
  set targetMB to missing value
  ${accountName ? `
    try
      set theAcc to account "${accountName}"
      set targetMB to mailbox "${targetMailbox}" of theAcc
    on error
      try
        set targetMB to (first mailbox of theAcc whose name is "${targetMailbox}")
      end try
    end try
  ` : `
    try
      set targetMB to mailbox "${targetMailbox}"
    end try
  `}
  if targetMB is missing value then error "Target mailbox ${targetMailbox} not found"

  repeat with acc in accounts
    repeat with mb in (every mailbox of acc)
      try
        set theMsg to (first message of mb whose id is ${messageId})
        if theMsg is not missing value then
          move theMsg to targetMB
          return "OK"
        end if
      end try
    end repeat
  end repeat
  error "Message ${messageId} not found"
end tell`;
    try {
      await this.execAppleScript(script);
      return true;
    } catch {
      return false;
    }
  }

  private async execAppleScriptDelete(messageId: string): Promise<boolean> {
    const script = `
tell application "Mail"
  repeat with acc in accounts
    repeat with mb in (every mailbox of acc)
      try
        set theMsg to (first message of mb whose id is ${messageId})
        if theMsg is not missing value then
          delete theMsg
          return "OK"
        end if
      end try
    end repeat
  end repeat
  error "Message ${messageId} not found"
end tell`;
    try {
      await this.execAppleScript(script);
      return true;
    } catch {
      return false;
    }
  }

  private async execAppleScriptGetMailboxes(accountName?: string): Promise<{ name: string; account: string }[]> {
    const script = `
tell application "Mail"
  set outList to ""
  ${accountName ? `
    set theAccount to account "${accountName}"
    set accName to name of theAccount
    repeat with b in (every mailbox of theAccount)
      set outList to outList & (name of b) & "|||" & accName & "---MB---"
    end repeat
  ` : `
    repeat with acc in accounts
      set accName to name of acc
      repeat with b in (every mailbox of acc)
        set outList to outList & (name of b) & "|||" & accName & "---MB---"
      end repeat
    end repeat
  `}
  return outList
end tell`;

    try {
      const raw = await this.execAppleScript(script);
      if (!raw) return [];
      const parts = raw.split("---MB---").filter(p => p.trim().length > 0);
      return parts.map(part => {
        const [name, account] = part.split("|||");
        return { name: name?.trim() || "", account: account?.trim() || "" };
      });
    } catch {
      return [];
    }
  }

  private async execAppleScriptListMailbox(
    mailboxName: string,
    accountName?: string,
    limit: number = 250,
    unreadOnly: boolean = false
  ): Promise<MailSummary[]> {
    const isInbox = mailboxName.toLowerCase() === "inbox";
    const script = `
tell application "Mail"
  set targetMB to missing value
  ${accountName ? `
    try
      set theAcc to account "${accountName}"
      try
        set targetMB to mailbox "${mailboxName}" of theAcc
      on error
        try
          set targetMB to (first mailbox of theAcc whose name is "${mailboxName}" or name is "${mailboxName.toUpperCase()}" or name is "${mailboxName.toLowerCase()}")
        end try
      end try
    end try
  ` : (isInbox ? `
    set targetMB to inbox
  ` : `
    try
      set targetMB to mailbox "${mailboxName}"
    on error
      try
        set targetMB to (first mailbox whose name is "${mailboxName}" or name is "${mailboxName.toUpperCase()}" or name is "${mailboxName.toLowerCase()}")
      end try
    end try
  `)}
  if targetMB is missing value then return ""
  
  if ${unreadOnly} then
    set theMessages to (messages of targetMB whose read status is false)
    set msgCount to count of theMessages
    set fetchCount to msgCount
    if fetchCount > ${limit} then set fetchCount to ${limit}
    set outList to ""
    if fetchCount > 0 then
      repeat with i from 1 to fetchCount
        set m to item i of theMessages
        set mId to id of m
        set mSub to subject of m
        set mSender to sender of m
        set mDate to date received of m as string
        set mRead to read status of m
        set outList to outList & mId & "|||" & mSub & "|||" & mSender & "|||" & mDate & "|||" & mRead & "---MSG---"
      end repeat
    end if
    return outList
  else
    set msgCount to count of messages of targetMB
    set startIndex to msgCount - ${limit - 1}
    if startIndex < 1 then set startIndex to 1
    set outList to ""
    if msgCount > 0 then
      repeat with i from msgCount to startIndex by -1
        set m to item i of (messages of targetMB)
        set mId to id of m
        set mSub to subject of m
        set mSender to sender of m
        set mDate to date received of m as string
        set mRead to read status of m
        set outList to outList & mId & "|||" & mSub & "|||" & mSender & "|||" & mDate & "|||" & mRead & "---MSG---"
      end repeat
    end if
    return outList
  end if
end tell`;

    try {
      const raw = await this.execAppleScript(script);
      if (!raw) return [];
      const parts = raw.split("---MSG---").filter(p => p.trim().length > 0);
      return parts.map(part => {
        const [id, subject, sender, date_received, read_status] = part.split("|||");
        return {
          id: id?.trim() || "",
          subject: subject?.trim() || "",
          sender: sender?.trim() || "",
          date_received: date_received?.trim() || "",
          read_status: read_status?.trim() === "true",
          account_name: accountName,
        };
      });
    } catch (err: any) {
      console.warn(`[AppleMailClient] Error listing mailbox "${mailboxName}": ${err.message}`);
      return [];
    }
  }

  private async execAppleScriptBatchMove(
    messageIds: string[],
    targetMailbox: string,
    accountName: string
  ): Promise<{ moved: number; failed: number }> {
    const idListStr = messageIds.map(id => `"${id}"`).join(", ");
    const script = `
tell application "Mail"
  set targetMB to missing value
  ${accountName ? `
    try
      set targetMB to mailbox "${targetMailbox}" of account "${accountName}"
    on error
      try
        set targetMB to (first mailbox of account "${accountName}" whose name is "${targetMailbox}")
      end try
    end try
  ` : `
    try
      set targetMB to mailbox "${targetMailbox}"
    end try
  `}
  if targetMB is missing value then return "0|${messageIds.length}"
  set idList to {${idListStr}}
  set movedCount to 0
  set failCount to 0
  repeat with msgId in idList
    try
      repeat with acc in accounts
        repeat with mb in (every mailbox of acc)
          try
            set theMsg to (first message of mb whose id is (msgId as integer))
            if theMsg is not missing value then
              move theMsg to targetMB
              set movedCount to movedCount + 1
              exit repeat
            end if
          end try
        end repeat
      end repeat
    on error
      set failCount to failCount + 1
    end try
  end repeat
  return (movedCount as string) & "|" & (failCount as string)
end tell`;

    try {
      const res = await this.execAppleScript(script);
      const [moved, failed] = res.split("|").map(n => parseInt(n || "0", 10));
      return { moved: moved || 0, failed: failed || 0 };
    } catch {
      return { moved: 0, failed: messageIds.length };
    }
  }

  private async execAppleScriptBatchDelete(
    messageIds: string[]
  ): Promise<{ deleted: number; failed: number }> {
    const idListStr = messageIds.map(id => `"${id}"`).join(", ");
    const script = `
tell application "Mail"
  set idList to {${idListStr}}
  set delCount to 0
  set failCount to 0
  repeat with msgId in idList
    try
      repeat with acc in accounts
        repeat with mb in (every mailbox of acc)
          try
            set theMsg to (first message of mb whose id is (msgId as integer))
            if theMsg is not missing value then
              delete theMsg
              set delCount to delCount + 1
              exit repeat
            end if
          end try
        end repeat
      end repeat
    on error
      set failCount to failCount + 1
    end try
  end repeat
  return (delCount as string) & "|" & (failCount as string)
end tell`;

    try {
      const res = await this.execAppleScript(script);
      const [deleted, failed] = res.split("|").map(n => parseInt(n || "0", 10));
      return { deleted: deleted || 0, failed: failed || 0 };
    } catch {
      return { deleted: 0, failed: messageIds.length };
    }
  }
}
