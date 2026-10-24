package service

import (
	"errors"
	"testing"

	"conference-sfu/backend/internal/models"
	"conference-sfu/backend/internal/repository"
)

var errMemberNotFound = errors.New("member not found")

// muteStubRepo 仅实现 MuteMember 测试需要的 RoomRepository 方法
type muteStubRepo struct {
	repository.RoomRepository
	members map[uint]*models.RoomMember
}

func (r *muteStubRepo) GetMember(roomID, userID uint) (*models.RoomMember, error) {
	if m, ok := r.members[userID]; ok {
		return m, nil
	}
	return nil, errMemberNotFound
}

func (r *muteStubRepo) UpdateMember(m *models.RoomMember) error {
	r.members[m.UserID] = m
	return nil
}

func newMuteTestService() *roomService {
	repo := &muteStubRepo{members: map[uint]*models.RoomMember{
		1: {RoomID: 1, UserID: 1, Role: models.RoleOwner},
		2: {RoomID: 1, UserID: 2, Role: models.RoleAdmin},
		3: {RoomID: 1, UserID: 3, Role: models.RoleMember},
	}}
	return &roomService{roomRepo: repo}
}

func TestMuteMember_Hierarchy(t *testing.T) {
	s := newMuteTestService()

	// 管理员不能禁言房主
	if err := s.MuteMember(1, 2, 1, true); err == nil {
		t.Fatal("admin muting owner should be denied")
	}
	// 房主可以禁言普通成员
	if err := s.MuteMember(1, 1, 3, true); err != nil {
		t.Fatalf("owner muting member: %v", err)
	}
	if !s.roomRepo.(*muteStubRepo).members[3].IsMuted {
		t.Fatal("member should be muted")
	}
	// 不能禁言自己
	if err := s.MuteMember(1, 1, 1, true); err == nil {
		t.Fatal("self mute should be denied")
	}
}
