package repository

import (
	"conference-sfu/backend/internal/models"

	"gorm.io/gorm"
)

type ChatRepository interface {
	CreateMessage(message *models.Message) error
	GetMessages(roomID uint, limit, offset int) ([]*models.Message, error)
	GetPrivateMessages(roomID uint, senderID, receiverID uint, limit, offset int) ([]*models.Message, error)
	DeleteMessage(id uint) error
	SavePublicKey(pubKey *models.E2EEPublicKey) error
	GetPublicKey(userID uint) (*models.E2EEPublicKey, error)
	DeletePublicKey(userID uint) error
}

type chatRepository struct {
	db *gorm.DB
}

func NewChatRepository(db *gorm.DB) ChatRepository {
	return &chatRepository{db: db}
}

func (r *chatRepository) CreateMessage(message *models.Message) error {
	return r.db.Create(message).Error
}

func (r *chatRepository) GetMessages(roomID uint, limit, offset int) ([]*models.Message, error) {
	var messages []*models.Message
	err := r.db.Where("room_id = ? AND receiver_id IS NULL", roomID).
		Order("created_at desc").
		Limit(limit).
		Offset(offset).
		Find(&messages).Error
	reverseMessages(messages)
	return messages, err
}

func (r *chatRepository) GetPrivateMessages(roomID uint, senderID, receiverID uint, limit, offset int) ([]*models.Message, error) {
	var messages []*models.Message
	err := r.db.Where(
		"room_id = ? AND ((sender_id = ? AND receiver_id = ?) OR (sender_id = ? AND receiver_id = ?))",
		roomID, senderID, receiverID, receiverID, senderID,
	).Order("created_at desc").
		Limit(limit).
		Offset(offset).
		Find(&messages).Error
	reverseMessages(messages)
	return messages, err
}

func (r *chatRepository) DeleteMessage(id uint) error {
	return r.db.Delete(&models.Message{}, id).Error
}

func reverseMessages(messages []*models.Message) {
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}
}

func (r *chatRepository) SavePublicKey(pubKey *models.E2EEPublicKey) error {
	// 使用 upsert：如果用户已有公钥则更新，否则创建
	var existing models.E2EEPublicKey
	err := r.db.Where("user_id = ?", pubKey.UserID).First(&existing).Error
	if err == nil {
		// 已存在，更新
		existing.PublicKey = pubKey.PublicKey
		existing.KeyType = pubKey.KeyType
		return r.db.Save(&existing).Error
	}
	// 不存在，创建新的
	return r.db.Create(pubKey).Error
}

func (r *chatRepository) GetPublicKey(userID uint) (*models.E2EEPublicKey, error) {
	var pubKey models.E2EEPublicKey
	err := r.db.Where("user_id = ?", userID).First(&pubKey).Error
	if err != nil {
		return nil, err
	}
	return &pubKey, nil
}

func (r *chatRepository) DeletePublicKey(userID uint) error {
	return r.db.Where("user_id = ?", userID).Delete(&models.E2EEPublicKey{}).Error
}