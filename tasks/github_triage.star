clara.describe("Automate GitHub issue triage using cross-referenced context from Mail, Webex, and ZK.")

def extract_keywords(title, body):
    prompt = """
Extract 3-5 concise search keywords or phrases from the following GitHub issue to find related context in chats and notes.
Output the keywords as a JSON list of strings.

Title: {title}
Body: {body}
""".format(title=title, body=body)

    # Use the 'general' category as per configuration.
    result = llm.generate(prompt = prompt, category = "general", decode = "json")
    return result.get("parsed", [])

def main(args = None):
    owner = "catchorg"
    repo = "Clara"
    
    if args:
        owner = args.get("owner", owner)
        repo = args.get("repo", repo)
    
    # 1. Fetch open issues.
    resp = github.list_issues(owner = owner, repo = repo, state = "open", perPage = 2)
    issues = resp["issues"]
    
    results = []
    for issue in issues:
        number = issue["number"]
        title = issue["title"]
        body = issue.get("body", "")
        
        # 2. Extract potential keywords using LLM.
        keywords = extract_keywords(title, body)
        
        # 3. Cross-reference search for EACH keyword.
        issue_context = {}
        for kw in keywords:
            search_result = clara.search(query = kw, limit = 3)
            issue_context[kw] = search_result
        
        # 4. Results.
        results.append({
            "issue": number,
            "title": title,
            "keywords": keywords,
            "context": issue_context,
        })
    
    return results

clara.task(main)
