package cmd

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/xiongzheX/weCodex/config"
)

var statusLoadRuntimeConfig = loadRuntimeConfig

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show static readiness status",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, cfgErr := statusLoadRuntimeConfig(cmd.OutOrStdout())

		configPresent := cfgErr == nil
		if cfgErr != nil && !errors.Is(cfgErr, os.ErrNotExist) {
			configPresent = true
		}

		var credentialsPresent bool
		var credentialsErr error
		var codexPresent bool
		var codexErr error
		if canUseDecodedConfigForDependentChecks(cfgErr) {
			credentialsPresent, credentialsErr = config.CredentialsFileExists(cfg)
			if credentialsErr != nil {
				credentialsPresent = false
			}

			codexPresent, codexErr = config.CodexCommandExists(cfg.CodexCommand)
			if codexErr != nil {
				codexPresent = false
			}
		} else if cfgErr != nil && !errors.Is(cfgErr, os.ErrNotExist) {
			credentialsErr = errors.New("skipped because config could not be loaded reliably")
			codexErr = errors.New("skipped because config could not be loaded reliably")
		}

		backendStatus := readinessBackendStatus(cfg, cfgErr)
		summary := BuildReadinessSummary(backendStatus, configPresent, credentialsPresent, codexPresent, cfgErr, codexErr)
		if credentialsErr != nil {
			summary += "\ncredentials error: " + credentialsErr.Error()
		}
		fmt.Fprintln(cmd.OutOrStdout(), summary)
		return nil
	},
}

func BuildReadinessSummary(backendStatus string, configPresent bool, credentialsPresent bool, codexPresent bool, configErr error, codexErr error) string {
	configStatus := "missing"
	switch {
	case configErr != nil && !errors.Is(configErr, os.ErrNotExist):
		configStatus = "invalid"
	case configPresent:
		configStatus = "exists"
	}

	credentialsStatus := "missing"
	if configErr != nil && !errors.Is(configErr, os.ErrNotExist) && !canUseDecodedConfigForDependentChecks(configErr) {
		credentialsStatus = "unknown"
	} else if credentialsPresent {
		credentialsStatus = "present"
	}

	codexStatus := "unresolvable"
	if configErr != nil && !errors.Is(configErr, os.ErrNotExist) && !canUseDecodedConfigForDependentChecks(configErr) {
		codexStatus = "unknown"
	} else if codexPresent {
		codexStatus = "resolvable"
	}

	ready := configStatus == "exists" && credentialsPresent && codexPresent
	readyStatus := "no"
	if ready {
		readyStatus = "yes"
	}

	lines := []string{
		"static checks only",
		"backend: " + backendStatus,
		"config: " + configStatus,
		"credentials: " + credentialsStatus,
		"codex command: " + codexStatus,
		"ready: " + readyStatus,
	}

	if configErr != nil && !errors.Is(configErr, os.ErrNotExist) {
		lines = append(lines, "config error: "+configErr.Error())
	}
	if codexErr != nil {
		lines = append(lines, "codex error: "+codexErr.Error())
	}

	return strings.Join(lines, "\n")
}

func canUseDecodedConfigForDependentChecks(configErr error) bool {
	if configErr == nil || errors.Is(configErr, os.ErrNotExist) {
		return true
	}

	msg := configErr.Error()
	return !strings.Contains(msg, "decode config file:") && !strings.Contains(msg, "read config file:") && !strings.Contains(msg, "resolve home dir:")
}

func readinessBackendStatus(cfg config.Config, configErr error) string {
	if errors.Is(configErr, os.ErrNotExist) {
		return "unknown"
	}
	if configErr != nil && !canUseDecodedConfigForDependentChecks(configErr) {
		return "unknown"
	}

	backendType := cfg.BackendType
	if backendType == "" {
		backendType = "acp"
	}
	return backendType
}
