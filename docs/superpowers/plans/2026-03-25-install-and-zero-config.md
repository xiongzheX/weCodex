# Install and Zero-Config Bootstrap Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make fresh `wecodex` installs usable with `wecodex status`, `wecodex login`, and `wecodex start` without manually creating `~/.weCodex/config.json`, while standardizing the public command name to lowercase and adding a Darwin install script.

**Architecture:** Keep the behavior change centered in the `config` package. Add an explicit `working_directory_mode` field plus a small bootstrap API that creates the default CLI config on first run and refreshes `working_directory` on later runs only when the config is in `auto` mode. Keep the command layer thin: `login`, `status`, and `start` should all resolve runtime config through one shared helper, print the same fixed bootstrap notice when a config file is first created, then continue with their existing command-specific logic.

**Tech Stack:** Go 1.24, Cobra CLI, stdlib `os`/`path/filepath`/`encoding/json`, existing `config` and `ilink` packages, Bash install script for Darwin.

---

## Context and constraints

- The approved spec is `/Users/xiongzhe/myIdea/weCodex/docs/superpowers/specs/2026-03-25-install-and-zero-config-design.md`.
- Today `cmd/login.go`, `cmd/status.go`, and `cmd/start.go` still load config directly instead of sharing a bootstrap path.
- `cmd/root.go` still exposes `Use: "weCodex"` and ACP-specific short text.
- There is currently no lowercase `go install github.com/xiongzhe/weCodex/cmd/wecodex@latest` entrypoint.
- There is currently no installer script in the repository.
- Existing login/status tests encode pre-bootstrap semantics and must be updated deliberately rather than patched around.
- **Do not create git commits during implementation unless the user explicitly asks.** The usual “commit” step is replaced below with a diff/verification step.

## File layout and responsibilities

- Modify: `/Users/xiongzhe/myIdea/weCodex/config/config.go`
  - Add `working_directory_mode` schema support, mode constants, and validation/default helpers.
- Modify: `/Users/xiongzhe/myIdea/weCodex/config/config_test.go`
  - Cover mode validation and round-trip behavior.
- Create: `/Users/xiongzhe/myIdea/weCodex/config/bootstrap.go`
  - Hold the bootstrap API that creates default config on missing file and updates auto-follow working directory.
- Create: `/Users/xiongzhe/myIdea/weCodex/config/bootstrap_test.go`
  - Cover first-run creation, auto/manual behavior, invalid-config refusal, and concurrent-create safety.
- Create: `/Users/xiongzhe/myIdea/weCodex/cmd/runtime_config.go`
  - Shared command-side helper that resolves the current working directory, calls the config bootstrap API, and prints the fixed bootstrap notice when needed.
- Create: `/Users/xiongzhe/myIdea/weCodex/cmd/runtime_config_test.go`
  - Cover fixed notice behavior and current-directory resolution failures.
- Modify: `/Users/xiongzhe/myIdea/weCodex/cmd/status.go`
  - Replace direct `config.Load()` usage with shared runtime config resolution.
- Modify: `/Users/xiongzhe/myIdea/weCodex/cmd/status_test.go`
  - Update missing-config expectations to bootstrap semantics and add invalid-config coverage.
- Modify: `/Users/xiongzhe/myIdea/weCodex/cmd/login.go`
  - Replace direct config load fallback behavior with bootstrap semantics.
- Modify: `/Users/xiongzhe/myIdea/weCodex/cmd/login_test.go`
  - Add bootstrap notice/config creation coverage and remove tests that rely on continuing through invalid config.
- Modify: `/Users/xiongzhe/myIdea/weCodex/cmd/start.go`
  - Replace direct config loading with shared runtime config resolution; ensure missing config now yields default CLI backend startup path.
- Modify: `/Users/xiongzhe/myIdea/weCodex/cmd/start_test.go`
  - Cover missing-config bootstrap plus output ordering.
- Modify: `/Users/xiongzhe/myIdea/weCodex/cmd/root.go`
  - Make the public command/help text prefer lowercase `wecodex` and generic “Codex runtime” wording.
- Create: `/Users/xiongzhe/myIdea/weCodex/cmd/root_test.go`
  - Cover the root command metadata.
- Create: `/Users/xiongzhe/myIdea/weCodex/cmd/wecodex/main.go`
  - Lowercase installable entrypoint for `go install github.com/xiongzhe/weCodex/cmd/wecodex@latest`.
- Create: `/Users/xiongzhe/myIdea/weCodex/scripts/install.sh`
  - Darwin-only installer that downloads the latest GitHub Release asset, installs `wecodex`, and creates the `weCodex` compatibility alias.
