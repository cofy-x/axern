package llmproxy

import "encoding/json"

func extractUsage(body []byte) *Usage {
	if len(body) == 0 {
		return nil
	}
	if usage := extractUsageFromJSON(body); usage != nil {
		return usage
	}
	return extractUsageFromSSE(body)
}

func extractUsageFromJSON(body []byte) *Usage {
	var root map[string]any
	if json.Unmarshal(body, &root) != nil {
		return nil
	}
	return usageFromRoot(root)
}

func extractUsageFromSSE(body []byte) *Usage {
	var total Usage
	found := false
	for _, line := range splitLines(string(body)) {
		line = trimSpace(line)
		if len(line) < 6 || line[:5] != "data:" {
			continue
		}
		data := trimSpace(line[5:])
		if data == "" || data == "[DONE]" {
			continue
		}
		var root map[string]any
		if json.Unmarshal([]byte(data), &root) != nil {
			continue
		}
		usage := usageFromRoot(root)
		if usage == nil {
			continue
		}
		total.InputTokens += usage.InputTokens
		total.OutputTokens += usage.OutputTokens
		total.CacheReadTokens += usage.CacheReadTokens
		total.TotalTokens += usage.TotalTokens
		found = true
	}
	if !found {
		return nil
	}
	return &total
}

func usageFromRoot(root map[string]any) *Usage {
	if usage := usageFromValue(root["usage"]); usage != nil {
		return usage
	}
	for _, key := range []string{"message", "delta"} {
		container, ok := root[key].(map[string]any)
		if !ok {
			continue
		}
		if usage := usageFromValue(container["usage"]); usage != nil {
			return usage
		}
	}
	return nil
}

func usageFromValue(value any) *Usage {
	obj, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	usage := &Usage{
		InputTokens:     intField(obj, "input_tokens", "prompt_tokens"),
		OutputTokens:    intField(obj, "output_tokens", "completion_tokens"),
		CacheReadTokens: intField(obj, "cache_read_input_tokens", "prompt_cache_hit_tokens"),
		TotalTokens:     intField(obj, "total_tokens"),
	}
	if usage.TotalTokens == 0 {
		usage.TotalTokens = usage.InputTokens + usage.OutputTokens
	}
	if usage.InputTokens == 0 && usage.OutputTokens == 0 && usage.CacheReadTokens == 0 && usage.TotalTokens == 0 {
		return nil
	}
	return usage
}

func intField(obj map[string]any, names ...string) int64 {
	for _, name := range names {
		switch value := obj[name].(type) {
		case float64:
			return int64(value)
		case int64:
			return value
		case json.Number:
			n, _ := value.Int64()
			return n
		}
	}
	return 0
}
