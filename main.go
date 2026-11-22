package main

// HwName —— Windows 硬件名称修改工具（Go 版）
// 支持修改 CPU、显卡、硬盘、声卡、显示器的显示名称；
// 支持临时修改与开机（服务）自动修改。

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// buildVersion 由构建时注入： -ldflags "-X main.buildVersion=xxx"
var buildVersion = "dev"

const (
	cpuSystemPath = `HARDWARE\DESCRIPTION\System`
	cpuAcpiPath   = `SYSTEM\ControlSet001\Enum\ACPI`

	// ACPI 下 CPU 设备名的关键字（Intel 平台）
	cpuAcpiVendor = "GenuineIntel"
)

func main() {
	// 服务模式：HwName.exe server ["CPU`名称"]
	if len(os.Args) >= 2 && strings.EqualFold(os.Args[1], "server") {
		if len(os.Args) >= 3 {
			// 兼容旧参数形式：直接指定 CPU 名称
			runServiceMode(os.Args[2])
		} else {
			runConfigService()
		}
		return
	}

	// 修改 HARDWARE / Enum 注册表需要管理员权限，未提权时自动以管理员身份重启。
	if !isElevated() {
		if relaunchElevated() {
			return
		}
		messageBox(0, "需要管理员权限",
			"本工具需要管理员权限才能修改硬件名称。", mbOK|mbIconError)
		return
	}

	runGUI()
}

// runServiceMode 只修改 CPU 名称（兼容旧的服务参数）。
func runServiceMode(rawName string) {
	name := strings.ReplaceAll(rawName, "`", " ")
	if err := applyCPUName(name); err != nil {
		logServer("设置 CPU 名称失败: " + err.Error())
		return
	}
	logServer("已设置 CPU 名称为: " + name)
}

// runConfigService 读取配置文件并应用全部名称（服务默认模式）。
func runConfigService() {
	cfg := loadConfig()
	if cfg.empty() {
		logServer("配置文件为空（" + configPath() + "），无需修改")
		return
	}
	if err := applyConfig(cfg); err != nil {
		logServer("开机修改失败: " + err.Error())
		return
	}
	logServer("开机修改完成: " + describeConfig(cfg))
}

// logServer 把服务模式的执行结果写入日志，便于排查。
func logServer(text string) {
	dir := appDataDir()
	if dir == "" {
		return
	}
	f, err := os.OpenFile(filepath.Join(dir, "HwName.log"),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "[%s] %s\r\n", time.Now().Format("2006-01-02 15:04:05"), text)
}

// readCPUName 读取当前 CPU 显示名称。
func readCPUName() (string, error) {
	root, err := openKey(cpuSystemPath, keyRead)
	if err != nil {
		return "", err
	}
	defer root.Close()

	cpu, err := root.OpenSub("CentralProcessor")
	if err != nil {
		return "", err
	}
	defer cpu.Close()

	first, err := cpu.OpenSub("0")
	if err != nil {
		return "", err
	}
	defer first.Close()

	return first.GetString("ProcessorNameString")
}

// applyCPUName 修改所有可修改的 CPU 名称位置。
func applyCPUName(name string) error {
	if err := writeProcessorName(name); err != nil {
		return err
	}
	// ACPI 项目在部分平台（如 AMD）不存在，与原有实现一致：失败不影响整体结果。
	_ = writeAcpiFriendlyName(name)
	return nil
}

// writeProcessorName 修改 HARDWARE\DESCRIPTION\System\CentralProcessor\0 下的名称。
func writeProcessorName(name string) error {
	root, err := openKey(cpuSystemPath, keyReadWrite)
	if err != nil {
		return err
	}
	defer root.Close()

	cpu, err := root.OpenSub("CentralProcessor")
	if err != nil {
		return err
	}
	defer cpu.Close()

	first, err := cpu.OpenSub("0")
	if err != nil {
		return err
	}
	defer first.Close()

	return first.SetString("ProcessorNameString", name)
}

// writeAcpiFriendlyName 修改 SYSTEM\ControlSet001\Enum\ACPI\<CPU 设备>\<实例> 下的 FriendlyName。
func writeAcpiFriendlyName(name string) error {
	root, err := openKey(cpuAcpiPath, keyReadWrite)
	if err != nil {
		return err
	}
	defer root.Close()

	devices := root.FindSub(cpuAcpiVendor)
	if len(devices) == 0 {
		return nil
	}

	var firstErr error
	for _, dev := range devices {
		node, err := root.OpenSub(dev)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		instances, err := node.SubKeys()
		if err != nil && firstErr == nil {
			firstErr = err
		}
		for _, instance := range instances {
			leaf, err := node.OpenSub(instance)
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			if err := leaf.SetString("FriendlyName", name); err != nil && firstErr == nil {
				firstErr = err
			}
			leaf.Close()
		}
		node.Close()
	}
	return firstErr
}
