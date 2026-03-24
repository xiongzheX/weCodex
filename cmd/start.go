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
	"github.com/xiongzhe/weCodex/bridge"
	"github.com/xiongzhe/weCodex/codexacp"
	"github.com/xiongzhe/weCodex/config"
	"github.com/xiongzhe/weCodex/ilink"
)

const startForegroundNotice = "running in foreground; bridge stays attached to this terminal until interrupted"

type startACPClient interface {
	bridge.ACPClient
	Start(ctx context.Context) error
	Stop() error
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
	startLoadConfig      = config.Load
	startLoadCredentials = ilink.LoadCredentials
	startNewACPClient = func(cfg codexacp.Config) startACPClient {
		return codexacp.NewClient(cfg)
	}
	startNewBridgeService = func(acp bridge.ACPClient) startBridgeService {
		return bridge.NewService(acp)
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

	cfg, err := startLoadConfig()
	if err != nil {
		return err
	}

	creds, err := startLoadCredentials(cfg)
	if err != nil {
		return err
	}

	acp := startNewACPClient(codexacp.Config{
		Command:          cfg.CodexCommand,
		Args:             cfg.CodexArgs,
		WorkingDirectory: cfg.WorkingDirectory,
		PermissionMode:   cfg.PermissionMode,
	})
	if err := acp.Start(ctx); err != nil {
		return err
	}
	defer func() {
		stopErr := acp.Stop()
		if err == nil && !ignoreStopErr {
			err = stopErr
		}
	}()

	cursorPath, err := startCursorPath()
	if err != nil {
		return err
	}
	sender, monitor := startNewILinkClientAndMonitor(creds, cursorPath)
	bridgeSvc := startNewBridgeService(acp)

	fmt.Fprintln(startOutputWriter(cmd), startForegroundNotice)

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
