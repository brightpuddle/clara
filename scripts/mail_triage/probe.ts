// scripts/mail_triage/probe.ts
// Diagnostic probe to inspect Apple Mail accounts, mailboxes, and sample unread messages.

import { AppleMailClient } from "./mail_client.ts";

console.log("=========================================");
console.log("   Clara Apple Mail Environment Probe    ");
console.log("=========================================\n");

const client = new AppleMailClient();

try {
  console.log("1. Inspecting configured Mail.app Mailboxes and Accounts...");
  const mailboxes = await client.getMailboxes();
  
  if (mailboxes.length === 0) {
    console.log("⚠️ No mailboxes returned. Please make sure Mail.app is running and accessibility permissions are granted.");
  } else {
    const byAccount: Record<string, string[]> = {};
    for (const mb of mailboxes) {
      if (!byAccount[mb.account]) byAccount[mb.account] = [];
      byAccount[mb.account].push(mb.name);
    }

    console.log(`✅ Discovered ${Object.keys(byAccount).length} Account(s):`);
    for (const [acc, mbs] of Object.entries(byAccount)) {
      const hasReview = mbs.some(m => m.toLowerCase().includes("review"));
      const hasArchive = mbs.some(m => m.toLowerCase().includes("archive"));
      console.log(`\n  📂 Account: "${acc}" (${mbs.length} mailboxes)`);
      console.log(`     - Has @Review mailbox: ${hasReview ? "✅" : "⚠️ (May need creating)"}`);
      console.log(`     - Has Archive mailbox: ${hasArchive ? "✅" : "⚠️"}`);
      console.log(`     - Mailboxes sample: ${mbs.slice(0, 8).join(", ")}${mbs.length > 8 ? "..." : ""}`);
    }
  }

  console.log("\n2. Checking unread messages in primary Inbox...");
  const unread = await client.listInbox(5, undefined, true);
  console.log(`✅ Found ${unread.length} sample unread message(s) in inbox:`);

  for (let i = 0; i < unread.length; i++) {
    const msg = unread[i];
    console.log(`\n  [#${i + 1}] ID: ${msg.id}`);
    console.log(`      Subject: ${msg.subject}`);
    console.log(`      From:    ${msg.sender}`);
    console.log(`      Date:    ${msg.date_received}`);
  }

  console.log("\n=========================================");
  console.log("   Probe completed successfully!         ");
  console.log("=========================================\n");
} catch (err: any) {
  console.error("\n❌ Probe encountered an error:", err.message);
  process.exit(1);
}
