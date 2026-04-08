package cache

import (
	"context"
	"log/slog"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/skylunna/tiny-agent-router/internal/cache/proto"
)

type Client struct {
	conn   *grpc.ClientConn
	client proto.SemanticCacheClient
}

func NewClient(addr string) (*Client, error) {
	conn, err := grpc.NewClient(addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 轮询连接状态，最多等待 5 秒
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if conn.GetState() == connectivity.Ready {
			slog.Info("🔗 semantic-cache connection ready", "addr", addr)
			break
		}
		if !conn.WaitForStateChange(ctx, conn.GetState()) {
			break // 上下文取消
		}
	}

	// 最终检查
	if conn.GetState() != connectivity.Ready {
		slog.Warn("⚠️ semantic-cache connection not ready, will retry on first call",
			"addr", addr, "state", conn.GetState())
	}

	return &Client{
		conn:   conn,
		client: proto.NewSemanticCacheClient(conn),
	}, nil
}

// Get 查询缓存
func (c *Client) Get(ctx context.Context, req *proto.CacheRequest) (*proto.CacheResponse, error) {
	// 缓存查询 200ms 超时
	ctx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()

	resp, err := c.client.Get(ctx, req)
	if err != nil {
		slog.Debug("Cache GET failed, bypassing", "error", err)
		return nil, nil
	}

	if resp.Hit {
		slog.Debug("Cache HIT",
			"request_id", req.RequestId,
			"similarity", resp.Similarity)
	}
	return resp, nil
}

// Put 异步写入缓存（不阻塞主请求）
func (c *Client) Put(ctx context.Context, req *proto.CacheRequest) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		_, err := c.client.Put(ctx, req)
		if err != nil {
			slog.Debug("Cache PUT failed", "error", err)
		}
	}()
}

func (c *Client) Close() error {
	return c.conn.Close()
}
