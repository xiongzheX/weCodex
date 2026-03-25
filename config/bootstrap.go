package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type BootstrapResult struct {
	Config  Config
	Created bool
}

func DefaultCLIConfig(cwd string) Config {
	return Config{
		BackendType:          "cli",
		CodexCommand:         "codex",
		CodexArgs:            []string{},
		WorkingDirectory:     cwd,
		WorkingDirectoryMode: WorkingDirectoryModeAuto,
		PermissionMode:       "readonly",
	}
}

func LoadOrBootstrap(cwd string) (BootstrapResult, error) {
	if cwd == "" {
		return BootstrapResult{}, fmt.Errorf("working directory cannot be empty")
	}

	cfg, err := Load()
	if err == nil {
		if cfg.EffectiveWorkingDirectoryMode() == WorkingDirectoryModeAuto && cfg.WorkingDirectory != cwd {
			cfg.WorkingDirectory = cwd
			if err := Save(cfg); err != nil {
				return BootstrapResult{Config: cfg, Created: false}, err
			}
		}
		return BootstrapResult{Config: cfg, Created: false}, nil
	}

	if !errors.Is(err, os.ErrNotExist) {
		return BootstrapResult{Config: cfg, Created: false}, err
	}

	defaultCfg := DefaultCLIConfig(cwd)
	if err := saveIfMissing(defaultCfg); err != nil {
		if errors.Is(err, os.ErrExist) {
			existing, loadErr := Load()
			if loadErr != nil {
				return BootstrapResult{Config: existing, Created: false}, loadErr
			}
			return BootstrapResult{Config: existing, Created: false}, nil
		}
		return BootstrapResult{Config: defaultCfg, Created: false}, err
	}

	return BootstrapResult{Config: defaultCfg, Created: true}, nil
}

func saveIfMissing(cfg Config) error {
	if err := cfg.Validate(); err != nil {
		return err
	}

	path, err := DefaultConfigPath()
	if err != nil {
		return err
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("encode config file: %w", err)
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(dir, configFileName+".*.tmp")
	if err != nil {
		return fmt.Errorf("create temp config file: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() {
		_ = os.Remove(tmpPath)
	}()

	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("set temp config file permissions: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp config file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp config file: %w", err)
	}

	if err := os.Link(tmpPath, path); err != nil {
		if errors.Is(err, os.ErrExist) {
			return os.ErrExist
		}
		return fmt.Errorf("link temp config file: %w", err)
	}

	return nil
}
