// scripts/mail_triage/mail_client.ts
// Mail Client Abstraction supporting both local and remote (Sophia satellite) Apple Mail MCP tools.

import type { MailSummary, MailDetail } from "./types.ts";
import { callTool } from "../clara.ts";
import { callToolWithTimeout, DEFAULT_SATELLITE_TIMEOUT_MS } from "./satellite.ts";

export type MailTargetHost = "local" | "laptop";

export class AppleMailClient {
  private targetHost: MailTargetHost;

  constructor(targetHost: MailTargetHost = "local") {
    this.targetHost = targetHost;
  }

  public getTargetHost(): MailTargetHost {
    return this.targetHost;
  }

  public setTargetHost(target: MailTargetHost) {
    this.targetHost = target;
  }

  private toolName(baseTool: string): string {
    return this.targetHost === "laptop" ? `laptop.${baseTool}` : baseTool;
  }

  /**
   * List unread messages in the inbox across an account (or default inbox).
   */
  public async listInbox(limit: number = 50, accountName?: string, unreadOnly: boolean = true): Promise<MailSummary[]> {
    try {
      const tool = this.toolName("macos.mail_list_inbox");
      const res = await callToolWithTimeout(tool, {
        limit,
        account_name: accountName,
        unread: unreadOnly,
      }, this.targetHost === "laptop" ? 10000 : 5000);

      if (Array.isArray(res) && res.length > 0) {
        return res.map(m => ({
          id: String(m.ID ?? m.id),
          message_id: m.message_id ? String(m.message_id).replace(/^<|>$/g, "").trim() : undefined,
          subject: m.subject || "",
          sender: m.sender || "",
          date_received: m.date_received || "",
          read_status: m.read_status === true || m.read_status === "true",
          account_name: accountName,
        }));
      }
      if (Array.isArray(res) && res.length === 0) {
        return [];
      }
    } catch (err: any) {
      if (this.targetHost === "laptop") {
        console.warn(`[mail_client] Remote satellite error on listInbox: ${err.message}`);
        throw err; // Propagate disconnect so engine halts gracefully
      }
    }

    return this.execAppleScriptListInbox(limit, accountName, unreadOnly);
  }

  /**
   * Fetch full message details including body and headers.
   */
  public async getMessage(messageId: string, accountName?: string): Promise<MailDetail> {
    try {
      const tool = this.toolName("macos.mail_get_message");
      const res = await callToolWithTimeout(tool, {
        message_id: messageId,
      }, this.targetHost === "laptop" ? 10000 : 5000);

      if (res && (res.id !== undefined || res.ID !== undefined) && res.subject !== undefined) {
        const rawMsgId = res.message_id ? String(res.message_id).replace(/^<|>$/g, "").trim() : undefined;
        return {
          id: String(res.ID ?? res.id),
          message_id: rawMsgId,
          subject: res.subject || "",
          sender: res.sender || "",
          date_received: res.date_received || "",
          read_status: res.read_status === true || res.read_status === "true",
          content: res.content || "",
          account_name: accountName,
        };
      }
    } catch (err: any) {
      if (this.targetHost === "laptop") {
        console.warn(`[mail_client] Remote satellite error on getMessage (${messageId}): ${err.message}`);
        throw err;
      }
    }

    return this.execAppleScriptGetMessage(messageId, accountName);
  }

  /**
   * Move a message to a specific destination mailbox.
   */
  public async moveMessage(messageId: string, targetMailbox: string, accountName: string): Promise<boolean> {
    try {
      const tool = this.toolName("macos.mail_move");
      const res = await callToolWithTimeout(tool, {
        message_id: messageId,
        target_mailbox: targetMailbox,
        account_name: accountName,
      }, this.targetHost === "laptop" ? 10000 : 5000);

      if (res && (res.moved || res.status === "moved")) {
        return true;
      }
    } catch (err: any) {
      if (this.targetHost === "laptop") {
        console.warn(`[mail_client] Remote satellite error on moveMessage (${messageId}): ${err.message}`);
        throw err;
      }
    }

    return this.execAppleScriptMove(messageId, targetMailbox, accountName);
  }

