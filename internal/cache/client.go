package cache

import (
	"context"
	"log/slog"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/skylunna/tiny-agent-router/internal/cache/proto"
)

type Client struct {
	conn   *grpc.ClientConn
	client proto.SemanticCacheClient
}

func NewClient(addr string) (*Client, error) {
	// 带超时和重试的连接
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := grpc.DialContext(ctx, addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(), // 启动时确保连接成功
	)
	if err != nil {
		return nil, err
	}

	slog.Info("🔗 Connected to semantic-cache", "addr", addr)
	return &Client{
		conn:   conn,
		client: proto.NewSemanticCacheClient(conn),
	}, nil
}

// Get 查询缓存（带超时，失败自动 bypass）
func (c *Client) Get(ctx context.Context, req *proto.CacheRequest) (*proto.CacheResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, 200*time.Millisecond) // 缓存查询必须快
	defer cancel()

	resp, err := c.client.Get(ctx, req)
	if err != nil {
		slog.Debug("Cache GET failed, bypassing", "error", err)
		return nil, nil // 返回 nil 表示 bypass，不影响主流程
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
	// 后台协程发送，即使失败也不影响主流程
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
