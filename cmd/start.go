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
	"github.com/xiongzheX/weCodex/backend"
	"github.com/xiongzheX/weCodex/bridge"
	"github.com/xiongzheX/weCodex/codexacp"
	"github.com/xiongzheX/weCodex/config"
	"github.com/xiongzheX/weCodex/ilink"
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
		startLogf("info: inbound message received from=%s type=%d state=%d text_len=%d context_token_len=%d", msg.FromUserID, msg.MessageType, msg.MessageState, len(msg.Text), len(msg.ContextToken))
		reply, handleErr := bridgeSvc.HandleMessage(ctx, msg)
		if handleErr != nil {
			return handleErr
		}
		resp, sendErr := sender.SendMessage(ctx, ilink.SendMessageRequest{
			Msg: ilink.SendMsg{
				FromUserID:   creds.ILinkBotID,
				ToUserID:     reply.ToUserID,
				ClientID:     reply.ContextToken,
				MessageType:  ilink.MessageTypeBot,
				MessageState: ilink.MessageStateFinish,
				ItemList: []ilink.MessageItem{{
					Type: ilink.ItemTypeText,
					TextItem: &ilink.TextItem{Text: reply.Text},
				}},
				ContextToken: reply.ContextToken,
			},
			BaseInfo: ilink.BaseInfo{},
		})
		if sendErr != nil {
			startLogf("warning: ilink sendmessage failed: %v", sendErr)
			return nil
		}
		startLogf("info: outbound message sent to=%s text_len=%d context_token_len=%d ilink_ret=%d ilink_errmsg=%q", reply.ToUserID, len(reply.Text), len(reply.ContextToken), resp.Ret, resp.ErrMsg)
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