  /**
   * Delete / trash a message.
   */
  public async deleteMessage(messageId: string): Promise<boolean> {
    try {
      const tool = this.toolName("macos.mail_delete");
      const res = await callToolWithTimeout(tool, {
        message_id: messageId,
      }, this.targetHost === "laptop" ? 10000 : 5000);

      if (res && (res.deleted || res.status === "deleted")) {
        return true;
      }
    } catch (err: any) {
      if (this.targetHost === "laptop") {
        console.warn(`[mail_client] Remote satellite error on deleteMessage (${messageId}): ${err.message}`);
        throw err;
      }
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
      const tool = this.toolName("macos.mail_get_mailboxes");
      const res = await callToolWithTimeout(tool, {
        account_name: accountName,
      }, this.targetHost === "laptop" ? 10000 : 5000);

      if (Array.isArray(res) && res.length > 0) {
        return res;
      }
      if (Array.isArray(res) && res.length === 0) {
        return [];
      }
    } catch (err: any) {
      if (this.targetHost === "laptop") {
        console.warn(`[mail_client] Remote satellite error on getMailboxes: ${err.message}`);
        throw err;
      }
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
  
  set msgList to {}
  if ${unreadOnly} then
    set theMessages to (messages of targetInbox whose read status is false)
    set msgCount to count of theMessages
    set fetchCount to msgCount
    if fetchCount > ${limit} then set fetchCount to ${limit}
    if fetchCount > 0 then
      repeat with i from 1 to fetchCount
        set m to item i of theMessages
        set mId to id of m as string
        set mSub to subject of m
        set mSender to sender of m
        set mDate to date received of m as string
        set mRead to read status of m as string
        set mMsgId to ""
        try
          set mMsgId to message id of m as string
        end try
        set end of msgList to mId & "|||" & mSub & "|||" & mSender & "|||" & mDate & "|||" & mRead & "|||" & mMsgId
      end repeat
    end if
  else
    set msgCount to count of messages of targetInbox
    set startIndex to msgCount - ${limit - 1}
    if startIndex < 1 then set startIndex to 1
    if msgCount > 0 then
      repeat with i from msgCount to startIndex by -1
        set m to item i of (messages of targetInbox)
        set mId to id of m as string
        set mSub to subject of m
        set mSender to sender of m
        set mDate to date received of m as string
        set mRead to read status of m as string
        set mMsgId to ""
        try
          set mMsgId to message id of m as string
        end try
        set end of msgList to mId & "|||" & mSub & "|||" & mSender & "|||" & mDate & "|||" & mRead & "|||" & mMsgId
      end repeat
    end if
  end if
  
  set AppleScript's text item delimiters to "---MSG---"
  set rawOut to msgList as string
  set AppleScript's text item delimiters to ""
  return rawOut
end tell`;

    try {
      const raw = await this.execAppleScript(script);
      if (!raw) return [];
      const parts = raw.split("---MSG---").filter(p => p.trim().length > 0);
      return parts.map(part => {
        const [id, subject, sender, date_received, read_status, message_id] = part.split("|||");
        return {
          id: id?.trim() || "",
          message_id: message_id ? message_id.replace(/^<|>$/g, "").trim() : undefined,
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
  
  set mId to (id of theMsg) as string
  set mSub to (subject of theMsg) as string
  set mSender to (sender of theMsg) as string
  set mContent to (content of theMsg) as string
  set mDate to (date received of theMsg) as string
  set mRead to (read status of theMsg) as string
  set mHeaders to ""
  try
    set mHeaders to (all headers of theMsg) as string
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

      const rawMsgId = headersMap["message-id"] ? headersMap["message-id"].replace(/^<|>$/g, "").trim() : undefined;

      return {
        id: id?.trim() || messageId,
        message_id: rawMsgId,
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
on resolveMailbox(theAcc, folderName)
  tell application "Mail"
    try
      return mailbox folderName of theAcc
    end try
    try
      return (first mailbox of theAcc whose name is folderName)
    end try
    if folderName is "Archive" or folderName is "All Mail" then
      try
        return (first mailbox of theAcc whose name is "Archive" or name is "All Mail" or name is "Archived")
      end try
    else if folderName is "@Review" or folderName is "Review" then
      try
        return (first mailbox of theAcc whose name is "@Review" or name is "Review")
      on error
        try
          return (make new mailbox at theAcc with properties {name:"@Review"})
        end try
      end try
    else if folderName is "Deleted Items" or folderName is "Trash" or folderName is "Bin" then
      try
        return (first mailbox of theAcc whose name is "Deleted Items" or name is "Trash" or name is "Bin" or name is "Deleted Messages")
      end try
    end if
    return missing value
  end tell
end resolveMailbox

tell application "Mail"
  set theMsg to missing value
  ${accountName ? `
    try
      set theAcc to account "${accountName}"
      try
        set theMsg to (first message of (mailbox "INBOX" of theAcc) whose id is ${messageId})
      on error
        try
          set theMsg to (first message of (mailbox "Inbox" of theAcc) whose id is ${messageId})
        end try
      end try
    end try
  ` : `
    try
      set theMsg to (first message of inbox whose id is ${messageId})
    end try
  `}

  if theMsg is missing value then
    try
      set theMsg to (first message of inbox whose id is ${messageId})
    end try
  end if

  if theMsg is missing value then
    repeat with acc in accounts
      repeat with mb in (every mailbox of acc)
        try
          set theMsg to (first message of mb whose id is ${messageId})
          if theMsg is not missing value then exit repeat
        end try
      end repeat
      if theMsg is not missing value then exit repeat
    end repeat
  end if

  if theMsg is missing value then error "Message ${messageId} not found"

  ${accountName ? `
    set theAcc to account "${accountName}"
  ` : `
    set theAcc to account of mailbox of theMsg
  `}
  
  set targetMB to my resolveMailbox(theAcc, "${targetMailbox}")
  if targetMB is missing value then error "Target mailbox ${targetMailbox} not found on account"

  move theMsg to targetMB
  try
    set read status of theMsg to true
  end try
  return "OK"
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
  set theMsg to missing value
  try
    set theMsg to (first message of inbox whose id is ${messageId})
  end try

  if theMsg is missing value then
    repeat with acc in accounts
      repeat with mb in (every mailbox of acc)
        try
          set theMsg to (first message of mb whose id is ${messageId})
          if theMsg is not missing value then exit repeat
        end try
      end repeat
      if theMsg is not missing value then exit repeat
    end repeat
  end if

  if theMsg is missing value then error "Message ${messageId} not found"
  delete theMsg
  return "OK"
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
  
  set msgList to {}
  if ${unreadOnly} then
    set theMessages to (messages of targetMB whose read status is false)
    set msgCount to count of theMessages
    set fetchCount to msgCount
    if fetchCount > ${limit} then set fetchCount to ${limit}
    if fetchCount > 0 then
      repeat with i from 1 to fetchCount
        set m to item i of theMessages
        set mId to id of m as string
        set mSub to subject of m
        set mSender to sender of m
        set mDate to date received of m as string
        set mRead to read status of m as string
        set end of msgList to mId & "|||" & mSub & "|||" & mSender & "|||" & mDate & "|||" & mRead
      end repeat
    end if
  else
    set msgCount to count of messages of targetMB
    set startIndex to msgCount - ${limit - 1}
    if startIndex < 1 then set startIndex to 1
    if msgCount > 0 then
      repeat with i from msgCount to startIndex by -1
        set m to item i of (messages of targetMB)
        set mId to id of m as string
        set mSub to subject of m
        set mSender to sender of m
        set mDate to date received of m as string
        set mRead to read status of m as string
        set end of msgList to mId & "|||" & mSub & "|||" & mSender & "|||" & mDate & "|||" & mRead
      end repeat
    end if
  end if
  
  set AppleScript's text item delimiters to "---MSG---"
  set rawOut to msgList as string
  set AppleScript's text item delimiters to ""
  return rawOut
end tell`;

    try {
      const raw = await this.execAppleScript(script);
      if (!raw) return [];
      const parts = raw.split("---MSG---").filter(p => p.trim().length > 0);
      return parts.map(part => {
        const [id, subject, sender, date_received, read_status, message_id] = part.split("|||");
        return {
          id: id?.trim() || "",
          message_id: message_id ? message_id.replace(/^<|>$/g, "").trim() : undefined,
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
on resolveMailbox(theAcc, folderName)
  tell application "Mail"
    try
      return mailbox folderName of theAcc
    end try
    try
      return (first mailbox of theAcc whose name is folderName)
    end try
    if folderName is "Archive" or folderName is "All Mail" then
      try
        return (first mailbox of theAcc whose name is "Archive" or name is "All Mail" or name is "Archived")
      end try
    else if folderName is "@Review" or folderName is "Review" then
      try
        return (first mailbox of theAcc whose name is "@Review" or name is "Review")
      on error
        try
          return (make new mailbox at theAcc with properties {name:"@Review"})
        end try
      end try
    else if folderName is "Deleted Items" or folderName is "Trash" or folderName is "Bin" then
      try
        return (first mailbox of theAcc whose name is "Deleted Items" or name is "Trash" or name is "Bin" or name is "Deleted Messages")
      end try
    end if
    return missing value
  end tell
end resolveMailbox

tell application "Mail"
  set idList to {${idListStr}}
  set movedCount to 0
  set failCount to 0
  
  set singleTargetMB to missing value
  ${accountName ? `
    try
      set specifiedAcc to account "${accountName}"
      set singleTargetMB to my resolveMailbox(specifiedAcc, "${targetMailbox}")
    end try
  ` : ""}

  repeat with msgId in idList
    try
      set theMsg to missing value
      
      -- 1. Fast lookup in inbox
      try
        set theMsg to (first message of inbox whose id is (msgId as integer))
      end try
      
      -- 2. Fallback to accounts if not in inbox
      if theMsg is missing value then
        repeat with acc in accounts
          repeat with mb in (every mailbox of acc)
            try
              set theMsg to (first message of mb whose id is (msgId as integer))
              if theMsg is not missing value then exit repeat
            end try
          end repeat
          if theMsg is not missing value then exit repeat
        end repeat
      end if
      
      if theMsg is not missing value then
        set destMB to singleTargetMB
        if destMB is missing value then
          try
            set msgAcc to account of mailbox of theMsg
            set destMB to my resolveMailbox(msgAcc, "${targetMailbox}")
          end try
        end if
        
        if destMB is not missing value then
          move theMsg to destMB
          try
            set read status of theMsg to true
          end try
          set movedCount to movedCount + 1
        else
          set failCount to failCount + 1
        end if
      else
        set failCount to failCount + 1
      end if
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
      set theMsg to missing value
      
      -- Fast lookup in inbox
      try
        set theMsg to (first message of inbox whose id is (msgId as integer))
      end try
      
      if theMsg is missing value then
        repeat with acc in accounts
          repeat with mb in (every mailbox of acc)
            try
              set theMsg to (first message of mb whose id is (msgId as integer))
              if theMsg is not missing value then exit repeat
            end try
          end repeat
          if theMsg is not missing value then exit repeat
        end repeat
      end if
      
      if theMsg is not missing value then
        delete theMsg
        set delCount to delCount + 1
      else
        set failCount to failCount + 1
      end if
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
