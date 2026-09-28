package repositories

import (
	"strings"

	"fuzhan/internal/utils"
	"time"

	"fuzhan/internal/models"
	"gorm.io/gorm"
)

// ApplyExistingFileFilter 为 operation_records 查询追加"文件仍然存在"过滤：
// 公共文件（upload_type 为 regular 或空）的上传/下载记录，仅当索引表
// file_records_public 中仍存在对应的 active 记录时返回（文件被删除后不再出现
// 于最近上传/下载/热门下载等页面）；search 记录与 temp/private 记录不过滤，
// 分别由各自页面独立管理。
func ApplyExistingFileFilter(q *gorm.DB) *gorm.DB {
	return q.Where(`(
		operation_records.action = 'search'
		OR operation_records.upload_type NOT IN ('regular', '')
		OR EXISTS (
			SELECT 1 FROM file_records_public frp
			WHERE frp.status = 'active'
			  AND frp.deleted_at IS NULL
			  AND (
				-- 优先按文件身份(file_record_id)关联：移动/重命名后 ID 不变
				(operation_records.file_record_id > 0 AND frp.id = operation_records.file_record_id)
				OR (operation_records.file_record_id = 0
					AND frp.root_name = operation_records.root_name
					AND (
						(operation_records.full_path IS NOT NULL AND operation_records.full_path != ''
							AND (frp.full_path = operation_records.full_path
								 OR frp.full_path = ('/' || operation_records.full_path)))
						OR (operation_records.full_path IS NULL OR operation_records.full_path = ''
							AND frp.file_name = operation_records.file_name
							AND TRIM(frp.file_path, '/') = TRIM(operation_records.file_path, '/'))
					))
			  )
		)
	)`)
}

// SessionRepository 会话仓库
type SessionRepository struct {
	db *gorm.DB
}

// NewSessionRepository 创建会话仓库实例
func NewSessionRepository(db *gorm.DB) *SessionRepository {
	return &SessionRepository{db: db}
}

// Create 创建上传会话
func (r *SessionRepository) Create(session *models.UploadSession) error {
	return r.db.Create(session).Error
}

// GetByID 根据ID获取会话
func (r *SessionRepository) GetByID(id uint) (*models.UploadSession, error) {
	var session models.UploadSession
	err := r.db.First(&session, id).Error
	if err != nil {
		return nil, err
	}
	return &session, nil
}

// Update 更新会话
func (r *SessionRepository) Update(session *models.UploadSession) error {
	return r.db.Save(session).Error
}

// UpdateStatus 更新会话状态
func (r *SessionRepository) UpdateStatus(id uint, status models.UploadStatus) error {
	return r.db.Model(&models.UploadSession{}).Where("id = ?", id).Update("status", status).Error
}

// RefreshExpiredAt 刷新会话过期时间（有交互则延期24小时）
func (r *SessionRepository) RefreshExpiredAt(id uint) error {
	newExpiredAt := utils.Now().Add(24 * time.Hour)
	return r.db.Model(&models.UploadSession{}).Where("id = ?", id).Update("expired_at", newExpiredAt).Error
}

// Delete 删除会话
func (r *SessionRepository) Delete(id uint) error {
	return r.db.Delete(&models.UploadSession{}, id).Error
}

