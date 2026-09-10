#!/usr/bin/env lua
-- examples/scripts/daily_summary.lua
-- Scheduled morning digest workflow executed on cron trigger.

print("=== Starting Morning Digest ===")
print("Run ID: " .. (os.getenv("CLARA_RUN_ID") or "N/A"))

-- External script orchestrates MCP tools to gather data and generate summary:
-- 1. Query sqlite store or zk vault
-- 2. Synthesize with LLM tool
-- 3. Post to notification channel or Discord/Webex

print("Fetching daily items...")
print("Digest compiled and posted.")
print("=== Done ===")
