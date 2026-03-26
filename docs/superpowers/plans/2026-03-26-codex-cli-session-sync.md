# Codex CLI Session Sync Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `wecodex` act as a remote entrypoint for Codex CLI threads so微信端 can `/new`, `/list`, and `/use N` within the current project directory and continue the same Codex CLI thread history as the computer.

**Architecture:** Keep Codex CLI as the source of truth. Add a small command/bridge layer that resolves the active project scope, lists Codex CLI threads for that scope, maps list numbers to thread IDs, and routes `/new` / `/use` / normal prompts to the appropriate thread. Keep local state minimal and derived from Codex CLI wherever possible.

**Tech Stack:** Go 1.24, existing `backend`, `bridge`, and `codexacp` packages, Cobra CLI, standard library.

---

## Context and constraints

- The approved spec is `/Users/xiongzhe/myIdea/weCodex/docs/superpowers/specs/2026-03-26-codex-cli-session-sync-design.md`.
- The current code already has a bridge service, command parser, and backend abstraction for prompt routing.
- The current bridge already supports `/help`, `/status`, and `/new`; `/new` today just clears a local session pointer.
- There is no session/thread listing or numbered thread switching yet.
- The current `backend.Client` contract only exposes `Start`, `Stop`, `Prompt`, and `Health`; the session-sync feature will need either new methods on the backend layer or a dedicated Codex CLI session adapter in a separate package.
- Do not create commits unless the user explicitly asks.

## File map and responsibilities

- Modify: `/Users/xiongzhe/myIdea/weCodex/bridge/types.go`
  - Add input kinds for `/list` and `/use`.
  - Add any small bridge data needed to render thread lists and switch results.
- Modify: `/Users/xiongzhe/myIdea/weCodex/bridge/router.go`
  - Parse `/list` and `/use N` as local commands.
  - Keep ordinary text as prompt input.
- Modify: `/Users/xiongzhe/myIdea/weCodex/bridge/service.go`
  - Route `/new`, `/list`, and `/use N` through the session/thread layer.
  - Keep existing prompt/session behavior intact for normal messages.
- Modify: `/Users/xiongzhe/myIdea/weCodex/bridge/service_test.go`
  - Update existing expectations around `/new`.
  - Add coverage for `/list`, `/use`, and same-thread routing behavior.
- Modify or extend: `/Users/xiongzhe/myIdea/weCodex/bridge/router_test.go`
  - Add parser tests for the new slash commands.
- Modify: `/Users/xiongzhe/myIdea/weCodex/backend/types.go`
  - Extend the backend interface with thread/session listing and switching primitives if the implementation path uses the backend contract directly.
- Modify: `/Users/xiongzhe/myIdea/weCodex/backend/cli_client.go`
  - Implement the Codex CLI-backed thread operations if the backend contract is extended there.
- Modify: `/Users/xiongzhe/myIdea/weCodex/backend/acp_client.go`
  - Keep ACP translation compiling if backend interfaces change.
- Modify or create: `/Users/xiongzhe/myIdea/weCodex/backend/cli_client_test.go`
  - Cover the new Codex CLI thread/session operations if they live in the backend.
- Create: `/Users/xiongzhe/myIdea/weCodex/bridge/session_registry.go`
  - Hold the small bridge-side thread registry and numbered-list mapping if the implementation needs a thin local adapter.
- Create: `/Users/xiongzhe/myIdea/weCodex/bridge/session_registry_test.go`
  - Cover numbering, `/use` lookup, and current-thread tracking.
- Create: `/Users/xiongzhe/myIdea/weCodex/codexcli/` package only if the backend abstraction becomes too broad for the existing `backend` package.
  - Keep Codex CLI thread discovery and switching separate from the generic backend interface if that makes the interface cleaner.

## Chunk 1: Define the command surface

### Task 1: Add `/list` and `/use` parsing

**Files:**
- Modify: `/Users/xiongzhe/myIdea/weCodex/bridge/types.go`
- Modify: `/Users/xiongzhe/myIdea/weCodex/bridge/router.go`
- Modify: `/Users/xiongzhe/myIdea/weCodex/bridge/router_test.go`

- [ ] **Step 1: Write the failing parser tests**