- Create: `/Users/xiongzhe/myIdea/weCodex/scripts/doc.go`
  - Tiny Go package anchor so installer tests can live next to the script.
- Create: `/Users/xiongzhe/myIdea/weCodex/scripts/install_script_test.go`
  - Smoke-test the installer via stubbed `curl`, `tar`, and `uname` commands.
- Modify: `/Users/xiongzhe/myIdea/weCodex/README.md`
  - Document lowercase command name, zero-config bootstrap behavior, the new `working_directory_mode`, the `go install` path, and the Darwin install script.

## Chunk 1: Config bootstrap foundation

### Task 1: Extend config schema with explicit working directory mode

**Files:**
- Modify: `/Users/xiongzhe/myIdea/weCodex/config/config.go`
- Modify: `/Users/xiongzhe/myIdea/weCodex/config/config_test.go`

- [ ] **Step 1: Write the failing config tests**

Add tests that lock in the new field semantics:

```go
func TestEffectiveWorkingDirectoryModeDefaultsToManual(t *testing.T) {
	cfg := Config{}
	if got := cfg.EffectiveWorkingDirectoryMode(); got != WorkingDirectoryModeManual {
		t.Fatalf("expected %q, got %q", WorkingDirectoryModeManual, got)
	}
}

func TestValidateAllowsAutoWorkingDirectoryMode(t *testing.T) {
	cfg := Config{
		BackendType:           "cli",
		CodexCommand:          "/usr/local/bin/codex",
		WorkingDirectory:      "/tmp/project",
		WorkingDirectoryMode:  WorkingDirectoryModeAuto,
		PermissionMode:        "readonly",
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected valid config, got %v", err)
	}
}

func TestValidateRejectsUnknownWorkingDirectoryMode(t *testing.T) {
	cfg := Config{
		BackendType:           "cli",
		CodexCommand:          "/usr/local/bin/codex",
		WorkingDirectory:      "/tmp/project",
		WorkingDirectoryMode:  "surprise",
		PermissionMode:        "readonly",
	}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "working_directory_mode") {
		t.Fatalf("expected working_directory_mode validation error, got %v", err)
	}
}
```

Also extend the existing load/save round-trip test to preserve `working_directory_mode`, and ensure the saved JSON keeps `"codex_args": []` when the default CLI config is written.

- [ ] **Step 2: Run the targeted config tests and verify they fail**

Run:

```bash
go test ./config -run 'TestEffectiveWorkingDirectoryModeDefaultsToManual|TestValidateAllowsAutoWorkingDirectoryMode|TestValidateRejectsUnknownWorkingDirectoryMode|TestLoadRoundTrip' -count=1
```

Expected: FAIL because `Config` does not yet have `WorkingDirectoryMode` or `EffectiveWorkingDirectoryMode()`.

- [ ] **Step 3: Implement the minimal schema changes**

Add the field, constants, and helper in `config/config.go`. Keep `codex_args` serialized without `omitempty` so the default bootstrap file persists `"codex_args": []` exactly as required by the spec:

```go
const (
	BackendTypeACP            = "acp"
	BackendTypeCLI            = "cli"
	WorkingDirectoryModeAuto  = "auto"
	WorkingDirectoryModeManual = "manual"
)

type Config struct {
	BackendType          string   `json:"backend_type,omitempty"`
	CodexCommand         string   `json:"codex_command"`
	CodexArgs            []string `json:"codex_args"`
	WorkingDirectory     string   `json:"working_directory"`
	WorkingDirectoryMode string   `json:"working_directory_mode,omitempty"`
	PermissionMode       string   `json:"permission_mode"`
	LogLevel             string   `json:"log_level,omitempty"`
	WechatAccountsDir    string   `json:"wechat_accounts_dir,omitempty"`
}

func (c Config) EffectiveWorkingDirectoryMode() string {
	if c.WorkingDirectoryMode == "" {
		return WorkingDirectoryModeManual
	}
	return c.WorkingDirectoryMode
}
```

Validation rules:
- still default omitted `backend_type` to `acp`
- still require `codex_args` only for ACP
- allow `working_directory_mode` to be omitted
- reject any non-empty mode that is not `auto` or `manual`

- [ ] **Step 4: Re-run the targeted config tests and verify they pass**

Run:

```bash
go test ./config -run 'TestEffectiveWorkingDirectoryModeDefaultsToManual|TestValidateAllowsAutoWorkingDirectoryMode|TestValidateRejectsUnknownWorkingDirectoryMode|TestLoadRoundTrip' -count=1
```

