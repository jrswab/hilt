// Package memory manages L2 archival memory for Hilt.
// This includes daily note files, critical.md state preservation,
// and graph-based memory structures that supplement the active
// conversation context.
package memory

import (
	"os"
	"path/filepath"
	"strings"
)

// Reader reads workspace memory files.
type Reader struct {
	workspace string
}

// NewReader creates a new Reader for the given workspace directory.
func NewReader(workspace string) *Reader {
	return &Reader{workspace: workspace}
}

// Workspace returns the absolute workspace path this reader uses.
func (r *Reader) Workspace() string {
	return r.workspace
}

// ReadAGENTSMD reads AGENTS.md from the workspace root.
// Returns ("", nil) if the file does not exist or is empty.
func (r *Reader) ReadAGENTSMD() (string, error) {
	return r.readFile(filepath.Join(r.workspace, "AGENTS.md"))
}

// ReadCriticalMD reads memory/critical.md.
// Creates the memory/ subdirectory if it does not exist.
// Returns ("", nil) if the file does not exist or is empty.
func (r *Reader) ReadCriticalMD() (string, error) {
	memoryDir := filepath.Join(r.workspace, "memory")
	if err := os.MkdirAll(memoryDir, 0755); err != nil {
		return "", err
	}
	return r.readFile(filepath.Join(memoryDir, "critical.md"))
}

// ReadDailyNote reads memory/YYYY-MM-DD.md.
// Returns ("", nil) for missing or empty file.
// Returns (content, nil) if file exists.
// Returns error only for I/O failures other than "not found".
func (r *Reader) ReadDailyNote(date string) (string, error) {
	memoryDir := filepath.Join(r.workspace, "memory")
	return r.readFile(filepath.Join(memoryDir, date+".md"))
}

// EnsureDailyNoteSkeleton creates today's daily note if missing.
// Creates the memory/ subdirectory if needed.
// If the file already exists, does nothing and returns nil.
func (r *Reader) EnsureDailyNoteSkeleton(date string) error {
	memoryDir := filepath.Join(r.workspace, "memory")
	if err := os.MkdirAll(memoryDir, 0755); err != nil {
		return err
	}
	path := filepath.Join(memoryDir, date+".md")
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	skeleton := "# " + date + "\n\n## Wing: Work\n\n### Hall: decisions\n\n### Hall: events\n\n### Hall: discoveries\n\n### Hall: tasks\n\n### Hall: blockers\n\n## Wing: Health\n\n### Hall: metrics\n"
	return os.WriteFile(path, []byte(skeleton), 0644)
}

// readFile reads a file and returns its trimmed content.
// Returns ("", nil) if the file does not exist or is empty.
func (r *Reader) readFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	content := strings.TrimSpace(string(data))
	return content, nil
}
