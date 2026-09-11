package handler

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"daidai-panel/database"
	"daidai-panel/middleware"
	"daidai-panel/model"
	"daidai-panel/pkg/response"
	"daidai-panel/service"

	"github.com/gin-gonic/gin"
)

type NotificationHandler struct{}

func NewNotificationHandler() *NotificationHandler {
	return &NotificationHandler{}
}

func updateNotificationTestState(channelID uint, status string) {
	if channelID == 0 {
		return
	}

	if err := database.DB.Model(&model.NotifyChannel{}).
		Where("id = ?", channelID).
		Updates(map[string]interface{}{
			"last_test_at":     time.Now(),
			"last_test_status": status,
		}).Error; err != nil {
		log.Printf("update notification test state failed: %v", err)
	}
}

func (h *NotificationHandler) List(c *gin.Context) {
	var channels []model.NotifyChannel
	database.DB.Order("created_at DESC").Find(&channels)

	data := make([]map[string]interface{}, len(channels))
	for i, ch := range channels {
		data[i] = ch.ToDict()
	}

	response.Success(c, gin.H{"data": data})
}

func (h *NotificationHandler) Create(c *gin.Context) {
	var req struct {
		Name      string `json:"name" binding:"required"`
		Type      string `json:"type" binding:"required"`
		Config    string `json:"config"`
		PushScope string `json:"push_scope"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请求参数错误")
		return
	}

	// config 的值必须全是字符串，否则 service.sendToChannel 反序列化直接失败，
	// 该渠道所有通知（含测试按钮）都会挂掉。详见 model.NormalizeNotifyChannelConfig。
	normalizedConfig, err := model.NormalizeNotifyChannelConfig(req.Config)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	// 不带该字段（例如独立发版的 APP）时 req.PushScope 是空串，归一后即「默认推送」，
	// 与升级前的行为完全一致。拼错的取值一律 400，不做「就近纠正」：
	// 把 "bind" 悄悄当成 default 落库，等于把用户的隔离意图反着执行。
	pushScope, ok := model.NormalizeNotifyPushScope(req.PushScope)
	if !ok {
		response.BadRequest(c, "推送范围只能是 default（默认推送）或 bound（绑定推送）")
		return
	}

	ch := model.NotifyChannel{
		Name:      req.Name,
		Type:      req.Type,
		Config:    normalizedConfig,
		PushScope: pushScope,
		Enabled:   true,
	}

	// notify_channels.name 是唯一索引，连点创建按钮的第二发会撞在这里。
	// 照 security.go 的 AddIPWhitelist 写法翻译成友好 400，不把 GORM 原文透给前端。
	if err := database.DB.Create(&ch).Error; err != nil {
		response.BadRequest(c, "同名通知渠道已存在")
		return
	}

	response.Created(c, gin.H{"message": "创建成功", "data": ch.ToDict()})
}

func (h *NotificationHandler) Update(c *gin.Context) {
	chID, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	var ch model.NotifyChannel
	if err := database.DB.First(&ch, chID).Error; err != nil {
		response.NotFound(c, "通知渠道不存在")
		return
	}

	var req map[string]interface{}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请求参数错误")
		return
	}

	// 这里是「按键更新」：请求里没出现的键一概不动已有值。
	// push_scope 必须遵守这条 —— 独立发版的 Flutter APP 编辑渠道时根本不会带这个字段，
	// 一旦改成「缺省即 default」，用户在 Web 上设的「绑定推送」会被 APP 的一次保存悄悄清掉。
	//
	// 另外注意：漏把新键登记进 allowed 的表现极其难查 —— 前端切换后保存提示「更新成功」，
	// 刷新又变回原值，全程不报错、无日志。加字段时务必同步这张白名单。
	allowed := map[string]bool{"name": true, "type": true, "config": true, "push_scope": true}
	updates := make(map[string]interface{})
	for k, v := range req {
		if !allowed[k] {
			continue
		}
		if k == "push_scope" {
			// 显式的 null 一律当成「没提供这个字段」，跳过、不改已有值。
			// 独立发版的 APP 很可能把未填字段序列化成 null，若按类型错误返回 400，
			// 它一升级就会全线保存失败，代价远大于这里放行的收益。
			// 其余非字符串类型（数字、布尔、对象）仍然 400，拼错的字符串值也仍然 400，校验能力不丢。
			if v == nil {
				continue
			}
			raw, ok := v.(string)
			if !ok {
				response.BadRequest(c, "推送范围必须是字符串")
				return
			}
			scope, valid := model.NormalizeNotifyPushScope(raw)
			if !valid {
				response.BadRequest(c, "推送范围只能是 default（默认推送）或 bound（绑定推送）")
				return
			}
			updates[k] = scope
			continue
		}
		if k == "config" {
			// 与 Create 走同一套归一：老客户端写坏的记录（例如 smtp_ssl 被写成 JSON 布尔）
			// 只要用户在任意端点一次「编辑 + 保存」就会自动被修好，不需要单独做数据迁移。
			rawConfig, ok := v.(string)
			if !ok {
				response.BadRequest(c, "通知渠道配置必须是 JSON 字符串")
				return
			}
			normalizedConfig, err := model.NormalizeNotifyChannelConfig(rawConfig)
			if err != nil {
				response.BadRequest(c, err.Error())
				return
			}
			updates[k] = normalizedConfig
			continue
		}
		updates[k] = v
	}

	if len(updates) > 0 {
		// name 加了唯一索引之后，改名撞上别的渠道会在这里报错。
		// 不接 .Error 的话前端会拿到「更新成功」、刷新又变回原值，属于最难查的一类问题。
		if err := database.DB.Model(&ch).Updates(updates).Error; err != nil {
			response.BadRequest(c, "同名通知渠道已存在")
			return
		}
	}

	database.DB.First(&ch, chID)
	response.Success(c, gin.H{"message": "更新成功", "data": ch.ToDict()})
}

func (h *NotificationHandler) Delete(c *gin.Context) {
	chID, _ := strconv.ParseUint(c.Param("id"), 10, 32)
	database.DB.Where("id = ?", chID).Delete(&model.NotifyChannel{})
	response.Success(c, gin.H{"message": "删除成功"})
}

func (h *NotificationHandler) Enable(c *gin.Context) {
	chID, _ := strconv.ParseUint(c.Param("id"), 10, 32)
	var ch model.NotifyChannel
	if err := database.DB.First(&ch, chID).Error; err != nil {
		response.NotFound(c, "通知渠道不存在")
		return
	}
	database.DB.Model(&ch).Update("enabled", true)
	ch.Enabled = true
	response.Success(c, gin.H{"message": "已启用", "data": ch.ToDict()})
}

func (h *NotificationHandler) Disable(c *gin.Context) {
	chID, _ := strconv.ParseUint(c.Param("id"), 10, 32)
	var ch model.NotifyChannel
	if err := database.DB.First(&ch, chID).Error; err != nil {
		response.NotFound(c, "通知渠道不存在")
		return
	}
	database.DB.Model(&ch).Update("enabled", false)
	ch.Enabled = false
	response.Success(c, gin.H{"message": "已禁用", "data": ch.ToDict()})
}

func (h *NotificationHandler) Test(c *gin.Context) {
	chID, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	var ch model.NotifyChannel
	if err := database.DB.First(&ch, chID).Error; err != nil {
		response.NotFound(c, "通知渠道不存在")
		return
	}

	err := service.SendNotificationToChannel(&ch, "呆呆面板测试通知", "这是一条测试通知消息，如果您收到此消息，说明通知渠道配置正确。")
	if err != nil {
		updateNotificationTestState(ch.ID, "failed")
		response.BadRequest(c, "发送失败: "+err.Error())
		return
	}

	updateNotificationTestState(ch.ID, "success")
	response.Success(c, gin.H{"message": "测试通知发送成功"})
}

func (h *NotificationHandler) Send(c *gin.Context) {
	var req struct {
		Title      string                 `json:"title" binding:"required"`
		Content    string                 `json:"content" binding:"required"`
		ChannelID  *uint                  `json:"channel_id"`
		ChannelIDs []uint                 `json:"channel_ids"`
		Context    map[string]interface{} `json:"context"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请求参数错误")
		return
	}

	// 调用方点名了渠道、但归一化后一个有效 ID 都不剩时直接 400，不再退化成广播。
	//
	// 老行为的问题：service.uniqueNotificationChannelIDs 会丢掉 id == 0，而这里只对单值
	// channel_id 做了 > 0 校验，channel_ids 数组原样透传。于是脚本传 {"channel_ids":[0]}
	// 会静默变成广播，响应里 used_all 仍是 false、requested_ids 仍是 [0] —— 接口在说谎。
	// 引入「绑定推送」后这条路径更危险：本想只发一个专用渠道，结果所有默认推送渠道都收到了。
	// 合法调用不会传 0，这里是纯收紧。
	//
	// 空数组（"channel_ids": []）不算「点名」，仍走广播：它字面上就没指定任何渠道，
	// 而且这是升级前的既有行为，不该让老客户端突然收到 400。
	// 面板自带的 notify.py / sendNotify.js 也只在非空时才写入这个键。
	targeted := req.ChannelID != nil || len(req.ChannelIDs) > 0

	channelIDs := make([]uint, 0, len(req.ChannelIDs)+1)
	if req.ChannelID != nil && *req.ChannelID > 0 {
		channelIDs = append(channelIDs, *req.ChannelID)
	}
	for _, id := range req.ChannelIDs {
		if id > 0 {
			channelIDs = append(channelIDs, id)
		}
	}
	if targeted && len(channelIDs) == 0 {
		response.BadRequest(c, "通知渠道 ID 无效：channel_id / channel_ids 必须是大于 0 的渠道 ID")
		return
	}

	context := make(map[string]string, len(req.Context))
	for key, value := range req.Context {
		context[key] = fmt.Sprint(value)
	}

	result, err := service.SendNotificationSyncWithOptions(req.Title, req.Content, service.NotificationDispatchOptions{
		ChannelIDs: channelIDs,
		Context:    context,
	})
	if err != nil {
		response.BadRequest(c, "发送失败: "+err.Error())
		return
	}

	if result.SentCount == 0 && result.FailedCount > 0 {
		response.BadRequest(c, "发送失败: "+strings.Join(result.Errors, "; "))
		return
	}

	message := fmt.Sprintf("通知发送完成，成功 %d 个渠道", result.SentCount)
	if result.FailedCount > 0 {
		message = fmt.Sprintf("%s，失败 %d 个渠道", message, result.FailedCount)
	}

	// used_all 的语义随「默认推送 / 绑定推送」一起变了：从「发给全部已启用渠道」变成
	// 「走广播，即发给全部已启用且 push_scope=default 的渠道」。设成「绑定推送」的渠道
	// 不会出现在广播里，只有被显式点名（channel_id / channel_ids）时才会收到。
	response.Success(c, gin.H{
		"message": message,
		"data": gin.H{
			"sent_count":     result.SentCount,
			"failed_count":   result.FailedCount,
			"channel_names":  result.ChannelNames,
			"errors":         result.Errors,
			"requested_ids":  channelIDs,
			"used_all":       len(channelIDs) == 0,
			"content_length": len([]rune(req.Content)),
		},
	})
}

