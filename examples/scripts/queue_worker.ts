#!/usr/bin/env bun
// examples/scripts/queue_worker.ts
// Supervised TypeScript worker loop executed with Bun.
// If it encounters a fatal unhandled error, it crashes ("let it fail"),
// and Clara supervisor automatically restarts it based on its restart policy.

import { runID, triggerID } from "../../scripts/clara";

console.log(`[Worker] Started queue processor (Run: ${runID()}, Trigger: ${triggerID()})`);

for (let i = 1; i <= 3; i++) {
  console.log(`[Worker] Heartbeat tick ${i}/3`);
  await new Promise((resolve) => setTimeout(resolve, 1000));
}

console.log("[Worker] Processing batch completed successfully.");
