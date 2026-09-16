package schema

import "github.com/cofy-x/axern/apps/axrun/internal/domain"

func validateShard(problems *collector, path string, field string, shard *domain.TaskShard) {
	if shard == nil {
		return
	}
	if shard.Index < 0 {
		problems.add(path, field+".index", "must be greater than or equal to zero")
	}
	if shard.Count <= 0 {
		problems.add(path, field+".count", "must be greater than zero")
	}
	if shard.Count > 0 && shard.Index >= shard.Count {
		problems.add(path, field+".index", "must be less than shard.count")
	}
}