Add tests that lock in the new command surface:

```go
func TestParseInputRecognizesListCommand(t *testing.T) {}
func TestParseInputRecognizesUseCommandWithNumber(t *testing.T) {}
func TestParseInputTreatsUnknownSlashCommandAsPromptText(t *testing.T) {}
```

The `/use` test should assert that `ParseInput("/use 2")` is not treated as ordinary prompt text and that the numeric argument is preserved in the parsed result.

- [ ] **Step 2: Run the parser tests and verify they fail**

Run:

```bash
go test ./bridge -run 'TestParseInputRecognizesListCommand|TestParseInputRecognizesUseCommandWithNumber|TestParseInputTreatsUnknownSlashCommandAsPromptText' -count=1
```

Expected: FAIL because `/list` and `/use` are not recognized yet.

- [ ] **Step 3: Implement the minimal parser changes**

Add new input kinds in `bridge/types.go` and parse `/list` and `/use <n>` in `bridge/router.go`. Keep all other text behavior unchanged.

- [ ] **Step 4: Re-run the parser tests and verify they pass**

Run:

```bash
go test ./bridge -run 'TestParseInputRecognizesListCommand|TestParseInputRecognizesUseCommandWithNumber|TestParseInputTreatsUnknownSlashCommandAsPromptText' -count=1
```

Expected: PASS.

- [ ] **Step 5: Check the diff**

Run:

```bash
git diff -- /Users/xiongzhe/myIdea/weCodex/bridge/types.go /Users/xiongzhe/myIdea/weCodex/bridge/router.go /Users/xiongzhe/myIdea/weCodex/bridge/router_test.go
```

Expected: only slash-command parsing changes.

### Task 2: Define the session/thread bridge contract

**Files:**
- Modify: `/Users/xiongzhe/myIdea/weCodex/backend/types.go`
- Modify: `/Users/xiongzhe/myIdea/weCodex/backend/acp_client.go`
- Modify: `/Users/xiongzhe/myIdea/weCodex/backend/cli_client.go`
- Modify: `/Users/xiongzhe/myIdea/weCodex/backend/cli_client_test.go`
- Modify: `/Users/xiongzhe/myIdea/weCodex/backend/acp_client.go`

- [ ] **Step 1: Write the failing backend contract tests**

Add tests that define the minimal new capabilities needed for session sync. Keep them focused on behavior, not implementation details.

```go
func TestCLIClientCanListProjectThreads(t *testing.T) {}
func TestCLIClientCanCreateThreadForCurrentProject(t *testing.T) {}
func TestCLIClientCanSwitchToThreadByNumberOrID(t *testing.T) {}
```

If the implementation path needs a separate interface for Codex CLI thread management, define the tests against that interface instead of forcing ACP to own the feature.

- [ ] **Step 2: Run the backend tests and verify they fail**

Run the smallest relevant package test set, for example:

```bash
go test ./backend -run 'TestCLIClientCanListProjectThreads|TestCLIClientCanCreateThreadForCurrentProject|TestCLIClientCanSwitchToThreadByNumberOrID' -count=1
```

Expected: FAIL because the backend layer does not expose thread/session listing or switching yet.

- [ ] **Step 3: Implement the smallest viable contract**

Choose the narrowest place to expose Codex CLI thread operations:

- If the existing `backend.Client` interface can stay small, add a dedicated Codex CLI session manager in a new package and keep `backend` unchanged.
- If the bridge really needs to call these operations through `backend.Client`, add only the methods required for `/list`, `/new`, and `/use`.

Keep ACP translation compiling even if ACP does not participate in this feature.

- [ ] **Step 4: Re-run the backend tests and verify they pass**

Run the same `go test ./backend ...` command again.

Expected: PASS.

- [ ] **Step 5: Check the diff**

Run:

```bash
git diff -- /Users/xiongzhe/myIdea/weCodex/backend/types.go /Users/xiongzhe/myIdea/weCodex/backend/cli_client.go /Users/xiongzhe/myIdea/weCodex/backend/acp_client.go /Users/xiongzhe/myIdea/weCodex/backend/cli_client_test.go
```

Expected: only the backend contract and implementation changes needed for thread/session support.

