package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestLoadOrBootstrapCreatesDefaultCLIConfig(t *testing.T) {
	setTestHome(t, t.TempDir())

	cwd := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatalf("mkdir cwd: %v", err)
	}

	result, err := LoadOrBootstrap(cwd)
	if err != nil {
		t.Fatalf("load or bootstrap: %v", err)
	}
	if !result.Created {
		t.Fatal("expected config to be created on first run")
	}

	want := DefaultCLIConfig(cwd)
	if !reflect.DeepEqual(result.Config, want) {
		t.Fatalf("expected %#v, got %#v", want, result.Config)
	}
}

func TestLoadOrBootstrapWritesDefaultConfigJSONShape(t *testing.T) {
	setTestHome(t, t.TempDir())

	cwd := filepath.Join(t.TempDir(), "project")
	if _, err := LoadOrBootstrap(cwd); err != nil {
		t.Fatalf("load or bootstrap: %v", err)
	}

	configPath, err := DefaultConfigPath()
	if err != nil {
		t.Fatalf("default config path: %v", err)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config file: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, `"codex_args": []`) {
		t.Fatalf("expected persisted config to include empty codex_args array, got %s", content)
	}
}

func TestLoadOrBootstrapUpdatesAutoWorkingDirectory(t *testing.T) {
	setTestHome(t, t.TempDir())

	oldCWD := filepath.Join(t.TempDir(), "old")
	newCWD := filepath.Join(t.TempDir(), "new")

	cfg := DefaultCLIConfig(oldCWD)
	if err := Save(cfg); err != nil {
		t.Fatalf("save initial config: %v", err)
	}

	result, err := LoadOrBootstrap(newCWD)
	if err != nil {
		t.Fatalf("load or bootstrap: %v", err)
	}
	if result.Created {
		t.Fatal("expected existing config to be reused")
	}
	if result.Config.WorkingDirectory != newCWD {
		t.Fatalf("expected working_directory %q, got %q", newCWD, result.Config.WorkingDirectory)
	}

	loaded, err := Load()
	if err != nil {
		t.Fatalf("load persisted config: %v", err)
	}
	if loaded.WorkingDirectory != newCWD {
		t.Fatalf("expected persisted working_directory %q, got %q", newCWD, loaded.WorkingDirectory)
	}
}

func TestLoadOrBootstrapLeavesManualWorkingDirectoryUntouched(t *testing.T) {
	setTestHome(t, t.TempDir())

	oldCWD := filepath.Join(t.TempDir(), "old")
	newCWD := filepath.Join(t.TempDir(), "new")

	cfg := DefaultCLIConfig(oldCWD)
	cfg.WorkingDirectoryMode = WorkingDirectoryModeManual
	if err := Save(cfg); err != nil {
		t.Fatalf("save initial config: %v", err)
	}

	result, err := LoadOrBootstrap(newCWD)
	if err != nil {
		t.Fatalf("load or bootstrap: %v", err)
	}
	if result.Config.WorkingDirectory != oldCWD {
		t.Fatalf("expected manual working_directory to remain %q, got %q", oldCWD, result.Config.WorkingDirectory)
	}

	loaded, err := Load()
	if err != nil {
		t.Fatalf("load persisted config: %v", err)
	}
	if loaded.WorkingDirectory != oldCWD {
		t.Fatalf("expected persisted working_directory %q, got %q", oldCWD, loaded.WorkingDirectory)
	}
}

func TestLoadOrBootstrapTreatsLegacyModeAsManual(t *testing.T) {
	setTestHome(t, t.TempDir())

	oldCWD := filepath.Join(t.TempDir(), "old")
	newCWD := filepath.Join(t.TempDir(), "new")

	cfg := DefaultCLIConfig(oldCWD)
	cfg.WorkingDirectoryMode = ""
	if err := Save(cfg); err != nil {
		t.Fatalf("save initial config: %v", err)
	}

	result, err := LoadOrBootstrap(newCWD)
	if err != nil {
		t.Fatalf("load or bootstrap: %v", err)
	}
	if result.Config.WorkingDirectory != oldCWD {
		t.Fatalf("expected legacy mode working_directory to remain %q, got %q", oldCWD, result.Config.WorkingDirectory)
	}
}

