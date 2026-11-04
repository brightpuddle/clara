import json,subprocess,sys
cmd=sys.argv[1:]
p=subprocess.Popen(cmd,stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=subprocess.DEVNULL,text=True,bufsize=1)
def rpc(i,m,params):
    p.stdin.write(json.dumps({"jsonrpc":"2.0","id":i,"method":m,"params":params})+"\n");p.stdin.flush()
    while True:
        r=json.loads(p.stdout.readline())
        if r.get("id")==i: return r
r=rpc(1,"initialize",{"protocolVersion":1,"clientCapabilities":{}})
print("agent:",json.dumps(r.get("result",{}).get("agentInfo")),"caps:",json.dumps(r.get("result",{}).get("agentCapabilities"))[:400])
r=rpc(2,"session/list",{})
s=r.get("result",{}).get("sessions",[]) if "result" in r else r
print("sessions:",len(s) if isinstance(s,list) else s)
for x in (s[:4] if isinstance(s,list) else []): print(" ",json.dumps(x)[:200])
p.kill()
