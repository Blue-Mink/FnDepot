package handler

import (
	"encoding/json"
	"errors"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/pkg/response"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm/clause"
)

// 当前登录用户的编辑器偏好读写（issue #116-5：换设备 / 换浏览器也记得住）。
//
// 落在 auth 组而不是 configs 组，是因为 /api/configs 整组要求 admin，
// 而脚本页只要 operator —— 详见 model/user_preference.go 顶部那段。

// editorPreferences 与 web/src/utils/editorPreferences.ts 的 EditorPreferences 逐字对应，
// 改一边必须改另一边（键名、取值范围、默认值三样都要对齐）。
//
// ⚠️ minimap / indent_guides 下发时是 JSON 布尔：前端 ensureEditorPreferencesLoaded() 用
// `typeof editor.minimap === 'boolean'` 判定该不该写回本地缓存，下发成字符串它会整项忽略。
// 而 indent_width 下发的是**字符串**（"auto" / "2" / "4" / "6" / "8"），
// 因为它在前端是 'auto' | number 的联合类型，统一走字符串两边都不用做类型分支。
type editorPreferences struct {
	WordWrap     string `json:"word_wrap"`
	Minimap      bool   `json:"minimap"`
	IndentGuides bool   `json:"indent_guides"`
	Whitespace   string `json:"whitespace"`
	IndentWidth  string `json:"indent_width"`
}

// 默认值必须与 web/src/utils/editorPreferences.ts 的 EDITOR_PREFERENCES_DEFAULTS 逐字相同。
// 不一致的表现是：从没存过偏好的用户打开编辑器，本地缓存先按前端默认渲染一次，
// 服务端值拉回来的瞬间观感又跳一下 —— 不报错、构建全绿，只有用户看得见。
// ⚠️ 改这里要同步改 editorPreferences.ts。
const (
	editorDefaultWordWrap     = "on"
	editorDefaultMinimap      = false
	editorDefaultIndentGuides = true
	editorDefaultWhitespace   = "selection"
	editorDefaultIndentWidth  = "auto"
)

// Editor 列的长度兜底。这一列只装 5 个短枚举值，正常撑死一百来字节；
// 4KB 是留给「将来多几个开关」的余量，同时挡住被改坏的客户端往里灌大字符串。
const editorPreferenceMaxBytes = 4 * 1024

func defaultEditorPreferences() editorPreferences {
	return editorPreferences{
		WordWrap:     editorDefaultWordWrap,
		Minimap:      editorDefaultMinimap,
		IndentGuides: editorDefaultIndentGuides,
		Whitespace:   editorDefaultWhitespace,
		IndentWidth:  editorDefaultIndentWidth,
	}
}

// 各枚举项的合法取值，校验与报错文案共用同一份，避免两边写岔。
var (
	editorWordWrapValues    = []string{"on", "off"}
	editorWhitespaceValues  = []string{"none", "selection", "all"}
	editorIndentWidthValues = []string{"auto", "2", "4", "6", "8"}
)

func editorValueAllowed(allowed []string, value string) bool {
	for _, item := range allowed {
		if item == value {
			return true
		}
	}
	return false
}

// editorFlag 是 minimap / indent_guides 的**入参**类型：既吃 JSON 布尔，也吃 "on" / "off" 字符串。
//
// 面板前端提交的是 JSON 布尔：web/src/utils/editorPreferences.ts 的 setEditorPreference 里
// 只有这两个键走 `value as boolean`，web/src/api/auth.ts 的 updatePreferences 签名也是
// Record<string, string | boolean>。所以 *bool 就够面板自己用了。
//
// 额外认 "on" / "off" / "true" / "false" 是给**面板之外**的客户端留的：
// APP、第三方脚本、以及把这两项当字符串开关发上来的历史客户端。
// 它们发字符串时如果只声明 *bool，反序列化会直接 400，而这类客户端对同步失败往往是静默的 ——
// 表现为这两个开关跨端永远不生效，一行报错都看不到。TestUpdateEditorPreferencesAcceptsOnOffFlags
// 覆盖的就是这条兼容路径（不是面板自己的路径）。
// 下发方向不受影响：GET 回去的永远是 JSON 布尔。
type editorFlag bool

// 反序列化阶段的取值错误只能从 ShouldBindJSON 里出来，用哨兵值把它捞出来单独回文案，
// 否则用户只会看到一句「请求参数错误」，不知道是哪一项、也不知道该填什么。
var errEditorFlagValue = errors.New("minimap / indent_guides 取值需为 true、false、on 或 off")

