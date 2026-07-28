package main

import (
	"sync"
	"time"

	"github.com/cofy-x/axern/network/bpfnet"
	"github.com/cofy-x/axern/runtime/axnoded/internal/bpfnetstatus"
	"github.com/cofy-x/axern/runtime/axnoded/internal/natbench"
)

type snatMapSampler struct {
	pinPath string
	stop    chan struct{}
	done    chan struct{}

	mu      sync.Mutex
	peak    bpfnet.SNATMapStats
	samples uint64
}

func startSNATMapSampler(pinPath string, interval time.Duration) *snatMapSampler {
	if interval <= 0 {
		interval = time.Second
	}
	sampler := &snatMapSampler{
		pinPath: pinPath,
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
	}
	sampler.observeCurrent()
	go sampler.run(interval)
	return sampler
}

func (s *snatMapSampler) Stop(final bpfnet.SNATMapStats) (bpfnet.SNATMapStats, uint64) {
	close(s.stop)
	<-s.done
	s.observe(final)
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.peak, s.samples
}

func (s *snatMapSampler) run(interval time.Duration) {
	defer close(s.done)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.observeCurrent()
		case <-s.stop:
			return
		}
	}
}

func (s *snatMapSampler) observeCurrent() {
	status, err := bpfnetstatus.Load(s.pinPath)
	if err != nil {
		return
	}
	s.observe(status.SNATMaps)
}

func (s *snatMapSampler) observe(sample bpfnet.SNATMapStats) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.samples++
	s.peak = natbench.MergeSNATMapStatsPeak(s.peak, sample)
}
