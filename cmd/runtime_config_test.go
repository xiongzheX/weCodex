package cmd

import (
	"bytes"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/xiongzheX/weCodex/config"
)

func TestLoadRuntimeConfigPrintsCreateNoticeWhenConfigIsBootstrapped(t *testing.T) {
	oldGetwd := runtimeConfigGetwd
	oldLoadOrBootstrap := runtimeConfigLoadOrBootstrap
	defer func() {
		runtimeConfigGetwd = oldGetwd
		runtimeConfigLoadOrBootstrap = oldLoadOrBootstrap
	}()

	runtimeConfigGetwd = func() (string, error) {
		return "/tmp/project", nil
	}
	wantCfg := config.Config{BackendType: "cli", CodexCommand: "codex", WorkingDirectory: "/tmp/project", PermissionMode: "readonly"}
	runtimeConfigLoadOrBootstrap = func(cwd string) (config.BootstrapResult, error) {
		if cwd != "/tmp/project" {
			t.Fatalf("unexpected cwd: %q", cwd)
		}
		return config.BootstrapResult{Config: wantCfg, Created: true}, nil
	}

	var out bytes.Buffer
	gotCfg, err := loadRuntimeConfig(&out)
	if err != nil {
		t.Fatalf("loadRuntimeConfig returned error: %v", err)
	}
	if !reflect.DeepEqual(gotCfg, wantCfg) {
		t.Fatalf("unexpected config: got %+v, want %+v", gotCfg, wantCfg)
	}
	if out.String() != defaultConfigCreatedNotice+"\n" {
		t.Fatalf("unexpected notice output: %q", out.String())
	}
}

func TestLoadRuntimeConfigDoesNotPrintNoticeWhenConfigAlreadyExists(t *testing.T) {
	oldGetwd := runtimeConfigGetwd
	oldLoadOrBootstrap := runtimeConfigLoadOrBootstrap
	defer func() {
		runtimeConfigGetwd = oldGetwd
		runtimeConfigLoadOrBootstrap = oldLoadOrBootstrap
	}()

	runtimeConfigGetwd = func() (string, error) {
		return "/tmp/project", nil
	}
	runtimeConfigLoadOrBootstrap = func(cwd string) (config.BootstrapResult, error) {
		return config.BootstrapResult{Config: config.Config{BackendType: "cli"}, Created: false}, nil
	}

	var out bytes.Buffer
	if _, err := loadRuntimeConfig(&out); err != nil {
		t.Fatalf("loadRuntimeConfig returned error: %v", err)
	}
	if out.Len() != 0 {
		t.Fatalf("expected no notice output, got %q", out.String())
	}
}

func TestLoadRuntimeConfigReturnsGetwdError(t *testing.T) {
	oldGetwd := runtimeConfigGetwd
	oldLoadOrBootstrap := runtimeConfigLoadOrBootstrap
	defer func() {
		runtimeConfigGetwd = oldGetwd
		runtimeConfigLoadOrBootstrap = oldLoadOrBootstrap
	}()

	getwdErr := errors.New("boom")
	runtimeConfigGetwd = func() (string, error) {
		return "", getwdErr
	}
	runtimeConfigLoadOrBootstrap = func(cwd string) (config.BootstrapResult, error) {
		t.Fatalf("did not expect LoadOrBootstrap call when getwd fails")
		return config.BootstrapResult{}, nil
	}

	cfg, err := loadRuntimeConfig(&bytes.Buffer{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "resolve current working directory") {
		t.Fatalf("expected wrapped getwd context, got %q", err.Error())
	}
	if !errors.Is(err, getwdErr) {
		t.Fatalf("expected wrapped getwd error, got %v", err)
	}
	if !reflect.DeepEqual(cfg, config.Config{}) {
		t.Fatalf("expected zero config on getwd error, got %+v", cfg)
	}
}

func TestLoadRuntimeConfigReturnsDecodedConfigAlongsideBootstrapError(t *testing.T) {
	oldGetwd := runtimeConfigGetwd
	oldLoadOrBootstrap := runtimeConfigLoadOrBootstrap
	defer func() {
		runtimeConfigGetwd = oldGetwd
		runtimeConfigLoadOrBootstrap = oldLoadOrBootstrap
	}()

	runtimeConfigGetwd = func() (string, error) {
		return "/tmp/project", nil
	}
	wantCfg := config.Config{BackendType: "cli", CodexCommand: "codex", WorkingDirectory: "/tmp/project", PermissionMode: "invalid"}
	bootstrapErr := errors.New("permission_mode must be readonly")
	runtimeConfigLoadOrBootstrap = func(cwd string) (config.BootstrapResult, error) {
		return config.BootstrapResult{Config: wantCfg, Created: false}, bootstrapErr
	}

	var out bytes.Buffer
	gotCfg, err := loadRuntimeConfig(&out)
	if !errors.Is(err, bootstrapErr) {
		t.Fatalf("expected bootstrap error, got %v", err)
	}
	if !reflect.DeepEqual(gotCfg, wantCfg) {
		t.Fatalf("unexpected config: got %+v, want %+v", gotCfg, wantCfg)
	}
	if out.Len() != 0 {
		t.Fatalf("expected no notice output, got %q", out.String())
	}
}
