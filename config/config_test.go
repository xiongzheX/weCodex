package config

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func setTestHome(t *testing.T, home string) {
	t.Helper()

	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	volume := filepath.VolumeName(home)
	if volume == "" {
		t.Setenv("HOMEDRIVE", "")
		t.Setenv("HOMEPATH", home)
		return
	}

	t.Setenv("HOMEDRIVE", volume)
	homePath := strings.TrimPrefix(home, volume)
	if homePath == "" {
		homePath = string(os.PathSeparator)
	}
	t.Setenv("HOMEPATH", homePath)
}

func TestValidateRequiresWorkingDirectory(t *testing.T) {
	cfg := Config{CodexCommand: "/usr/local/bin/codex", CodexArgs: []string{"acp"}, PermissionMode: "readonly"}
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "working_directory") {
		t.Fatalf("expected missing working_directory error, got %v", err)
	}
}

func TestValidateRequiresCodexArgs(t *testing.T) {
	cfg := Config{
		CodexCommand:     "/usr/local/bin/codex",
		WorkingDirectory: "/tmp/project",
		PermissionMode:   "readonly",
	}
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "codex_args") {
		t.Fatalf("expected codex_args validation error, got %v", err)
	}
}

func TestValidateAllowsCLIBackendWithoutCodexArgs(t *testing.T) {
	cfg := Config{
		BackendType:      "cli",
		CodexCommand:     "/usr/local/bin/codex",
		WorkingDirectory: "/tmp/project",
		PermissionMode:   "readonly",
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected cli backend without codex_args to be valid, got %v", err)
	}
}

func TestEffectiveWorkingDirectoryModeDefaultsToManual(t *testing.T) {
	cfg := Config{}
	if got := cfg.EffectiveWorkingDirectoryMode(); got != WorkingDirectoryModeManual {
		t.Fatalf("expected default effective working directory mode %q, got %q", WorkingDirectoryModeManual, got)
	}
}

func TestValidateAllowsAutoWorkingDirectoryMode(t *testing.T) {
	cfg := Config{
		CodexCommand:          "/usr/local/bin/codex",
		CodexArgs:             []string{"acp"},
		WorkingDirectory:      "/tmp/project",
		WorkingDirectoryMode:  WorkingDirectoryModeAuto,
		PermissionMode:        "readonly",
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected auto working_directory_mode to be valid, got %v", err)
	}
}

func TestValidateRejectsUnknownWorkingDirectoryMode(t *testing.T) {
	cfg := Config{
		CodexCommand:          "/usr/local/bin/codex",
		CodexArgs:             []string{"acp"},
		WorkingDirectory:      "/tmp/project",
		WorkingDirectoryMode:  "unknown",
		PermissionMode:        "readonly",
	}

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "working_directory_mode") {
		t.Fatalf("expected working_directory_mode validation error, got %v", err)
	}
}

func TestValidateRequiresACPArgsForACPBackend(t *testing.T) {
	cfg := Config{
		BackendType:      "acp",
		CodexCommand:     "/usr/local/bin/codex",
		WorkingDirectory: "/tmp/project",
		PermissionMode:   "readonly",
	}

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "codex_args") {
		t.Fatalf("expected codex_args validation error for acp backend, got %v", err)
	}
}

func TestValidateRejectsUnknownBackendType(t *testing.T) {
	cfg := Config{
		BackendType:      "unknown",
		CodexCommand:     "/usr/local/bin/codex",
		CodexArgs:        []string{"acp"},
		WorkingDirectory: "/tmp/project",
		PermissionMode:   "readonly",
	}

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "backend_type") {
		t.Fatalf("expected backend_type validation error, got %v", err)
	}
}

func TestValidateRejectsUnknownPermissionMode(t *testing.T) {
	cfg := Config{
		CodexCommand:     "/usr/local/bin/codex",
		CodexArgs:        []string{"acp"},
		WorkingDirectory: "/tmp/project",
		PermissionMode:   "write",
	}
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "permission_mode") {
		t.Fatalf("expected permission_mode validation error, got %v", err)
	}
}