Expected: PASS.

- [ ] **Step 5: Record the diff and stop short of committing**

Run:

```bash
git diff -- /Users/xiongzhe/myIdea/weCodex/config/config.go /Users/xiongzhe/myIdea/weCodex/config/config_test.go
```

Expected: only the schema/helper/test changes above are present. Do **not** commit unless the user explicitly asks.

### Task 2: Add the config bootstrap API

**Files:**
- Create: `/Users/xiongzhe/myIdea/weCodex/config/bootstrap.go`
- Create: `/Users/xiongzhe/myIdea/weCodex/config/bootstrap_test.go`
- Modify: `/Users/xiongzhe/myIdea/weCodex/config/config.go`

- [ ] **Step 1: Write the failing bootstrap tests**

Add tests that define the full runtime behavior:

```go
func TestLoadOrBootstrapCreatesDefaultCLIConfig(t *testing.T) {}
func TestLoadOrBootstrapWritesDefaultConfigJSONShape(t *testing.T) {}
func TestLoadOrBootstrapUpdatesAutoWorkingDirectory(t *testing.T) {}
func TestLoadOrBootstrapLeavesManualWorkingDirectoryUntouched(t *testing.T) {}
func TestLoadOrBootstrapTreatsLegacyModeAsManual(t *testing.T) {}
func TestLoadOrBootstrapReturnsErrorForInvalidExistingConfig(t *testing.T) {}
func TestLoadOrBootstrapReturnsErrorForEmptyConfigFile(t *testing.T) {}
func TestLoadOrBootstrapReturnsErrorForEmptyWorkingDirectoryInput(t *testing.T) {}
func TestLoadOrBootstrapConcurrentCreateLeavesSingleValidConfig(t *testing.T) {}
```

Use assertions that require:
- the missing-file path creates this exact on-disk JSON shape (not just an in-memory `Config`):

```json
{
  "backend_type": "cli",
  "codex_command": "codex",
  "codex_args": [],
  "working_directory": "<cwd>",
  "working_directory_mode": "auto",
  "permission_mode": "readonly"
}
```

- existing `auto` configs update their stored `working_directory` to the current command directory
- existing `manual` configs keep the saved directory untouched
- legacy configs with no mode behave like `manual`
- invalid existing configs return an error and are not overwritten
- an existing empty config file returns an error and is not replaced with defaults
- an empty `cwd` input returns a clear error instead of attempting bootstrap
- concurrent first-run bootstrap with different cwd values produces one valid config file, and the first successful writer keeps its persisted `working_directory`
- missing `~/.weCodex/` is created automatically before the first successful save attempt

- [ ] **Step 2: Run the bootstrap tests and verify they fail**

Run:

```bash
go test ./config -run 'TestLoadOrBootstrapCreatesDefaultCLIConfig|TestLoadOrBootstrapWritesDefaultConfigJSONShape|TestLoadOrBootstrapUpdatesAutoWorkingDirectory|TestLoadOrBootstrapLeavesManualWorkingDirectoryUntouched|TestLoadOrBootstrapTreatsLegacyModeAsManual|TestLoadOrBootstrapReturnsErrorForInvalidExistingConfig|TestLoadOrBootstrapReturnsErrorForEmptyConfigFile|TestLoadOrBootstrapReturnsErrorForEmptyWorkingDirectoryInput|TestLoadOrBootstrapConcurrentCreateLeavesSingleValidConfig' -count=1
```

Expected: FAIL because `LoadOrBootstrap` and the default-config constructor do not exist yet.

- [ ] **Step 3: Implement the minimal bootstrap layer**

Create `config/bootstrap.go` with a narrow API and no command-specific strings:

```go
type BootstrapResult struct {
	Config  Config
	Created bool
}

func DefaultCLIConfig(cwd string) Config {
	return Config{
		BackendType:          BackendTypeCLI,
		CodexCommand:         "codex",
		CodexArgs:            []string{},
		WorkingDirectory:     cwd,
		WorkingDirectoryMode: WorkingDirectoryModeAuto,
		PermissionMode:       "readonly",
	}
}

func LoadOrBootstrap(cwd string) (BootstrapResult, error) {
	if strings.TrimSpace(cwd) == "" {
		return BootstrapResult{}, fmt.Errorf("working directory is required")
	}

	cfg, err := Load()
	if err == nil {
		if cfg.EffectiveWorkingDirectoryMode() == WorkingDirectoryModeAuto && cfg.WorkingDirectory != cwd {
			cfg.WorkingDirectory = cwd
			if err := Save(cfg); err != nil {
				return BootstrapResult{}, err
			}
		}
		return BootstrapResult{Config: cfg}, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return BootstrapResult{Config: cfg}, err
	}

	cfg = DefaultCLIConfig(cwd)
	created, err := saveIfMissing(cfg)
	if err != nil {
		return BootstrapResult{}, err
	}
	if !created {
		existing, err := Load()
		if err != nil {
			return BootstrapResult{Config: existing}, err
		}
		return BootstrapResult{Config: existing}, nil
	}
	return BootstrapResult{Config: cfg, Created: true}, nil
}
```

