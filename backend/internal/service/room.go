package service

import (
	"conference-sfu/backend/internal/auth"
	"conference-sfu/backend/internal/models"
	"conference-sfu/backend/internal/repository"
	"errors"
	"math/rand"
	"strconv"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
)

type RoomService interface {
	CreateRoom(ownerID uint, req *models.CreateRoomRequest) (*models.Room, error)
	GetRoomByID(id uint) (*models.Room, error)
	GetRoomByRoomID(roomID string) (*models.Room, error)
	GetMyRooms(userID uint) ([]*models.Room, error)
	JoinRoom(userID uint, roomID string, verifyCode string) (*models.JoinRoomResponse, error)
	LeaveRoom(roomID uint, userID uint) error
	LockRoom(roomID uint, userID uint, locked bool) error
	DeleteRoom(roomID uint, userID uint) error
	KickMember(roomID uint, requesterID uint, targetID uint) error
	MuteMember(roomID uint, requesterID uint, targetID uint, muted bool) error
	SetAdmin(roomID uint, ownerID uint, targetID uint, isAdmin bool) error
	TransferOwner(roomID uint, oldOwnerID uint, newOwnerID uint) error
	GetRoomMembersInfo(roomID uint) ([]*models.RoomMemberInfo, error)
	CheckPermission(roomID uint, userID uint, requiredRole models.Role) (bool, error)
	HasPermission(roomID uint, userID uint, permission models.Permission) (bool, error)
	GetMemberPermissions(roomID uint, userID uint) ([]models.Permission, error)
	GetMemberRole(roomID uint, userID uint) (models.Role, error)
	UpdateMediaState(roomID uint, userID uint, videoOn, audioOn *bool) (bool, bool, error)
	GenerateSFUToken(roomID uint, userID uint) (string, error)
	PickSFUEndpoint(roomID uint) (string, error)
	LogAudit(roomID, userID uint, targetID *uint, action, details, ipAddress, userAgent string) error
	GetAuditLogs(roomID uint, limit, offset int) ([]*models.AuditLog, error)
}

type roomService struct {
	roomRepo              repository.RoomRepository
	userRepo              repository.UserRepository
	redis                 *repository.RedisClient
	sfuJWTSecret          string
	sfuTokenExpiryMinutes int
	logger                *zap.Logger
}

func NewRoomService(roomRepo repository.RoomRepository, userRepo repository.UserRepository, redis *repository.RedisClient, sfuJWTSecret string, sfuTokenExpiryMinutes int, logger *zap.Logger) RoomService {
	return &roomService{
		roomRepo:              roomRepo,
		userRepo:              userRepo,
		redis:                 redis,
		sfuJWTSecret:          sfuJWTSecret,
		sfuTokenExpiryMinutes: sfuTokenExpiryMinutes,
		logger:                logger,
	}
}