func TestSaveWrites0600Permissions(t *testing.T) {
	setTestHome(t, t.TempDir())

	cfg := Config{
		CodexCommand:     "/usr/local/bin/codex",
		CodexArgs:        []string{"acp"},
		WorkingDirectory: "/tmp/project",
		PermissionMode:   "readonly",
	}

	if err := Save(cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	configPath, err := DefaultConfigPath()
	if err != nil {
		t.Fatalf("default config path: %v", err)
	}

	info, err := os.Stat(configPath)
	if err != nil {
		t.Fatalf("stat config: %v", err)
	}

	perm := info.Mode().Perm()
	if runtime.GOOS == "windows" {
		if perm&0o077 != 0 {
			t.Fatalf("expected config file to remain owner-only on windows, got %o", perm)
		}
		return
	}
	if perm != 0o600 {
		t.Fatalf("expected 0600 permissions, got %o", perm)
	}
}

func TestSavePreservesEmptyCodexArgsArray(t *testing.T) {
	setTestHome(t, t.TempDir())

	cfg := Config{
		BackendType:      "cli",
		CodexCommand:     "/usr/local/bin/codex",
		CodexArgs:        []string{},
		WorkingDirectory: "/tmp/project",
		PermissionMode:   "readonly",
	}

	if err := Save(cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	configPath, err := DefaultConfigPath()
	if err != nil {
		t.Fatalf("default config path: %v", err)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}

	if !strings.Contains(string(data), `"codex_args": []`) {
		t.Fatalf("expected saved config JSON to include empty codex_args array, got %s", string(data))
	}
}

func TestSaveNormalizesNilCodexArgsArrayForCLIBackend(t *testing.T) {
	setTestHome(t, t.TempDir())

	cfg := Config{
		BackendType:      "cli",
		CodexCommand:     "/usr/local/bin/codex",
		WorkingDirectory: "/tmp/project",
		PermissionMode:   "readonly",
	}

	if err := Save(cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	configPath, err := DefaultConfigPath()
	if err != nil {
		t.Fatalf("default config path: %v", err)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, `"codex_args": []`) {
		t.Fatalf("expected saved config JSON to normalize nil codex_args to empty array, got %s", content)
	}
	if strings.Contains(content, `"codex_args": null`) {
		t.Fatalf("expected saved config JSON not to contain null codex_args, got %s", content)
	}
}

func TestLoadRoundTrip(t *testing.T) {
	setTestHome(t, t.TempDir())

	want := Config{
		CodexCommand:          "/usr/local/bin/codex",
		CodexArgs:             []string{"acp"},
		WorkingDirectory:      "/tmp/project",
		WorkingDirectoryMode:  WorkingDirectoryModeAuto,
		PermissionMode:        "readonly",
		WechatAccountsDir:     "/tmp/accounts",
	}

	if err := Save(want); err != nil {
		t.Fatalf("save config: %v", err)
	}

	got, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("expected %#v, got %#v", want, got)
	}
}

func TestLoadPreservesBackendType(t *testing.T) {
	setTestHome(t, t.TempDir())

	want := Config{
		BackendType:      "cli",
		CodexCommand:     "/usr/local/bin/codex",
		WorkingDirectory: "/tmp/project",
		PermissionMode:   "readonly",
	}

	if err := Save(want); err != nil {
		t.Fatalf("save config: %v", err)
	}

	got, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if got.BackendType != want.BackendType {
		t.Fatalf("expected backend_type %q, got %q", want.BackendType, got.BackendType)
	}
}

func TestDefaultConfigPathReturnsHomeScopedPath(t *testing.T) {
	home := t.TempDir()
	setTestHome(t, home)

	path, err := DefaultConfigPath()
	if err != nil {
		t.Fatalf("default config path: %v", err)
	}
	want := filepath.Join(home, ".weCodex", "config.json")
	if path != want {
		t.Fatalf("expected %q, got %q", want, path)
	}
}

func TestCredentialsPathUsesWechatAccountsDirOverride(t *testing.T) {
	overrideDir := filepath.Join(t.TempDir(), "wechat-accounts")
	cfg := Config{WechatAccountsDir: overrideDir}
	path, err := CredentialsPath(cfg)
	if err != nil {
		t.Fatalf("credentials path: %v", err)
	}
	want := filepath.Join(overrideDir, "account.json")
	if path != want {
		t.Fatalf("expected %q, got %q", want, path)
	}
}

func TestCredentialsPathUsesDefaultHomeScopedPath(t *testing.T) {
	home := t.TempDir()
	setTestHome(t, home)

	path, err := CredentialsPath(Config{})
	if err != nil {
		t.Fatalf("credentials path: %v", err)
	}

	want := filepath.Join(home, ".weCodex", "account.json")
	if path != want {
		t.Fatalf("expected %q, got %q", want, path)
	}
}

func TestLoadMissingConfigReturnsNotExist(t *testing.T) {
	setTestHome(t, t.TempDir())

	got, err := Load()
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected os.ErrNotExist, got %v", err)
	}
	if !reflect.DeepEqual(got, Config{}) {
		t.Fatalf("expected zero config, got %#v", got)
	}
}

func TestLoadReturnsDecodedConfigAlongsideValidationError(t *testing.T) {
	setTestHome(t, t.TempDir())

	configPath, err := DefaultConfigPath()
	if err != nil {
		t.Fatalf("default config path: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}

	contents := `{"codex_command":"/usr/local/bin/codex","codex_args":["acp"],"working_directory":"/tmp/project","permission_mode":"write","wechat_accounts_dir":"/tmp/accounts"}`
	if err := os.WriteFile(configPath, []byte(contents), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	got, err := Load()
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
	if !strings.Contains(err.Error(), "permission_mode") {
		t.Fatalf("expected permission_mode validation error, got %v", err)
	}
	if got.CodexCommand != "/usr/local/bin/codex" {
		t.Fatalf("expected decoded codex command, got %q", got.CodexCommand)
	}
	if got.WechatAccountsDir != "/tmp/accounts" {
		t.Fatalf("expected decoded wechat accounts dir, got %q", got.WechatAccountsDir)
	}
}

func TestCredentialsFileExistsUsesResolvedPath(t *testing.T) {
	home := t.TempDir()
	setTestHome(t, home)

	exists, err := CredentialsFileExists(Config{})
	if err != nil {
		t.Fatalf("credentials file exists before create: %v", err)
	}
	if exists {
		t.Fatal("expected credentials file to be missing")
	}

	path, err := CredentialsPath(Config{})
	if err != nil {
		t.Fatalf("credentials path: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir credentials dir: %v", err)
	}
	if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
		t.Fatalf("write credentials file: %v", err)
	}

	exists, err = CredentialsFileExists(Config{})
	if err != nil {
		t.Fatalf("credentials file exists after create: %v", err)
	}
	if !exists {
		t.Fatal("expected credentials file to exist")
	}
}

func TestCodexCommandExistsUsesLookPath(t *testing.T) {
	binDir := t.TempDir()
	commandName := "mock-codex-command"
	commandPath := filepath.Join(binDir, commandName)
	if runtime.GOOS == "windows" {
		commandName += ".exe"
		commandPath += ".exe"
	}

	if err := os.WriteFile(commandPath, []byte(""), 0o700); err != nil {
		t.Fatalf("write mock command: %v", err)
	}

	originalPath := os.Getenv("PATH")
	if runtime.GOOS == "windows" {
		t.Setenv("PATHEXT", ".EXE")
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+originalPath)

	exists, err := CodexCommandExists(commandName)
	if err != nil {
		t.Fatalf("check existing command: %v", err)
	}
	if !exists {
		t.Fatal("expected mock command to exist")
	}

	exists, err = CodexCommandExists("definitely-not-a-real-command-for-wecodex-tests")
	if err != nil {
		t.Fatalf("check missing command: %v", err)
	}
	if exists {
		t.Fatal("expected missing command to be unresolved")
	}
}