// ListByUser 根据用户ID查询上传会话（非临时/非私有，即公共上传）
func (r *SessionRepository) ListByUser(userID string, page, pageSize int) ([]models.UploadSession, int64, error) {
	var sessions []models.UploadSession
	query := r.db.Model(&models.UploadSession{}).
		Where("target_type = ? OR target_type = ?", models.TargetTypeRegular, "").
		Where("user_id = ?", userID)

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	err := query.Order("created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&sessions).Error
	return sessions, total, err
}

// ListByUserAndType 根据用户ID和存储类型查询上传会话
func (r *SessionRepository) ListByUserAndType(userID string, targetType models.TargetType, page, pageSize int) ([]models.UploadSession, int64, error) {
	var sessions []models.UploadSession
	query := r.db.Model(&models.UploadSession{}).
		Where("target_type = ?", targetType).
		Where("user_id = ?", userID)

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	err := query.Order("created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&sessions).Error
	return sessions, total, err
}

// ChunkRepository 分片仓库
type ChunkRepository struct {
	db *gorm.DB
}

// NewChunkRepository 创建分片仓库实例
func NewChunkRepository(db *gorm.DB) *ChunkRepository {
	return &ChunkRepository{db: db}
}

// Create 创建分片记录
func (r *ChunkRepository) Create(chunk *models.UploadedChunk) error {
	return r.db.Create(chunk).Error
}

// GetUploadedIndexes 获取已上传的分片索引
func (r *ChunkRepository) GetUploadedIndexes(sessionID uint) ([]int, error) {
	var chunks []models.UploadedChunk
	err := r.db.Where("session_id = ?", sessionID).Order("chunk_index ASC").Find(&chunks).Error
	if err != nil {
		return nil, err
	}
	indexes := make([]int, len(chunks))
	for i, c := range chunks {
		indexes[i] = c.ChunkIndex
	}
	return indexes, nil
}

// DeleteBySessionID 删除会话的所有分片记录
func (r *ChunkRepository) DeleteBySessionID(sessionID uint) error {
	return r.db.Where("session_id = ?", sessionID).Delete(&models.UploadedChunk{}).Error
}

// RecordRepository 上传记录仓库
type RecordRepository struct {
	db *gorm.DB
}

// NewRecordRepository 创建上传记录仓库实例
func NewRecordRepository(db *gorm.DB) *RecordRepository {
	return &RecordRepository{db: db}
}

// Create 创建上传记录
func (r *RecordRepository) Create(record *models.OperationRecord) error {
	return r.db.Create(record).Error
}

// CreateBatch 批量创建操作记录（单事务分片写入，降低高写流量下的逐条 insert 开销）
func (r *RecordRepository) CreateBatch(records []*models.OperationRecord) error {
	if len(records) == 0 {
		return nil
	}
	return r.db.CreateInBatches(records, 200).Error
}

// ResolvePublicFileID 按路径反查公共文件索引记录 ID（仅 active）。
// fullPath 允许带或不带前导斜杠，两种格式都能命中。
func (r *RecordRepository) ResolvePublicFileID(rootName, fullPath string) (uint, error) {
	if rootName == "" || fullPath == "" {
		return 0, nil
	}
	// 兼容历史记录 full_path 有无前缀两种写法
	var candidates []string
	if strings.HasPrefix(fullPath, "/") {
		candidates = []string{fullPath, strings.TrimPrefix(fullPath, "/")}
	} else {
		candidates = []string{fullPath, "/" + fullPath}
	}
	var ids []uint
	err := r.db.Model(&models.FileRecordPublic{}).
		Where("root_name = ? AND status = ? AND deleted_at IS NULL", rootName, models.FileStatusActive).
		Where("full_path IN ?", candidates).
		Order("id ASC").Limit(1).Pluck("id", &ids).Error
	if err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	return ids[0], nil
}

// UpdateFileRecordID 更新操作记录关联的文件索引记录 ID
func (r *RecordRepository) UpdateFileRecordID(recordID, fileRecordID uint) error {
	return r.db.Model(&models.OperationRecord{}).Where("id = ?", recordID).
		Update("file_record_id", fileRecordID).Error
}

// AttachCurrentPaths 将操作记录中的路径替换为对应文件索引记录的当前路径。
// 文件移动/重命名后 file_record_id 不变，据此可取得最新位置。
func (r *RecordRepository) AttachCurrentPaths(records []models.OperationRecord) []models.OperationRecord {
	if len(records) == 0 {
		return records
	}
	var ids []uint
	for i := range records {
		if records[i].FileRecordID > 0 {
			ids = append(ids, records[i].FileRecordID)
		}
	}
	if len(ids) == 0 {
		return records
	}
	var fres []models.FileRecordPublic
	if err := r.db.Where("id IN ? AND status = ? AND deleted_at IS NULL", ids, models.FileStatusActive).
		Find(&fres).Error; err != nil {
		return records
	}
	byID := make(map[uint]models.FileRecordPublic, len(fres))
	for _, f := range fres {
		byID[f.ID] = f
	}
	for i := range records {
		f, ok := byID[records[i].FileRecordID]
		if !ok {
			continue
		}
		records[i].FileName = f.FileName
		records[i].FilePath = f.FilePath
		records[i].FullPath = f.FullPath
		records[i].RootName = f.RootName
		records[i].Notes = f.Notes
		if f.FileSize > 0 {
			records[i].FileSize = f.FileSize
		}
	}
	return records
}

// GetByID 根据ID获取记录
func (r *RecordRepository) GetByID(id uint) (*models.OperationRecord, error) {
	var record models.OperationRecord
	err := r.db.First(&record, id).Error
	if err != nil {
		return nil, err
	}
	return &record, nil
}

// ListByUserID 获取用户的所有记录
func (r *RecordRepository) ListByUserID(userID string, page, pageSize int) ([]models.OperationRecord, int64, error) {
	var records []models.OperationRecord
	var total int64

	query := r.db.Model(&models.OperationRecord{}).Where("user_id = ?", userID)
	query.Count(&total)

	offset := (page - 1) * pageSize
	err := query.Order("created_at DESC").Offset(offset).Limit(pageSize).Find(&records).Error
	return records, total, err
}

// ListByIP 获取IP的所有记录
func (r *RecordRepository) ListByIP(clientIP string, page, pageSize int) ([]models.OperationRecord, int64, error) {
	var records []models.OperationRecord
	var total int64

	query := r.db.Model(&models.OperationRecord{}).Where("client_ip = ?", clientIP)
	query.Count(&total)

	offset := (page - 1) * pageSize
	err := query.Order("created_at DESC").Offset(offset).Limit(pageSize).Find(&records).Error
	return records, total, err
}

// ListRecent 获取最近的记录
func (r *RecordRepository) ListRecent(clientIP string, limit int) ([]models.OperationRecord, error) {
	var records []models.OperationRecord
	query := r.db.Where("client_ip = ?", clientIP)
	query = ApplyExistingFileFilter(query)
	err := query.Order("created_at DESC").Limit(limit).Find(&records).Error
	records = r.AttachCurrentPaths(records)
	return records, err
}

// ListRecentAll 获取最近的记录（不限IP，用于管理界面）
// action 参数可指定特定操作类型，如 "upload", "download", "search"，为空则返回所有类型
func (r *RecordRepository) ListRecentAll(limit int, action string) ([]models.OperationRecord, error) {
	var records []models.OperationRecord
	query := r.db.Model(&models.OperationRecord{})
	if action != "" {
		query = query.Where("`action` = ?", action)
	}
	query = ApplyExistingFileFilter(query)
	err := query.Order("created_at DESC").Limit(limit).Find(&records).Error
	records = r.AttachCurrentPaths(records)
	return records, err
}

// ListRecentAllPaginated 获取最近的记录（分页版，不限IP）
// 返回记录列表和总记录数
// 去重维度：已建立身份关联的记录（file_record_id > 0）按 file_id 分组，仅保留最新一条，
// 即使移动/重命名后历史快照路径不同也不会重复；未关联的记录（历史数据/临时等）回退到
// 根目录+路径+文件名分组；search 记录按关键词分组。去重在 SQL 层完成，避免分页重复。
func (r *RecordRepository) ListRecentAllPaginated(page, pageSize int, action string) ([]models.OperationRecord, int64, error) {
	offset := (page - 1) * pageSize

	// 分组键：search 记录不关联文件身份，按关键词去重（相同关键词只保留最新一次）；
	// 其他记录（upload/download）已关联的按 file_id，未关联的按路径快照
	dedupGroupKey := `(CASE WHEN file_record_id > 0
		THEN 'f' || CAST(file_record_id AS TEXT)
		ELSE 'p' || COALESCE(root_name,'') || COALESCE(file_path,'') || COALESCE(file_name,'')
		END)`
	if action == "search" {
		dedupGroupKey = `COALESCE(search_query, '')`
	}

	// 总数为去重后的不同文件数（GORM 的 Distinct().Count() 会被降级为 COUNT(*)，
	// 因此用子查询统计 DISTINCT 行数）
	distinctSub := r.db.Table("operation_records").Select(dedupGroupKey)
	if action != "" {
		distinctSub = distinctSub.Where("`action` IN ?", ExpandRecordActions(action))
	}
	distinctSub = ApplyExistingFileFilter(distinctSub)
	var total int64
	if err := r.db.Table("(?) AS d", distinctSub.Distinct()).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// 利用窗口函数在每个文件分组内取最新一条记录
	rowsQuery := r.db.Model(&models.OperationRecord{}).Select(
		"*, ROW_NUMBER() OVER (PARTITION BY " + dedupGroupKey + " ORDER BY created_at DESC, id DESC) AS __rn",
	)
	if action != "" {
		rowsQuery = rowsQuery.Where("`action` IN ?", ExpandRecordActions(action))
	}
	rowsQuery = ApplyExistingFileFilter(rowsQuery)

	var records []models.OperationRecord
	err := r.db.Table("(?) AS t", rowsQuery).
		Where("__rn = 1").
		Order("created_at DESC, id DESC").
		Offset(offset).Limit(pageSize).
		Find(&records).Error
	records = r.AttachCurrentPaths(records)
	return records, total, err
}

// Delete 删除记录
func (r *RecordRepository) Delete(id uint) error {
	return r.db.Delete(&models.OperationRecord{}, id).Error
}

// GetAll 获取所有记录（用于统计）
func (r *RecordRepository) GetAll() ([]models.OperationRecord, error) {
	var records []models.OperationRecord
	err := r.db.Order("created_at DESC").Find(&records).Error
	return records, err
}

// GetRecent 获取最近的记录（不限IP）
func (r *RecordRepository) GetRecent(limit int) ([]models.OperationRecord, error) {
	var records []models.OperationRecord
	query := r.db.Where("`action` IN ?", []string{"upload", "download"})
	query = ApplyExistingFileFilter(query)
	err := query.Order("created_at DESC").Limit(limit).Find(&records).Error
	records = r.AttachCurrentPaths(records)
	return records, err
}

// GetRecentSearches 获取最近的搜索记录
func (r *RecordRepository) GetRecentSearches(limit int) ([]models.OperationRecord, error) {
	var records []models.OperationRecord
	err := r.db.Where("`action` = ? AND search_query IS NOT NULL AND search_query != ''", "search").Order("created_at DESC").Limit(limit).Find(&records).Error
	return records, err
}

// downloadRecordActions 下载类记录的操作类型集合。
// 公开下载（download）与按哈希下载（download-by-hash）在统计口径上同属"下载"，
// 删除/清空/列表都必须按同一集合处理；否则"清空下载记录"会残留哈希下载记录，
// 对应文件的下载次数也就无法回退到 0。
var downloadRecordActions = []string{"download", "download-by-hash"}

// ExpandRecordActions 把界面/接口上的记录类型展开为实际落库的 action 集合。
// 目前仅 "download" 需要展开（含 download-by-hash），其余类型原样返回。
func ExpandRecordActions(action string) []string {
	if action == "download" {
		return downloadRecordActions
	}
	return []string{action}
}

// DeleteByIDAndAction 按 ID 且限定操作类型删除单条操作记录
// 返回删除的记录数（0 表示记录不存在或类型不匹配）
func (r *RecordRepository) DeleteByIDAndAction(id uint, action string) (int64, error) {
	result := r.db.Where("id = ? AND `action` IN ?", id, ExpandRecordActions(action)).
		Delete(&models.OperationRecord{})
	return result.RowsAffected, result.Error
}

// CollectRecountTargets 收集即将被删除的记录所影响的计数重算目标，必须在删除之前调用
// （记录删除后无法再反查）。paths 为公共文件完整路径集合，用于重算 file_records_public.download_count；
// tempIDs 为临时文件 ID 集合，用于重算 temp_files.download_count。
// action 先经 ExpandRecordActions 展开（"download" 含 download-by-hash）；
// id > 0 时只收集该条记录，否则收集该类型命中的全部记录。仅收集未软删除的记录。
// 只有下载类记录会影响计数，其余类型（upload/search/preview）直接返回空。
func (r *RecordRepository) CollectRecountTargets(action string, id uint) (paths []string, tempIDs []uint, err error) {
	if !isDownloadAction(action) {
		return nil, nil, nil
	}

	where := "deleted_at IS NULL AND `action` IN (?)"
	args := []interface{}{ExpandRecordActions(action)}
	if id > 0 {
		where += " AND id = ?"
		args = append(args, id)
	}

	// dl_event=1 即公开下载事件（含 source_type 判定），据此排除临时/私有下载的路径，
	// 避免把 /temp/<code> 之类的路径带进公共文件重算。
	pathArgs := append(append([]interface{}{}, args...), 1)
	if err = r.db.Raw("SELECT DISTINCT full_path FROM operation_records WHERE "+where+" AND dl_event = ?", pathArgs...).
		Scan(&paths).Error; err != nil {
		return nil, nil, err
	}

	tempArgs := append(append([]interface{}{}, args...), models.SourceTypeTemp)
	if err = r.db.Raw("SELECT DISTINCT source_id FROM operation_records WHERE "+where+
		" AND source_type = ? AND source_id > 0", tempArgs...).Scan(&tempIDs).Error; err != nil {
		return nil, nil, err
	}
	return paths, tempIDs, nil
}

// isDownloadAction 判断记录类型是否属于"下载"（download 与 download-by-hash 同属下载）。
func isDownloadAction(action string) bool {
	return action == "download" || action == "download-by-hash"
}

// DeleteByFile 文件/目录删除时级联清理对应的上传/下载操作记录。
// rootName: 根目录名；relPath: 相对路径（以 / 开头）；isDir: 是否为目录；
// recordIDs: 被删除的索引记录 ID（覆盖移动/重命名后保留旧路径快照的记录）。
func (r *RecordRepository) DeleteByFile(rootName, relPath string, isDir bool, recordIDs []uint) (int64, error) {
	q := r.db.Model(&models.OperationRecord{}).
		Where("`action` IN ?", []string{"upload", "download", "download-by-hash"})
	if isDir {
		prefix := "/" + rootName + "/" + strings.Trim(relPath, "/") + "/"
		if len(recordIDs) > 0 {
			q = q.Where(r.db.Where("file_record_id IN ?", recordIDs).
				Or(r.db.Where("file_record_id = 0 AND root_name = ? AND full_path LIKE ?", rootName, prefix)))
		} else {
			q = q.Where("root_name = ? AND full_path LIKE ?", rootName, prefix)
		}
	} else {
		full := "/" + rootName + "/" + strings.TrimPrefix(relPath, "/")
		if len(recordIDs) > 0 {
			q = q.Where(r.db.Where("file_record_id IN ?", recordIDs).
				Or(r.db.Where("file_record_id = 0 AND root_name = ? AND full_path = ?", rootName, full)))
		} else {
			q = q.Where("root_name = ? AND full_path = ?", rootName, full)
		}
	}
	result := q.Delete(&models.OperationRecord{})
	return result.RowsAffected, result.Error
}

// DeleteByAction 删除指定操作类型的所有记录
// action 有效值: "search", "upload", "download", "preview"；先经 ExpandRecordActions 展开
// （"download" 含 download-by-hash），与列表、计数重算保持同一口径。
// 返回删除的记录数
func (r *RecordRepository) DeleteByAction(action string) (int64, error) {
	result := r.db.Where("`action` IN ?", ExpandRecordActions(action)).Delete(&models.OperationRecord{})
	return result.RowsAffected, result.Error
}
