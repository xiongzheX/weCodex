package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

const (
	configDirName      = ".weCodex"
	configFileName     = "config.json"
	credentialsFileName = "account.json"
)

type Config struct {
	CodexCommand      string   `json:"codex_command"`
	CodexArgs         []string `json:"codex_args,omitempty"`
	WorkingDirectory  string   `json:"working_directory"`
	PermissionMode    string   `json:"permission_mode"`
	LogLevel          string   `json:"log_level,omitempty"`
	WechatAccountsDir string   `json:"wechat_accounts_dir,omitempty"`
}

func (c Config) Validate() error {
	if c.CodexCommand == "" {
		return fmt.Errorf("codex_command is required")
	}
	if len(c.CodexArgs) == 0 {
		return fmt.Errorf("codex_args is required")
	}
	if c.WorkingDirectory == "" {
		return fmt.Errorf("working_directory is required")
	}
	if c.PermissionMode != "readonly" {
		return fmt.Errorf("permission_mode must be readonly")
	}
	return nil
}

func DefaultConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(home, configDirName, configFileName), nil
}

func CredentialsPath(cfg Config) (string, error) {
	if cfg.WechatAccountsDir != "" {
		return filepath.Join(cfg.WechatAccountsDir, credentialsFileName), nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(home, configDirName, credentialsFileName), nil
}

func Load() (Config, error) {
	path, err := DefaultConfigPath()
	if err != nil {
		return Config{}, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Config{}, os.ErrNotExist
		}
		return Config{}, fmt.Errorf("read config file: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("decode config file: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return cfg, err
	}

	return cfg, nil
}

func Save(cfg Config) error {
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

	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("rename temp config file: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("set config file permissions: %w", err)
	}

	return nil
}

func CredentialsFileExists(cfg Config) (bool, error) {
	path, err := CredentialsPath(cfg)
	if err != nil {
		return false, err
	}

	_, err = os.Stat(path)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, fmt.Errorf("stat credentials file: %w", err)
}

func CodexCommandExists(command string) (bool, error) {
	if command == "" {
		return false, nil
	}

	_, err := exec.LookPath(command)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, exec.ErrNotFound) {
		return false, nil
	}
	return false, fmt.Errorf("look up command %q: %w", command, err)
}