func (s *roomService) CreateRoom(ownerID uint, req *models.CreateRoomRequest) (*models.Room, error) {
	// 生成唯一的 6 位 room_id：库中已有同号房间则重试，避免撞唯一索引后
	// 直接报数据库错误（6 位数字空间有限，房间多时碰撞概率不可忽略）
	var roomID string
	for i := 0; i < 5; i++ {
		candidate := generateRoomID()
		if _, err := s.roomRepo.FindByRoomID(candidate); err != nil {
			roomID = candidate
			break
		}
		s.logger.Warn("room_id collision, retrying", zap.String("room_id", candidate))
	}
	if roomID == "" {
		return nil, errors.New("failed to generate unique room id, please retry")
	}
	inviteCode := generateInviteCode()

	room := &models.Room{
		RoomID:      roomID,
		Name:        req.Name,
		Topic:       req.Topic,
		OwnerID:     ownerID,
		MaxMembers:  req.MaxMembers,
		IsPermanent: req.IsPermanent,
		AuthType:    req.AuthType,
		InviteCode:  inviteCode,
		IsLocked:    false,
	}

	if req.AuthType == models.AuthTypePassword && req.Password != "" {
		hashed, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
		if err != nil {
			return nil, err
		}
		room.Password = string(hashed)
	}

	tx := s.roomRepo.Begin()

	err := tx.Create(room)
	if err != nil {
		tx.Rollback()
		return nil, err
	}

	member := &models.RoomMember{
		RoomID:    room.ID,
		UserID:    ownerID,
		Role:      models.RoleOwner,
		IsMuted:   false,
		IsVideoOn: false,
		IsAudioOn: true,
		JoinedAt:  time.Now(),
	}
	err = tx.AddMember(member)
	if err != nil {
		tx.Rollback()
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	s.redis.AddToRoom(room.ID, ownerID)
	s.redis.DeleteUserRooms(ownerID)

	return room, nil
}

func (s *roomService) GetRoomByID(id uint) (*models.Room, error) {
	// Try cache first
	if cached, err := s.redis.GetRoom(id); err == nil && cached != nil {
		return cached, nil
	}

	// Cache miss, fetch from DB
	room, err := s.roomRepo.FindByID(id)
	if err != nil {
		return nil, err
	}

	// Cache the result
	s.redis.SetRoom(room)
	return room, nil
}

func (s *roomService) GetRoomByRoomID(roomID string) (*models.Room, error) {
	return s.roomRepo.FindByRoomID(roomID)
}

func (s *roomService) GetMyRooms(userID uint) ([]*models.Room, error) {
	// Try cache first
	if cached, err := s.redis.GetUserRooms(userID); err == nil && cached != nil {
		return cached, nil
	}

	// Cache miss, fetch from DB
	rooms, err := s.roomRepo.FindRoomsByUserID(userID)
	if err != nil {
		return nil, err
	}

	// Cache the result
	s.redis.SetUserRooms(userID, rooms)
	return rooms, nil
}

func (s *roomService) JoinRoom(userID uint, roomIDStr string, verifyCode string) (*models.JoinRoomResponse, error) {
	// 先尝试按 6 位 room_id 查找，再尝试按数据库主键 id 查找
	var room *models.Room
	var err error

	// 先按 6 位 room_id 查找
	room, err = s.roomRepo.FindByRoomID(roomIDStr)
	if err != nil {
		// 失败则尝试按数据库主键 id 查找
		roomID, parseErr := strconv.Atoi(roomIDStr)
		if parseErr == nil {
			room, err = s.roomRepo.FindByID(uint(roomID))
		}
	}
	if err != nil || room == nil {
		return nil, errors.New("room not found")
	}

	isKicked, err := s.roomRepo.IsKicked(room.ID, userID)
	if err == nil && isKicked {
		return nil, errors.New("you are kicked from this room, please try again later")
	}

	// 已在房间（未离开）的成员（如刷新页面重新入会）幂等返回，不受满员/锁房限制
	activeMember, err := s.roomRepo.GetMember(room.ID, userID)
	if err != nil {
		activeMember = nil
	}

	// 锁房只拦截新成员：已在会议中的人刷新页面不能被挡在门外
	if room.IsLocked && activeMember == nil {
		return nil, errors.New("room is locked")
	}

	switch room.AuthType {
	case models.AuthTypePassword:
		if verifyCode == "" {
			return nil, errors.New("password required")
		}
		err := bcrypt.CompareHashAndPassword([]byte(room.Password), []byte(verifyCode))
		if err != nil {
			return nil, errors.New("invalid password")
		}
	case models.AuthTypeInviteCode:
		if verifyCode != room.InviteCode {
			return nil, errors.New("invalid invite code")
		}
	}

	if activeMember == nil {
		if err := s.admitMember(room, userID); err != nil {
			return nil, err
		}
	}

	// 以成员表为准刷新人数。只更新 current_size 列：Save 整行会把并发的锁房等修改覆盖掉
	newCount, err := s.roomRepo.CountMembers(room.ID)
	if err != nil {
		return nil, err
	}
	room.CurrentSize = int(newCount)
	if err := s.roomRepo.UpdateCurrentSize(room.ID, room.CurrentSize); err != nil {
		s.logger.Warn("Failed to update room size", zap.Uint("roomID", room.ID), zap.Error(err))
	}

	s.redis.AddToRoom(room.ID, userID)

	// Invalidate room and user rooms cache
	s.redis.DeleteRoom(room.ID)
	s.redis.DeleteUserRooms(userID)

	membersInfo, err := s.GetRoomMembersInfo(room.ID)
	if err != nil {
		return nil, err
	}

	// 获取用户权限列表
	permissions, err := s.GetMemberPermissions(room.ID, userID)
	if err != nil {
		return nil, err
	}

	sfuToken, err := s.GenerateSFUToken(room.ID, userID)
	if err != nil {
		return nil, err
	}

	return &models.JoinRoomResponse{
		Room:        room,
		Members:     membersInfo,
		Permissions: permissions,
		SFUToken:    sfuToken,
	}, nil
}

// admitMember 在单个事务内完成"锁房间行 → 校验锁房/人数 → 创建或恢复成员记录 → 更新人数"。
// 行锁把并发入会串行化：两个人同时抢最后一个名额只会放进一个；同一用户的并发
// 重复请求在持锁后重新检查，不会产生第二条成员记录（数据库唯一索引是最后兜底）。
func (s *roomService) admitMember(room *models.Room, userID uint) error {
	tx := s.roomRepo.Begin()

	lockedRoom, err := tx.LockRoomForUpdate(room.ID)
	if err != nil {
		tx.Rollback()
		return err
	}
	if lockedRoom.IsLocked {
		tx.Rollback()
		return errors.New("room is locked")
	}

	// 持锁后再确认：并发的另一次加入可能刚刚把这个用户放进来了
	if member, err := tx.GetMember(room.ID, userID); err == nil && member != nil {
		tx.Rollback()
		return nil
	}

	count, err := tx.CountMembers(room.ID)
	if err != nil {
		tx.Rollback()
		return err
	}
	if int(count) >= lockedRoom.MaxMembers {
		tx.Rollback()
		return errors.New("room is full")
	}

	existing, err := tx.GetAnyMember(room.ID, userID)
	if err != nil {
		existing = nil
	}
	if existing == nil {
		member := &models.RoomMember{
			RoomID:    room.ID,
			UserID:    userID,
			Role:      models.RoleMember,
			IsMuted:   false,
			IsVideoOn: false,
			IsAudioOn: true,
			JoinedAt:  time.Now(),
		}
		if err := tx.AddMember(member); err != nil {
			tx.Rollback()
			return err
		}
	} else {
		// 用户曾经加入过：复用同一条记录（唯一索引要求一人一行）
		now := time.Now()
		existing.LeftAt = nil
		existing.JoinedAt = now
		if err := tx.UpdateMember(existing); err != nil {
			tx.Rollback()
			return err
		}
	}

	if err := tx.UpdateCurrentSize(room.ID, int(count)+1); err != nil {
		tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	room.IsLocked = lockedRoom.IsLocked
	room.CurrentSize = int(count) + 1
	return nil
}

func (s *roomService) LeaveRoom(roomID uint, userID uint) error {
	err := s.roomRepo.RemoveMember(roomID, userID)
	if err != nil {
		return err
	}

	s.redis.RemoveFromRoom(roomID, userID)

	count, err := s.roomRepo.CountMembers(roomID)
	if err != nil {
		return err
	}
	if err := s.roomRepo.UpdateCurrentSize(roomID, int(count)); err != nil {
		return err
	}

	// Invalidate caches
	s.redis.DeleteRoom(roomID)
	s.redis.DeleteUserRooms(userID)

	// 注意：临时房间在人数归零时不自动删除——断线宽限期场景下，
	// 最后一个用户断线（宽限期内未重连）不应销毁房间，否则其他用户
	// 短暂掉线后无法重新加入。房间仅在房主“解散房间”时删除。
	return nil
}

func (s *roomService) LockRoom(roomID uint, userID uint, locked bool) error {
	role, err := s.GetMemberRole(roomID, userID)
	if err != nil {
		return err
	}
	if role != models.RoleOwner {
		return errors.New("permission denied")
	}

	room, err := s.roomRepo.FindByID(roomID)
	if err != nil {
		return err
	}

	room.IsLocked = locked
	if err := s.roomRepo.Update(room); err != nil {
		return err
	}

	// Invalidate room cache
	s.redis.DeleteRoom(roomID)
	return nil
}

func (s *roomService) DeleteRoom(roomID uint, userID uint) error {
	role, err := s.GetMemberRole(roomID, userID)
	if err != nil || role != models.RoleOwner {
		return errors.New("permission denied")
	}

	if err := s.roomRepo.Delete(roomID); err != nil {
		return err
	}

	// Invalidate room cache
	s.redis.DeleteRoom(roomID)
	s.redis.DeleteUserRooms(userID)
	return nil
}

func (s *roomService) KickMember(roomID uint, requesterID uint, targetID uint) error {
	if requesterID == targetID {
		return errors.New("cannot kick yourself")
	}

	hasPerm, err := s.CheckPermission(roomID, requesterID, models.RoleAdmin)
	if err != nil || !hasPerm {
		return errors.New("permission denied")
	}

	requesterRole, err := s.GetMemberRole(roomID, requesterID)
	if err != nil {
		s.logger.Warn("Failed to get requester role",
			zap.Uint("roomID", roomID),
			zap.Uint("requesterID", requesterID),
			zap.Error(err))
		return errors.New("permission denied")
	}
	targetRole, err := s.GetMemberRole(roomID, targetID)
	if err != nil {
		s.logger.Warn("Failed to get target role",
			zap.Uint("roomID", roomID),
			zap.Uint("targetID", targetID),
			zap.Error(err))
		return errors.New("permission denied")
	}

	if targetRole == models.RoleOwner || (targetRole == models.RoleAdmin && requesterRole != models.RoleOwner) {
		return errors.New("permission denied")
	}

	tx := s.roomRepo.Begin()

	err = tx.RemoveMember(roomID, targetID)
	if err != nil {
		tx.Rollback()
		return err
	}

	expiresAt := time.Now().Add(10 * time.Minute)
	kb := &models.KickBan{
		RoomID:    roomID,
		UserID:    targetID,
		KickedAt:  time.Now(),
		ExpiresAt: expiresAt,
		Reason:    "kicked by admin",
	}
	if err := tx.CreateKickBan(kb); err != nil {
		tx.Rollback()
		return err
	}

	count, err := tx.CountMembers(roomID)
	if err != nil {
		tx.Rollback()
		return err
	}
	if err := tx.UpdateCurrentSize(roomID, int(count)); err != nil {
		tx.Rollback()
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	s.redis.RemoveFromRoom(roomID, targetID)
	s.redis.DeleteRoom(roomID)
	s.redis.DeleteUserRooms(targetID)

	return nil
}

func (s *roomService) MuteMember(roomID uint, requesterID uint, targetID uint, muted bool) error {
	if requesterID == targetID {
		return errors.New("cannot mute yourself")
	}
	hasPerm, err := s.CheckPermission(roomID, requesterID, models.RoleAdmin)
	if err != nil || !hasPerm {
		return errors.New("permission denied")
	}

	// 角色层级检查（与踢人对齐）：不能禁言房主；管理员之间不能互相禁言
	requesterRole, err := s.GetMemberRole(roomID, requesterID)
	if err != nil {
		return errors.New("permission denied")
	}
	targetRole, err := s.GetMemberRole(roomID, targetID)
	if err != nil {
		return errors.New("member not found")
	}
	if targetRole == models.RoleOwner || (targetRole == models.RoleAdmin && requesterRole != models.RoleOwner) {
		return errors.New("permission denied")
	}

	member, err := s.roomRepo.GetMember(roomID, targetID)
	if err != nil {
		return err
	}

	member.IsMuted = muted
	return s.roomRepo.UpdateMember(member)
}

func (s *roomService) SetAdmin(roomID uint, ownerID uint, targetID uint, isAdmin bool) error {
	role, err := s.GetMemberRole(roomID, ownerID)
	if err != nil || role != models.RoleOwner {
		return errors.New("permission denied")
	}

	member, err := s.roomRepo.GetMember(roomID, targetID)
	if err != nil {
		return err
	}

	if isAdmin {
		member.Role = models.RoleAdmin
	} else {
		member.Role = models.RoleMember
	}

	return s.roomRepo.UpdateMember(member)
}

func (s *roomService) TransferOwner(roomID uint, oldOwnerID uint, newOwnerID uint) error {
	role, err := s.GetMemberRole(roomID, oldOwnerID)
	if err != nil || role != models.RoleOwner {
		return errors.New("permission denied")
	}

	room, err := s.roomRepo.FindByID(roomID)
	if err != nil {
		return err
	}

	oldOwnerMember, err := s.roomRepo.GetMember(roomID, oldOwnerID)
	if err != nil {
		return err
	}
	newOwnerMember, err := s.roomRepo.GetMember(roomID, newOwnerID)
	if err != nil {
		return err
	}

	oldOwnerMember.Role = models.RoleMember
	newOwnerMember.Role = models.RoleOwner
	room.OwnerID = newOwnerID

	tx := s.roomRepo.Begin()

	if err := tx.UpdateMember(oldOwnerMember); err != nil {
		tx.Rollback()
		return err
	}
	if err := tx.UpdateMember(newOwnerMember); err != nil {
		tx.Rollback()
		return err
	}
	if err := tx.Update(room); err != nil {
		tx.Rollback()
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	s.redis.DeleteRoom(roomID)
	return nil
}

func (s *roomService) GetRoomMembersInfo(roomID uint) ([]*models.RoomMemberInfo, error) {
	members, err := s.roomRepo.GetMembers(roomID)
	if err != nil {
		return nil, err
	}
	if len(members) == 0 {
		return []*models.RoomMemberInfo{}, nil
	}

	userIDs := make([]uint, 0, len(members))
	for _, member := range members {
		userIDs = append(userIDs, member.UserID)
	}

	users, err := s.userRepo.FindByIDs(userIDs)
	if err != nil {
		return nil, err
	}
	usersByID := make(map[uint]*models.User, len(users))
	for _, user := range users {
		usersByID[user.ID] = user
	}

	result := make([]*models.RoomMemberInfo, 0, len(members))
	for _, member := range members {
		user, ok := usersByID[member.UserID]
		if !ok {
			continue
		}
		result = append(result, &models.RoomMemberInfo{
			ID:        member.ID,
			UserID:    member.UserID,
			Nickname:  user.Nickname,
			Avatar:    user.Avatar,
			Role:      member.Role,
			IsMuted:   member.IsMuted,
			IsVideoOn: member.IsVideoOn,
			IsAudioOn: member.IsAudioOn,
		})
	}
	return result, nil
}

func (s *roomService) CheckPermission(roomID uint, userID uint, requiredRole models.Role) (bool, error) {
	role, err := s.GetMemberRole(roomID, userID)
	if err != nil {
		return false, err
	}

	switch requiredRole {
	case models.RoleOwner:
		return role == models.RoleOwner, nil
	case models.RoleAdmin:
		return role == models.RoleOwner || role == models.RoleAdmin, nil
	case models.RoleMember:
		return true, nil
	default:
		return false, nil
	}
}

// HasPermission 检查用户是否拥有指定的细粒度权限
func (s *roomService) HasPermission(roomID uint, userID uint, permission models.Permission) (bool, error) {
	role, err := s.GetMemberRole(roomID, userID)
	if err != nil {
		return false, err
	}
	return role.HasPermission(permission), nil
}

// GetMemberPermissions 获取用户在房间内的所有权限列表
func (s *roomService) GetMemberPermissions(roomID uint, userID uint) ([]models.Permission, error) {
	role, err := s.GetMemberRole(roomID, userID)
	if err != nil {
		return nil, err
	}
	perms, ok := models.RolePermissions[role]
	if !ok {
		return []models.Permission{}, nil
	}
	return perms, nil
}

func (s *roomService) GetMemberRole(roomID uint, userID uint) (models.Role, error) {
	member, err := s.roomRepo.GetMember(roomID, userID)
	if err != nil {
		return "", err
	}
	return member.Role, nil
}

func (s *roomService) UpdateMediaState(roomID uint, userID uint, videoOn, audioOn *bool) (bool, bool, error) {
	member, err := s.roomRepo.GetMember(roomID, userID)
	if err != nil {
		return false, false, err
	}
	if videoOn != nil {
		member.IsVideoOn = *videoOn
	}
	if audioOn != nil {
		member.IsAudioOn = *audioOn
	}
	if err := s.roomRepo.UpdateMember(member); err != nil {
		return false, false, err
	}
	return member.IsVideoOn, member.IsAudioOn, nil
}

func (s *roomService) GenerateSFUToken(roomID uint, userID uint) (string, error) {
	if _, err := s.roomRepo.GetMember(roomID, userID); err != nil {
		return "", errors.New("permission denied")
	}
	isKicked, err := s.roomRepo.IsKicked(roomID, userID)
	if err == nil && isKicked {
		return "", errors.New("you are kicked from this room, please try again later")
	}
	expiresIn := time.Duration(s.sfuTokenExpiryMinutes) * time.Minute
	if expiresIn <= 0 {
		expiresIn = 5 * time.Minute
	}
	return auth.GenerateSFUJoinToken(userID, roomID, s.sfuJWTSecret, expiresIn)
}

// PickSFUEndpoint 返回房间所在 SFU 实例面向浏览器的 WebSocket 地址（集群模式）。
// 首次访问时选择负载最低的实例并固定房间亲和；没有实例注册（单机模式）时返回空串，
// 前端回退到默认 VITE_SFU_HOST。
func (s *roomService) PickSFUEndpoint(roomID uint) (string, error) {
	// 已有亲和映射且实例仍在线
	if instanceID, err := s.redis.GetRoomSFUInstance(roomID); err == nil && instanceID != "" {
		if inst, err := s.redis.GetSFUInstanceByID(instanceID); err == nil {
			return inst.WSURL, nil
		}
	}
	inst, err := s.redis.PickLeastLoadedSFU()
	if err != nil {
		// 无实例注册：单机部署模式
		return "", nil
	}
	if err := s.redis.SetRoomSFUInstance(roomID, inst.ID); err != nil {
		s.logger.Warn("Failed to pin room to SFU instance", zap.Error(err))
	}
	s.logger.Info("Room pinned to SFU instance",
		zap.Uint("roomID", roomID), zap.String("instance", inst.ID), zap.String("ws_url", inst.WSURL))
	return inst.WSURL, nil
}

func (s *roomService) LogAudit(roomID, userID uint, targetID *uint, action, details, ipAddress, userAgent string) error {
	log := &models.AuditLog{
		RoomID:    roomID,
		UserID:    userID,
		TargetID:  targetID,
		Action:    action,
		Details:   details,
		IPAddress: ipAddress,
		UserAgent: userAgent,
		CreatedAt: time.Now(),
	}
	return s.roomRepo.CreateAuditLog(log)
}

func (s *roomService) GetAuditLogs(roomID uint, limit, offset int) ([]*models.AuditLog, error) {
	return s.roomRepo.GetAuditLogs(roomID, limit, offset)
}

func generateRoomID() string {
	// Go 1.20+ 全局 rand 默认自动播种，无需（也不应）调用已废弃的 rand.Seed
	digits := "0123456789"
	result := make([]byte, 6)
	for i := 0; i < 6; i++ {
		result[i] = digits[rand.Intn(len(digits))]
	}
	return string(result)
}

func generateInviteCode() string {
	return uuid.New().String()[:8]
}
