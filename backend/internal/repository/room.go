package repository

import (
	"conference-sfu/backend/internal/models"
	"time"

	"gorm.io/gorm"
)

type RoomRepository interface {
	Create(room *models.Room) error
	FindByRoomID(roomID string) (*models.Room, error)
	FindByID(id uint) (*models.Room, error)
	FindByOwnerID(ownerID uint) ([]*models.Room, error)
	FindRoomsByUserID(userID uint) ([]*models.Room, error)
	Update(room *models.Room) error
	Delete(id uint) error
	AddMember(member *models.RoomMember) error
	UpdateMember(member *models.RoomMember) error
	GetMembers(roomID uint) ([]*models.RoomMember, error)
	GetMember(roomID uint, userID uint) (*models.RoomMember, error)
	GetAnyMember(roomID uint, userID uint) (*models.RoomMember, error)
	RemoveMember(roomID uint, userID uint) error
	CountMembers(roomID uint) (int64, error)
	CreateKickBan(kb *models.KickBan) error
	IsKicked(roomID uint, userID uint) (bool, error)
	RemoveExpiredKicks() error
	CreateAuditLog(log *models.AuditLog) error
	GetAuditLogs(roomID uint, limit, offset int) ([]*models.AuditLog, error)
}

type roomRepository struct {
	db *gorm.DB
}

func NewRoomRepository(db *gorm.DB) RoomRepository {
	return &roomRepository{db: db}
}

func (r *roomRepository) Create(room *models.Room) error {
	return r.db.Create(room).Error
}

func (r *roomRepository) FindByRoomID(roomID string) (*models.Room, error) {
	var room models.Room
	err := r.db.Where("room_id = ?", roomID).First(&room).Error
	if err != nil {
		return nil, err
	}
	return &room, nil
}

func (r *roomRepository) FindByID(id uint) (*models.Room, error) {
	var room models.Room
	err := r.db.Where("id = ?", id).First(&room).Error
	if err != nil {
		return nil, err
	}
	return &room, nil
}

func (r *roomRepository) FindByOwnerID(ownerID uint) ([]*models.Room, error) {
	var rooms []*models.Room
	err := r.db.Where("owner_id = ? AND deleted_at IS NULL", ownerID).Find(&rooms).Error
	return rooms, err
}

func (r *roomRepository) FindRoomsByUserID(userID uint) ([]*models.Room, error) {
	var rooms []*models.Room
	err := r.db.Joins("JOIN room_members ON room_members.room_id = rooms.id").
		Where("room_members.user_id = ? AND rooms.deleted_at IS NULL", userID).
		Find(&rooms).Error
	return rooms, err
}

func (r *roomRepository) Update(room *models.Room) error {
	return r.db.Save(room).Error
}

func (r *roomRepository) Delete(id uint) error {
	return r.db.Delete(&models.Room{}, id).Error
}

func (r *roomRepository) AddMember(member *models.RoomMember) error {
	return r.db.Create(member).Error
}

func (r *roomRepository) UpdateMember(member *models.RoomMember) error {
	return r.db.Save(member).Error
}

func (r *roomRepository) GetMembers(roomID uint) ([]*models.RoomMember, error) {
	var members []*models.RoomMember
	err := r.db.Where("room_id = ? AND left_at IS NULL", roomID).Find(&members).Error
	return members, err
}

func (r *roomRepository) GetMember(roomID uint, userID uint) (*models.RoomMember, error) {
	var member models.RoomMember
	err := r.db.Where("room_id = ? AND user_id = ? AND left_at IS NULL", roomID, userID).First(&member).Error
	if err != nil {
		return nil, err
	}
	return &member, nil
}

func (r *roomRepository) GetAnyMember(roomID uint, userID uint) (*models.RoomMember, error) {
	var member models.RoomMember
	err := r.db.Where("room_id = ? AND user_id = ?", roomID, userID).Order("id DESC").First(&member).Error
	if err != nil {
		return nil, err
	}
	return &member, nil
}

func (r *roomRepository) RemoveMember(roomID uint, userID uint) error {
	now := time.Now()
	return r.db.Model(&models.RoomMember{}).
		Where("room_id = ? AND user_id = ? AND left_at IS NULL", roomID, userID).
		Update("left_at", &now).Error
}

func (r *roomRepository) CountMembers(roomID uint) (int64, error) {
	var count int64
	err := r.db.Model(&models.RoomMember{}).Where("room_id = ? AND left_at IS NULL", roomID).Count(&count).Error
	return count, err
}

func (r *roomRepository) CreateKickBan(kb *models.KickBan) error {
	return r.db.Create(kb).Error
}

func (r *roomRepository) IsKicked(roomID uint, userID uint) (bool, error) {
	var count int64
	err := r.db.Model(&models.KickBan{}).Where("room_id = ? AND user_id = ? AND expires_at > NOW()", roomID, userID).Count(&count).Error
	return count > 0, err
}

func (r *roomRepository) RemoveExpiredKicks() error {
	return r.db.Where("expires_at <= NOW()").Delete(&models.KickBan{}).Error
}

func (r *roomRepository) CreateAuditLog(log *models.AuditLog) error {
	return r.db.Create(log).Error
}

func (r *roomRepository) GetAuditLogs(roomID uint, limit, offset int) ([]*models.AuditLog, error) {
	var logs []*models.AuditLog
	err := r.db.Where("room_id = ?", roomID).
		Order("created_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&logs).Error
	return logs, err
}