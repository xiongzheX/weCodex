package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/xiongzhe/weCodex/backend"
	"github.com/xiongzhe/weCodex/bridge"
	"github.com/xiongzhe/weCodex/codexacp"
	"github.com/xiongzhe/weCodex/config"
	"github.com/xiongzhe/weCodex/ilink"
)

const startForegroundNotice = "running in foreground; bridge stays attached to this terminal until interrupted"

type startBackendClient interface {
	backend.Client
}

type startBridgeService interface {
	HandleMessage(ctx context.Context, msg ilink.InboundMessage) (bridge.OutboundReply, error)
}

type startSender interface {
	SendMessage(ctx context.Context, req ilink.SendMessageRequest) (ilink.SendMessageResponse, error)
}

type startMonitorRunner interface {
	Run(ctx context.Context, handle func(ilink.InboundMessage) error) error
}

var (
	startLoadConfig        = config.Load
	startLoadRuntimeConfig = loadRuntimeConfig
	startLoadCredentials   = ilink.LoadCredentials
	startNewACPClient      = func(cfg codexacp.Config) startBackendClient {
		return backend.NewACPClient(cfg)
	}
	startNewCLIClient = func(cfg config.Config) backend.Client {
		return backend.NewCLIClient(cfg)
	}
	startNewBridgeService = func(client backend.Client) startBridgeService {
		return bridge.NewService(client)
	}
	startNewILinkClientAndMonitor = func(creds ilink.Credentials, cursorPath string) (startSender, startMonitorRunner) {
		client := ilink.NewClient(creds)
		return client, ilink.NewMonitor(client, cursorPath)
	}
	startOutputWriter = func(cmd *cobra.Command) io.Writer {
		return cmd.OutOrStdout()
	}
	startLogf = log.Printf
	startUserHomeDir = os.UserHomeDir
)

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start bridge in foreground",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runStart(cmd.Context(), cmd)
	},
}

func runStart(ctx context.Context, cmd *cobra.Command) (err error) {
	ignoreStopErr := false
	out := startOutputWriter(cmd)

	cfg, err := startLoadRuntimeConfig(out)
	if err != nil {
		return err
	}

	creds, err := startLoadCredentials(cfg)
	if err != nil {
		return err
	}

	var backendClient backend.Client
	if cfg.BackendType == "cli" {
		backendClient = startNewCLIClient(cfg)
	} else {
		backendClient = startNewACPClient(codexacp.Config{
			Command:          cfg.CodexCommand,
			Args:             cfg.CodexArgs,
			WorkingDirectory: cfg.WorkingDirectory,
			PermissionMode:   cfg.PermissionMode,
		})
	}
	if err := backendClient.Start(ctx); err != nil {
		return err
	}
	defer func() {
		stopErr := backendClient.Stop()
		if err == nil && !ignoreStopErr {
			err = stopErr
		}
	}()

	cursorPath, err := startCursorPath()
	if err != nil {
		return err
	}
	sender, monitor := startNewILinkClientAndMonitor(creds, cursorPath)
	bridgeSvc := startNewBridgeService(backendClient)

	fmt.Fprintln(out, startForegroundNotice)

	err = monitor.Run(ctx, func(msg ilink.InboundMessage) error {
		reply, handleErr := bridgeSvc.HandleMessage(ctx, msg)
		if handleErr != nil {
			return handleErr
		}
		_, sendErr := sender.SendMessage(ctx, ilink.SendMessageRequest{
			ToUserID:     reply.ToUserID,
			ContextToken: reply.ContextToken,
			Text:         reply.Text,
		})
		if sendErr != nil {
			startLogf("warning: ilink sendmessage failed: %v", sendErr)
			return nil
		}
		return nil
	})
	if errors.Is(err, context.Canceled) {
		ignoreStopErr = true
		return nil
	}
	return err
}

func startCursorPath() (string, error) {
	home, err := startUserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(home, ".weCodex", "ilink_cursor.json"), nil
}
