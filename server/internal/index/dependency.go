package index

import (
    "errors"
    "fmt"
    "net/url"

    "gorm.io/gorm"
    "fuzhan/internal/models"
    "fuzhan/internal/utils"
)

// ErrFileRecordNotFound 文件记录不存在
var ErrFileRecordNotFound = errors.New("文件记录不存在")

// DependencyService 文件依赖服务
type DependencyService struct {
    db *gorm.DB
}

// NewDependencyService 创建依赖服务
func NewDependencyService(db *gorm.DB) *DependencyService {
    return &DependencyService{db: db}
}

// CreateDependency 创建依赖关系
func (s *DependencyService) CreateDependency(fileRecordID, dependsOnID uint, relation, description string) error {
    if fileRecordID == dependsOnID {
        return fmt.Errorf("文件不能依赖于自身")
    }

    // 检查源文件和目标文件是否存在且非目录
    for _, id := range []uint{fileRecordID, dependsOnID} {
        var record models.FileRecordPublic
        if err := s.db.First(&record, id).Error; err != nil {
            return fmt.Errorf("文件记录不存在: %d", id)
        }
        if record.IsDir {
            return fmt.Errorf("目录不能设为依赖: %s", record.FileName)
        }
    }

    // 检查关系类型
    validRelations := map[string]bool{"requires": true, "referenced_by": true, "related": true}
    if !validRelations[relation] {
        return fmt.Errorf("无效的关系类型: %s（有效值: requires, referenced_by, related）", relation)
    }

    // 检查是否已存在相同依赖
    var existing int64
    s.db.Model(&models.FileDependency{}).
        Where("file_record_id = ? AND depends_on_id = ? AND relation = ?",
            fileRecordID, dependsOnID, relation).
        Count(&existing)
    if existing > 0 {
        return fmt.Errorf("该依赖关系已存在")
    }

    // 检查依赖链深度（防止循环依赖）
    if err := s.checkDepth(fileRecordID, dependsOnID, 0); err != nil {
        return err
    }

    dep := models.FileDependency{
        FileRecordID:  fileRecordID,
        DependsOnID:   dependsOnID,
        Relation:      relation,
        Description:   description,
    }
    return s.db.Create(&dep).Error
}

// ListUpstreamDependencies 查询上游依赖（该文件依赖哪些文件）
func (s *DependencyService) ListUpstreamDependencies(fileRecordID uint) ([]models.FileDependency, error) {
    var deps []models.FileDependency
    if err := s.db.Where("file_record_id = ?", fileRecordID).
        Preload("TargetFile").
        Find(&deps).Error; err != nil {
        return nil, fmt.Errorf("查询上游依赖失败: %w", err)
    }
    if deps == nil {
        deps = make([]models.FileDependency, 0)
    }
    return deps, nil
}

// ListDownstreamDependencies 查询下游依赖（哪些文件依赖该文件）
func (s *DependencyService) ListDownstreamDependencies(fileRecordID uint) ([]models.FileDependency, error) {
    var deps []models.FileDependency
    if err := s.db.Where("depends_on_id = ?", fileRecordID).
        Preload("SourceFile").
        Find(&deps).Error; err != nil {
        return nil, fmt.Errorf("查询下游依赖失败: %w", err)
    }
    if deps == nil {
        deps = make([]models.FileDependency, 0)
    }
    return deps, nil
}

// ListDependenciesByRecord 查询某文件的所有依赖（上+下游）
func (s *DependencyService) ListDependenciesByRecord(fileRecordID uint) (upstream, downstream []models.FileDependency, err error) {
    upstream, err = s.ListUpstreamDependencies(fileRecordID)
    if err != nil {
        return
    }
    downstream, err = s.ListDownstreamDependencies(fileRecordID)
    return
}

// DeleteDependency 删除依赖关系
func (s *DependencyService) DeleteDependency(depID uint) error {
    result := s.db.Delete(&models.FileDependency{}, depID)
    if result.Error != nil {
        return fmt.Errorf("删除依赖关系失败: %w", result.Error)
    }
    if result.RowsAffected == 0 {
        return fmt.Errorf("依赖关系不存在: %d", depID)
    }
    return nil
}

// ListDependencies 分页查询所有依赖关系
type DependencyListQuery struct {
    Page     int `form:"page"`
    PageSize int `form:"pageSize"`
}

func (q *DependencyListQuery) Normalize() {
    if q.Page <= 0 {
        q.Page = DefaultPage
    }
    if q.PageSize <= 0 || q.PageSize > MaxPageSize {
        q.PageSize = DefaultPageSize
    }
}

func (s *DependencyService) ListDependencies(query DependencyListQuery) ([]models.FileDependency, int64, error) {
    query.Normalize()

    var total int64
    if err := s.db.Model(&models.FileDependency{}).Count(&total).Error; err != nil {
        return nil, 0, fmt.Errorf("查询总数失败: %w", err)
    }

    offset := (query.Page - 1) * query.PageSize
    var deps []models.FileDependency
    if err := s.db.Order("id DESC").
        Offset(offset).Limit(query.PageSize).
        Find(&deps).Error; err != nil {
        return nil, 0, fmt.Errorf("查询依赖关系失败: %w", err)
    }

    if deps == nil {
        deps = make([]models.FileDependency, 0)
    }
    return deps, total, nil
}

