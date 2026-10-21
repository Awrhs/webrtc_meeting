package sfu

import (
	"context"
	"fmt"
	"time"

	sfupb "conference-sfu/backend/api/gen/go/sfu_control"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

type ControlClient struct {
	client       sfupb.SFUControlClient
	conn         *grpc.ClientConn
	logger       *zap.Logger
	controlToken string
}

func NewControlClient(sfuAddr string, controlToken string, logger *zap.Logger) (*ControlClient, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := grpc.DialContext(ctx, sfuAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to SFU gRPC server: %w", err)
	}

	return &ControlClient{
		client:       sfupb.NewSFUControlClient(conn),
		conn:         conn,
		logger:       logger,
		controlToken: controlToken,
	}, nil
}

func (c *ControlClient) Close() error {
	return c.conn.Close()
}

func (c *ControlClient) controlContext(timeout time.Duration) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	if c.controlToken == "" {
		return ctx, cancel
	}
	return metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+c.controlToken), cancel
}

func (c *ControlClient) KickUser(roomID, userID uint, reason string) error {
	ctx, cancel := c.controlContext(5 * time.Second)
	defer cancel()

	_, err := c.client.KickUser(ctx, &sfupb.KickUserRequest{
		RoomId: uint32(roomID),
		UserId: uint32(userID),
		Reason: reason,
	})
	if err != nil {
		c.logger.Error("Failed to call KickUser via gRPC", zap.Error(err))
		return err
	}

	return nil
}

func (c *ControlClient) MuteUser(roomID, userID uint, isMuted bool) error {
	ctx, cancel := c.controlContext(5 * time.Second)
	defer cancel()

	_, err := c.client.MuteUser(ctx, &sfupb.MuteUserRequest{
		RoomId:  uint32(roomID),
		UserId:  uint32(userID),
		IsMuted: isMuted,
	})
	if err != nil {
		c.logger.Error("Failed to call MuteUser via gRPC", zap.Error(err))
		return err
	}

	return nil
}

// SetVideoEnabled 启用/禁用用户视频轨道转发
func (c *ControlClient) SetVideoEnabled(roomID, userID uint, enabled bool) error {
	ctx, cancel := c.controlContext(5 * time.Second)
	defer cancel()

	_, err := c.client.SetVideoEnabled(ctx, &sfupb.SetVideoEnabledRequest{
		RoomId:  uint32(roomID),
		UserId:  uint32(userID),
		Enabled: enabled,
	})
	if err != nil {
		c.logger.Error("Failed to call SetVideoEnabled via gRPC", zap.Error(err))
		return err
	}

	return nil
}

func (c *ControlClient) ClosePeerConnection(roomID, userID uint) error {
	ctx, cancel := c.controlContext(5 * time.Second)
	defer cancel()

	_, err := c.client.ClosePeerConnection(ctx, &sfupb.ClosePeerConnectionRequest{
		RoomId: uint32(roomID),
		UserId: uint32(userID),
	})
	if err != nil {
		c.logger.Error("Failed to call ClosePeerConnection via gRPC", zap.Error(err))
		return err
	}

	return nil
}

func (c *ControlClient) GetRoomStats(roomID uint) (*sfupb.GetRoomStatsResponse, error) {
	ctx, cancel := c.controlContext(5 * time.Second)
	defer cancel()

	return c.client.GetRoomStats(ctx, &sfupb.GetRoomStatsRequest{
		RoomId: uint32(roomID),
	})
}

func (c *ControlClient) ListRooms() (*sfupb.ListRoomsResponse, error) {
	ctx, cancel := c.controlContext(5 * time.Second)
	defer cancel()

	return c.client.ListRooms(ctx, &sfupb.ListRoomsRequest{})
}
