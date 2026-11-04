import { OpenCode } from "@opencode/client"
import { Service } from "@opencode/client/service"
import { appendFileSync, writeFileSync } from "node:fs"

const secs = Number(process.argv[2] ?? 60)
const out = new URL("./out/events_ts.jsonl", import.meta.url).pathname
writeFileSync(out, "")
const endpoint = await Service.ensure()
const client = OpenCode.make({ baseUrl: endpoint.url, headers: Service.headers(endpoint) })

setTimeout(() => process.exit(0), secs * 1000)
try {
  for await (const ev of client.event.subscribe()) {
    appendFileSync(out, JSON.stringify({ t: Date.now(), ev }) + "\n")
  }
} catch (e) { console.error("ended:", String(e)) }