func (f *editorFlag) UnmarshalJSON(data []byte) error {
	var asBool bool
	if err := json.Unmarshal(data, &asBool); err == nil {
		*f = editorFlag(asBool)
		return nil
	}

	var asString string
	if err := json.Unmarshal(data, &asString); err == nil {
		switch asString {
		case "on", "true":
			*f = true
			return nil
		case "off", "false":
			*f = false
			return nil
		}
	}

	return errEditorFlagValue
}

// editorPreferencesPatch 是 PUT 的入参：指针为 nil 表示这一项不改。
// 字段级合并是刻意的 —— 两个标签页各改各的开关时，后提交的那个不能把前一个的改动盖回去
// （前端每次只提交被点的那一个键，正是为了配合这里）。
type editorPreferencesPatch struct {
	WordWrap     *string     `json:"word_wrap"`
	Minimap      *editorFlag `json:"minimap"`
	IndentGuides *editorFlag `json:"indent_guides"`
	Whitespace   *string     `json:"whitespace"`
	IndentWidth  *string     `json:"indent_width"`
}

// currentPreferenceUser 取当前登录用户。偏好是 per-user 的，拿不到用户就没有可读写的对象。
func currentPreferenceUser(c *gin.Context) (*model.User, bool) {
	username, _ := c.Get("username")

	var user model.User
	if err := database.DB.Where("username = ?", username).First(&user).Error; err != nil {
		response.NotFound(c, "用户不存在")
		return nil, false
	}
	return &user, true
}

// loadEditorPreferences 读出某个用户的编辑器偏好。
//
// 第一个返回值永远是「一整套可用的值」：记录不存在、Editor 是空串、JSON 解析失败 ——
// 一律回落默认而**不报错**。这一列纯粹是观感偏好，服务端没有任何运行时逻辑依赖它，
// 让一条脏数据把编辑器页面整个打不开，代价远大于「悄悄回落默认」。
// 解析成功但某一项是枚举外的脏值时，只让那一项回落默认，其余照用。
//
// 第二个返回值 stored 回答的是另一个问题：**这套值到底是用户存的，还是我们现编的默认值**。
// 光看第一个返回值分不出来，因为「从没存过」和「用户主动把 5 项都设成默认值」下发的字节完全一样。
// 这个区分不是可有可无的洁癖，它挡的是一个每个升级用户都会撞上的数据丢失：
// 老用户在 v3.2.2/3.2.3 把偏好存在浏览器本地（dd:editor:*），升级后第一次打开编辑器，
// 前端 ensureEditorPreferencesLoaded() 会拿服务端下发的值逐项写回本地缓存 ——
// 服务端这边其实一行记录都没有、下发的全是默认值，于是本机那份老偏好被默认值**无条件冲掉**，
// 而且是覆盖式写入、刷新也回不来，正好和 editorPreferences.ts 文件头「升级后偏好平滑保留」的承诺相反。
// 有了 stored，前端就能在 stored=false 时反过来把本机那份 PUT 上去（首次上行迁移）。
// ⚠️ 所以它不是冗余字段，删掉就等于把上面那条数据丢失重新放回来。
//
// 判据（与 web 端逐字对齐）：有行 + Editor 非空 + 能解析成 JSON 对象且装得进 editorPreferences。
// 后两条不满足时一律当「没存过」——反正这种行里也读不出任何可用的用户选择，
// 让前端把本机那份重新迁上来，比拿默认值把它盖掉划算得多。
func loadEditorPreferences(userID uint) (editorPreferences, bool) {
	prefs := defaultEditorPreferences()

	var record model.UserPreference
	if err := database.DB.Where("user_id = ?", userID).First(&record).Error; err != nil {
		return prefs, false
	}
	if record.Editor == "" {
		return prefs, false
	}

	// 先确认它确实是个 JSON 对象。不能只看下面那次 Unmarshal 报没报错：
	// Editor 存成 "null" 时，解到结构体上既不报错也不改任何字段，
	// 会被误判成「用户存过一套刚好等于默认值的偏好」。数组、裸字符串、裸数字同理（那些会报错）。
	var object map[string]json.RawMessage
	if err := json.Unmarshal([]byte(record.Editor), &object); err != nil || object == nil {
		return prefs, false
	}

	// 反序列化到「已经填好默认值」的结构体上：JSON 里缺的键保持默认，
	// 这样服务端新增一项开关时，老记录不会因为缺键而拿到 Go 零值（比如 indent_guides 变 false）。
	// 变量叫 decoded 不叫 stored：stored 现在是「存过没有」那个返回值的名字，两者别混。
	decoded := prefs
	if err := json.Unmarshal([]byte(record.Editor), &decoded); err != nil {
		return prefs, false
	}

	if editorValueAllowed(editorWordWrapValues, decoded.WordWrap) {
		prefs.WordWrap = decoded.WordWrap
	}
	if editorValueAllowed(editorWhitespaceValues, decoded.Whitespace) {
		prefs.Whitespace = decoded.Whitespace
	}
	if editorValueAllowed(editorIndentWidthValues, decoded.IndentWidth) {
		prefs.IndentWidth = decoded.IndentWidth
	}
	// 布尔项没有「枚举外的值」可言，解析出来是什么就是什么。
	prefs.Minimap = decoded.Minimap
	prefs.IndentGuides = decoded.IndentGuides

	// 单项枚举外的脏值（比如 whitespace 存成 "boundary"）不影响这里的 stored：
	// 用户确实存过，只是那一项回落默认，其余仍然是他自己的选择。
	return prefs, true
}

