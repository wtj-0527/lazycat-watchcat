package notify

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	gohelper "gitee.com/linakesi/lzc-sdk/lang/go"
	"gitee.com/linakesi/lzc-sdk/lang/go/common"
	"gitee.com/linakesi/lzc-sdk/lang/go/localdevice"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"github.com/wtj-0527/lazycat-maoyan/internal/store"
)

type LazyCat struct {
	store  *store.Store
	logger *slog.Logger
}

func NewLazyCat(st *store.Store, logger *slog.Logger) *LazyCat {
	return &LazyCat{store: st, logger: logger}
}

func (n *LazyCat) ProcessPending(ctx context.Context, limit int) {
	if limit <= 0 {
		limit = 20
	}
	for i := 0; i < limit; i++ {
		item, err := n.store.NextNotification(ctx)
		if store.IsNoPendingNotification(err) {
			return
		}
		if err != nil {
			n.logger.Warn("read notification outbox", "error", err)
			return
		}
		err = n.send(ctx, item.Title, item.Body, item.Deeplink)
		if err == nil {
			_ = n.store.MarkNotificationSent(ctx, item.ID)
			n.logger.Info("lazycat notification delivered", "alert", item.AlertFingerprint, "transition", item.Transition)
			continue
		}
		_ = n.store.MarkNotificationFailed(ctx, item.ID, item.Attempts+1, err.Error())
		n.logger.Warn("lazycat notification deferred", "alert", item.AlertFingerprint, "attempt", item.Attempts+1, "error", err)
		return
	}
}

func (n *LazyCat) send(ctx context.Context, title, body, deeplink string) error {
	callCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	gateway, err := gohelper.NewAPIGateway(callCtx)
	if err != nil {
		return fmt.Errorf("open LazyCat API gateway: %w", err)
	}
	defer gateway.Close()
	uids, err := gateway.Users.ListUIDs(callCtx, &common.ListUIDsRequest{})
	if err != nil {
		return fmt.Errorf("list LazyCat users: %w", err)
	}
	sent := 0
	var lastErr error
	for _, uid := range uids.GetUids() {
		devices, err := gateway.Devices.ListEndDevices(callCtx, &common.ListEndDeviceRequest{Uid: uid})
		if err != nil {
			lastErr = err
			continue
		}
		for _, device := range devices.GetDevices() {
			if device == nil || !device.GetIsOnline() {
				continue
			}
			if err := notifyDevice(callCtx, device.GetDeviceApiUrl(), title, body, deeplink); err != nil {
				lastErr = err
				continue
			}
			sent++
		}
	}
	if sent == 0 {
		if lastErr != nil {
			return fmt.Errorf("no notification delivered: %w", lastErr)
		}
		return fmt.Errorf("no online LazyCat client")
	}
	return nil
}

func notifyDevice(ctx context.Context, deviceAPIURL, title, body, deeplink string) error {
	parsed, err := url.Parse(strings.TrimSpace(deviceAPIURL))
	if err != nil || parsed.Host == "" {
		return fmt.Errorf("invalid device API URL")
	}
	cred, err := gohelper.BuildClientCredOption(gohelper.CAPath, gohelper.APPKeyPath, gohelper.APPCertPath)
	if err != nil {
		return fmt.Errorf("build device credentials: %w", err)
	}
	conn, err := grpc.DialContext(ctx, parsed.Host, grpc.WithBlock(), cred)
	if err != nil {
		return fmt.Errorf("connect device API: %w", err)
	}
	defer conn.Close()
	token, err := gohelper.RequestAuthToken(ctx, conn)
	if err != nil {
		return fmt.Errorf("request device token: %w", err)
	}
	req := &localdevice.NotifyRequest{Title: title, Body: body}
	if strings.TrimSpace(deeplink) != "" {
		req.DeeplinkUrl = &deeplink
	}
	notifyCtx := metadata.AppendToOutgoingContext(ctx, "lzc_dapi_auth_token", token.Token)
	if _, err := localdevice.NewNotificationServiceClient(conn).Notify(notifyCtx, req); err != nil {
		return fmt.Errorf("send device notification: %w", err)
	}
	return nil
}
