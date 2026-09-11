package handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"

	"github.com/gin-gonic/gin"
)

// issue #116-5：编辑器偏好要跟着人走（换设备 / 换浏览器也记得住），
// 所以它落在 per-user 的 user_preferences 表，而不是全局的 system_configs。

// testutil.SetupTestEnv 自己维护了一份 AutoMigrate 清单（没有走 appboot.allModels），
// user_preferences 现在**已经在**那份清单里（见 testutil/testenv.go），
// 所以这里不再补建表 —— 早先那次 AutoMigrate 已经是纯冗余。
// 函数留着不内联：偏好用例全走这一个入口，将来这张表再掉出清单只用改这一处。
func setupEditorPrefsEnv(t *testing.T) {
	t.Helper()

	testutil.SetupTestEnv(t)
}

func editorPrefsRequest(t *testing.T, engine *gin.Engine, method, token, body string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(method, "/api/v1/auth/preferences", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

// editorPrefsDecode 把响应体解成 map，这样除了值还能顺带断言 JSON 类型：
// 前端 ensureEditorPreferencesLoaded() 用 `typeof x === 'boolean'` 判定 minimap /
// indent_guides 要不要写回本地缓存，下发成字符串它会整项忽略、且不报任何错。
func editorPrefsDecode(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var payload struct {
		Editor map[string]any `json:"editor"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode preferences body %q: %v", rec.Body.String(), err)
	}
	if payload.Editor == nil {
		t.Fatalf("response has no editor object: %s", rec.Body.String())
	}
	return payload.Editor
}

// editorPrefsDecodeStored 取响应体顶层的 stored —— 「这套值是用户存过的，还是服务端现编的默认值」。
// 没有它，前端分不出「从没存过」和「用户主动把 5 项都设成默认值」，
// 于是升级用户存在浏览器本地（dd:editor:*）的老偏好会被默认值无条件冲掉、刷新也回不来。
// 字段缺失直接失败：漏下发和下发 false 对前端是两回事（缺失会被当成 undefined，
// 走不进「stored !== true 就把本机那份迁上去」那条分支）。
func editorPrefsDecodeStored(t *testing.T, rec *httptest.ResponseRecorder) bool {
	t.Helper()

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode preferences body %q: %v", rec.Body.String(), err)
	}
	raw, exists := payload["stored"]
	if !exists {
		t.Fatalf("response has no stored field: %s", rec.Body.String())
	}
	stored, ok := raw.(bool)
	if !ok {
		t.Fatalf("stored must be a JSON boolean, got %T: %s", raw, rec.Body.String())
	}
	return stored
}

// editorPrefsAssertDefaults 钉死那 5 个默认值。
// 它们必须与 web/src/utils/editorPreferences.ts 的 EDITOR_PREFERENCES_DEFAULTS 逐字相同，
// 否则「从没存过偏好的用户」打开编辑器时观感会跳一下（本地缓存先按前端默认渲染，
// 服务端值拉回来又改一次）。
func editorPrefsAssertDefaults(t *testing.T, editor map[string]any) {
	t.Helper()

	if editor["word_wrap"] != "on" {
		t.Fatalf("word_wrap default should be on, got %#v", editor["word_wrap"])
	}
	if editor["minimap"] != false {
		t.Fatalf("minimap default should be false, got %#v", editor["minimap"])
	}
	if editor["indent_guides"] != true {
		t.Fatalf("indent_guides default should be true, got %#v", editor["indent_guides"])
	}
	if editor["whitespace"] != "selection" {
		t.Fatalf("whitespace default should be selection, got %#v", editor["whitespace"])
	}
	if editor["indent_width"] != "auto" {
		t.Fatalf("indent_width default should be auto, got %#v", editor["indent_width"])
	}
}

func TestGetEditorPreferencesReturnsDefaultsForFreshUser(t *testing.T) {
	setupEditorPrefsEnv(t)

	user := testutil.MustCreateUser(t, "prefs-fresh", "operator")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)
	engine := newProtectedRouter()

	rec := editorPrefsRequest(t, engine, http.MethodGet, token, "")
	editor := editorPrefsDecode(t, rec)
	editorPrefsAssertDefaults(t, editor)

	// 这套默认值是服务端现编的，不是用户存的 —— stored 必须是 false，
	// 否则前端会拿它去覆盖本机（dd:editor:*）那份老偏好。
	if editorPrefsDecodeStored(t, rec) {
		t.Fatalf("从没存过偏好的用户 stored 必须为 false: %s", rec.Body.String())
	}

	// 类型也要对：minimap / indent_guides 是 JSON 布尔，indent_width 是字符串。
	if _, ok := editor["minimap"].(bool); !ok {
		t.Fatalf("minimap must be a JSON boolean, got %T", editor["minimap"])
	}
	if _, ok := editor["indent_width"].(string); !ok {
		t.Fatalf("indent_width must be a JSON string, got %T", editor["indent_width"])
	}
}

// 只提交一个键时，其余键必须保持原值 —— 前端每次只 PUT 被点的那一个开关，
// 两个标签页各改各的，谁也不能把对方的改动盖回去。
func TestUpdateEditorPreferencesMergesPerField(t *testing.T) {
	setupEditorPrefsEnv(t)

	user := testutil.MustCreateUser(t, "prefs-merge", "operator")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)
	engine := newProtectedRouter()

	// 第一次：只改 word_wrap 与 indent_width。
	editorPrefsDecode(t, editorPrefsRequest(t, engine, http.MethodPut, token, `{"editor":{"word_wrap":"off"}}`))
	editorPrefsDecode(t, editorPrefsRequest(t, engine, http.MethodPut, token, `{"editor":{"indent_width":"4"}}`))

	// 第二次：只改 whitespace，前两项不能被带回默认。
	mergedRec := editorPrefsRequest(t, engine, http.MethodPut, token, `{"editor":{"whitespace":"all"}}`)
	merged := editorPrefsDecode(t, mergedRec)
	// PUT 的响应恒为 stored=true：写完必然是存过了。
	if !editorPrefsDecodeStored(t, mergedRec) {
		t.Fatalf("PUT 响应的 stored 必须为 true: %s", mergedRec.Body.String())
	}
	if merged["word_wrap"] != "off" || merged["indent_width"] != "4" || merged["whitespace"] != "all" {
		t.Fatalf("PUT response should contain merged values, got %#v", merged)
	}
	// 没碰过的两项保持默认。
	if merged["minimap"] != false || merged["indent_guides"] != true {
		t.Fatalf("untouched fields should keep defaults, got %#v", merged)
	}

	// GET 回来仍然是合并后的完整值。
	reloadedRec := editorPrefsRequest(t, engine, http.MethodGet, token, "")
	reloaded := editorPrefsDecode(t, reloadedRec)
	// 存过之后 GET 的 stored 必须翻成 true，前端这时才允许拿服务端值写回本地。
	if !editorPrefsDecodeStored(t, reloadedRec) {
		t.Fatalf("PUT 之后 GET 的 stored 必须为 true: %s", reloadedRec.Body.String())
	}
	if reloaded["word_wrap"] != "off" || reloaded["indent_width"] != "4" || reloaded["whitespace"] != "all" {
		t.Fatalf("GET should return merged values, got %#v", reloaded)
	}
	if reloaded["minimap"] != false || reloaded["indent_guides"] != true {
		t.Fatalf("GET untouched fields should keep defaults, got %#v", reloaded)
	}
}

// 这条用例覆盖的是**面板之外**的客户端：APP / 第三方脚本 / 把这两项当字符串开关发的历史客户端。
// 面板前端自己发的是 JSON 布尔（web/src/utils/editorPreferences.ts 的 setEditorPreference 里
// minimap / indent_guides 走 `value as boolean`，web/src/api/auth.ts 的 updatePreferences
// 签名也是 Record<string, string | boolean>），所以它并不依赖 "on"/"off" 这条路径。
// 但这类客户端对同步失败往往是静默的：只认布尔的话它们每次点开关都会 400，
// 表现为这两项跨端永远不生效、一行报错都看不到。
// 下发方向两边一致：GET 回去的永远是 JSON 布尔。
func TestUpdateEditorPreferencesAcceptsOnOffFlags(t *testing.T) {
	setupEditorPrefsEnv(t)

	user := testutil.MustCreateUser(t, "prefs-flags", "operator")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)
	engine := newProtectedRouter()

	editor := editorPrefsDecode(t, editorPrefsRequest(t, engine, http.MethodPut, token,
		`{"editor":{"minimap":"on","indent_guides":"off"}}`))
	if editor["minimap"] != true || editor["indent_guides"] != false {
		t.Fatalf("on/off strings should map to booleans, got %#v", editor)
	}

	// JSON 布尔同样要认（APP / 第三方脚本更可能直接发布尔）。
	editor = editorPrefsDecode(t, editorPrefsRequest(t, engine, http.MethodPut, token,
		`{"editor":{"minimap":false,"indent_guides":true}}`))
	if editor["minimap"] != false || editor["indent_guides"] != true {
		t.Fatalf("JSON booleans should be accepted, got %#v", editor)
	}
}

func TestUpdateEditorPreferencesRejectsInvalidValues(t *testing.T) {
	setupEditorPrefsEnv(t)

	user := testutil.MustCreateUser(t, "prefs-invalid", "operator")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)
	engine := newProtectedRouter()

	cases := map[string]string{
		// "boundary" 是 CodeMirror 的档位名，不在我们这三档里。
		"whitespace":   `{"editor":{"whitespace":"boundary"}}`,
		"indent_width": `{"editor":{"indent_width":"3"}}`,
		"word_wrap":    `{"editor":{"word_wrap":"wrap"}}`,
		"minimap":      `{"editor":{"minimap":"maybe"}}`,
	}
	for name, body := range cases {
		rec := editorPrefsRequest(t, engine, http.MethodPut, token, body)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: expected 400, got %d body=%s", name, rec.Code, rec.Body.String())
		}
	}

	// 非法请求一个都不能落库，GET 仍然是默认值。
	editorPrefsAssertDefaults(t, editorPrefsDecode(t, editorPrefsRequest(t, engine, http.MethodGet, token, "")))
}

// 走 per-user 表的全部理由：A 改了不该让 B 跟着变。
func TestEditorPreferencesAreIsolatedPerUser(t *testing.T) {
	setupEditorPrefsEnv(t)

	alice := testutil.MustCreateUser(t, "prefs-alice", "admin")
	bob := testutil.MustCreateUser(t, "prefs-bob", "operator")
	aliceToken := testutil.MustCreateAccessToken(t, alice.Username, alice.Role)
	bobToken := testutil.MustCreateAccessToken(t, bob.Username, bob.Role)
	engine := newProtectedRouter()

	editorPrefsDecode(t, editorPrefsRequest(t, engine, http.MethodPut, aliceToken,
		`{"editor":{"word_wrap":"off","whitespace":"all","indent_width":"8","minimap":"on"}}`))

	// Bob 完全没被影响，仍然是默认值。
	editorPrefsAssertDefaults(t, editorPrefsDecode(t, editorPrefsRequest(t, engine, http.MethodGet, bobToken, "")))

	// Bob 自己改一项，也不能反过来污染 Alice。
	editorPrefsDecode(t, editorPrefsRequest(t, engine, http.MethodPut, bobToken, `{"editor":{"indent_width":"2"}}`))

	aliceEditor := editorPrefsDecode(t, editorPrefsRequest(t, engine, http.MethodGet, aliceToken, ""))
	if aliceEditor["indent_width"] != "8" || aliceEditor["whitespace"] != "all" || aliceEditor["minimap"] != true {
		t.Fatalf("alice preferences were affected by bob: %#v", aliceEditor)
	}

	bobEditor := editorPrefsDecode(t, editorPrefsRequest(t, engine, http.MethodGet, bobToken, ""))
	if bobEditor["indent_width"] != "2" || bobEditor["whitespace"] != "selection" {
		t.Fatalf("bob preferences are wrong: %#v", bobEditor)
	}

	// 每人一行，不是共用一行被反复覆盖。
	var count int64
	if err := database.DB.Model(&model.UserPreference{}).Count(&count).Error; err != nil {
		t.Fatalf("count user preferences: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected one row per user, got %d", count)
	}
}

// 脏数据不该让用户打不开编辑器：解析不出来就整体回落默认，绝不 500。
func TestGetEditorPreferencesFallsBackOnCorruptedRow(t *testing.T) {
	setupEditorPrefsEnv(t)

	user := testutil.MustCreateUser(t, "prefs-corrupt", "operator")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)
	engine := newProtectedRouter()

	// wantStored 是这一轮的重点：读不出任何可用选择的行一律当「没存过」（false），
	// 好让前端把本机那份重新迁上来，而不是拿默认值把它盖掉。
	cases := []struct {
		editor     string
		wantStored bool
	}{
		{editor: `{"word_wrap":`, wantStored: false},                 // 截断的 JSON
		{editor: `not json at all`, wantStored: false},               // 压根不是 JSON
		{editor: ``, wantStored: false},                              // 空串（从没改过任何开关）
		{editor: `null`, wantStored: false},                          // 合法 JSON、但不是对象；解到结构体上不报错也不改字段
		{editor: `[1,2]`, wantStored: false},                         // 合法 JSON、是数组不是对象
		{editor: `{"word_wrap":123,"minimap":1}`, wantStored: false}, // 是对象、但类型全错，一项也读不出来
		// 合法 JSON 对象、只有单项是枚举外的脏值：用户确实存过，
		// 那一项回落默认、其余照用，所以 stored 仍然是 true。
		{editor: `{"whitespace":"boundary"}`, wantStored: true},
	}
	for _, tc := range cases {
		if err := database.DB.Where("user_id = ?", user.ID).Delete(&model.UserPreference{}).Error; err != nil {
			t.Fatalf("reset preference row: %v", err)
		}
		if err := database.DB.Create(&model.UserPreference{UserID: user.ID, Editor: tc.editor}).Error; err != nil {
			t.Fatalf("seed dirty preference %q: %v", tc.editor, err)
		}

		rec := editorPrefsRequest(t, engine, http.MethodGet, token, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("dirty row %q should still return 200, got %d body=%s", tc.editor, rec.Code, rec.Body.String())
		}
		editorPrefsAssertDefaults(t, editorPrefsDecode(t, rec))
		if got := editorPrefsDecodeStored(t, rec); got != tc.wantStored {
			t.Fatalf("dirty row %q: stored want %v got %v, body=%s", tc.editor, tc.wantStored, got, rec.Body.String())
		}
	}
}

// stored 的完整生命周期：没有行 → 空串 → 脏 JSON 一路都是 false，
// 存过一次之后翻成 true 并且一直是 true。
//
// 这条用例钉的是 issue #116 的续集：v3.2.2/3.2.3 的用户把编辑器偏好存在浏览器本地
// （dd:editor:*），升级后第一次打开编辑器时，前端会拿服务端下发的值逐项写回本地缓存。
// 服务端这边一行记录都没有、下发的全是默认值，本机那份老偏好就被无条件冲掉、刷新也回不来。
// stored=false 是前端唯一能识破「这是默认值不是你的选择」的信号，
// 它在那种情况下会反过来把本机那 5 项 PUT 上去（首次上行迁移）。
func TestEditorPreferencesStoredFlagTracksPersistence(t *testing.T) {
	setupEditorPrefsEnv(t)

	user := testutil.MustCreateUser(t, "prefs-stored", "operator")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)
	engine := newProtectedRouter()

	// 1) 一行都没有。
	if editorPrefsDecodeStored(t, editorPrefsRequest(t, engine, http.MethodGet, token, "")) {
		t.Fatalf("没有 user_preferences 行时 stored 必须为 false")
	}

	// 2) 有行、但 Editor 是空串（比如别的字段先把行建出来了）。
	if err := database.DB.Create(&model.UserPreference{UserID: user.ID, Editor: ""}).Error; err != nil {
		t.Fatalf("seed empty preference row: %v", err)
	}
	if editorPrefsDecodeStored(t, editorPrefsRequest(t, engine, http.MethodGet, token, "")) {
		t.Fatalf("Editor 为空串时 stored 必须为 false")
	}

	// 3) 有行、Editor 是解不开的脏 JSON。
	if err := database.DB.Model(&model.UserPreference{}).
		Where("user_id = ?", user.ID).
		Update("editor", `{"word_wrap":`).Error; err != nil {
		t.Fatalf("seed corrupted preference row: %v", err)
	}
	if editorPrefsDecodeStored(t, editorPrefsRequest(t, engine, http.MethodGet, token, "")) {
		t.Fatalf("Editor 是脏 JSON 时 stored 必须为 false")
	}

	// 4) 存一次 —— 哪怕存的值恰好等于默认值，也必须能和「没存过」区分开。
	putRec := editorPrefsRequest(t, engine, http.MethodPut, token, `{"editor":{"word_wrap":"on"}}`)
	if !editorPrefsDecodeStored(t, putRec) {
		t.Fatalf("PUT 响应的 stored 必须为 true: %s", putRec.Body.String())
	}

	getRec := editorPrefsRequest(t, engine, http.MethodGet, token, "")
	if !editorPrefsDecodeStored(t, getRec) {
		t.Fatalf("存过之后 GET 的 stored 必须为 true: %s", getRec.Body.String())
	}
	// 值本身仍然是完整的一套（stored 只是多带的标记，不改变 editor 的下发口径）。
	editorPrefsAssertDefaults(t, editorPrefsDecode(t, getRec))
}
