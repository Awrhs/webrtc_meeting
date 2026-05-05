package service

import (
	"conference-sfu/backend/internal/models"
	"conference-sfu/backend/internal/repository"
	"errors"
	"math/rand"
	"strconv"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

type RoomService interface {
	CreateRoom(ownerID uint, req *models.CreateRoomRequest) (*models.Room, error)
	GetRoomByID(id uint) (*models.Room, error)
	GetRoomByRoomID(roomID string) (*models.Room, error)
	GetMyRooms(userID uint) ([]*models.Room, error)
	JoinRoom(userID uint, roomID string, verifyCode string) (*models.Room, []*models.RoomMemberInfo, error)
	LeaveRoom(roomID uint, userID uint) error
	LockRoom(roomID uint, userID uint, locked bool) error
	DeleteRoom(roomID uint, userID uint) error
	KickMember(roomID uint, requesterID uint, targetID uint) error
	MuteMember(roomID uint, requesterID uint, targetID uint, muted bool) error
	SetAdmin(roomID uint, ownerID uint, targetID uint, isAdmin bool) error
	TransferOwner(roomID uint, oldOwnerID uint, newOwnerID uint) error
	GetRoomMembersInfo(roomID uint) ([]*models.RoomMemberInfo, error)
	CheckPermission(roomID uint, userID uint, requiredRole models.Role) (bool, error)
	GetMemberRole(roomID uint, userID uint) (models.Role, error)
	UpdateMediaState(roomID uint, userID uint, videoOn, audioOn *bool) error
}

type roomService struct {
	roomRepo repository.RoomRepository
	userRepo repository.UserRepository
	redis    *repository.RedisClient
}

func NewRoomService(roomRepo repository.RoomRepository, userRepo repository.UserRepository, redis *repository.RedisClient) RoomService {
	return &roomService{roomRepo: roomRepo, userRepo: userRepo, redis: redis}
}

func (s *roomService) CreateRoom(ownerID uint, req *models.CreateRoomRequest) (*models.Room, error) {
	roomID := generateRoomID()
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

	err := s.roomRepo.Create(room)
	if err != nil {
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
	s.roomRepo.AddMember(member)

	s.redis.AddToRoom(room.ID, ownerID)

	// Invalidate user's rooms cache after creating a new room
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

func (s *roomService) JoinRoom(userID uint, roomIDStr string, verifyCode string) (*models.Room, []*models.RoomMemberInfo, error) {
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
		return nil, nil, errors.New("room not found")
	}

	if room.IsLocked {
		return nil, nil, errors.New("room is locked")
	}

	isKicked, err := s.roomRepo.IsKicked(room.ID, userID)
	if err != nil && isKicked {
		return nil, nil, errors.New("you are kicked from this room, please try again later")
	}

	count, err := s.roomRepo.CountMembers(room.ID)
	if err != nil {
		return nil, nil, err
	}
	if int(count) >= room.MaxMembers {
		return nil, nil, errors.New("room is full")
	}

	switch room.AuthType {
	case models.AuthTypePassword:
		if verifyCode == "" {
			return nil, nil, errors.New("password required")
		}
		err := bcrypt.CompareHashAndPassword([]byte(room.Password), []byte(verifyCode))
		if err != nil {
			return nil, nil, errors.New("invalid password")
		}
	case models.AuthTypeInviteCode:
		if verifyCode != room.InviteCode {
			return nil, nil, errors.New("invalid invite code")
		}
	}

	// 先检查用户是否在房间中（未离开）
	activeMember, _ := s.roomRepo.GetMember(room.ID, userID)
	if activeMember == nil {
		// 用户不在房间中，检查是否有历史记录
		existingMember, _ := s.roomRepo.GetAnyMember(room.ID, userID)
		if existingMember == nil {
			// 新成员，创建记录
			member := &models.RoomMember{
				RoomID:    room.ID,
				UserID:    userID,
				Role:      models.RoleMember,
				IsMuted:   false,
				IsVideoOn: false,
				IsAudioOn: true,
				JoinedAt:  time.Now(),
			}
			err = s.roomRepo.AddMember(member)
		} else {
			// 用户曾经加入过，更新记录
			now := time.Now()
			existingMember.LeftAt = nil
			existingMember.JoinedAt = now
			err = s.roomRepo.UpdateMember(existingMember)
		}
	}
	// 如果用户已经在房间中（activeMember != nil），不做任何操作

	if err != nil {
		return nil, nil, err
	}

	// 重新统计当前房间人数，确保计数正确
	newCount, err := s.roomRepo.CountMembers(room.ID)
	if err != nil {
		return nil, nil, err
	}
	room.CurrentSize = int(newCount)
	s.roomRepo.Update(room)

	s.redis.AddToRoom(room.ID, userID)

	// Invalidate room and user rooms cache
	s.redis.DeleteRoom(room.ID)
	s.redis.DeleteUserRooms(userID)

	membersInfo, err := s.GetRoomMembersInfo(room.ID)
	return room, membersInfo, err
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

	room, err := s.roomRepo.FindByID(roomID)
	if err != nil {
		return err
	}

	room.CurrentSize = int(count)
	s.roomRepo.Update(room)

	// Invalidate caches
	s.redis.DeleteRoom(roomID)
	s.redis.DeleteUserRooms(userID)

	if count == 0 && !room.IsPermanent {
		return s.roomRepo.Delete(roomID)
	}

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

	requesterRole, _ := s.GetMemberRole(roomID, requesterID)
	targetRole, _ := s.GetMemberRole(roomID, targetID)

	if targetRole == models.RoleOwner || (targetRole == models.RoleAdmin && requesterRole != models.RoleOwner) {
		return errors.New("permission denied")
	}

	err = s.roomRepo.RemoveMember(roomID, targetID)
	if err != nil {
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
	s.roomRepo.CreateKickBan(kb)

	s.redis.RemoveFromRoom(roomID, targetID)

	count, _ := s.roomRepo.CountMembers(roomID)
	room, _ := s.roomRepo.FindByID(roomID)
	room.CurrentSize = int(count)
	s.roomRepo.Update(room)

	// Invalidate caches after kicking a member
	s.redis.DeleteRoom(roomID)
	s.redis.DeleteUserRooms(targetID)

	return nil
}

func (s *roomService) MuteMember(roomID uint, requesterID uint, targetID uint, muted bool) error {
	hasPerm, err := s.CheckPermission(roomID, requesterID, models.RoleAdmin)
	if err != nil || !hasPerm {
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

	s.roomRepo.UpdateMember(oldOwnerMember)
	s.roomRepo.UpdateMember(newOwnerMember)
	s.roomRepo.Update(room)

	return nil
}

func (s *roomService) GetRoomMembersInfo(roomID uint) ([]*models.RoomMemberInfo, error) {
	members, err := s.roomRepo.GetMembers(roomID)
	if err != nil {
		return nil, err
	}

	result := make([]*models.RoomMemberInfo, len(members))
	for i, member := range members {
		user, err := s.userRepo.FindByID(member.UserID)
		if err != nil {
			continue
		}
		result[i] = &models.RoomMemberInfo{
			ID:        member.ID,
			UserID:    member.UserID,
			Nickname:  user.Nickname,
			Avatar:    user.Avatar,
			Role:      member.Role,
			IsMuted:   member.IsMuted,
			IsVideoOn: member.IsVideoOn,
			IsAudioOn: member.IsAudioOn,
		}
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

func (s *roomService) GetMemberRole(roomID uint, userID uint) (models.Role, error) {
	member, err := s.roomRepo.GetMember(roomID, userID)
	if err != nil {
		return "", err
	}
	return member.Role, nil
}

func (s *roomService) UpdateMediaState(roomID uint, userID uint, videoOn, audioOn *bool) error {
	member, err := s.roomRepo.GetMember(roomID, userID)
	if err != nil {
		return err
	}
	if videoOn != nil {
		member.IsVideoOn = *videoOn
	}
	if audioOn != nil {
		member.IsAudioOn = *audioOn
	}
	return s.roomRepo.UpdateMember(member)
}

func generateRoomID() string {
	rand.Seed(time.Now().UnixNano())
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