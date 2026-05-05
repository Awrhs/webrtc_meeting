package service

import (
	"conference-sfu/backend/internal/models"
	"conference-sfu/backend/internal/repository"
)

type ChatService interface {
	SendMessage(senderID uint, roomID uint, receiverID *uint, msgType models.MessageType, content string) (*models.Message, error)
	GetMessages(roomID uint, limit, offset int) ([]*models.MessageResponse, error)
	GetPrivateMessages(roomID uint, userID, otherID uint, limit, offset int) ([]*models.MessageResponse, error)
	SavePublicKey(userID uint, publicKey, keyType string) error
	GetPublicKey(userID uint) (*models.E2EEGetPublicKeyResponse, error)
	DeletePublicKey(userID uint) error
}

type chatService struct {
	repo  repository.ChatRepository
	roomRepo repository.RoomRepository
}

func NewChatService(repo repository.ChatRepository, roomRepo repository.RoomRepository) ChatService {
	return &chatService{repo: repo, roomRepo: roomRepo}
}

func (s *chatService) SendMessage(senderID uint, roomID uint, receiverID *uint, msgType models.MessageType, content string) (*models.Message, error) {
	message := &models.Message{
		RoomID:     roomID,
		SenderID:   senderID,
		ReceiverID: receiverID,
		Type:       msgType,
		Content:    content,
		Encrypted:  receiverID != nil,
	}

	err := s.repo.CreateMessage(message)
	return message, err
}

func (s *chatService) GetMessages(roomID uint, limit, offset int) ([]*models.MessageResponse, error) {
	messages, err := s.repo.GetMessages(roomID, limit, offset)
	if err != nil {
		return nil, err
	}

	return s.convertToResponse(messages), nil
}

func (s *chatService) GetPrivateMessages(roomID uint, userID, otherID uint, limit, offset int) ([]*models.MessageResponse, error) {
	messages, err := s.repo.GetPrivateMessages(roomID, userID, otherID, limit, offset)
	if err != nil {
		return nil, err
	}

	return s.convertToResponse(messages), nil
}

func (s *chatService) convertToResponse(messages []*models.Message) []*models.MessageResponse {
	result := make([]*models.MessageResponse, len(messages))
	for i, msg := range messages {
		result[i] = &models.MessageResponse{
			ID:         msg.ID,
			RoomID:     msg.RoomID,
			SenderID:   msg.SenderID,
			ReceiverID: msg.ReceiverID,
			Type:       msg.Type,
			Content:    msg.Content,
			Encrypted:  msg.Encrypted,
			CreatedAt:  msg.CreatedAt,
		}
	}
	return result
}

func (s *chatService) SavePublicKey(userID uint, publicKey, keyType string) error {
	if keyType == "" {
		keyType = "ecdh_p256"
	}
	pubKey := &models.E2EEPublicKey{
		UserID:    userID,
		PublicKey: publicKey,
		KeyType:   keyType,
	}
	return s.repo.SavePublicKey(pubKey)
}

func (s *chatService) GetPublicKey(userID uint) (*models.E2EEGetPublicKeyResponse, error) {
	pubKey, err := s.repo.GetPublicKey(userID)
	if err != nil {
		return &models.E2EEGetPublicKeyResponse{
			UserID: userID,
			Exists: false,
		}, nil
	}
	return &models.E2EEGetPublicKeyResponse{
		UserID:    pubKey.UserID,
		PublicKey: pubKey.PublicKey,
		KeyType:   pubKey.KeyType,
		Exists:    true,
	}, nil
}

func (s *chatService) DeletePublicKey(userID uint) error {
	return s.repo.DeletePublicKey(userID)
}