Implementation notes:
- keep using `Save()` for normal overwrites (for auto-mode directory refresh)
- use a dedicated `saveIfMissing` helper for first-run bootstrap so the first successful writer wins
- `saveIfMissing` must create `~/.weCodex/` before writing if the directory is missing
- `saveIfMissing` must use an atomic create primitive for the destination file (`O_CREATE|O_EXCL`, `link` + `EEXIST`, or equivalent) rather than `stat`-then-rename
- write the temp file fully before the atomic create attempt; if another process wins, discard temp and load the existing config
- after losing the create race, load and return the existing file without an immediate auto-mode rewrite in that same call
- preserve decoded config values alongside validation errors, matching the current `Load()` contract, so `status` can still render an invalid-config summary without overwriting the file
- keep malformed/invalid existing configs as hard errors

- [ ] **Step 4: Re-run the bootstrap tests and verify they pass**

Run:

```bash
go test ./config -run 'TestLoadOrBootstrapCreatesDefaultCLIConfig|TestLoadOrBootstrapWritesDefaultConfigJSONShape|TestLoadOrBootstrapUpdatesAutoWorkingDirectory|TestLoadOrBootstrapLeavesManualWorkingDirectoryUntouched|TestLoadOrBootstrapTreatsLegacyModeAsManual|TestLoadOrBootstrapReturnsErrorForInvalidExistingConfig|TestLoadOrBootstrapReturnsErrorForEmptyConfigFile|TestLoadOrBootstrapReturnsErrorForEmptyWorkingDirectoryInput|TestLoadOrBootstrapConcurrentCreateLeavesSingleValidConfig' -count=1
```

Expected: PASS.

- [ ] **Step 5: Record the diff and stop short of committing**

Run:

```bash
git diff -- /Users/xiongzhe/myIdea/weCodex/config/config.go /Users/xiongzhe/myIdea/weCodex/config/bootstrap.go /Users/xiongzhe/myIdea/weCodex/config/bootstrap_test.go /Users/xiongzhe/myIdea/weCodex/config/config_test.go
```

Expected: only config bootstrap behavior is present. Do **not** commit unless the user explicitly asks.

## Chunk 2: Command integration

### Task 3: Add one shared command-side runtime config helper and wire `status`

**Files:**
- Create: `/Users/xiongzhe/myIdea/weCodex/cmd/runtime_config.go`
- Create: `/Users/xiongzhe/myIdea/weCodex/cmd/runtime_config_test.go`
- Modify: `/Users/xiongzhe/myIdea/weCodex/cmd/status.go`
- Modify: `/Users/xiongzhe/myIdea/weCodex/cmd/status_test.go`

- [ ] **Step 1: Write the failing command-helper and status tests**

Add tests for both the shared helper and the `status` command behavior:

```go
func TestLoadRuntimeConfigPrintsCreateNoticeWhenConfigIsBootstrapped(t *testing.T) {}
func TestLoadRuntimeConfigDoesNotPrintNoticeWhenConfigAlreadyExists(t *testing.T) {}
func TestLoadRuntimeConfigReturnsGetwdError(t *testing.T) {}
func TestLoadRuntimeConfigReturnsDecodedConfigAlongsideBootstrapError(t *testing.T) {}
func TestStatusCommandBootstrapsMissingConfigAndReportsCLIBackend(t *testing.T) {}
func TestStatusCommandReportsInvalidExistingConfigWithoutOverwritingIt(t *testing.T) {}
```

Replace the old missing-config expectation in `TestStatusCommandReportsUnknownBackendWhenConfigIsMissing` with the new behavior:
- output begins with `default config created: ~/.weCodex/config.json (backend: cli)`
- output includes `backend: cli`
- output includes `config: exists`
- command no longer reports `config: missing` on the first run

- [ ] **Step 2: Run the targeted status tests and verify they fail**

Run:

```bash
go test ./cmd -run 'TestLoadRuntimeConfigPrintsCreateNoticeWhenConfigIsBootstrapped|TestLoadRuntimeConfigDoesNotPrintNoticeWhenConfigAlreadyExists|TestLoadRuntimeConfigReturnsGetwdError|TestLoadRuntimeConfigReturnsDecodedConfigAlongsideBootstrapError|TestStatusCommandBootstrapsMissingConfigAndReportsCLIBackend|TestStatusCommandReportsInvalidExistingConfigWithoutOverwritingIt' -count=1
```

Expected: FAIL because there is no shared helper and `status` still calls `config.Load()` directly.

- [ ] **Step 3: Implement the shared helper and update `status`**

Create `cmd/runtime_config.go`:

```go
const defaultConfigCreatedNotice = "default config created: ~/.weCodex/config.json (backend: cli)"

var (
	runtimeConfigGetwd             = os.Getwd
	runtimeConfigLoadOrBootstrap   = config.LoadOrBootstrap
)

func loadRuntimeConfig(out io.Writer) (config.Config, error) {
	cwd, err := runtimeConfigGetwd()
	if err != nil {
		return config.Config{}, fmt.Errorf("resolve current working directory: %w", err)
	}
	result, err := runtimeConfigLoadOrBootstrap(cwd)
	if result.Created {
		fmt.Fprintln(out, defaultConfigCreatedNotice)
	}
	return result.Config, err
}
```

Add a command-local seam in `cmd/status.go`, for example:

```go
var statusLoadRuntimeConfig = loadRuntimeConfig
```

Then update `status.go` to use that seam before performing credential and command-path checks.

The helper contract must stay explicit for low-context implementers:
- `loadRuntimeConfig` returns zero config only for true infrastructure failures such as `Getwd`
- when bootstrap/load returns decoded config plus a validation error, `loadRuntimeConfig` must return both the decoded config and the error
- `status` should treat that returned pair the same way it currently treats decoded-config validation errors from `config.Load()`

Behavior decisions to preserve:
- `status` remains static-check only
- it still reports `codex command: unresolvable` when `codex` is not in `PATH`
- it should **not** auto-heal invalid existing config
- if bootstrap/load returns a decoded config plus validation error, `status` should keep rendering the invalid summary (`config: invalid`, `config error: ...`) instead of hard-failing the command

- [ ] **Step 4: Re-run the targeted status tests and verify they pass**

Run:

```bash
go test ./cmd -run 'TestLoadRuntimeConfigPrintsCreateNoticeWhenConfigIsBootstrapped|TestLoadRuntimeConfigDoesNotPrintNoticeWhenConfigAlreadyExists|TestLoadRuntimeConfigReturnsGetwdError|TestLoadRuntimeConfigReturnsDecodedConfigAlongsideBootstrapError|TestStatusCommandBootstrapsMissingConfigAndReportsCLIBackend|TestStatusCommandReportsInvalidExistingConfigWithoutOverwritingIt' -count=1
go test ./cmd -run 'Test.*Status' -count=1
```

Expected: PASS.

- [ ] **Step 5: Record the diff and stop short of committing**

Run:

```bash
git diff -- /Users/xiongzhe/myIdea/weCodex/cmd/runtime_config.go /Users/xiongzhe/myIdea/weCodex/cmd/runtime_config_test.go /Users/xiongzhe/myIdea/weCodex/cmd/status.go /Users/xiongzhe/myIdea/weCodex/cmd/status_test.go
```

Expected: only shared runtime-config loading plus `status` wiring changes are present. Do **not** commit unless the user explicitly asks.

### Task 4: Wire `login` to the shared runtime config helper

**Files:**
- Modify: `/Users/xiongzhe/myIdea/weCodex/cmd/login.go`
- Modify: `/Users/xiongzhe/myIdea/weCodex/cmd/login_test.go`

- [ ] **Step 1: Write the failing login tests**

Add/replace tests that match the approved bootstrap semantics:

```go
func TestRunLoginBootstrapsMissingConfigAndPrintsNotice(t *testing.T) {}
func TestRunLoginReturnsBootstrapErrorForInvalidExistingConfig(t *testing.T) {}
```

Add a command-local seam in `cmd/login.go`, for example:

```go
var loginLoadRuntimeConfig = loadRuntimeConfig
```

Update `withStubbedLoginDeps` to save/restore that seam. Delete or rewrite tests that rely on the old “continue through invalid config” behavior, especially the ones that currently preserve `WechatAccountsDir` from invalid decoded config.

- [ ] **Step 2: Run the targeted login tests and verify they fail**

Run:

