package memory

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewReader(t *testing.T) {
	workspace := t.TempDir()
	r := NewReader(workspace)
	if r.workspace != workspace {
		t.Errorf("workspace = %q, want %q", r.workspace, workspace)
	}
	if r.Workspace() != workspace {
		t.Errorf("Workspace() = %q, want %q", r.Workspace(), workspace)
	}
}

func TestReadAGENTSMD(t *testing.T) {
	workspace := t.TempDir()
	r := NewReader(workspace)

	t.Run("returns empty when file missing", func(t *testing.T) {
		content, err := r.ReadAGENTSMD()
		if err != nil {
			t.Fatalf("ReadAGENTSMD: %v", err)
		}
		if content != "" {
			t.Errorf("expected empty, got %q", content)
		}
	})

	t.Run("returns content when file exists", func(t *testing.T) {
		path := filepath.Join(workspace, "AGENTS.md")
		if err := os.WriteFile(path, []byte("# Agents\n\nTest rules."), 0644); err != nil {
			t.Fatalf("writing AGENTS.md: %v", err)
		}
		content, err := r.ReadAGENTSMD()
		if err != nil {
			t.Fatalf("ReadAGENTSMD: %v", err)
		}
		if content != "# Agents\n\nTest rules." {
			t.Errorf("content = %q", content)
		}
	})
}

func TestReadCriticalMD(t *testing.T) {
	workspace := t.TempDir()
	r := NewReader(workspace)

	t.Run("creates memory dir and returns empty when missing", func(t *testing.T) {
		content, err := r.ReadCriticalMD()
		if err != nil {
			t.Fatalf("ReadCriticalMD: %v", err)
		}
		if content != "" {
			t.Errorf("expected empty, got %q", content)
		}
		memoryDir := filepath.Join(workspace, "memory")
		if _, err := os.Stat(memoryDir); os.IsNotExist(err) {
			t.Error("memory/ subdirectory should have been created")
		}
	})

	t.Run("returns content when file exists", func(t *testing.T) {
		path := filepath.Join(workspace, "memory", "critical.md")
		if err := os.WriteFile(path, []byte("Important state"), 0644); err != nil {
			t.Fatalf("writing critical.md: %v", err)
		}
		content, err := r.ReadCriticalMD()
		if err != nil {
			t.Fatalf("ReadCriticalMD: %v", err)
		}
		if content != "Important state" {
			t.Errorf("content = %q", content)
		}
	})
}

func TestEnsureDailyNoteSkeleton(t *testing.T) {
	workspace := t.TempDir()
	r := NewReader(workspace)

	t.Run("creates skeleton for missing file", func(t *testing.T) {
		if err := r.EnsureDailyNoteSkeleton("2024-01-15"); err != nil {
			t.Fatalf("EnsureDailyNoteSkeleton: %v", err)
		}
		path := filepath.Join(workspace, "memory", "2024-01-15.md")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading skeleton: %v", err)
		}

		expected := `# 2024-01-15

## Wing: Work

### Hall: decisions

### Hall: events

### Hall: discoveries

### Hall: tasks

### Hall: blockers

## Wing: Health

### Hall: metrics
`
		if string(data) != expected {
			t.Errorf("skeleton mismatch:\ngot:\n%s\nwant:\n%s", string(data), expected)
		}
	})

	t.Run("does nothing if file exists", func(t *testing.T) {
		path := filepath.Join(workspace, "memory", "2024-01-16.md")
		if err := os.WriteFile(path, []byte("existing content"), 0644); err != nil {
			t.Fatalf("writing daily note: %v", err)
		}
		if err := r.EnsureDailyNoteSkeleton("2024-01-16"); err != nil {
			t.Fatalf("EnsureDailyNoteSkeleton: %v", err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading daily note: %v", err)
		}
		if string(data) != "existing content" {
			t.Errorf("file was overwritten, got %q", string(data))
		}
	})
}

func TestReadDailyNote(t *testing.T) {
	workspace := t.TempDir()
	r := NewReader(workspace)

	t.Run("returns empty when file missing", func(t *testing.T) {
		content, err := r.ReadDailyNote("2024-01-15")
		if err != nil {
			t.Fatalf("ReadDailyNote: %v", err)
		}
		if content != "" {
			t.Errorf("expected empty, got %q", content)
		}
	})

	t.Run("returns content for existing file", func(t *testing.T) {
		memoryDir := filepath.Join(workspace, "memory")
		if err := os.MkdirAll(memoryDir, 0755); err != nil {
			t.Fatalf("creating memory dir: %v", err)
		}
		path := filepath.Join(memoryDir, "2024-01-17.md")
		if err := os.WriteFile(path, []byte("Today's note"), 0644); err != nil {
			t.Fatalf("writing daily note: %v", err)
		}
		content, err := r.ReadDailyNote("2024-01-17")
		if err != nil {
			t.Fatalf("ReadDailyNote: %v", err)
		}
		if content != "Today's note" {
			t.Errorf("content = %q", content)
		}
	})
}

func TestReadFileTrimsWhitespace(t *testing.T) {
	workspace := t.TempDir()
	r := NewReader(workspace)

	t.Run("trims leading and trailing whitespace", func(t *testing.T) {
		path := filepath.Join(workspace, "test.md")
		if err := os.WriteFile(path, []byte("  \n  hello  \n  "), 0644); err != nil {
			t.Fatalf("writing file: %v", err)
		}
		content, err := r.readFile(path)
		if err != nil {
			t.Fatalf("readFile: %v", err)
		}
		if content != "hello" {
			t.Errorf("content = %q, want \"hello\"", content)
		}
	})

	t.Run("returns empty for empty file", func(t *testing.T) {
		path := filepath.Join(workspace, "empty.md")
		if err := os.WriteFile(path, []byte(""), 0644); err != nil {
			t.Fatalf("writing file: %v", err)
		}
		content, err := r.readFile(path)
		if err != nil {
			t.Fatalf("readFile: %v", err)
		}
		if content != "" {
			t.Errorf("content = %q, want empty", content)
		}
	})
}
