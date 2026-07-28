package rollout

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"

	"github.com/cofy-x/axern/apps/axrun/internal/domain"
	"github.com/cofy-x/axern/apps/axrun/internal/taskset"
)

type ControlPlannedTask struct {
	TaskID     string
	TaskDigest string
	TaskJSON   []byte
}
type ControlPlan struct {
	ResultDigest     string
	SourceDigest     string
	DescriptorDigest string
	Payloads         []taskset.PayloadDescriptor
	PlanJSON         []byte
	Tasks            []ControlPlannedTask
}

func (s Service) PlanForControl(params Params) (ControlPlan, error) {
	normalized, err := NormalizeParams(params)
	if err != nil {
		return ControlPlan{}, err
	}
	now := currentTime(s.Now)
	prepared, err := prepareRolloutRun(runContext(normalized), normalized, now)
	if err != nil {
		return ControlPlan{}, err
	}
	plans := newEpisodePlans(prepared.RolloutRun, prepared.Tasks)
	planJSON, err := json.Marshal(newRolloutPlan(prepared.RolloutRun, prepared.PlanSelection, plans, now))
	if err != nil {
		return ControlPlan{}, err
	}
	canonical := map[string]taskset.Task{}
	for _, task := range prepared.TaskSet.Descriptor.Tasks {
		canonical[task.Instance.ID] = task
	}
	tasks := make([]ControlPlannedTask, 0, len(prepared.Tasks))
	for _, resolved := range prepared.Tasks {
		task, ok := canonical[resolved.ID]
		if !ok {
			return ControlPlan{}, fmt.Errorf("selected task %q is missing from canonical descriptor", resolved.ID)
		}
		data, err := json.Marshal(task)
		if err != nil {
			return ControlPlan{}, err
		}
		sum := sha256.Sum256(data)
		tasks = append(tasks, ControlPlannedTask{
			TaskID:     resolved.ID,
			TaskDigest: "sha256:" + hex.EncodeToString(sum[:]),
			TaskJSON:   data,
		})
	}
	sum := sha256.Sum256(planJSON)
	return ControlPlan{
		ResultDigest:     "sha256:" + hex.EncodeToString(sum[:]),
		SourceDigest:     prepared.TaskSet.Descriptor.SourceDigest,
		DescriptorDigest: prepared.TaskSet.DescriptorDigest,
		Payloads:         append([]taskset.PayloadDescriptor(nil), prepared.TaskSet.Descriptor.Payloads...),
		PlanJSON:         planJSON,
		Tasks:            tasks,
	}, nil
}

type ControlEpisode struct {
	Episode domain.Episode
	Reward  domain.Reward
}

func ReadControlEpisode(result Result) (ControlEpisode, error) {
	if len(result.Episodes) != 1 {
		return ControlEpisode{}, fmt.Errorf("controlled episode execution produced %d episodes", len(result.Episodes))
	}
	data, err := os.ReadFile(result.Episodes[0].EpisodeJSONPath)
	if err != nil {
		return ControlEpisode{}, err
	}
	var episode domain.Episode
	if err := json.Unmarshal(data, &episode); err != nil {
		return ControlEpisode{}, err
	}
	rewardData, err := os.ReadFile(result.Episodes[0].RewardJSONPath)
	if err != nil {
		return ControlEpisode{}, err
	}
	var reward domain.Reward
	if err := json.Unmarshal(rewardData, &reward); err != nil {
		return ControlEpisode{}, err
	}
	return ControlEpisode{Episode: episode, Reward: reward}, nil
}

func ControlRunID(rolloutID, episodeID string, generation int64) string {
	return fmt.Sprintf("%s-%s-g%d", rolloutID, episodeID, generation)
}