func TestLoadOrBootstrapReturnsErrorForInvalidExistingConfig(t *testing.T) {
	setTestHome(t, t.TempDir())

	configPath, err := DefaultConfigPath()
	if err != nil {
		t.Fatalf("default config path: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}

	invalid := `{"backend_type":"cli","codex_command":"codex","codex_args":[],"working_directory":"/tmp/project","permission_mode":"write"}`
	if err := os.WriteFile(configPath, []byte(invalid), 0o600); err != nil {
		t.Fatalf("write invalid config: %v", err)
	}

	result, err := LoadOrBootstrap("/tmp/other")
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "permission_mode") {
		t.Fatalf("expected permission_mode validation error, got %v", err)
	}
	if result.Created {
		t.Fatal("expected invalid existing config not to be created")
	}
	if result.Config.CodexCommand != "codex" {
		t.Fatalf("expected decoded config to be returned, got %#v", result.Config)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config file: %v", err)
	}
	if string(data) != invalid {
		t.Fatalf("expected invalid file to remain unchanged, got %q", string(data))
	}
}

func TestLoadOrBootstrapReturnsErrorForEmptyConfigFile(t *testing.T) {
	setTestHome(t, t.TempDir())

	configPath, err := DefaultConfigPath()
	if err != nil {
		t.Fatalf("default config path: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.WriteFile(configPath, []byte{}, 0o600); err != nil {
		t.Fatalf("write empty config file: %v", err)
	}

	result, err := LoadOrBootstrap("/tmp/project")
	if err == nil {
		t.Fatal("expected decode error for empty config file")
	}
	if result.Created {
		t.Fatal("expected empty existing config not to be recreated")
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config file: %v", err)
	}
	if len(data) != 0 {
		t.Fatalf("expected empty config file to remain empty, got %q", string(data))
	}
}

func TestLoadOrBootstrapReturnsErrorForEmptyWorkingDirectoryInput(t *testing.T) {
	setTestHome(t, t.TempDir())

	_, err := LoadOrBootstrap("")
	if err == nil {
		t.Fatal("expected error for empty working directory")
	}
	if !strings.Contains(err.Error(), "working directory") {
		t.Fatalf("expected clear working directory error, got %v", err)
	}
}

func TestLoadOrBootstrapConcurrentCreateLeavesSingleValidConfig(t *testing.T) {
	setTestHome(t, t.TempDir())

	cwdA := filepath.Join(t.TempDir(), "project-a")
	cwdB := filepath.Join(t.TempDir(), "project-b")

	var wg sync.WaitGroup
	wg.Add(2)

	type callResult struct {
		result BootstrapResult
		err    error
	}
	results := make(chan callResult, 2)
	start := make(chan struct{})

	go func() {
		defer wg.Done()
		<-start
		res, err := LoadOrBootstrap(cwdA)
		results <- callResult{result: res, err: err}
	}()
	go func() {
		defer wg.Done()
		<-start
		res, err := LoadOrBootstrap(cwdB)
		results <- callResult{result: res, err: err}
	}()

	close(start)
	wg.Wait()
	close(results)

	createdCount := 0
	winnerCWD := ""
	for item := range results {
		if item.err != nil {
			t.Fatalf("load or bootstrap in goroutine: %v", item.err)
		}
		if item.result.Created {
			createdCount++
			winnerCWD = item.result.Config.WorkingDirectory
		}
	}

	if createdCount != 1 {
		t.Fatalf("expected exactly one creator, got %d", createdCount)
	}

	loaded, err := Load()
	if err != nil {
		t.Fatalf("load final config: %v", err)
	}
	if loaded.WorkingDirectory != winnerCWD {
		t.Fatalf("expected final working_directory %q from winning create, got %q", winnerCWD, loaded.WorkingDirectory)
	}
	if loaded.CodexArgs == nil {
		t.Fatal("expected codex_args to be non-nil empty slice")
	}
}
