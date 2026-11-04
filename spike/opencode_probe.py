#!/usr/bin/env python3
"""Throwaway spike: observe opencode v2 events, forms and prompt flow.

Writes raw artifacts to ./out/. Uses `opencode api` for transport/auth.
Run: python3 spike/opencode_probe.py
"""
import json, os, subprocess, sys, threading, time, tempfile

OUT = os.path.join(os.path.dirname(__file__), "out")
os.makedirs(OUT, exist_ok=True)


def api(*args, data=None, check=True):
    cmd = ["opencode", "api", *args]
    if data is not None:
        cmd += ["--data", json.dumps(data)]
    r = subprocess.run(cmd, capture_output=True, text=True)
    if check and r.returncode != 0:
        print("API FAIL", cmd, r.stdout[:400], r.stderr[:400])
    out = r.stdout.strip()
    try:
        return json.loads(out) if out else None
    except json.JSONDecodeError:
        return out


def stream_events(path, stop):
    p = subprocess.Popen(["opencode", "api", "get", "/api/event"],
                         stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True, bufsize=1)
    with open(path, "w") as f:
        def killer():
            stop.wait(); p.kill()
        threading.Thread(target=killer, daemon=True).start()
        for line in p.stdout:
            f.write(json.dumps({"t": time.time(), "line": line.rstrip("\n")}) + "\n")
            f.flush()


def main():
    stop = threading.Event()
    threading.Thread(target=stream_events, args=(f"{OUT}/events.jsonl", stop), daemon=True).start()
    time.sleep(1.5)

    workdir = tempfile.mkdtemp(prefix="cockpit-spike-")
    sess = api("post", "/api/session", data={
        "title": "cockpit spike",
        "location": {"directory": workdir},
        "model": {"id": "gemini-3.8-flash", "providerID": "github-copilot"},
    })
    json.dump(sess, open(f"{OUT}/session.json", "w"), indent=1)
    sid = (sess.get("data") or sess)["id"]
    print("session", sid, "dir", workdir)

    api("post", f"/api/session/{sid}/prompt", data={"text":
        "Use your question tool to ask me whether I prefer red or blue (offer those two options). "
        "After I answer, reply with exactly one sentence stating my answer."})

    answered = False
    deadline = time.time() + 150
    while time.time() < deadline:
        forms = api("get", f"/api/session/{sid}/form", check=False)
        json.dump(forms, open(f"{OUT}/forms_last.json", "w"), indent=1)
        items = (forms or {}).get("data") if isinstance(forms, dict) else None
        if items and not answered:
            f0 = items[0]
            json.dump(f0, open(f"{OUT}/form_pending.json", "w"), indent=1)
            print("FORM:", json.dumps(f0)[:600])
            answer = {}
            for fld in f0["fields"]:
                opts = fld.get("options") or []
                answer[fld["key"]] = opts[0]["value"] if opts else "red"
            r = api("post", f"/api/session/{sid}/form/{f0['id']}/reply", data={"answer": answer})
            print("reply ->", r)
            answered = True
        active = api("get", "/api/session/active", check=False)
        if answered and sid not in ((active or {}).get("data") or {}):
            break
        time.sleep(1.5)

    msgs = api("get", f"/api/session/{sid}/message")
    json.dump(msgs, open(f"{OUT}/messages.json", "w"), indent=1)
    time.sleep(1)
    stop.set()
    print("answered:", answered, "-> artifacts in", OUT)


if __name__ == "__main__":
    sys.exit(main())
