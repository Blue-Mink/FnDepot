package model

import "time"

// UserPreference 是「跟人走」的界面偏好，一个用户一行（issue #116-5）。
//
// 为什么不复用 system_configs：
//   - system_configs 是**全局**键值表，A 改了 B 跟着变；而编辑器开关是纯个人习惯，
//     两个人同时用面板时必须各存各的。
//   - /api/configs 整组挂着 JWTAuth() + RequireAdmin()，而脚本页背后的 /scripts 只要
//     RequireRole("operator")。operator 打得开编辑器、看得见那些开关，却读不了也写不了
//     system_configs —— 复用它等于让非管理员的偏好永远存不下来。
//
// 形状抄 TwoFactorAuth（本仓唯一的「per-user 单行配置表」先例）：user_id 唯一索引 + 单行 upsert。
//
// 为什么整组偏好塞进一列 JSON 而不是一列一个字段：
// 这些值服务端没有任何运行时逻辑依赖，从头到尾只是「存进去、原样发回前端」，
// 一列一个字段的唯一收益（能被 SQL 过滤/排序）在这里完全用不上，
// 代价却是每加一个开关都要改表结构 + 补 EnsureColumns。
// 将来要加别的偏好组（比如主题、列表密度）就再加一列 JSON，不要往 Editor 里混。
type UserPreference struct {
	ID     uint `gorm:"primarykey" json:"id"`
	UserID uint `gorm:"uniqueIndex;not null" json:"user_id"`
	// 编辑器偏好 JSON，形状见 server/handler/user_preference.go 的 editorPreferences。
	// 允许为空串：表示这个用户从没改过任何开关，读的时候整体回落默认值。
	Editor    string    `gorm:"type:text;not null;default:''" json:"editor"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (UserPreference) TableName() string {
	return "user_preferences"
}
