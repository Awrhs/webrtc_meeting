package sfu

import (
	"context"
	"net"
	"strings"

	sfupb "conference-sfu/backend/api/gen/go/sfu_control"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type ControlServer struct {
	sfupb.UnimplementedSFUControlServer
	sfuServer *SFUServer
	logger    *zap.Logger
}

func NewControlServer(sfuServer *SFUServer, logger *zap.Logger) *ControlServer {
	return &ControlServer{
		sfuServer: sfuServer,
		logger:    logger,
	}
}

func (s *ControlServer) KickUser(ctx context.Context, req *sfupb.KickUserRequest) (*sfupb.KickUserResponse, error) {
	err := s.sfuServer.KickUser(uint(req.RoomId), uint(req.UserId), req.Reason)
	if err != nil {
		s.logger.Error("Failed to kick user", zap.Error(err))
		return &sfupb.KickUserResponse{
			Success: false,
			Message: err.Error(),
		}, status.Error(codes.Internal, err.Error())
	}

	return &sfupb.KickUserResponse{
		Success: true,
		Message: "User kicked successfully",
	}, nil
}

func (s *ControlServer) MuteUser(ctx context.Context, req *sfupb.MuteUserRequest) (*sfupb.MuteUserResponse, error) {
	err := s.sfuServer.MuteUser(uint(req.RoomId), uint(req.UserId), req.IsMuted)
	if err != nil {
		s.logger.Error("Failed to mute user", zap.Error(err))
		return &sfupb.MuteUserResponse{
			Success: false,
			Message: err.Error(),
		}, status.Error(codes.Internal, err.Error())
	}

	return &sfupb.MuteUserResponse{
		Success: true,
		Message: "User mute state updated",
	}, nil
}

func (s *ControlServer) SetVideoEnabled(ctx context.Context, req *sfupb.SetVideoEnabledRequest) (*sfupb.SetVideoEnabledResponse, error) {
	// 启用/禁用用户视频轨道转发（与音频静音对称）
	err := s.sfuServer.SetVideoEnabled(uint(req.RoomId), uint(req.UserId), req.Enabled)
	if err != nil {
		s.logger.Error("Failed to set video enabled", zap.Error(err))
		return &sfupb.SetVideoEnabledResponse{
			Success: false,
			Message: err.Error(),
		}, status.Error(codes.Internal, err.Error())
	}

	return &sfupb.SetVideoEnabledResponse{
		Success: true,
		Message: "Video state updated",
	}, nil
}

func (s *ControlServer) GetRoomStats(ctx context.Context, req *sfupb.GetRoomStatsRequest) (*sfupb.GetRoomStatsResponse, error) {
	count, peerStats, err := s.sfuServer.GetRoomStats(uint(req.RoomId))
	if err != nil {
		return nil, status.Error(codes.NotFound, err.Error())
	}

	peers := make([]*sfupb.PeerStats, 0, len(peerStats))
	for _, st := range peerStats {
		peers = append(peers, &sfupb.PeerStats{
			UserId:          uint32(st.UserID),
			AudioEnabled:    st.AudioEnabled,
			VideoEnabled:    st.VideoEnabled,
			BytesReceived:   int64(st.BytesReceived),
			BytesSent:       int64(st.BytesSent),
			Jitter:          uint32(st.Jitter),
			PacketsLost:     uint32(st.PacketsLost),
			ConnectionState: st.ConnectionState,
		})
	}

	return &sfupb.GetRoomStatsResponse{
		RoomId:    req.RoomId,
		PeerCount: uint32(count),
		Peers:     peers,
	}, nil
}

func (s *ControlServer) ListRooms(ctx context.Context, req *sfupb.ListRoomsRequest) (*sfupb.ListRoomsResponse, error) {
	rooms, err := s.sfuServer.ListRooms()
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	roomList := make([]*sfupb.RoomInfo, 0, len(rooms))
	for roomID, peerCount := range rooms {
		roomList = append(roomList, &sfupb.RoomInfo{
			RoomId:    uint32(roomID),
			PeerCount: uint32(peerCount),
		})
	}

	return &sfupb.ListRoomsResponse{
		Rooms: roomList,
	}, nil
}

func (s *ControlServer) ClosePeerConnection(ctx context.Context, req *sfupb.ClosePeerConnectionRequest) (*sfupb.ClosePeerConnectionResponse, error) {
	err := s.sfuServer.ClosePeerConnection(uint(req.RoomId), uint(req.UserId))
	if err != nil {
		return &sfupb.ClosePeerConnectionResponse{
			Success: false,
			Message: err.Error(),
		}, status.Error(codes.Internal, err.Error())
	}

	return &sfupb.ClosePeerConnectionResponse{
		Success: true,
		Message: "Peer connection closed",
	}, nil
}

func StartGRPCServer(sfuServer *SFUServer, port string, controlToken string, logger *zap.Logger) (*grpc.Server, error) {
	lis, err := net.Listen("tcp", ":"+port)
	if err != nil {
		return nil, err
	}

	grpcServer := grpc.NewServer(grpc.UnaryInterceptor(controlAuthInterceptor(controlToken)))
	controlServer := NewControlServer(sfuServer, logger)
	sfupb.RegisterSFUControlServer(grpcServer, controlServer)

	go func() {
		logger.Info("SFU gRPC control server starting", zap.String("port", port))
		if err := grpcServer.Serve(lis); err != nil {
			logger.Fatal("Failed to serve gRPC", zap.Error(err))
		}
	}()

	return grpcServer, nil
}

func controlAuthInterceptor(controlToken string) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		if controlToken == "" {
			return nil, status.Error(codes.Unauthenticated, "missing control token")
		}
		md, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			return nil, status.Error(codes.Unauthenticated, "missing metadata")
		}
		values := md.Get("authorization")
		if len(values) == 0 || strings.TrimPrefix(values[0], "Bearer ") != controlToken {
			return nil, status.Error(codes.Unauthenticated, "invalid control token")
		}
		return handler(ctx, req)
	}
}
