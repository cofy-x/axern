package proc

const RuntimeStartExitCode = 127

func MergeEnv(base []string, overrides []string) []string {
	index := make(map[string]int, len(base)+len(overrides))
	out := make([]string, 0, len(base)+len(overrides))
	for _, item := range base {
		name, _, ok := CutEnv(item)
		if !ok {
			continue
		}
		index[name] = len(out)
		out = append(out, item)
	}
	for _, item := range overrides {
		name, _, ok := CutEnv(item)
		if !ok {
			continue
		}
		if idx, exists := index[name]; exists {
			out[idx] = item
			continue
		}
		index[name] = len(out)
		out = append(out, item)
	}
	return out
}

func CutEnv(item string) (string, string, bool) {
	for i := range item {
		if item[i] == '=' {
			if i == 0 {
				return "", "", false
			}
			return item[:i], item[i+1:], true
		}
	}
	return "", "", false
}
