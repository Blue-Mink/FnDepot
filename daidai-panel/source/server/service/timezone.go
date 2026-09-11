package service

import (
	"os"
	"sync"
	"time"

	"daidai-panel/model"
)

var panelTimezoneState = struct {
	sync.RWMutex
	name string
}{
	name: model.DefaultPanelTimezone,
}

func ApplyPanelTimezone(value string) error {
	normalized, err := model.NormalizeSystemConfigValue(model.PanelTimezoneConfigKey, value)
	if err != nil {
		return err
	}

	location, err := time.LoadLocation(normalized)
	if err != nil {
		return err
	}

	// Go 进程内的 time.Now() 使用 time.Local；子进程和脚本再通过 TZ 继承同一个面板时区。
	time.Local = location
	_ = os.Setenv("TZ", normalized)

	panelTimezoneState.Lock()
	panelTimezoneState.name = normalized
	panelTimezoneState.Unlock()
	return nil
}

func ApplyRegisteredPanelTimezone() error {
	return ApplyPanelTimezone(model.GetRegisteredConfig(model.PanelTimezoneConfigKey))
}

func CurrentPanelTimezone() string {
	panelTimezoneState.RLock()
	name := panelTimezoneState.name
	panelTimezoneState.RUnlock()

	if name == "" {
		return model.DefaultPanelTimezone
	}
	return name
}
