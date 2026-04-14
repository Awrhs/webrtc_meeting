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