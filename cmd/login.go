package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/skip2/go-qrcode"
	"github.com/spf13/cobra"
	"github.com/xiongzheX/weCodex/ilink"
)

var (
	loginLoadRuntimeConfig = loadRuntimeConfig
	loginFetchQRCode       = ilink.FetchQRCode
	loginPollQRStatus      = ilink.PollQRStatus
	loginSaveCredentials   = ilink.SaveCredentials
	loginRenderTerminalQRCode = func(out io.Writer, payload string) error {
		if strings.TrimSpace(payload) == "" {
			return errors.New("terminal QR renderer unavailable")
		}
		qr, err := qrcode.New(payload, qrcode.Medium)
		if err != nil {
			return err
		}
		_, err = fmt.Fprint(out, qr.ToSmallString(false))
		return err
	}
	loginOutputWriter = func(cmd *cobra.Command) io.Writer {
		return cmd.OutOrStdout()
	}
)

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Authenticate with iLink QR login",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runLogin(cmd.Context(), cmd)
	},
}

func runLogin(ctx context.Context, cmd *cobra.Command) error {
	out := loginOutputWriter(cmd)

	cfg, err := loginLoadRuntimeConfig(out)
	if err != nil {
		return err
	}

	qrResp, err := loginFetchQRCode(ctx, "")
	if err != nil {
		return err
	}
	if strings.TrimSpace(qrResp.QRCode) == "" {
		return errors.New("fetch QR code: missing required fields: qrcode")
	}

	payload := qrResp.QRCodeImgContent
	if payload == "" {
		payload = qrResp.QRCode
	}

	fmt.Fprintln(out, "Scan the QR code to login:")
	if err := loginRenderTerminalQRCode(out, payload); err != nil {
		fmt.Fprintln(out, payload)
	}

	creds, err := loginPollQRStatus(ctx, "", qrResp.QRCode, func(status string) {
		fmt.Fprintf(out, "QR status: %s\n", status)
	})
	if err != nil {
		return err
	}

	if err := loginSaveCredentials(cfg, creds); err != nil {
		return err
	}

	fmt.Fprintln(out, "Login succeeded.")
	return nil
}