// Types 下发全部通知渠道及其字段定义。
//
// 这里以前是一份硬编码的 {type,name} 列表，与 model 里的字段注册表是两处分开维护的，
// 加渠道时漏改一处就会出现「类型下拉里有，但打开没有任何输入框」。现在统一从注册表取，
// 结构上不可能再分叉。
//
// 响应保持纯可加：老客户端只读 type / name 两个键，多出来的 icon / fields 对它们无感；
// 老面板不返回 fields 时，新客户端判断 fields 为空即回落本地冻结快照。
func (h *NotificationHandler) Types(c *gin.Context) {
	response.Success(c, gin.H{"data": model.NotifyChannelDefinitions()})
}

func (h *NotificationHandler) RegisterRoutes(r *gin.RouterGroup) {
	notifySend := r.Group("/notifications", middleware.JWTAuth(), middleware.OpenAPIAccess("notifications"))
	{
		notifySend.POST("/send", middleware.RequireRole("operator"), h.Send)
	}

	notify := r.Group("/notifications", middleware.JWTAuth(), middleware.RequireUserToken(), middleware.RequireAdmin())
	{
		notify.GET("", h.List)
		notify.POST("", h.Create)
		notify.PUT("/:id", h.Update)
		notify.DELETE("/:id", h.Delete)
		notify.PUT("/:id/enable", h.Enable)
		notify.PUT("/:id/disable", h.Disable)
		notify.POST("/:id/test", h.Test)
		notify.GET("/types", h.Types)
	}
}
