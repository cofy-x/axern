package imagefsd

func (mgr *manager) newDaemon(opts *DaemonCreateOpt) (*Daemon, error) {
	switch normalizeSourceType(opts.SourceType) {
	case SourceTypeNydus:
		return mgr.setupNydusDaemon(opts)
	case SourceTypeOSS:
		fallthrough
	default:
		return mgr.setupOSSDaemon(opts)
	}
}

func (mgr *manager) CreateDaemon(opts *DaemonCreateOpt) error {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()

	if d, ok := mgr.daemons[opts.ID]; ok {
		d.updateExpired()
		// Clear mountFailed: a new CreateDaemon call means the caller intends
		// to use this daemon, so GC should not clean it up.
		d.mountFailed.Store(false)
		return nil
	}
	d, err := mgr.newDaemon(opts)
	if err != nil {
		return err
	}
	mgr.daemons[d.meta.ID] = d
	return nil
}

func (mgr *manager) GetDaemon(id string) *Daemon {
	mgr.mu.RLock()
	defer mgr.mu.RUnlock()

	d, ok := mgr.daemons[id]
	if ok {
		d.updateExpired()
		// Clear mountFailed: the caller retrieved this daemon to use it
		// (typically followed by d.Mount()). This prevents GC from cleaning
		// up the daemon in the window between GetDaemon and Mount.
		d.mountFailed.Store(false)
		return d
	}
	return nil
}
