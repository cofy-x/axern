package axern

import "testing"

func TestWorkspaceVolumesDisabledByDefault(t *testing.T) {
	if got := workspaceVolumes(Config{}); got != nil {
		t.Fatalf("workspaceVolumes = %#v, want nil", got)
	}
}

func TestWorkspaceVolumesEnablesHostBackedWorkspace(t *testing.T) {
	got := workspaceVolumes(Config{WorkspaceVolume: true})
	if len(got) != 1 ||
		got[0].Name != "workspace" ||
		got[0].Target != "/workspace" ||
		len(got[0].Options) != 1 ||
		got[0].Options[0] != "rbind" {
		t.Fatalf("workspaceVolumes = %#v", got)
	}
}
