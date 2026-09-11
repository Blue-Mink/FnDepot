package database_test

import (
	"testing"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

// TestEnsureColumnsUnlocksNonSubscriptionTasks 覆盖存量清理：
// 早期版本的订阅锁写入点没判断任务归属，手动建的任务改名/改定时也会被加锁。
// 升级后启动一次就要把这些误锁解掉，同时一行订阅任务都不能碰——
// 订阅任务的锁记录的是用户手改名称/定时的意图，清掉会让下一次拉取覆盖用户的改动。
func TestEnsureColumnsUnlocksNonSubscriptionTasks(t *testing.T) {
	testutil.SetupTestEnv(t)

	// 手动建的任务，被老版本误锁
	manualTask := mustCreateMigrationTask(t, "手动任务", "", true)
	// 订阅任务，锁是用户真实意图
	subscriptionTask := mustCreateMigrationTask(t, "订阅任务", "subscription:7", true)
	// 订阅任务 + 用户自定义标签混排，逗号分隔下同样要认出订阅归属
	mixedTask := mustCreateMigrationTask(t, "混排标签订阅任务", "我的标签,subscription:7", true)
	// 用户自建标签里恰好含 "subscription:" 子串，但不是订阅归属标签（边界不对）。
	// 裸子串匹配会把它当成订阅任务而跳过清理，锁就留在库里了：列表页照样显示「已锁定」，
	// 详情页却按标签边界判它不是订阅任务、不渲染「恢复为订阅默认」入口，用户永远解不开。
	// 所以这行必须被解锁——它是这次把 SQL 收紧到标签边界的意义所在。
	lookalikeTask := mustCreateMigrationTask(t, "标签含子串的手动任务", "my-subscription:foo", true)
	// 历史脏数据：逗号后带空格。后端 hasSubscriptionLabel 有 TrimSpace，认它是订阅任务、照常加锁，
	// SQL 侧漏掉这种形态就会误解锁，让用户手改的名称/定时在下次拉取时被覆盖。
	spacedTask := mustCreateMigrationTask(t, "带空格的订阅任务", "我的标签, subscription:7", true)
	// 本来就没锁的手动任务，迁移不该把它改成别的样子
	untouchedTask := mustCreateMigrationTask(t, "未加锁手动任务", "我的标签", false)

	database.EnsureColumns()

	assertTaskLocked(t, manualTask.ID, false)
	assertTaskLocked(t, subscriptionTask.ID, true)
	assertTaskLocked(t, mixedTask.ID, true)
	assertTaskLocked(t, lookalikeTask.ID, false)
	assertTaskLocked(t, spacedTask.ID, true)
	assertTaskLocked(t, untouchedTask.ID, false)

	// 幂等：每次启动都会跑，第二次不该再改动任何行
	database.EnsureColumns()
	assertTaskLocked(t, manualTask.ID, false)
	assertTaskLocked(t, subscriptionTask.ID, true)
	assertTaskLocked(t, mixedTask.ID, true)
	assertTaskLocked(t, lookalikeTask.ID, false)
	assertTaskLocked(t, spacedTask.ID, true)
	assertTaskLocked(t, untouchedTask.ID, false)
}

func mustCreateMigrationTask(t *testing.T, name, labels string, locked bool) *model.Task {
	t.Helper()

	task := &model.Task{
		Name:               name,
		Command:            "task demo.js",
		CronExpression:     "9 5 * * *",
		TaskType:           model.TaskTypeCron,
		Status:             model.TaskStatusEnabled,
		Labels:             labels,
		SubscriptionLocked: locked,
	}
	// Select("*")：locked=false 时也要真写进去，不能让 GORM 按零值跳过。
	if err := database.DB.Select("*").Create(task).Error; err != nil {
		t.Fatalf("create task %q: %v", name, err)
	}
	return task
}

func assertTaskLocked(t *testing.T, id uint, want bool) {
	t.Helper()

	var task model.Task
	if err := database.DB.First(&task, id).Error; err != nil {
		t.Fatalf("reload task %d: %v", id, err)
	}
	if task.SubscriptionLocked != want {
		t.Fatalf("任务 %q subscription_locked want %v got %v", task.Name, want, task.SubscriptionLocked)
	}
}
