# lib/search.star — fans out across registered search integrations and returns
# merged results.
#
# Change the sources list (or the per-source logic) here to update every
# intent that imports this module.
#
# Usage:
#   load("lib/search.star", "query")
#   results = query("budget report Q1", sources=["zk", "web"])

def query(q, sources=["web", "zk"]):
    """Search across configured sources and return merged results."""
    results = []
    if "web" in sources:
        results += web.search({"query": q}).get("results", [])
    if "zk" in sources:
        results += zk.search({"query": q}).get("notes", [])
    return results
