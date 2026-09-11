package handler_test

import (
	"net/http"
	"strconv"
	"testing"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

func lockTestTaskPath(id uint) string {
	return "/api/v1/tasks/" + strconv.FormatUint(uint64(id), 10)
}

// 订阅锁的写入点回归：用户手改任务名或定时时，服务端自己推导 subscription_locked，
// 前端不能直接传这个字段（它不在 Update 的 allowedFields 里）。

// 订阅归属标签，由订阅同步写入（service.subscriptionTaskLabel），也是加锁与否的唯一判定依据。
const subscriptionOwnedLabel = "subscription:1"

// labels 用变参：不传 = 用户手动建的任务（不归任何订阅），传了 = 由订阅管理。
func mustCreateLockTestTask(t *testing.T, name, cron string, labels ...string) *model.Task {
	t.Helper()

	task := model.Task{
		Name:           name,
		Command:        "task demo/" + name + ".js",
		CronExpression: cron,
		TaskType:       model.TaskTypeCron,
		Status:         model.TaskStatusEnabled,
	}
	task.SetLabelsFromSlice(labels)
	if err := database.DB.Select("*").Create(&task).Error; err != nil {
		t.Fatalf("create task: %v", err)
	}
	return &task
}

func reloadLockTestTask(t *testing.T, id uint) *model.Task {
	t.Helper()

	var task model.Task
	if err := database.DB.First(&task, id).Error; err != nil {
		t.Fatalf("reload task: %v", err)
	}
	return &task
}

// 订阅任务（labels 带 subscription:N）：用户手改名称/定时 → 加锁，原有行为不能回归。
func TestUpdateTaskMarksSubscriptionLockedOnManualChange(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "task-lock-operator", "operator")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)
	auth := map[string]string{"Authorization": "Bearer " + token}

	cases := []struct {
		name     string
		cron     string
		body     string
		wantLock bool
	}{
		// 改定时 → 加锁
		{"改 cron", "9 5 * * *", `{"cron_expression":"30 7 * * *"}`, true},
		// 改任务名 → 同一把锁
		{"改任务名", "9 5 * * *", `{"name":"我自己改的名字"}`, true},
		// 改成手动执行：handler 会强制把 cron 置空，同样算用户改动，必须加锁，
		// 否则订阅每次拉取都在偷偷回灌订阅源的 cron
		{"改成手动执行", "9 5 * * *", `{"task_type":"manual"}`, true},
		// 只改无关字段 → 不加锁，任务继续跟随订阅
		{"只改超时", "9 5 * * *", `{"timeout":60}`, false},
		// 传了 cron 但和原值一样 → 不算改动
		{"cron 原样回传", "9 5 * * *", `{"cron_expression":"9 5 * * *"}`, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			task := mustCreateLockTestTask(t, "lock-"+tc.name, tc.cron, subscriptionOwnedLabel)
			rec := performJSONRequest(engine, http.MethodPut,
				lockTestTaskPath(task.ID), tc.body, auth, "")
			if rec.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
			}

			after := reloadLockTestTask(t, task.ID)
			if after.SubscriptionLocked != tc.wantLock {
				t.Fatalf("subscription_locked want %v got %v", tc.wantLock, after.SubscriptionLocked)
			}

			payload := decodeJSONMap(t, rec)
			data, ok := payload["data"].(map[string]interface{})
			if !ok {
				t.Fatalf("expected task data object, got %#v", payload["data"])
			}
			if data["subscription_locked"] != tc.wantLock {
				t.Fatalf("响应体 subscription_locked want %v got %#v", tc.wantLock, data["subscription_locked"])
			}
		})
	}
}

// 用户手动建的任务（labels 里没有 subscription:）无论怎么改都不该加锁：
// 没有任何订阅会来覆盖它，这把锁不产生行为，只会在界面上显示一个误导用户的「已锁定」。
func TestUpdateTaskKeepsManualTaskUnlocked(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "task-unlock-operator", "operator")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)
	auth := map[string]string{"Authorization": "Bearer " + token}

	cases := []struct {
		name string
		body string
	}{
		{"改 cron", `{"cron_expression":"30 7 * * *"}`},
		{"改任务名", `{"name":"我自己改的名字"}`},
		{"改成手动执行", `{"task_type":"manual"}`},
		// 带普通标签也一样：只有 subscription: 前缀的内部标签才代表订阅归属
		{"带普通标签改 cron", `{"labels":["我的标签"],"cron_expression":"30 7 * * *"}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			task := mustCreateLockTestTask(t, "manual-"+tc.name, "9 5 * * *")
			rec := performJSONRequest(engine, http.MethodPut,
				lockTestTaskPath(task.ID), tc.body, auth, "")
			if rec.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
			}

			if reloadLockTestTask(t, task.ID).SubscriptionLocked {
				t.Fatalf("非订阅任务不应被加锁")
			}

			payload := decodeJSONMap(t, rec)
			data, ok := payload["data"].(map[string]interface{})
			if !ok {
				t.Fatalf("expected task data object, got %#v", payload["data"])
			}
			if data["subscription_locked"] != false {
				t.Fatalf("响应体 subscription_locked want false got %#v", data["subscription_locked"])
			}
		})
	}
}

// 前端直接传 subscription_locked 必须被忽略：既不能凭空加锁，也不能靠传 false 把锁解掉。
// 这里刻意用订阅任务：非订阅任务本来就不会加锁，测不出「前端传值被忽略」这条。
func TestUpdateTaskIgnoresClientSuppliedSubscriptionLocked(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "task-lock-ignore-operator", "operator")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)
	auth := map[string]string{"Authorization": "Bearer " + token}

	// ① 没有任何实际改动，光传 subscription_locked=true → 不加锁
	task := mustCreateLockTestTask(t, "lock-ignore-a", "9 5 * * *", subscriptionOwnedLabel)
	rec := performJSONRequest(engine, http.MethodPut,
		lockTestTaskPath(task.ID), `{"subscription_locked":true}`, auth, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if reloadLockTestTask(t, task.ID).SubscriptionLocked {
		t.Fatalf("前端传 subscription_locked=true 不应该生效")
	}

	// ② 已加锁的任务，改 cron 的同时传 subscription_locked=false → 锁必须还在
	locked := mustCreateLockTestTask(t, "lock-ignore-b", "9 5 * * *", subscriptionOwnedLabel)
	if err := database.DB.Model(locked).Update("subscription_locked", true).Error; err != nil {
		t.Fatalf("preset lock: %v", err)
	}
	rec = performJSONRequest(engine, http.MethodPut,
		lockTestTaskPath(locked.ID),
		`{"cron_expression":"0 8 * * *","subscription_locked":false}`, auth, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !reloadLockTestTask(t, locked.ID).SubscriptionLocked {
		t.Fatalf("前端传 subscription_locked=false 不应该解锁")
	}
}

// 「恢复为订阅默认」是唯一的解锁入口。
func TestRestoreSubscriptionDefaultClearsLock(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "task-lock-restore-operator", "operator")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)
	auth := map[string]string{"Authorization": "Bearer " + token}

	task := mustCreateLockTestTask(t, "lock-restore", "9 5 * * *")
	if err := database.DB.Model(task).Update("subscription_locked", true).Error; err != nil {
		t.Fatalf("preset lock: %v", err)
	}

	rec := performJSONRequest(engine, http.MethodPut,
		lockTestTaskPath(task.ID)+"/restore-subscription-default", "", auth, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if reloadLockTestTask(t, task.ID).SubscriptionLocked {
		t.Fatalf("恢复为订阅默认后 subscription_locked 应被清除")
	}
}
