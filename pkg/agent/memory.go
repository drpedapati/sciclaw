// PicoClaw - Ultra-lightweight personal AI agent
// Inspired by and based on nanobot: https://github.com/HKUDS/nanobot
// License: MIT
//
// Copyright (c) 2026 PicoClaw contributors

package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// MemoryStore manages persistent memory for the agent.
// Supports daily notes (memory/YYYY-MM-DD.md) and long-term memory (MEMORY.md).
type MemoryStore struct {
	workspace  string
	memoryDir  string
	memoryFile string
}

// NewMemoryStore creates a new MemoryStore with the given workspace path.
// It ensures the memory directory exists.
func NewMemoryStore(workspace string) *MemoryStore {
	memoryDir := filepath.Join(workspace, "memory")
	memoryFile := filepath.Join(memoryDir, "MEMORY.md")

	// Ensure memory directory exists
	os.MkdirAll(memoryDir, 0755)

	return &MemoryStore{
		workspace:  workspace,
		memoryDir:  memoryDir,
		memoryFile: memoryFile,
	}
}

// getMemoryDir returns the memory directory path.
func (ms *MemoryStore) getMemoryDir() string {
	return ms.memoryDir
}

// getMemoryFile returns the long-term memory file path.
func (ms *MemoryStore) getMemoryFile() string {
	return ms.memoryFile
}

// getTodayFile returns the path to today's memory file (YYYY-MM-DD.md).
func (ms *MemoryStore) getTodayFile() string {
	today := time.Now().Format("2006-01-02")
	return filepath.Join(ms.memoryDir, today+".md")
}

// ReadToday reads today's memory notes.
// Returns empty string if the file doesn't exist.
func (ms *MemoryStore) ReadToday() string {
	todayFile := ms.getTodayFile()
	if data, err := os.ReadFile(todayFile); err == nil {
		return string(data)
	}
	return ""
}

// AppendToday appends content to today's memory notes.
// If the file doesn't exist, it creates a new file with a date header.
func (ms *MemoryStore) AppendToday(content string) error {
	todayFile := ms.getTodayFile()

	var existingContent string
	if data, err := os.ReadFile(todayFile); err == nil {
		existingContent = string(data)
	}

	var newContent string
	if existingContent == "" {
		// Add header for new day
		header := fmt.Sprintf("# %s\n\n", time.Now().Format("2006-01-02"))
		newContent = header + content
	} else {
		// Append to existing content
		newContent = existingContent + "\n" + content
	}

	return os.WriteFile(todayFile, []byte(newContent), 0644)
}

// ReadLongTerm reads the long-term memory (MEMORY.md).
// Returns empty string if the file doesn't exist.
func (ms *MemoryStore) ReadLongTerm() string {
	if data, err := os.ReadFile(ms.memoryFile); err == nil {
		return string(data)
	}
	return ""
}

// WriteLongTerm writes content to the long-term memory file (MEMORY.md).
func (ms *MemoryStore) WriteLongTerm(content string) error {
	return os.WriteFile(ms.memoryFile, []byte(content), 0644)
}

// GetRecentMemories returns memories from the last N days.
// It reads and combines the contents of memory files from the past days.
// Contents are joined with "---" separator.
func (ms *MemoryStore) GetRecentMemories(days int) string {
	var memories []string

	for i := 0; i < days; i++ {
		date := time.Now().AddDate(0, 0, -i)
		dateStr := date.Format("2006-01-02")
		filePath := filepath.Join(ms.memoryDir, dateStr+".md")

		if data, err := os.ReadFile(filePath); err == nil {
			memories = append(memories, string(data))
		}
	}

	if len(memories) == 0 {
		return ""
	}

	return strings.Join(memories, "\n\n---\n\n")
}

// GetMemoryContext returns formatted memory context for the agent prompt.
// It includes long-term memory and today's notes sections if they exist.
// Returns empty string if no memory exists.
func (ms *MemoryStore) GetMemoryContext() string {
	var parts []string

	// Long-term memory
	longTerm := ms.ReadLongTerm()
	if longTerm != "" {
		parts = append(parts, "## Long-term Memory\n\n"+longTerm)
	}

	// Today's notes
	today := ms.ReadToday()
	if today != "" {
		parts = append(parts, "## Today's Notes\n\n"+today)
	}

	if len(parts) == 0 {
		return ""
	}

	return strings.Join(parts, "\n\n")
}
