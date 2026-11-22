package main

// 配置持久化：把自定义名称记录到 %APPDATA%\HwName.json，
// 供“开机自动修改”的服务在启动时读取并应用。

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

const configFileName = "HwName.json"

// deviceOverride 一条设备改名记录（含原始名称，用于恢复默认）。
type deviceOverride struct {
	ID           string `json:"id"`
	Kind         string `json:"kind"`
	Name         string `json:"name"`
	OrigFriendly string `json:"orig_friendly,omitempty"`
	OrigDriver   string `json:"orig_driver,omitempty"`
}

// appConfig 整个工具的配置。
type appConfig struct {
	CPU         string           `json:"cpu,omitempty"`
	CPUOriginal string           `json:"cpu_original,omitempty"`
	Devices     []deviceOverride `json:"devices,omitempty"`
}

func configPath() string {
	dir := appDataDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, configFileName)
}

func loadConfig() appConfig {
	var c appConfig
	path := configPath()
	if path == "" {
		return c
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return c
	}
	_ = json.Unmarshal(data, &c)
	return c
}

func saveConfig(c appConfig) error {
	path := configPath()
	if path == "" {
		return errors.New("%APPDATA% 未设置")
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func (c appConfig) empty() bool {
	return c.CPU == "" && len(c.Devices) == 0
}

func (c *appConfig) findDevice(id string) (deviceOverride, bool) {
	for _, d := range c.Devices {
		if strings.EqualFold(d.ID, id) {
			return d, true
		}
	}
	return deviceOverride{}, false
}

func (c *appConfig) setDevice(e deviceOverride) {
	for i := range c.Devices {
		if strings.EqualFold(c.Devices[i].ID, e.ID) {
			c.Devices[i] = e
			return
		}
	}
	c.Devices = append(c.Devices, e)
}

func (c *appConfig) removeDevice(id string) bool {
	for i := range c.Devices {
		if strings.EqualFold(c.Devices[i].ID, id) {
			c.Devices = append(c.Devices[:i], c.Devices[i+1:]...)
			return true
		}
	}
	return false
}

// applyConfig 应用配置中的全部名称（服务模式与安装服务时使用）。
func applyConfig(c appConfig) error {
	var errs []string

	if strings.TrimSpace(c.CPU) != "" {
		if err := applyCPUName(c.CPU); err != nil {
			errs = append(errs, "CPU: "+err.Error())
		}
	}

	for _, e := range c.Devices {
		d, ok := resolveDevice(e.ID)
		if !ok {
			errs = append(errs, "找不到设备: "+e.ID)
			continue
		}
		if err := applyDeviceName(d, e.Name); err != nil {
			errs = append(errs, d.KindLabel+": "+err.Error())
		}
	}

	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "\n"))
	}
	return nil
}

// describeConfig 生成简短说明，便于写入日志/界面提示。
func describeConfig(c appConfig) string {
	var parts []string
	if strings.TrimSpace(c.CPU) != "" {
		parts = append(parts, "CPU")
	}
	for _, e := range c.Devices {
		parts = append(parts, kindLabel(e.Kind))
	}
	if len(parts) == 0 {
		return "无"
	}
	return strings.Join(parts, "、")
}
