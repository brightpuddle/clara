#!/usr/bin/env python3
"""Throwaway spike: drive `agy acp` over stdio and log every JSON-RPC message.

Run: python3 docs/spikes/agy_acp_probe.py [prompt]
Artifacts: docs/spikes/out/agy_acp.jsonl (recording: internal/agents/testdata/acp/)
"""
import json, os, subprocess, sys, tempfile, threading, time

OUT = os.path.join(os.path.dirname(__file__), "out")
os.makedirs(OUT, exist_ok=True)
LOG = open(f"{OUT}/agy_acp.jsonl", "w")
workdir = tempfile.mkdtemp(prefix="cockpit-agy-spike-")
PROMPT = sys.argv[1] if len(sys.argv) > 1 else (
    "Ask me whether I prefer red or blue and wait for my answer. "
    "Then reply with exactly one sentence stating my answer.")

p = subprocess.Popen(["agy", "acp", "--dangerously-skip-permissions"], cwd=workdir,
                     stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, bufsize=1)
nid = 0
pending = {}
lock = threading.Lock()


def log(direction, obj):
    LOG.write(json.dumps({"t": time.time(), "dir": direction, "msg": obj}) + "\n"); LOG.flush()


def send(obj):
    log("out", obj)
    p.stdin.write(json.dumps(obj) + "\n"); p.stdin.flush()


def call(method, params):
    global nid
    with lock:
        nid += 1; i = nid
    ev = threading.Event(); pending[i] = [ev, None]
    send({"jsonrpc": "2.0", "id": i, "method": method, "params": params})
    return i, ev


def reader():
    for line in p.stdout:
        line = line.strip()
        if not line:
            continue
        try:
            m = json.loads(line)
        except json.JSONDecodeError:
            log("raw", line); continue
        log("in", m)
        if "method" in m and "id" in m:
            # request from agent to client
            meth = m["method"]
            if meth == "session/request_permission":
                opts = m["params"].get("options", [])
                pick = next((o for o in opts if "allow" in o.get("kind", "")), opts[0] if opts else None)
                send({"jsonrpc": "2.0", "id": m["id"], "result": {"outcome": {"outcome": "selected", "optionId": pick["optionId"]} if pick else {"outcome": "cancelled"}}})
            else:
                send({"jsonrpc": "2.0", "id": m["id"], "error": {"code": -32601, "message": "unsupported by spike"}})
        elif "id" in m and m["id"] in pending:
            pending[m["id"]][1] = m; pending[m["id"]][0].set()


def errlog():
    with open(f"{OUT}/agy_acp.stderr", "w") as f:
        for line in p.stderr:
            f.write(line); f.flush()


threading.Thread(target=reader, daemon=True).start()
threading.Thread(target=errlog, daemon=True).start()

i, ev = call("initialize", {"protocolVersion": 1, "clientCapabilities": {}})
ev.wait(30); init = pending[i][1]; print("initialize ->", json.dumps(init)[:900])
i, ev = call("session/new", {"cwd": workdir, "mcpServers": []})
ev.wait(30); sn = pending[i][1]; print("session/new ->", json.dumps(sn)[:400])
sid = (sn or {}).get("result", {}).get("sessionId")
MODEL = os.environ.get("AGY_MODEL")
if MODEL:
    i, ev = call("session/set_model", {"sessionId": sid, "modelId": MODEL}); ev.wait(15); print("set_model ->", json.dumps(pending[i][1])[:300])
i, ev = call("session/prompt", {"sessionId": sid, "prompt": [{"type": "text", "text": PROMPT}]})
ev.wait(120); print("session/prompt ->", json.dumps(pending[i][1])[:400])
p.kill()
print("workdir", workdir)
