package tui

import (
	"sort"
	"strings"
)

type CompletionItem struct {
	Display string
	Insert  string
}

type autocompleteClient interface {
	ListIntents() ([]IntentSummary, error)
	ListProviders() ([]ProviderSummary, error)
	ListTools(filter string) ([]ToolInfo, error)
	ShowTool(name string) (ToolInfo, error)
}

func completeInput(client autocompleteClient, input string, specs []CommandSpec) []CompletionItem {
	if !strings.HasPrefix(input, "/") {
		return nil
	}

	tokens, current, trailingSpace := completionTokens(input)
	if len(tokens) == 0 && current == "" {
		return commandWordSuggestions(specs, nil, "")
	}

	switch {
	case len(tokens) >= 2 && tokens[0] == "tool" && tokens[1] == "call":
		return completeToolCallInput(client, tokens, current, trailingSpace)
	case len(tokens) >= 2 && tokens[0] == "tool" && tokens[1] == "list":
		return completeToolListInput(client, tokens, current, trailingSpace)
	case len(tokens) >= 2 && tokens[0] == "tool" && tokens[1] == "show":
		return completeToolShowInput(client, tokens, current, trailingSpace)
	case len(tokens) >= 2 && tokens[0] == "intent" && tokens[1] == "trigger":
		return completeIntentRunInput(client, current)
	default:
		return commandWordSuggestions(specs, tokens, current)
	}
}

func completionTokens(input string) (tokens []string, current string, trailingSpace bool) {
	trimmed := strings.TrimLeft(input, " ")
	trailingSpace = strings.HasSuffix(trimmed, " ")
	parsed, err := splitCommandLine(strings.TrimSpace(trimmed))
	if err != nil || len(parsed) == 0 {
		return nil, "", trailingSpace
	}

	parsed[0] = strings.TrimPrefix(parsed[0], "/")
	if trailingSpace {
		return parsed, "", true
	}
	if len(parsed) == 1 {
		return nil, parsed[0], false
	}
	return parsed[:len(parsed)-1], parsed[len(parsed)-1], false
}

func commandWordSuggestions(
	specs []CommandSpec,
	completed []string,
	current string,
) []CompletionItem {
	candidates := map[string]struct{}{}
	for _, spec := range specs {
		parts := splitSpecPath(spec.Path)
		if len(parts) <= len(completed) {
			continue
		}

		match := true
		for i := range completed {
			if parts[i] != completed[i] {
				match = false
				break
			}
		}
		if !match {
			continue
		}

		next := parts[len(completed)]
		if strings.HasPrefix(next, current) {
			candidates[next] = struct{}{}
		}
	}

	items := make([]CompletionItem, 0, len(candidates))
	for candidate := range candidates {
		all := append([]string{}, completed...)
		all = append(all, candidate)
		items = append(items, CompletionItem{
			Display: candidate,
			Insert:  "/" + strings.Join(all, " ") + " ",
		})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Display < items[j].Display })
	return items
}

func completeIntentRunInput(client autocompleteClient, current string) []CompletionItem {
	intents, err := client.ListIntents()
	if err != nil {
		return nil
	}
	items := make([]CompletionItem, 0, len(intents))
	for _, intent := range intents {
		if !strings.HasPrefix(intent.ID, current) {
			continue
		}
		items = append(items, CompletionItem{
			Display: intent.ID,
			Insert:  "/intent run " + intent.ID + " ",
		})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Display < items[j].Display })
	return items
}

func completeToolCallInput(
	client autocompleteClient,
	tokens []string,
	current string,
	trailingSpace bool,
) []CompletionItem {
	if len(tokens) == 2 && trailingSpace {
		return providerSuggestions(client, "/tool call ", "", false)
	}
	if len(tokens) == 2 {
		if strings.Contains(current, ".") {
			return providerToolSuggestions(client, "/tool call ", current, true)
		}
		return providerSuggestions(client, "/tool call ", current, false)
	}

	toolName := tokens[2]
	if !strings.Contains(toolName, ".") {
		if trailingSpace {
			return nil
		}
		return providerSuggestions(client, "/tool call ", toolName, false)
	}

	specified := []string{}
	if len(tokens) > 3 {
		specified = append(specified, tokens[3:]...)
	}
	if !trailingSpace && current != "" && current != toolName {
		toolName = current
	}
	if !strings.Contains(toolName, ".") {
		return providerSuggestions(client, "/tool call ", toolName, false)
	}
	if len(tokens) == 3 && (!trailingSpace || strings.HasSuffix(toolName, ".")) {
		return providerToolSuggestions(client, "/tool call ", toolName, true)
	}
	return toolParamSuggestions(client, toolName, specified, current, trailingSpace)
}

