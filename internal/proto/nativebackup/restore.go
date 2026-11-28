// Package nativebackup defines the private operator exchange for native restore.
package nativebackup

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"
)

const Format = "axern.native-node-restore.v1"

type Plan struct {
	Format     string      `json:"format"`
	OldNode    string      `json:"old_node"`
	NewNode    string      `json:"new_node"`
	Workspaces []Workspace `json:"workspaces"`
}

type Workspace struct {
	ID           string `json:"id"`
	Namespace    string `json:"namespace"`
	RootIdentity string `json:"root_identity"`
	OldIdentity  string `json:"old_identity"`
	NewIdentity  string `json:"new_identity,omitempty"`
	Bytes        int64  `json:"bytes"`
	Inodes       int64  `json:"inodes"`
	Generation   int64  `json:"generation"`
}

var (
	nodeName      = regexp.MustCompile(`^[a-z][a-z0-9-]{2,62}$`)
	workspaceName = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,128}$`)
	identity      = regexp.MustCompile(`^[a-f0-9]{64}$`)
)

func Decode(reader io.Reader, mapped bool) (Plan, error) {
	const maxPlanSize = 16 << 20
	input, err := io.ReadAll(io.LimitReader(reader, maxPlanSize+1))
	if err != nil {
		return Plan{}, err
	}
	if len(input) > maxPlanSize {
		return Plan{}, errors.New("native restore plan exceeds its size limit")
	}
	var plan Plan
	decoder := json.NewDecoder(bytes.NewReader(input))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&plan); err != nil {
		return Plan{}, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return Plan{}, errors.New("native restore plan has trailing data")
	}
	return plan, plan.Validate(mapped)
}

func (plan Plan) Validate(mapped bool) error {
	if plan.Format != Format || plan.OldNode == plan.NewNode ||
		!nodeName.MatchString(plan.OldNode) || !nodeName.MatchString(plan.NewNode) {
		return errors.New("native restore requires distinct valid Node identities")
	}
	if len(plan.Workspaces) == 0 {
		return errors.New("native restore requires an explicit retained Workspace inventory")
	}
	var previous string
	for _, workspace := range plan.Workspaces {
		if !workspaceName.MatchString(workspace.ID) || workspace.ID == "root" || workspace.ID <= previous ||
			workspace.Namespace == "" || strings.TrimSpace(workspace.Namespace) != workspace.Namespace ||
			!identity.MatchString(workspace.RootIdentity) || !identity.MatchString(workspace.OldIdentity) ||
			workspace.Bytes <= 0 || workspace.Bytes%512 != 0 || workspace.Inodes <= 0 || workspace.Generation < 0 {
			return errors.New("native restore Workspace inventory is invalid or unordered")
		}
		if mapped && (!identity.MatchString(workspace.NewIdentity) || workspace.NewIdentity == workspace.OldIdentity) ||
			!mapped && workspace.NewIdentity != "" {
			return errors.New("native restore Workspace storage identity does not match its phase")
		}
		previous = workspace.ID
	}
	return nil
}