// GetDependencyTree 获取文件依赖树
// 上游递归查询（最多10层），下游查询一层
// 检测循环依赖时返回错误
func (s *DependencyService) GetDependencyTree(fileRecordID uint) (*DependencyTreeNode, error) {
    // 查询源文件记录
    var src models.FileRecordPublic
    if err := s.db.First(&src, fileRecordID).Error; err != nil {
        return nil, ErrFileRecordNotFound
    }

    root := &DependencyTreeNode{
		ID:          src.ID,
		FileName:    src.FileName,
		FilePath:    src.FilePath,
		FullPath:    src.FullPath,
		RootName:    src.RootName,
		FileSize:    src.FileSize,
		DownloadURL: fmt.Sprintf("/api/v1/download/%s/%s", src.RootName, url.PathEscape(src.FilePath)),
	}

    // 递归构建上游依赖树
    visited := make(map[uint]bool)
    children, err := s.buildUpstreamTree(fileRecordID, visited, 0)
    if err != nil {
        return nil, err
    }
    root.Children = children

    // 查询下游依赖（一层）
    downstream, err := s.buildDownstream(fileRecordID)
    if err != nil {
        return nil, err
    }
    root.Downstream = downstream

    return root, nil
}

// buildUpstreamTree 递归构建上游依赖树
func (s *DependencyService) buildUpstreamTree(fileRecordID uint, visited map[uint]bool, depth int) ([]*DependencyTreeNode, error) {
    if depth >= 10 {
        return nil, fmt.Errorf("依赖链过深（超过10层），可能存在循环依赖")
    }

    if visited[fileRecordID] {
        return nil, fmt.Errorf("检测到循环依赖: 文件 %d 已在当前路径中出现过", fileRecordID)
    }
    visited[fileRecordID] = true
    defer func() { delete(visited, fileRecordID) }()

    var deps []models.FileDependency
    if err := s.db.Where("file_record_id = ?", fileRecordID).
        Preload("TargetFile").
        Find(&deps).Error; err != nil {
        return nil, fmt.Errorf("查询上游依赖失败: %w", err)
    }

    var nodes []*DependencyTreeNode
    for _, dep := range deps {
        if dep.TargetFile == nil {
            utils.Warn("上游依赖关联文件记录缺失",
                utils.String("relation", dep.Relation),
                utils.String("target", fmt.Sprintf("depends_on_id=%d", dep.DependsOnID)))
            continue
        }
        node := &DependencyTreeNode{
			ID:           dep.DependsOnID,
			DependencyID: dep.ID,
			FileName:     dep.TargetFile.FileName,
			FilePath:     dep.TargetFile.FilePath,
			FullPath:     dep.TargetFile.FullPath,
			RootName:     dep.TargetFile.RootName,
			FileSize:     dep.TargetFile.FileSize,
			IsDir:        dep.TargetFile.IsDir,
			Relation:     dep.Relation,
			DownloadURL:  fmt.Sprintf("/api/v1/download/%s/%s", dep.TargetFile.RootName, url.PathEscape(dep.TargetFile.FilePath)),
		}

        // 递归查询上游的上游
        children, err := s.buildUpstreamTree(dep.DependsOnID, visited, depth+1)
        if err != nil {
            // 深度超限或循环依赖时，返回空 children 继续，不阻塞整体
            utils.Warn("上游依赖递归受限，子树截断",
                utils.String("file", dep.TargetFile.FileName),
                utils.Err(err))
            node.Children = make([]*DependencyTreeNode, 0)
        } else {
            node.Children = children
        }

        nodes = append(nodes, node)
    }

    if nodes == nil {
        nodes = make([]*DependencyTreeNode, 0)
    }
    return nodes, nil
}

// buildDownstream 查询一层下游依赖
func (s *DependencyService) buildDownstream(fileRecordID uint) ([]*DependencyTreeNode, error) {
    var deps []models.FileDependency
    if err := s.db.Where("depends_on_id = ?", fileRecordID).
        Preload("SourceFile").
        Find(&deps).Error; err != nil {
        return nil, fmt.Errorf("查询下游依赖失败: %w", err)
    }

    var nodes []*DependencyTreeNode
    for _, dep := range deps {
        if dep.SourceFile == nil {
            utils.Warn("下游依赖关联文件记录缺失",
                utils.String("relation", dep.Relation),
                utils.String("source", fmt.Sprintf("file_record_id=%d", dep.FileRecordID)))
            continue
        }
        node := &DependencyTreeNode{
			ID:           dep.FileRecordID,
			DependencyID: dep.ID,
			FileName:     dep.SourceFile.FileName,
			FilePath:     dep.SourceFile.FilePath,
			FullPath:     dep.SourceFile.FullPath,
			RootName:     dep.SourceFile.RootName,
			FileSize:     dep.SourceFile.FileSize,
			IsDir:        dep.SourceFile.IsDir,
			Relation:     dep.Relation,
			DownloadURL:  fmt.Sprintf("/api/v1/download/%s/%s", dep.SourceFile.RootName, url.PathEscape(dep.SourceFile.FilePath)),
			Children:     make([]*DependencyTreeNode, 0),
		}
        nodes = append(nodes, node)
    }

    if nodes == nil {
        nodes = make([]*DependencyTreeNode, 0)
    }
    return nodes, nil
}

// checkDepth 检查依赖链深度
func (s *DependencyService) checkDepth(sourceID, targetID uint, depth int) error {
    if depth >= 10 {
        return fmt.Errorf("依赖链过深（超过10层），可能存在循环依赖")
    }

    // 检查 targetID 是否依赖了 sourceID（构成循环）
    // 或者间接依赖链过长
    var deps []models.FileDependency
    if err := s.db.Where("file_record_id = ?", targetID).Find(&deps).Error; err != nil {
        return fmt.Errorf("检查依赖链失败: %w", err)
    }

    for _, dep := range deps {
        if dep.DependsOnID == sourceID {
            return fmt.Errorf("检测到循环依赖: %d <-> %d", sourceID, targetID)
        }
        // 递归检查
        if err := s.checkDepth(sourceID, dep.DependsOnID, depth+1); err != nil {
            return err
        }
    }
    return nil
}