func completeToolListInput(
	client autocompleteClient,
	tokens []string,
	current string,
	trailingSpace bool,
) []CompletionItem {
	if len(tokens) == 2 && trailingSpace {
		return providerSuggestions(client, "/tool list ", "", false)
	}
	if len(tokens) == 2 {
		return providerSuggestions(client, "/tool list ", current, false)
	}
	return nil
}

func completeToolShowInput(
	client autocompleteClient,
	tokens []string,
	current string,
	trailingSpace bool,
) []CompletionItem {
	if len(tokens) == 2 && trailingSpace {
		return providerSuggestions(client, "/tool show ", "", true)
	}
	if len(tokens) == 2 {
		if strings.Contains(current, ".") {
			return providerToolSuggestions(client, "/tool show ", current, false)
		}
		return providerSuggestions(client, "/tool show ", current, true)
	}
	if len(tokens) == 3 && !trailingSpace {
		if strings.Contains(tokens[2], ".") {
			return providerToolSuggestions(client, "/tool show ", tokens[2], false)
		}
		return providerSuggestions(client, "/tool show ", tokens[2], true)
	}
	return nil
}

func providerSuggestions(
	client autocompleteClient,
	commandPrefix string,
	prefix string,
	appendDot bool,
) []CompletionItem {
	providers, err := client.ListProviders()
	if err != nil {
		return nil
	}
	items := make([]CompletionItem, 0, len(providers))
	for _, provider := range providers {
		if !strings.HasPrefix(provider.Name, prefix) {
			continue
		}
		insert := commandPrefix + provider.Name
		if appendDot {
			insert += "."
		}
		items = append(items, CompletionItem{
			Display: provider.Name,
			Insert:  insert,
		})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Display < items[j].Display })
	return items
}

func providerToolSuggestions(
	client autocompleteClient,
	commandPrefix string,
	qualifiedPrefix string,
	appendSpace bool,
) []CompletionItem {
	provider, toolPrefix, ok := strings.Cut(qualifiedPrefix, ".")
	if !ok {
		return nil
	}
	tools, err := client.ListTools(provider)
	if err != nil {
		return nil
	}

	items := make([]CompletionItem, 0, len(tools))
	for _, tool := range tools {
		if !strings.HasPrefix(tool.Name, provider+".") {
			continue
		}
		suffix := strings.TrimPrefix(tool.Name, provider+".")
		if !strings.HasPrefix(suffix, toolPrefix) {
			continue
		}
		insert := commandPrefix + tool.Name
		if appendSpace {
			insert += " "
		}
		items = append(items, CompletionItem{
			Display: suffix,
			Insert:  insert,
		})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Display < items[j].Display })
	return items
}

func toolParamSuggestions(
	client autocompleteClient,
	toolName string,
	specified []string,
	current string,
	trailingSpace bool,
) []CompletionItem {
	tool, err := client.ShowTool(toolName)
	if err != nil {
		return nil
	}

	currentName := current
	if key, _, ok := strings.Cut(currentName, "="); ok {
		currentName = key
	}

	used := map[string]struct{}{}
	for _, token := range specified {
		key, _, ok := strings.Cut(token, "=")
		if ok && key != "" {
			used[key] = struct{}{}
		}
	}
	if !trailingSpace && currentName != "" {
		delete(used, currentName)
	}

	params := append([]ToolParam(nil), tool.Parameters...)
	sort.Slice(params, func(i, j int) bool {
		if params[i].Required != params[j].Required {
			return params[i].Required
		}
		return params[i].Name < params[j].Name
	})

	items := make([]CompletionItem, 0, len(params))
	for _, param := range params {
		if _, exists := used[param.Name]; exists {
			continue
		}
		if currentName != "" && !strings.HasPrefix(param.Name, currentName) {
			continue
		}

		insertSpecified := append([]string{}, specified...)
		if !trailingSpace && len(insertSpecified) > 0 {
			insertSpecified = insertSpecified[:len(insertSpecified)-1]
		}

		insert := "/tool call " + toolName
		if len(insertSpecified) > 0 {
			insert += " " + strings.Join(insertSpecified, " ")
		}
		insert = strings.TrimSpace(insert) + " " + param.Name + "="

		items = append(items, CompletionItem{
			Display: param.Name + "=",
			Insert:  insert,
		})
	}

	return items
}

func splitSpecPath(path string) []string {
	return strings.Fields(strings.TrimPrefix(path, "/"))
}