## Chunk 2: Bridge command wiring

### Task 3: Route `/new`, `/list`, and `/use` through a session registry

**Files:**
- Create: `/Users/xiongzhe/myIdea/weCodex/bridge/session_registry.go`
- Create: `/Users/xiongzhe/myIdea/weCodex/bridge/session_registry_test.go`
- Modify: `/Users/xiongzhe/myIdea/weCodex/bridge/service.go`
- Modify: `/Users/xiongzhe/myIdea/weCodex/bridge/service_test.go`

- [ ] **Step 1: Write the failing bridge tests**

Add tests that describe the user-visible behavior:

```go
func TestHandleMessageListShowsProjectThreads(t *testing.T) {}
func TestHandleMessageUseSwitchesCurrentThreadByNumber(t *testing.T) {}
func TestHandleMessageNewCreatesFreshThreadAndSelectsIt(t *testing.T) {}
func TestHandleMessageNormalPromptUsesCurrentThread(t *testing.T) {}
```

The `/list` test should assert that the response is numbered and that the currently active thread is marked.

The `/use` test should assert that `/use 2` changes the active thread and that subsequent prompts go to the newly selected thread.

The `/new` test should assert that a new Codex CLI thread is created and becomes active.

- [ ] **Step 2: Run the bridge tests and verify they fail**

Run:

```bash
go test ./bridge -run 'TestHandleMessageListShowsProjectThreads|TestHandleMessageUseSwitchesCurrentThreadByNumber|TestHandleMessageNewCreatesFreshThreadAndSelectsIt|TestHandleMessageNormalPromptUsesCurrentThread' -count=1
```

Expected: FAIL because the service does not yet know how to list or switch CLI threads.

- [ ] **Step 3: Implement the session registry and service routing**

Create a small bridge-side registry that:

- holds the current project scope
- stores the numbered mapping returned by Codex CLI
- resolves `/use N` to a concrete thread ID
- tracks the active thread for subsequent prompts

Update `bridge/service.go` so:

- `/new` calls the registry/adapter to create a Codex CLI thread and selects it
- `/list` asks the registry for the current project threads and renders them
- `/use N` resolves the numbered selection and switches active thread
- ordinary prompts go to the current active thread

Keep the existing busy-lock behavior for normal prompts unless it conflicts with the new thread-routing contract.

- [ ] **Step 4: Re-run the bridge tests and verify they pass**

Run:

```bash
go test ./bridge -run 'TestHandleMessageListShowsProjectThreads|TestHandleMessageUseSwitchesCurrentThreadByNumber|TestHandleMessageNewCreatesFreshThreadAndSelectsIt|TestHandleMessageNormalPromptUsesCurrentThread' -count=1
```

Expected: PASS.

- [ ] **Step 5: Check the diff**

Run:

```bash
git diff -- /Users/xiongzhe/myIdea/weCodex/bridge/session_registry.go /Users/xiongzhe/myIdea/weCodex/bridge/session_registry_test.go /Users/xiongzhe/myIdea/weCodex/bridge/service.go /Users/xiongzhe/myIdea/weCodex/bridge/service_test.go
```

Expected: only bridge routing and registry code.

### Task 4: Render numbered thread lists and switch results clearly

**Files:**
- Modify: `/Users/xiongzhe/myIdea/weCodex/bridge/service.go`
- Modify: `/Users/xiongzhe/myIdea/weCodex/bridge/service_test.go`

- [ ] **Step 1: Write the failing formatting tests**

Add tests for the text output shape:

```go
func TestBuildThreadListTextMarksActiveThread(t *testing.T) {}
func TestBuildThreadListTextUsesStableNumbersWithinSnapshot(t *testing.T) {}
func TestUseConfirmationMentionsSelectedThread(t *testing.T) {}
```

- [ ] **Step 2: Run the formatting tests and verify they fail**

Run:

```bash
go test ./bridge -run 'TestBuildThreadListTextMarksActiveThread|TestBuildThreadListTextUsesStableNumbersWithinSnapshot|TestUseConfirmationMentionsSelectedThread' -count=1
```

Expected: FAIL because the text formatting helpers do not exist yet.

