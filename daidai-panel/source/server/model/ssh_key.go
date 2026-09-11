package model

import (
	"time"
)

type SSHKey struct {
	ID uint `gorm:"primarykey" json:"id"`
	// 密钥名在语义上唯一：订阅选密钥时只认名字，同名两条用户无从分辨选中的是哪一把。
	// 老库里的同名残留由 database.DeduplicateBeforeUniqueIndex() 在 AutoMigrate 之前改名让路。
	Name       string    `gorm:"size:128;uniqueIndex;not null" json:"name"`
	PrivateKey string    `gorm:"type:text;not null" json:"-"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

func (SSHKey) TableName() string {
	return "ssh_keys"
}

func (k *SSHKey) ToDict() map[string]interface{} {
	return map[string]interface{}{
		"id":         k.ID,
		"name":       k.Name,
		"created_at": k.CreatedAt,
		"updated_at": k.UpdatedAt,
	}
}

func (k *SSHKey) ToDictWithKey() map[string]interface{} {
	result := k.ToDict()
	result["private_key"] = k.PrivateKey
	return result
}
