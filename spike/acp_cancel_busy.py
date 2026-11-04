#!/usr/bin/env python3
"""Throwaway spike: ACP cancel and busy-session behaviour.

Usage: acp_cancel_busy.py <cancel|busy> <model-id|-> -- <agent cmd...>
Prints a timeline; raw messages go to out/acp_<mode>_<agent>.jsonl
"""
import json, os, subprocess, sys, tempfile, threading, time

mode, model = sys.argv[1], sys.argv[2]
cmd = sys.argv[4:]
agent = os.path.basename(cmd[0])
OUT = os.path.join(os.path.dirname(__file__), "out")
os.makedirs(OUT, exist_ok=True)
LOG = open(f"{OUT}/acp_{mode}_{agent}.jsonl", "w")
T0 = time.time()
workdir = tempfile.mkdtemp(prefix="cockpit-acp-")
p = subprocess.Popen(cmd, cwd=workdir, stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                     stderr=subprocess.DEVNULL, text=True, bufsize=1)
nid = [0]
pending = {}


def tl(msg):
    print(f"[{time.time()-T0:6.1f}s] {msg}", flush=True)


def send(obj):
    LOG.write(json.dumps({"t": time.time()-T0, "dir": "out", "msg": obj}) + "\n"); LOG.flush()
    p.stdin.write(json.dumps(obj) + "\n"); p.stdin.flush()


def call(method, params, label=None):
    nid[0] += 1; i = nid[0]
    ev = threading.Event(); pending[i] = {"ev": ev, "res": None, "label": label or method}
    send({"jsonrpc": "2.0", "id": i, "method": method, "params": params})
    return i


def notify(method, params):
    send({"jsonrpc": "2.0", "method": method, "params": params})


def reader():
    for line in p.stdout:
        try:
            m = json.loads(line)
        except Exception:
            continue
        LOG.write(json.dumps({"t": time.time()-T0, "dir": "in", "msg": m}) + "\n"); LOG.flush()
        if "method" in m and "id" in m:
            if m["method"] == "session/request_permission":
                opts = m["params"].get("options", [])
                pick = next((o for o in opts if "allow" in o.get("kind", "")), opts[0] if opts else None)
                tl("permission request -> allow")
                send({"jsonrpc": "2.0", "id": m["id"], "result": {"outcome": {"outcome": "selected", "optionId": pick["optionId"]}}})
            else:
                send({"jsonrpc": "2.0", "id": m["id"], "error": {"code": -32601, "message": "unsupported"}})
        elif m.get("method") == "session/update":
            u = m["params"]["update"]; k = u.get("sessionUpdate")
            if k in ("tool_call", "tool_call_update"):
                tl(f"update {k} {u.get('name') or u.get('title')} status={u.get('status')}")
            elif k == "agent_message_chunk":
                tl("update agent_message: " + json.dumps(u["content"].get("text", ""))[:80])
            elif k in ("user_message_chunk",):
                tl("update user_message: " + json.dumps(u["content"].get("text", ""))[:60])
        elif "id" in m and m["id"] in pending:
            d = pending[m["id"]]; d["res"] = m; d["ev"].set()
            tl(f"RESPONSE to {d['label']}: " + json.dumps(m.get("result") or m.get("error"))[:160])


threading.Thread(target=reader, daemon=True).start()

def wait(i, t):
    pending[i]["ev"].wait(t); return pending[i]["res"]

wait(call("initialize", {"protocolVersion": 1, "clientCapabilities": {}}), 30)
sn = wait(call("session/new", {"cwd": workdir, "mcpServers": []}), 30)
sid = sn["result"]["sessionId"]
if model != "-":
    r = wait(call("session/set_model", {"sessionId": sid, "modelId": model}), 15)
    tl("set_model: " + json.dumps(r)[:120])
tl("session " + sid)

LONG = os.environ.get("PROMPT_A") or "Run the shell command `sleep 40` and wait for it. When it finishes, reply with exactly the word done."
a = call("session/prompt", {"sessionId": sid, "prompt": [{"type": "text", "text": LONG}]}, "prompt A")
tl("sent prompt A")
time.sleep(float(os.environ.get("DELAY_B", "10")))

if mode == "watch":
    wait(a, 60)
    tl("turn A settled; now watching for unsolicited updates for 45s")
    time.sleep(45)
elif mode == "cancel":
    tl("sending session/cancel (notification)")
    notify("session/cancel", {"sessionId": sid})
    r = wait(a, 30)
    tl("prompt A settled after cancel: " + json.dumps(r.get("result") if r else None)[:140])
    # Is the session still usable?
    b = call("session/prompt", {"sessionId": sid, "prompt": [{"type": "text", "text": "Reply with exactly the word ok."}]}, "prompt B (after cancel)")
    wait(b, 60)
else:
    b = call("session/prompt", {"sessionId": sid, "prompt": [{"type": "text", "text": "Also reply with the word second when you are able."}]}, "prompt B (while busy)")
    tl("sent prompt B while A running")
    rb = wait(b, 15)
    tl("prompt B settled within 15s? " + str(rb is not None))
    ra = wait(a, 90)
    tl("prompt A settled? " + str(ra is not None))
    if rb is None:
        wait(b, 60)

time.sleep(2)
p.kill()
