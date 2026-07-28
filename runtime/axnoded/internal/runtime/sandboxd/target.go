package sandboxd

type Target struct {
	Snapshot CapabilitySnapshot
	Client   *Client
}

func TargetFromLabels(labels map[string]string, capabilities ...string) (Target, error) {
	snapshot, err := SnapshotFromLabels(labels)
	if err != nil {
		return Target{}, err
	}
	for _, capability := range capabilities {
		if err := snapshot.RequireCapability(capability); err != nil {
			return Target{}, err
		}
	}
	return Target{
		Snapshot: snapshot,
		Client:   NewClient(snapshot.SocketPath),
	}, nil
}
