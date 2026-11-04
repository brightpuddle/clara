import json,subprocess,sys,tempfile
p=subprocess.Popen(["agy","acp","--dangerously-skip-permissions"],stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=subprocess.DEVNULL,text=True,bufsize=1)
def rpc(i,m,params):
    p.stdin.write(json.dumps({"jsonrpc":"2.0","id":i,"method":m,"params":params})+"\n");p.stdin.flush()
    notes=[]
    while True:
        r=json.loads(p.stdout.readline())
        if r.get("id")==i: return r,notes
        notes.append(r)
rpc(1,"initialize",{"protocolVersion":1,"clientCapabilities":{}})
r,_=rpc(2,"session/list",{}); s=r.get("result",{}).get("sessions",[])
print("list ->",len(s),json.dumps(s[:2])[:500])
sid=sys.argv[1]
r,n=rpc(3,"session/load",{"sessionId":sid,"cwd":sys.argv[2],"mcpServers":[]})
kinds=[x.get("params",{}).get("update",{}).get("sessionUpdate") for x in n]
print("load ->",json.dumps(r)[:200]); print("replayed updates:",kinds)
p.kill()