func (h *AuthHandler) GetPreferences(c *gin.Context) {
	user, ok := currentPreferenceUser(c)
	if !ok {
		return
	}

	// editor 无论如何都下发一整套可用的值，与加 stored 之前逐字一致 ——
	// 不认得 stored 的老客户端（APP / 历史前端）行为完全不变。
	prefs, stored := loadEditorPreferences(user.ID)
	response.Success(c, gin.H{"editor": prefs, "stored": stored})
}

func (h *AuthHandler) UpdatePreferences(c *gin.Context) {
	var req struct {
		Editor editorPreferencesPatch `json:"editor"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		// minimap / indent_guides 的取值错误在反序列化阶段就被拦下了，回它自己的文案；
		// 其余（JSON 语法坏了、editor 不是对象之类）走通用文案。
		if errors.Is(err, errEditorFlagValue) {
			response.BadRequest(c, errEditorFlagValue.Error())
			return
		}
		response.BadRequest(c, "请求参数错误")
		return
	}

	user, ok := currentPreferenceUser(c)
	if !ok {
		return
	}

	// 先读出当前值再逐项覆盖 —— 这就是「字段级合并」：请求里没带的键保持原值。
	// 这里不关心「之前存过没有」：不管有没有，这次写完就一定是存过了。
	prefs, _ := loadEditorPreferences(user.ID)
	patch := req.Editor

	if patch.WordWrap != nil {
		if !editorValueAllowed(editorWordWrapValues, *patch.WordWrap) {
			response.BadRequest(c, "word_wrap 取值需为 on 或 off")
			return
		}
		prefs.WordWrap = *patch.WordWrap
	}
	if patch.Whitespace != nil {
		if !editorValueAllowed(editorWhitespaceValues, *patch.Whitespace) {
			response.BadRequest(c, "whitespace 取值需为 none、selection 或 all")
			return
		}
		prefs.Whitespace = *patch.Whitespace
	}
	if patch.IndentWidth != nil {
		if !editorValueAllowed(editorIndentWidthValues, *patch.IndentWidth) {
			response.BadRequest(c, "indent_width 取值需为 auto、2、4、6 或 8")
			return
		}
		prefs.IndentWidth = *patch.IndentWidth
	}
	if patch.Minimap != nil {
		prefs.Minimap = bool(*patch.Minimap)
	}
	if patch.IndentGuides != nil {
		prefs.IndentGuides = bool(*patch.IndentGuides)
	}

	encoded, err := json.Marshal(prefs)
	if err != nil {
		response.InternalError(c, "保存编辑器偏好失败")
		return
	}
	if len(encoded) > editorPreferenceMaxBytes {
		response.BadRequest(c, "编辑器偏好数据过大")
		return
	}

	// upsert 而不是「先查后建」：user_id 上有唯一索引，两个标签页同时改开关时
	// 后到的那条 INSERT 会撞唯一约束，DO UPDATE 才能让它正常落库。
	record := model.UserPreference{UserID: user.ID, Editor: string(encoded)}
	if err := database.DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"editor", "updated_at"}),
	}).Create(&record).Error; err != nil {
		response.InternalError(c, "保存编辑器偏好失败")
		return
	}

	// 回下发合并后的完整值，前端可以直接拿它覆盖本地缓存，不用自己再拼一次。
	// stored 恒为 true：走到这一行说明落库成功了，这个用户从此就是「存过偏好」的用户。
	// 写死而不是回查一次，是为了不给一次写操作再加一次 SELECT。
	response.Success(c, gin.H{"editor": prefs, "stored": true})
}
