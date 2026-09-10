package services

import "fuzhan/internal/models"

// ListByUser 根据用户ID和存储类型查询上传会话
func (s *UploadSessionService) ListByUser(userID string, targetType models.TargetType, page, pageSize int) ([]models.UploadSession, int64, error) {
	var sessions []models.UploadSession
	query := s.db.Model(&models.UploadSession{}).
		Where("target_type = ?", targetType).
		Where("user_id = ?", userID)

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	err := query.Order("created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&sessions).Error
	return sessions, total, err
}

// ListByIPAndType 根据客户端IP和存储类型查询上传会话（临时上传以IP为身份，无 UserID）
func (s *UploadSessionService) ListByIPAndType(clientIP string, targetType models.TargetType, page, pageSize int) ([]models.UploadSession, int64, error) {
	var sessions []models.UploadSession
	query := s.db.Model(&models.UploadSession{}).
		Where("target_type = ?", targetType).
		Where("client_ip = ?", clientIP)

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	err := query.Order("created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&sessions).Error
	return sessions, total, err
}
