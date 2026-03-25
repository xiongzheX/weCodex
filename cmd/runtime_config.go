package cmd

import (
	"fmt"
	"io"
	"os"

	"github.com/xiongzheX/weCodex/config"
)

const defaultConfigCreatedNotice = "default config created: ~/.weCodex/config.json (backend: cli)"

var (
	runtimeConfigGetwd           = os.Getwd
	runtimeConfigLoadOrBootstrap = config.LoadOrBootstrap
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
