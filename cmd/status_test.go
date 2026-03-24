package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xiongzhe/weCodex/config"
)

func assertContains(t *testing.T, got string, want string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Fatalf("expected summary to contain %q, got %q", want, got)
	}
}

func TestBuildReadinessSummaryReportsMissingConfig(t *testing.T) {
	summary := BuildReadinessSummary(false, false, false, os.ErrNotExist, nil)

	assertContains(t, summary, "static checks only")
	assertContains(t, summary, "config: missing")
	assertContains(t, summary, "ready: no")
}

func TestBuildReadinessSummaryReportsMissingCredentials(t *testing.T) {
	summary := BuildReadinessSummary(true, false, true, nil, nil)

	assertContains(t, summary, "config: exists")
	assertContains(t, summary, "credentials: missing")
	assertContains(t, summary, "codex command: resolvable")
	assertContains(t, summary, "ready: no")
}

func TestBuildReadinessSummaryReportsInvalidConfig(t *testing.T) {
	summary := BuildReadinessSummary(true, true, true, errors.New("permission_mode must be readonly"), nil)

	assertContains(t, summary, "config: invalid")
	assertContains(t, summary, "ready: no")
}

func TestBuildReadinessSummaryReportsMissingCodexCommand(t *testing.T) {
	summary := BuildReadinessSummary(true, true, false, nil, nil)

	assertContains(t, summary, "config: exists")
	assertContains(t, summary, "credentials: present")
	assertContains(t, summary, "codex command: unresolvable")
	assertContains(t, summary, "ready: no")
}

func TestBuildReadinessSummaryReportsReadyState(t *testing.T) {
	summary := BuildReadinessSummary(true, true, true, nil, nil)

	assertContains(t, summary, "config: exists")
	assertContains(t, summary, "credentials: present")
	assertContains(t, summary, "codex command: resolvable")
	assertContains(t, summary, "ready: yes")
}

func TestStatusCommandReportsCredentialsErrorDetail(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cfgDir := filepath.Join(home, ".weCodex")
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	cfg := config.Config{
		CodexCommand:      "go",
		CodexArgs:         []string{"version"},
		WorkingDirectory:  home,
		PermissionMode:    "readonly",
		WechatAccountsDir: filepath.Join(home, "accounts"),
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "config.json"), data, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := os.WriteFile(filepath.Join(home, "accounts"), []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("WriteFile accounts sentinel: %v", err)
	}

	var out bytes.Buffer
	statusCmd.SetOut(&out)
	statusCmd.SetErr(&out)
	statusCmd.SetArgs(nil)
	defer statusCmd.SetOut(nil)
	defer statusCmd.SetErr(nil)
	defer statusCmd.SetArgs(nil)

	if err := statusCmd.RunE(statusCmd, nil); err != nil {
		t.Fatalf("RunE returned error: %v", err)
	}

	assertContains(t, out.String(), "credentials error: stat credentials file:")
}

func TestRootStatusCommandSkipsDependentChecksForUndecodableConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cfgDir := filepath.Join(home, ".weCodex")
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "config.json"), []byte(`{"codex_command":`), 0o600); err != nil {
		t.Fatalf("WriteFile malformed config: %v", err)
	}

	var out bytes.Buffer
	root := newRootCmd()
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"status"})

	if _, err := root.ExecuteC(); err != nil {
		t.Fatalf("ExecuteC returned error: %v", err)
	}

	assertContains(t, out.String(), "config: invalid")
	assertContains(t, out.String(), "credentials: unknown")
	assertContains(t, out.String(), "codex command: unknown")
	assertContains(t, out.String(), "config error: decode config file:")
	assertContains(t, out.String(), "credentials error: skipped because config could not be loaded reliably")
	assertContains(t, out.String(), "codex error: skipped because config could not be loaded reliably")
}
