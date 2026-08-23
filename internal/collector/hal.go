package collector

import (
	"context"
	"time"

	gohelper "gitee.com/linakesi/lzc-sdk/lang/go"
	"gitee.com/linakesi/lzc-sdk/lang/go/sys"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/wtj-0527/lazycat-maoyan/internal/protocol"
)

type halClient interface {
	GetFanRpm(context.Context, *emptypb.Empty, ...grpc.CallOption) (*sys.FanRpm, error)
}

type HALCollector struct {
	client halClient
	close  func() error
}

func NewHALCollector(ctx context.Context) (*HALCollector, error) {
	conn, err := gohelper.NewAPIConn(ctx)
	if err != nil {
		return nil, err
	}
	return &HALCollector{client: sys.NewHalServiceClient(conn), close: conn.Close}, nil
}

func newHALCollectorWithClient(client halClient) *HALCollector {
	return &HALCollector{client: client}
}

func (h *HALCollector) Close() error {
	if h == nil || h.close == nil {
		return nil
	}
	return h.close()
}

func (h *HALCollector) Collect(ctx context.Context, now time.Time) ([]protocol.MetricPoint, error) {
	response, err := h.client.GetFanRpm(ctx, &emptypb.Empty{})
	if err != nil {
		return nil, err
	}
	if response.GetRpm() <= 0 {
		return nil, nil
	}
	return []protocol.MetricPoint{{
		Name: "system.fan.rpm", Value: float64(response.GetRpm()), Unit: "rpm",
		Labels: map[string]string{"source": "lazycat-hal"}, CollectedAt: now,
	}}, nil
}
