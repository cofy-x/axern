package oci

func (m *Manager) acquireImageLock(imageURL string) func() {
	m.mutex.Lock()
	entry, exists := m.imageLocks[imageURL]
	if !exists {
		entry = &imageLockEntry{}
		m.imageLocks[imageURL] = entry
	}
	entry.refs++
	m.mutex.Unlock()

	entry.mu.Lock()
	return func() {
		entry.mu.Unlock()
		m.mutex.Lock()
		entry.refs--
		if entry.refs == 0 {
			delete(m.imageLocks, imageURL)
		}
		m.mutex.Unlock()
	}
}

func (m *Manager) acquireLayerLock(layerDigest string) func() {
	m.mutex.Lock()
	entry, exists := m.layerLocks[layerDigest]
	if !exists {
		entry = &imageLockEntry{}
		m.layerLocks[layerDigest] = entry
	}
	entry.refs++
	m.mutex.Unlock()

	entry.mu.Lock()
	return func() {
		entry.mu.Unlock()
		m.mutex.Lock()
		entry.refs--
		if entry.refs == 0 {
			delete(m.layerLocks, layerDigest)
		}
		m.mutex.Unlock()
	}
}

func (m *Manager) acquireChainLock(chainID string) func() {
	m.mutex.Lock()
	entry, exists := m.chainLocks[chainID]
	if !exists {
		entry = &imageLockEntry{}
		m.chainLocks[chainID] = entry
	}
	entry.refs++
	m.mutex.Unlock()

	entry.mu.Lock()
	return func() {
		entry.mu.Unlock()
		m.mutex.Lock()
		entry.refs--
		if entry.refs == 0 {
			delete(m.chainLocks, chainID)
		}
		m.mutex.Unlock()
	}
}

func (m *Manager) getContainer(imageURL string) *ContainerInfo {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	info, exists := m.containers[imageURL]
	if !exists {
		return nil
	}
	return &ContainerInfo{
		MountID:      info.MountID,
		ImageURL:     info.ImageURL,
		MountPath:    info.MountPath,
		LayerDigests: append([]string(nil), info.LayerDigests...),
		ChainIDs:     append([]string(nil), info.ChainIDs...),
		LowerDirs:    append([]string(nil), info.LowerDirs...),
		Env:          append([]string(nil), info.Env...),
		ImageConfig:  cloneImageConfig(info.ImageConfig),
	}
}

func (m *Manager) setContainer(imageURL string, info *ContainerInfo) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.containers[imageURL] = info
}

func (m *Manager) deleteContainer(imageURL string) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	delete(m.containers, imageURL)
}
