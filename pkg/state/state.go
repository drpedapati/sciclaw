package state

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// State represents the persistent state for a workspace.
// It includes information about the last active channel/chat.
type State struct {
	// LastChannel is the last channel used for communication
	LastChannel string `json:"last_channel,omitempty"`

	// LastChatID is the last chat ID used for communication
	LastChatID string `json:"last_chat_id,omitempty"`

	// Timestamp is the last time this state was updated
	Timestamp time.Time `json:"timestamp"`
}

// Manager manages persistent state with atomic saves.
type Manager struct {
	workspace string
	state     *State
	mu        sync.RWMutex
	stateFile string
}

var (
	stateReadFile         = os.ReadFile
	stateBootstrapTimeout = 750 * time.Millisecond
)

// NewManager creates a new state manager for the given workspace.
func NewManager(workspace string) *Manager {
	stateDir := filepath.Join(workspace, "state")
	stateFile := filepath.Join(stateDir, "state.json")
	oldStateFile := filepath.Join(workspace, "state.json")

	// Create state directory if it doesn't exist
	os.MkdirAll(stateDir, 0755)

	sm := &Manager{
		workspace: workspace,
		stateFile: stateFile,
		state:     &State{},
	}

	loadedState, loadedFromLegacy, err := loadBootstrapWithTimeout(stateFile, oldStateFile, stateBootstrapTimeout)
	if err != nil {
		log.Printf("[WARN] state: bootstrap skipped for %s: %v", workspace, err)
	} else if loadedState != nil {
		sm.state = loadedState
		if loadedFromLegacy {
			// Keep startup non-blocking on cloud-backed filesystems.
			// The state will be persisted in the new location on next write.
			log.Printf("[INFO] state: loaded legacy state from %s", oldStateFile)
		}
	}

	return sm
}

// SetLastChannel atomically updates the last channel and saves the state.
// This method uses a temp file + rename pattern for atomic writes,
// ensuring that the state file is never corrupted even if the process crashes.
func (sm *Manager) SetLastChannel(channel string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Update state
	sm.state.LastChannel = channel
	sm.state.Timestamp = time.Now()

	// Atomic save using temp file + rename
	if err := sm.saveAtomic(); err != nil {
		return fmt.Errorf("failed to save state atomically: %w", err)
	}

	return nil
}

// SetLastChatID atomically updates the last chat ID and saves the state.
func (sm *Manager) SetLastChatID(chatID string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Update state
	sm.state.LastChatID = chatID
	sm.state.Timestamp = time.Now()

	// Atomic save using temp file + rename
	if err := sm.saveAtomic(); err != nil {
		return fmt.Errorf("failed to save state atomically: %w", err)
	}

	return nil
}

// GetLastChannel returns the last channel from the state.
func (sm *Manager) GetLastChannel() string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.state.LastChannel
}

// GetLastChatID returns the last chat ID from the state.
func (sm *Manager) GetLastChatID() string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.state.LastChatID
}

// GetTimestamp returns the timestamp of the last state update.
func (sm *Manager) GetTimestamp() time.Time {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.state.Timestamp
}

// saveAtomic performs an atomic save using temp file + rename.
// This ensures that the state file is never corrupted:
// 1. Write to a temp file
// 2. Rename temp file to target (atomic on POSIX systems)
// 3. If rename fails, cleanup the temp file
//
// Must be called with the lock held.
func (sm *Manager) saveAtomic() error {
	// Create temp file in the same directory as the target
	tempFile := sm.stateFile + ".tmp"

	// Marshal state to JSON
	data, err := json.MarshalIndent(sm.state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	// Write to temp file
	if err := os.WriteFile(tempFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write temp file: %w", err)
	}

	// Atomic rename from temp to target
	if err := os.Rename(tempFile, sm.stateFile); err != nil {
		// Cleanup temp file if rename fails
		os.Remove(tempFile)
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	return nil
}

// load loads the state from disk.
func (sm *Manager) load() error {
	loaded, err := loadStateFromPath(sm.stateFile)
	if err != nil {
		return err
	}
	if loaded != nil {
		sm.state = loaded
	}
	return nil
}

func loadBootstrapWithTimeout(stateFile, oldStateFile string, timeout time.Duration) (*State, bool, error) {
	if timeout <= 0 {
		return loadBootstrap(stateFile, oldStateFile)
	}

	type result struct {
		state      *State
		fromLegacy bool
		err        error
	}

	done := make(chan result, 1)
	go func() {
		st, legacy, err := loadBootstrap(stateFile, oldStateFile)
		done <- result{
			state:      st,
			fromLegacy: legacy,
			err:        err,
		}
	}()

	select {
	case out := <-done:
		return out.state, out.fromLegacy, out.err
	case <-time.After(timeout):
		return nil, false, fmt.Errorf("state load timed out")
	}
}

func loadBootstrap(stateFile, oldStateFile string) (*State, bool, error) {
	if st, err := loadStateFromPath(stateFile); err != nil {
		return nil, false, err
	} else if st != nil {
		return st, false, nil
	}

	if st, err := loadStateFromPath(oldStateFile); err != nil {
		return nil, false, err
	} else if st != nil {
		return st, true, nil
	}

	return nil, false, nil
}

func loadStateFromPath(path string) (*State, error) {
	data, err := stateReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read state file %s: %w", path, err)
	}

	var st State
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, fmt.Errorf("failed to unmarshal state %s: %w", path, err)
	}
	return &st, nil
}
