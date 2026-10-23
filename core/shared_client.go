package core

import (
	"io"
	"sync"
	"sync/atomic"
)

// SharedClient holds a provider module's process-wide client and enforces the
// repeat-Initialize contract in one place. Setups run one at a time, so a
// call that arrives while the first setup is still running waits for it and
// then gets the same answer as a call made afterwards: nil for the same
// account or one that names no account settings, a config error naming the
// differing setting otherwise. Reads through Get and IsInitialized never wait
// for a setup in progress.
type SharedClient[T io.Closer] struct {
	setupMu               sync.Mutex
	waitingSetups         atomic.Int32
	mu                    sync.RWMutex
	client                T
	ready                 bool
	reveniumOf            func(T) *ReveniumConfig
	standaloneConstructor string
}

func NewSharedClient[T io.Closer](standaloneConstructor string, reveniumOf func(T) *ReveniumConfig) *SharedClient[T] {
	return &SharedClient[T]{reveniumOf: reveniumOf, standaloneConstructor: standaloneConstructor}
}

// Initialize installs the client build returns, unless a client is already
// installed, in which case it only checks that named asks for the same
// Revenium account. named must be taken from the caller's options before any
// environment values are merged in.
func (s *SharedClient[T]) Initialize(named *ReveniumConfig, build func() (T, error)) error {
	s.waitingSetups.Add(1)
	s.setupMu.Lock()
	s.waitingSetups.Add(-1)
	defer s.setupMu.Unlock()

	if active, ok := s.Current(); ok {
		return CheckReinitialization(s.reveniumOf(active), named, s.standaloneConstructor)
	}
	client, err := build()
	if err != nil {
		return err
	}
	s.install(client)
	return nil
}

func (s *SharedClient[T]) install(client T) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.client = client
	s.ready = true
}

// Current returns the installed client and whether there is one.
func (s *SharedClient[T]) Current() (T, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.client, s.ready
}

func (s *SharedClient[T]) Get() (T, error) {
	client, ok := s.Current()
	if !ok {
		return client, NewConfigError("middleware not initialized, call Initialize() first", nil)
	}
	return client, nil
}

func (s *SharedClient[T]) IsInitialized() bool {
	_, ok := s.Current()
	return ok
}

// Reset forgets the installed client and returns it so the caller can close
// it; the next Initialize sets up a new one.
func (s *SharedClient[T]) Reset() (T, bool) {
	s.setupMu.Lock()
	defer s.setupMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	client, ok := s.client, s.ready
	var zero T
	s.client, s.ready = zero, false
	return client, ok
}

// ResetAndClose forgets the installed client and closes it outside the
// locks, returning what Close reported.
func (s *SharedClient[T]) ResetAndClose() error {
	previous, ok := s.Reset()
	if !ok {
		return nil
	}
	return previous.Close()
}
