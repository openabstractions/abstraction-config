package service

import (
	"errors"
	"sync"

	config "github.com/openabstractions/abstraction-config/go"
	wire "github.com/openabstractions/abstraction-config/go/abstraction/config"
)

// RestartState is the native consumer's admitted snapshot. Its zero value is
// ready to use. Admission invalidates the existing Config observation gate.
// It records successful consumer admission, rather than reading configuration.
type RestartState struct {
	mu      sync.Mutex
	values  map[string]*wire.Value
	changed chan struct{}
}

func cloneRestartValue(value *wire.Value) *wire.Value {
	if value == nil {
		return nil
	}
	data, err := wire.ToJSON(value)
	if err != nil {
		panic(err)
	} // Admit validates before retaining a value.
	var copy wire.Value
	if err := wire.FromJSON(data, &copy); err != nil {
		panic(err)
	}
	return &copy
}

func (s *RestartState) Admit(key string, value *wire.Value) error {
	var descriptor *wire.KeyDescriptor
	for _, candidate := range wire.ConfigKeyDescriptors() {
		if candidate.Key == key {
			d := candidate
			descriptor = &d
			break
		}
	}
	if descriptor == nil || descriptor.Apply != wire.ApplyModeRestart {
		return errors.New("config: restart admission requires a restart key")
	}
	if value != nil {
		if err := config.ValidateSetting(key, value); err != nil {
			return err
		}
	}
	copy := cloneRestartValue(value)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.values == nil {
		s.values = make(map[string]*wire.Value)
	}
	s.values[key] = copy
	if s.changed != nil {
		close(s.changed)
	}
	s.changed = make(chan struct{})
	return nil
}

func (s *RestartState) snapshot() (map[string]*wire.Value, <-chan struct{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.changed == nil {
		s.changed = make(chan struct{})
	}
	values := make(map[string]*wire.Value, len(s.values))
	for key, value := range s.values {
		values[key] = cloneRestartValue(value)
	}
	return values, s.changed
}