- [ ] **Step 3: Implement the minimal formatting helpers**

Add tiny helper functions that keep list formatting deterministic and easy to test.

- [ ] **Step 4: Re-run the formatting tests and verify they pass**

Run the same `go test` command again.

Expected: PASS.

- [ ] **Step 5: Check the diff**

Run:

```bash
git diff -- /Users/xiongzhe/myIdea/weCodex/bridge/service.go /Users/xiongzhe/myIdea/weCodex/bridge/service_test.go
```

Expected: only output formatting changes.

## Chunk 3: CLI-facing thread operations

### Task 5: Implement Codex CLI thread discovery and switching

**Files:**
- Create or modify the chosen Codex CLI session adapter package.
- Modify: `/Users/xiongzhe/myIdea/weCodex/backend/cli_client.go` only if the adapter lives there.
- Modify: `/Users/xiongzhe/myIdea/weCodex/backend/cli_client_test.go`
- Create: `/Users/xiongzhe/myIdea/weCodex/codexcli/*` only if the adapter is split out.

- [ ] **Step 1: Write the failing adapter tests**

Define tests that lock in the actual Codex CLI behavior surface you need:

```go
func TestListThreadsReturnsCurrentProjectThreads(t *testing.T) {}
func TestCreateThreadUsesCodexCliBackend(t *testing.T) {}
func TestSwitchThreadByNumberMapsToThreadID(t *testing.T) {}
```

Use stubs/mocks for subprocess calls or Codex CLI APIs; do not depend on a real CLI in unit tests.

- [ ] **Step 2: Run the adapter tests and verify they fail**

Run the smallest relevant package tests.

Expected: FAIL because the adapter does not exist or does not expose thread operations yet.

- [ ] **Step 3: Implement the Codex CLI adapter**

Add the smallest implementation that can:

- discover threads in the current project scope
- create a new thread for `/new`
- switch by numbered list selection for `/use N`
- return thread IDs and display names for `/list`

Keep the current prompt path working.

- [ ] **Step 4: Re-run the adapter tests and verify they pass**

Run the same test command again.

Expected: PASS.

- [ ] **Step 5: Check the diff**

Run:

```bash
git diff -- <adapter files>
```

Expected: only Codex CLI thread/session logic.

## Chunk 4: Verification and docs

### Task 6: Update command help and any user-facing docs that mention session behavior

**Files:**
- Modify: `/Users/xiongzhe/myIdea/weCodex/cmd/root.go`
- Modify: `/Users/xiongzhe/myIdea/weCodex/README.md`
- Modify: any bridge help text that still describes `/new` as only clearing a local session

- [ ] **Step 1: Write the failing help/doc tests or assertions**

Add or update tests that assert the public help text mentions `/list` and `/use` if the project has test coverage for help text.

- [ ] **Step 2: Run the tests and verify they fail**

Run the relevant command tests.

Expected: FAIL until help text and docs are updated.

- [ ] **Step 3: Update the text**

Change the user-facing text to describe Codex CLI threads, `/list`, and `/use N` clearly.

- [ ] **Step 4: Re-run the tests and verify they pass**

Run the same test command again.

Expected: PASS.

- [ ] **Step 5: Check the diff**

Run:

```bash
git diff -- /Users/xiongzhe/myIdea/weCodex/cmd/root.go /Users/xiongzhe/myIdea/weCodex/README.md
```

Expected: only doc/help updates.

### Task 7: Run the full relevant test suite

**Files:**
- None

- [ ] **Step 1: Run the focused package tests**

Run:

```bash
go test ./bridge ./backend ./codexacp ./cmd
```

- [ ] **Step 2: Fix any failures**

If tests fail, fix the smallest surface that matches the failure and rerun the same command.

- [ ] **Step 3: Run the repository-wide tests if they are reasonably fast**

Run:

```bash
go test ./...
```

- [ ] **Step 4: Confirm the implementation matches the spec**

Check that:

- `/new` creates a Codex CLI thread
- `/list` shows only the current project scope
- `/use N` switches by list number
- normal prompts continue on the selected thread

- [ ] **Step 5: Stop short of committing unless the user explicitly asks**

No commit is required for this plan unless the user requests one.
