package models

import (
	"strings"

	"fuzhan/internal/utils"

	"gorm.io/gorm"
)

// authRecordsUniqueIndex 外部身份绑定的唯一索引名（(auth_type, provider_name, external_id)）
const authRecordsUniqueIndex = "idx__auth_records__auth_type_provider_name_external_id"

// MigrateUserIdentity 执行认证归属相关的迁移与回填（启动时幂等执行，不阻断启动）。
//
// 严格按方案 §7.2 顺序执行：
//  1. AutoMigrate 已由调用方完成（补齐 users.auth_source / users.created_via 列）；
//  2. 归一化 auth_records.provider_name（LDAP 固定为 "ldap"）；
//  3. 回填 users.auth_source（有 LDAP 绑定或空密码的历史账号 → ldap，其余 → local）；
//  4. 回填 users.created_via（仅空值 → legacy）；
//  5. auth_records 去重（同一外部身份且同一用户保留 id 最小；跨用户冲突不删除并报告）；
//  6. 无未决冲突时创建绑定唯一索引；
//  7. 一致性审计（仅输出中文日志）。
func MigrateUserIdentity(db *gorm.DB) {
	if db == nil {
		return
	}

	normalizeAuthRecordProviders(db)
	backfillUserAuthSource(db)
	backfillUserCreatedVia(db)

	conflicts := dedupeAuthRecordBindings(db)
	if conflicts > 0 {
		utils.Warn("认证绑定存在跨用户冲突，本次跳过唯一索引创建，请人工裁决后重启",
			utils.String("index", authRecordsUniqueIndex),
			utils.Int("conflict_groups", conflicts))
	} else {
		createIndexIfMissing(db, &AuthRecord{}, authRecordsUniqueIndex, true, "auth_type", "provider_name", "external_id")
	}

	auditUserIdentity(db)
}

// normalizeAuthRecordProviders 把 LDAP 绑定的 provider_name 统一为稳定标识 "ldap"。
func normalizeAuthRecordProviders(db *gorm.DB) {
	res := db.Model(&AuthRecord{}).
		Where("auth_type = ?", AuthSourceLDAP).
		Where("provider_name IS NULL OR provider_name <> ?", AuthSourceLDAP).
		Update("provider_name", AuthSourceLDAP)
	if res.Error != nil {
		utils.Warn("归一化 LDAP 绑定的 provider_name 失败", utils.Err(res.Error))
		return
	}
	if res.RowsAffected > 0 {
		utils.Info("已归一化 LDAP 绑定的 provider_name", utils.Int64("rows", res.RowsAffected))
	}
}

// backfillUserAuthSource 按历史数据推断认证归属（仅首次执行时生效）。
// 一旦库中存在非本地归属（说明回填已完成，或已有 OAuth2 账号），就不再按历史规则覆盖，
// 避免覆盖管理员后续通过绑定/转本地写入的真实归属。
func backfillUserAuthSource(db *gorm.DB) {
	var nonLocal int64
	if err := db.Model(&User{}).
		Where("auth_source IS NOT NULL AND auth_source NOT IN ?", []string{"", AuthSourceLocal}).
		Count(&nonLocal).Error; err != nil {
		utils.Warn("检查认证归属回填状态失败", utils.Err(err))
		return
	}
	if nonLocal > 0 {
		return
	}

	ldapUserIDs := db.Model(&AuthRecord{}).Select("user_id").Where("auth_type = ?", AuthSourceLDAP)
	localOrEmpty := "(auth_source IS NULL OR auth_source = '' OR auth_source = ?)"

	// 1) 存在 LDAP 绑定 → ldap
	res := db.Model(&User{}).
		Where(localOrEmpty, AuthSourceLocal).
		Where("id IN (?)", ldapUserIDs).
		Update("auth_source", AuthSourceLDAP)
	if res.Error != nil {
		utils.Warn("回填 LDAP 用户认证归属失败", utils.Err(res.Error))
	}
	boundCount := res.RowsAffected

	// 2) 无 LDAP 绑定且密码为空 → ldap（历史上 LDAP 建号但绑定写入失败的防御回填）
	res = db.Model(&User{}).
		Where(localOrEmpty, AuthSourceLocal).
		Where("id NOT IN (?)", ldapUserIDs).
		Where("password_hash = ''").
		Update("auth_source", AuthSourceLDAP)
	if res.Error != nil {
		utils.Warn("回填空密码用户认证归属失败", utils.Err(res.Error))
	}
	emptyPasswordCount := res.RowsAffected

	// 3) 其余保持/归为 local
	res = db.Model(&User{}).
		Where("auth_source IS NULL OR auth_source = ''").
		Update("auth_source", AuthSourceLocal)
	if res.Error != nil {
		utils.Warn("回填本地用户认证归属失败", utils.Err(res.Error))
	}

	if boundCount > 0 || emptyPasswordCount > 0 {
		utils.Info("认证归属回填完成",
			utils.Int64("ldap_bound", boundCount),
			utils.Int64("ldap_empty_password", emptyPasswordCount))
	}
}