```bash
go test ./cmd -run 'TestRunLoginBootstrapsMissingConfigAndPrintsNotice|TestRunLoginReturnsBootstrapErrorForInvalidExistingConfig' -count=1
```

Expected: FAIL because `login` still uses the old `config.Load()` fallback logic.

- [ ] **Step 3: Implement the minimal login changes**

Replace this block in `cmd/login.go`:

```go
cfg, cfgErr := loginLoadConfig()
if cfgErr != nil {
	if errors.Is(cfgErr, os.ErrNotExist) || !canUseDecodedConfigForDependentChecks(cfgErr) {
		cfg = config.Config{}
	}
}
```

with the command-local shared runtime-config seam:

```go
cfg, err := loginLoadRuntimeConfig(out)
if err != nil {
	return err
}
```

Keep everything else in `runLogin` unchanged:
- QR fetch
- terminal QR rendering fallback
- polling callback output
- credentials save
- success message

- [ ] **Step 4: Re-run the targeted login tests and verify they pass**

Run:

```bash
go test ./cmd -run 'TestRunLoginBootstrapsMissingConfigAndPrintsNotice|TestRunLoginReturnsBootstrapErrorForInvalidExistingConfig' -count=1
go test ./cmd -run 'TestRunLogin' -count=1
```

Expected: PASS.

- [ ] **Step 5: Record the diff and stop short of committing**

Run:

```bash
git diff -- /Users/xiongzhe/myIdea/weCodex/cmd/login.go /Users/xiongzhe/myIdea/weCodex/cmd/login_test.go /Users/xiongzhe/myIdea/weCodex/cmd/runtime_config.go /Users/xiongzhe/myIdea/weCodex/cmd/runtime_config_test.go
```

Expected: only login/bootstrap behavior changes are present. Do **not** commit unless the user explicitly asks.

### Task 5: Wire `start` to the shared runtime config helper

**Files:**
- Modify: `/Users/xiongzhe/myIdea/weCodex/cmd/start.go`
- Modify: `/Users/xiongzhe/myIdea/weCodex/cmd/start_test.go`

- [ ] **Step 1: Write the failing start tests**

Add tests that lock in the missing-config startup path:

```go
func TestRunStartBootstrapsMissingConfigAndUsesCLIBackend(t *testing.T) {}
func TestRunStartPrintsBootstrapNoticeBeforeForegroundNotice(t *testing.T) {}
func TestRunStartReturnsBootstrapErrorImmediately(t *testing.T) {}
```

Add a command-local seam in `cmd/start.go`, for example:

```go
var startLoadRuntimeConfig = loadRuntimeConfig
```

Update `withStubbedStartDeps` to save/restore that seam. These tests should verify:
- missing config now resolves to a CLI config instead of failing early
- the exact bootstrap notice appears before `running in foreground; ...`
- both lines are written to the same command output writer in strict order
- invalid existing config still fails before credentials/backend startup

- [ ] **Step 2: Run the targeted start tests and verify they fail**

Run:

```bash
go test ./cmd -run 'TestRunStartBootstrapsMissingConfigAndUsesCLIBackend|TestRunStartPrintsBootstrapNoticeBeforeForegroundNotice|TestRunStartReturnsBootstrapErrorImmediately' -count=1
```

Expected: FAIL because `start` still calls `startLoadConfig()` directly.

- [ ] **Step 3: Implement the minimal start changes**

Update `runStart` so config comes from the shared helper instead of direct loading:

```go
cfg, err := startLoadRuntimeConfig(startOutputWriter(cmd))
if err != nil {
	return err
}
```

Then keep the existing backend selection logic:
- `backend_type == "cli"` → `startNewCLIClient(cfg)`
- otherwise → existing ACP adapter path

Do **not** change:
- iLink credential loading
- cursor path logic
- bridge service creation
- monitor loop
- send warning handling
- foreground cancellation semantics

- [ ] **Step 4: Re-run the targeted start tests and verify they pass**

Run:

```bash
go test ./cmd -run 'TestRunStartBootstrapsMissingConfigAndUsesCLIBackend|TestRunStartPrintsBootstrapNoticeBeforeForegroundNotice|TestRunStartReturnsBootstrapErrorImmediately' -count=1
go test ./cmd -run 'TestRunStart' -count=1
```

Expected: PASS.

- [ ] **Step 5: Record the diff and stop short of committing**

Run:

```bash
git diff -- /Users/xiongzhe/myIdea/weCodex/cmd/start.go /Users/xiongzhe/myIdea/weCodex/cmd/start_test.go /Users/xiongzhe/myIdea/weCodex/cmd/runtime_config.go /Users/xiongzhe/myIdea/weCodex/cmd/runtime_config_test.go
```

