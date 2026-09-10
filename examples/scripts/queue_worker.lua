#!/usr/bin/env lua
-- examples/scripts/queue_worker.lua
-- Supervised worker loop. If it encounters a fatal error, it crashes ("let it fail"),
-- and the Clara supervisor restarts it based on its restart policy.

print("Worker started (PID: " .. (os.getenv("CLARA_RUN_ID") or "worker") .. ")")

-- Example polling loop or event consumer
-- In a real workflow, this might poll a Redis queue, subscribe to a websocket, etc.
for i = 1, 3 do
    print("Worker heartbeat tick: " .. i)
    -- Simulate sleep
    os.execute("sleep 1")
end

print("Worker cycle complete.")