// backfillUserCreatedVia 仅把空值回填为 legacy，不覆盖后续写入的真实创建渠道。
func backfillUserCreatedVia(db *gorm.DB) {
	res := db.Model(&User{}).
		Where("created_via IS NULL OR created_via = ''").
		Update("created_via", CreatedViaLegacy)
	if res.Error != nil {
		utils.Warn("回填账号创建渠道失败", utils.Err(res.Error))
		return
	}
	if res.RowsAffected > 0 {
		utils.Info("已回填账号创建渠道", utils.Int64("rows", res.RowsAffected))
	}
}

// authRecordDuplicateGroup 重复绑定分组（同一 auth_type/provider/external_id）
type authRecordDuplicateGroup struct {
	AuthType     string
	ProviderName string
	ExternalID   string
	RecordCount  int64
	UserCount    int64
}

// dedupeAuthRecordBindings 清理重复绑定；返回未决的跨用户冲突分组数。
// 同组且同 user_id 的多条保留 id 最小；跨用户冲突不删除，交由管理员裁决。
func dedupeAuthRecordBindings(db *gorm.DB) int {
	var groups []authRecordDuplicateGroup
	if err := db.Model(&AuthRecord{}).
		Select("auth_type, COALESCE(provider_name, '') AS provider_name, external_id, " +
			"COUNT(*) AS record_count, COUNT(DISTINCT user_id) AS user_count").
		Group("auth_type, COALESCE(provider_name, ''), external_id").
		Having("COUNT(*) > 1").
		Find(&groups).Error; err != nil {
		utils.Warn("检查重复认证绑定失败", utils.Err(err))
		// 无法确认是否无冲突，保守跳过唯一索引创建
		return 1
	}

	conflicts := 0
	removed := int64(0)
	for _, g := range groups {
		if g.UserCount > 1 {
			conflicts++
			utils.Warn("认证绑定存在跨用户冲突，保留原记录",
				utils.String("auth_type", g.AuthType),
				utils.String("provider_name", g.ProviderName),
				utils.String("external_id", g.ExternalID),
				utils.Int64("records", g.RecordCount),
				utils.Int64("users", g.UserCount))
			continue
		}

		var ids []uint
		if err := db.Model(&AuthRecord{}).
			Where("auth_type = ? AND COALESCE(provider_name, '') = ? AND external_id = ?",
				g.AuthType, g.ProviderName, g.ExternalID).
			Order("id ASC").
			Pluck("id", &ids).Error; err != nil {
			utils.Warn("查询重复认证绑定失败", utils.Err(err))
			conflicts++
			continue
		}
		if len(ids) <= 1 {
			continue
		}
		if err := db.Delete(&AuthRecord{}, ids[1:]).Error; err != nil {
			utils.Warn("清理重复认证绑定失败", utils.Err(err))
			conflicts++
			continue
		}
		removed += int64(len(ids) - 1)
	}
	if removed > 0 {
		utils.Info("已清理重复认证绑定", utils.Int64("removed", removed))
	}
	return conflicts
}

// auditUserIdentity 一致性审计：只输出违规账号清单（中文日志），不阻断启动。
func auditUserIdentity(db *gorm.DB) {
	checks := []struct {
		problem string
		where   string
	}{
		{"本地账号存在外部绑定", "auth_source = ? AND EXISTS (SELECT 1 FROM auth_records r WHERE r.user_id = users.id)"},
		{"本地账号缺少密码", "auth_source = ? AND password_hash = ''"},
		{"外部账号无绑定记录", "auth_source IN ? AND NOT EXISTS (SELECT 1 FROM auth_records r WHERE r.user_id = users.id)"},
		{"外部账号存在本地密码", "auth_source IN ? AND password_hash <> ''"},
	}

	argsFor := func(where string) []any {
		if strings.Contains(where, "auth_source IN ?") {
			return []any{[]string{AuthSourceLDAP, AuthSourceOAuth2}}
		}
		return []any{AuthSourceLocal}
	}

	for _, c := range checks {
		args := argsFor(c.where)
		var count int64
		if err := db.Model(&User{}).Where(c.where, args...).Count(&count).Error; err != nil {
			utils.Warn("认证归属一致性审计失败", utils.String("problem", c.problem), utils.Err(err))
			continue
		}
		if count == 0 {
			continue
		}
		var names []string
		if err := db.Model(&User{}).Where(c.where, args...).Order("id ASC").Limit(10).Pluck("username", &names).Error; err != nil {
			utils.Warn("认证归属审计读取示例账号失败", utils.String("problem", c.problem), utils.Err(err))
		}
		utils.Warn("认证归属一致性审计发现异常账号",
			utils.String("problem", c.problem),
			utils.Int64("count", count),
			utils.String("examples", strings.Join(names, ",")))
	}
}
