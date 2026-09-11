package handler

import (
	"net/http"
	"testing"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

// nodejs / linux 依赖以前只有 Python 分支做「先查后插」，连点两次就会重复提交同一个包的安装。
// 这里守住补上的存在性检查：同名且已安装/安装中/排队中的直接跳过，不再重复建行。
func TestNodeAndLinuxDependencyCreateSkipsExistingName(t *testing.T) {
	for _, depType := range []string{model.DepTypeNodeJS, model.DepTypeLinux} {
		t.Run(depType, func(t *testing.T) {
			testutil.SetupTestEnv(t)

			originalRunner := dependencyInstallRunner
			defer func() {
				dependencyInstallRunner = originalRunner
			}()
			// 不真的去装包，只把状态推到 installed，模拟「第一次已经装好了」。
			dependencyInstallRunner = func(id uint, installType string, name string) {
				database.DB.Model(&model.Dependency{}).Where("id = ?", id).Update("status", model.DepStatusInstalled)
			}

			engine := newDepsTestRouter()
			token := testutil.MustCreateAccessToken(t, "admin", "admin")

			existing := model.Dependency{Type: depType, Name: "axios", Status: model.DepStatusInstalled}
			if err := database.DB.Create(&existing).Error; err != nil {
				t.Fatalf("seed dependency: %v", err)
			}

			rec := performDepsJSONRequest(engine, http.MethodPost, "/api/v1/deps", map[string]any{
				"type":  depType,
				"names": []string{"axios"},
			}, map[string]string{
				"Authorization": "Bearer " + token,
			})
			if rec.Code != http.StatusCreated {
				t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
			}

			var count int64
			database.DB.Model(&model.Dependency{}).
				Where("type = ? AND name = ?", depType, "axios").
				Count(&count)
			if count != 1 {
				t.Fatalf("同名依赖不应重复建行，期望 1 条，实际 %d 条", count)
			}
		})
	}
}

// 反向守住：同一个生态里不同名字的包必须照常建行，别把跳过逻辑写宽了。
func TestNodeDependencyCreateStillAddsDifferentName(t *testing.T) {
	testutil.SetupTestEnv(t)

	originalRunner := dependencyInstallRunner
	defer func() {
		dependencyInstallRunner = originalRunner
	}()
	dependencyInstallRunner = func(id uint, installType string, name string) {
		database.DB.Model(&model.Dependency{}).Where("id = ?", id).Update("status", model.DepStatusInstalled)
	}

	engine := newDepsTestRouter()
	token := testutil.MustCreateAccessToken(t, "admin", "admin")

	existing := model.Dependency{Type: model.DepTypeNodeJS, Name: "axios", Status: model.DepStatusInstalled}
	if err := database.DB.Create(&existing).Error; err != nil {
		t.Fatalf("seed dependency: %v", err)
	}

	rec := performDepsJSONRequest(engine, http.MethodPost, "/api/v1/deps", map[string]any{
		"type":  model.DepTypeNodeJS,
		"names": []string{"dayjs"},
	}, map[string]string{
		"Authorization": "Bearer " + token,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var count int64
	database.DB.Model(&model.Dependency{}).Where("type = ?", model.DepTypeNodeJS).Count(&count)
	if count != 2 {
		t.Fatalf("不同名的包应正常新增，期望 2 条，实际 %d 条", count)
	}
}
