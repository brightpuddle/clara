#!/usr/bin/env lua
-- examples/scripts/handle_urgent_email.lua
-- Demonstrates external workflow script handling an urgent email event.
-- Follows BEAM 'let it fail' philosophy: errors crash the script and are audited by Clara.

print("=== Starting Urgent Email Handler ===")
print("Run ID: " .. (os.getenv("CLARA_RUN_ID") or "N/A"))
print("Trigger: " .. (os.getenv("CLARA_TRIGGER_ID") or "N/A"))

-- Read raw event JSON from stdin
local input = io.read("*a")
print("Received Event Payload: " .. (input or "{}"))

-- Perform real-world workflow logic:
-- 1. Call MCP tool 'notify.send' to send a macOS or Webex notification
-- 2. Call MCP tool 'db.exec' or 'shell.exec' as required

print("Dispatching incident notification...")
-- Example command invocation:
-- clara.call_tool("notify.send", { message = "Production Outage Detected!", title = "Alert" })

print("=== Urgent Email Workflow Completed Successfully ===")
