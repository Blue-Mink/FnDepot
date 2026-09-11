package service

import (
	"os"
	"testing"
	"time"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

func restorePanelTimezoneForTest(t *testing.T) {
	t.Helper()

	oldLocal := time.Local
	oldTZ, hadTZ := os.LookupEnv("TZ")
	oldName := CurrentPanelTimezone()

	t.Cleanup(func() {
		time.Local = oldLocal
		if hadTZ {
			_ = os.Setenv("TZ", oldTZ)
		} else {
			_ = os.Unsetenv("TZ")
		}
		panelTimezoneState.Lock()
		panelTimezoneState.name = oldName
		panelTimezoneState.Unlock()
	})
}

func TestApplyPanelTimezoneUpdatesLocalAndEnv(t *testing.T) {
	restorePanelTimezoneForTest(t)

	if err := ApplyPanelTimezone("Asia/Tokyo"); err != nil {
		t.Fatalf("apply timezone: %v", err)
	}

	if got := CurrentPanelTimezone(); got != "Asia/Tokyo" {
		t.Fatalf("expected current panel timezone Asia/Tokyo, got %q", got)
	}
	if got := os.Getenv("TZ"); got != "Asia/Tokyo" {
		t.Fatalf("expected TZ=Asia/Tokyo, got %q", got)
	}
	if got := time.Local.String(); got != "Asia/Tokyo" {
		t.Fatalf("expected time.Local Asia/Tokyo, got %q", got)
	}
}

func TestApplyRegisteredPanelTimezoneUsesSavedConfig(t *testing.T) {
	restorePanelTimezoneForTest(t)
	testutil.SetupTestEnv(t)

	if err := model.SetConfig(model.PanelTimezoneConfigKey, "UTC"); err != nil {
		t.Fatalf("set timezone: %v", err)
	}
	if err := ApplyRegisteredPanelTimezone(); err != nil {
		t.Fatalf("apply registered timezone: %v", err)
	}

	if got := CurrentPanelTimezone(); got != "UTC" {
		t.Fatalf("expected current panel timezone UTC, got %q", got)
	}
	if got := os.Getenv("TZ"); got != "UTC" {
		t.Fatalf("expected TZ=UTC, got %q", got)
	}
}

func TestBuildManagedRuntimeEnvMapInjectsPanelTimezone(t *testing.T) {
	restorePanelTimezoneForTest(t)
	root := testutil.SetupTestEnv(t)

	if err := model.SetConfig(model.PanelTimezoneConfigKey, "Asia/Tokyo"); err != nil {
		t.Fatalf("set timezone: %v", err)
	}
	if err := ApplyRegisteredPanelTimezone(); err != nil {
		t.Fatalf("apply registered timezone: %v", err)
	}
	if err := database.DB.Create(&model.EnvVar{
		Name:    "TZ",
		Value:   "UTC",
		Enabled: true,
	}).Error; err != nil {
		t.Fatalf("create user TZ env: %v", err)
	}

	envMap, err := BuildManagedRuntimeEnvMapForPythonVersion(root, root, nil, time.Hour, "3.10")
	if err != nil {
		t.Fatalf("build managed runtime env map: %v", err)
	}
	if got := envMap["TZ"]; got != "Asia/Tokyo" {
		t.Fatalf("expected task TZ to follow panel timezone Asia/Tokyo, got %q", got)
	}
}