Expected: only start/bootstrap behavior changes are present. Do **not** commit unless the user explicitly asks.

## Chunk 3: Naming, install surface, docs, and verification

### Task 6: Rebrand the public command to lowercase and add the `go install` entrypoint

**Files:**
- Modify: `/Users/xiongzhe/myIdea/weCodex/cmd/root.go`
- Create: `/Users/xiongzhe/myIdea/weCodex/cmd/root_test.go`
- Create: `/Users/xiongzhe/myIdea/weCodex/cmd/wecodex/main.go`

- [ ] **Step 1: Write the failing root-command tests**

Add tests like:

```go
func TestNewRootCmdUsesLowercaseCommandName(t *testing.T) {
	root := newRootCmd()
	if root.Use != "wecodex" {
		t.Fatalf("expected root.Use to be wecodex, got %q", root.Use)
	}
}

func TestNewRootCmdUsesGenericCodexRuntimeDescription(t *testing.T) {
	root := newRootCmd()
	if strings.Contains(strings.ToLower(root.Short), "acp") {
		t.Fatalf("expected generic runtime wording, got %q", root.Short)
	}
}
```

Also rely on `go test ./cmd/wecodex` as a compile check for the new entrypoint package.

- [ ] **Step 2: Run the targeted root/entrypoint tests and verify they fail**

Run:

```bash
go test ./cmd -run 'TestNewRootCmdUsesLowercaseCommandName|TestNewRootCmdUsesGenericCodexRuntimeDescription' -count=1
go test ./cmd/wecodex -count=1
```

Expected: FAIL because the new root tests fail and `cmd/wecodex` does not exist yet.

- [ ] **Step 3: Implement the minimal naming changes**

Update `cmd/root.go`:

```go
root := &cobra.Command{
	Use:           "wecodex",
	Short:         "WeChat bridge for Codex runtime",
	SilenceErrors: true,
}
```

Create `cmd/wecodex/main.go`:

```go
package main

import "github.com/xiongzhe/weCodex/cmd"

func main() {
	cmd.Execute()
}
```

Keep repository-root `main.go` unchanged so existing `go build .` still works.

- [ ] **Step 4: Re-run the targeted root/entrypoint tests and verify they pass**

Run:

```bash
go test ./cmd -run 'TestNewRootCmdUsesLowercaseCommandName|TestNewRootCmdUsesGenericCodexRuntimeDescription' -count=1
go test ./cmd/wecodex -count=1
```

Expected: PASS.

- [ ] **Step 5: Record the diff and stop short of committing**

Run:

```bash
git diff -- /Users/xiongzhe/myIdea/weCodex/cmd/root.go /Users/xiongzhe/myIdea/weCodex/cmd/root_test.go /Users/xiongzhe/myIdea/weCodex/cmd/wecodex/main.go
```

Expected: only lowercase branding and entrypoint additions are present. Do **not** commit unless the user explicitly asks.

### Task 7: Add the Darwin install script and update the README

**Files:**
- Create: `/Users/xiongzhe/myIdea/weCodex/scripts/install.sh`
- Create: `/Users/xiongzhe/myIdea/weCodex/scripts/doc.go`
- Create: `/Users/xiongzhe/myIdea/weCodex/scripts/install_script_test.go`
- Modify: `/Users/xiongzhe/myIdea/weCodex/README.md`

- [ ] **Step 1: Write the failing installer and README-oriented tests**

Create Go smoke tests around the shell script with stubbed external tools:

```go
func TestInstallScriptRejectsNonDarwinPlatforms(t *testing.T) {}
func TestInstallScriptRejectsUnsupportedArchitecture(t *testing.T) {}
func TestInstallScriptRequestsExpectedReleaseAsset(t *testing.T) {}
func TestInstallScriptSkipsNonWritablePathEntries(t *testing.T) {}
func TestInstallScriptInstallsWeCodexCompatibilityAlias(t *testing.T) {}
```

Test strategy:
- prepend temp directories to `PATH`
- place fake `uname`, `curl`, and `tar` executables there
- have fake `curl` capture the requested URL
- have fake `tar` create an extracted `wecodex` binary in the temp workspace
- run `bash scripts/install.sh`
- assert unsupported `uname -m` values fail clearly
- assert the first writable directory in `PATH` receives the install even when earlier entries are non-writable
- assert that directory receives:
  - executable `wecodex`
  - compatibility alias `weCodex` (symlink or copied wrapper)

- [ ] **Step 2: Run the targeted installer tests and verify they fail**

Run:

```bash
go test ./scripts -run 'TestInstallScriptRejectsNonDarwinPlatforms|TestInstallScriptRejectsUnsupportedArchitecture|TestInstallScriptRequestsExpectedReleaseAsset|TestInstallScriptSkipsNonWritablePathEntries|TestInstallScriptInstallsWeCodexCompatibilityAlias' -count=1
```

Expected: FAIL because the `scripts` package and installer do not exist yet.

- [ ] **Step 3: Implement the installer and README updates**

Installer behavior to implement in `scripts/install.sh`:
- require `uname -s` to be `Darwin`
- map `uname -m` to `arm64` or `amd64`
- choose the first writable directory already present in `PATH`
- download the latest release asset from GitHub Releases using a fixed asset contract:

```bash
asset="wecodex-darwin-${arch}.tar.gz"
url="https://github.com/xiongzhe/weCodex/releases/latest/download/${asset}"
```

- extract `wecodex`
- install `wecodex` into the chosen path directory
- create `weCodex` as a compatibility alias in the same directory
- print a short success message showing the install directory

README updates:
- make `wecodex` the primary public command everywhere
- add the `go install github.com/xiongzhe/weCodex/cmd/wecodex@latest` path
- describe the Darwin install script path and behavior
- explain first-run auto-generated config and the exact default JSON fields
- explain `working_directory_mode: auto|manual`
- update usage examples to `wecodex status`, `wecodex login`, `wecodex start`
- update the FAQ so missing-config guidance says the file is auto-created rather than manually authored

- [ ] **Step 4: Re-run the targeted installer tests and verify they pass**

Run:

```bash
go test ./scripts -run 'TestInstallScriptRejectsNonDarwinPlatforms|TestInstallScriptRejectsUnsupportedArchitecture|TestInstallScriptRequestsExpectedReleaseAsset|TestInstallScriptSkipsNonWritablePathEntries|TestInstallScriptInstallsWeCodexCompatibilityAlias' -count=1
```

Expected: PASS.

- [ ] **Step 5: Record the diff and stop short of committing**

Run:

```bash
git diff -- /Users/xiongzhe/myIdea/weCodex/scripts/install.sh /Users/xiongzhe/myIdea/weCodex/scripts/doc.go /Users/xiongzhe/myIdea/weCodex/scripts/install_script_test.go /Users/xiongzhe/myIdea/weCodex/README.md
```

Expected: only Darwin installer plus documentation changes are present. Do **not** commit unless the user explicitly asks.

### Task 8: Run full verification and smoke checks

**Files:**
- Verify only; no new files required unless a test gap is discovered

- [ ] **Step 1: Run the focused package suite**

Run:

```bash
go test ./config ./cmd ./scripts -count=1
```

Expected: PASS.

- [ ] **Step 2: Run the full repository test suite**

Run:

```bash
go test ./... -count=1
```

Expected: PASS.

- [ ] **Step 3: Run a fresh-home status smoke test**

Run:

```bash
tmp_home=$(mktemp -d) && HOME="$tmp_home" go run ./cmd/wecodex status
```

Expected output characteristics:
- first line includes `default config created: ~/.weCodex/config.json (backend: cli)`
- summary includes `backend: cli`
- summary includes `config: exists`
- command proceeds to static readiness checks instead of reporting `config: missing`

- [ ] **Step 4: Verify the lowercase entrypoint compiles directly**

Run:

```bash
go build ./cmd/wecodex
```

Expected: PASS with no output.

- [ ] **Step 5: Re-read the spec, compare against final behavior, and manually verify README content**

Manually check the implementation against `/Users/xiongzhe/myIdea/weCodex/docs/superpowers/specs/2026-03-25-install-and-zero-config-design.md` and confirm all success criteria are covered:
- zero-config bootstrap for `status`, `login`, `start`
- default CLI backend
- current-directory bootstrap with explicit `working_directory_mode`
- legacy mode defaulting to `manual`
- fixed bootstrap notice
- lowercase command branding
- lowercase `go install` entrypoint
- Darwin install script with `weCodex` compatibility alias

Also manually verify `README.md` contains all required public-facing updates:
- `wecodex` as the primary command in examples
- `go install github.com/xiongzhe/weCodex/cmd/wecodex@latest`
- Darwin install script instructions
- auto-generated default config JSON including `"codex_args": []`
- `working_directory_mode: auto|manual` semantics
- FAQ/help text no longer telling users to manually create `~/.weCodex/config.json`

If any criterion is missing, add a new task before claiming completion.
