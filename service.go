package main

// 服务相关：调用 NssmUI.exe 安装/卸载服务、复制自身、创建桌面快捷方式。

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

const (
	svcName      = "HwName"
	nssmFileName = "NssmUI.exe"
	appFileName  = "HwName.exe"
	shortcutName = "HwName.lnk"
)

// exeDir 返回当前可执行文件所在目录。
func exeDir() string {
	exe, err := os.Executable()
	if err != nil {
		if wd, err := os.Getwd(); err == nil {
			return wd
		}
		return "."
	}
	return filepath.Dir(exe)
}

// appDataDir 返回 %APPDATA%。
func appDataDir() string {
	return os.Getenv("APPDATA")
}

// savedExePath 返回服务使用的程序副本路径 %APPDATA%\HwName.exe。
func savedExePath() string {
	dir := appDataDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, appFileName)
}

// nssmPath 返回缓存在 %APPDATA% 的 NssmUI.exe 路径。
func nssmPath() string {
	return filepath.Join(appDataDir(), nssmFileName)
}

// runNssm 调用 NssmUI.exe，返回其输出（UTF-16 自动解码）。
func runNssm(args ...string) (string, error) {
	dir := appDataDir()
	if dir == "" {
		return "", errors.New("%APPDATA% 未设置")
	}
	tool := nssmPath()
	if !fileExists(tool) {
		if err := copyFile(filepath.Join(exeDir(), nssmFileName), tool); err != nil {
			return "", fmt.Errorf("准备 %s 失败: %w", nssmFileName, err)
		}
	}

	cmd := exec.Command(tool, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err := cmd.Run()

	text := normalizeOutput(decodeUTF16(outBuf.Bytes())) +
		normalizeOutput(decodeUTF16(errBuf.Bytes()))
	if text == "" && err != nil {
		text = err.Error()
	}
	return text, err
}

// normalizeOutput 去掉换行回车与首尾空白，便于界面显示。
func normalizeOutput(s string) string {
	s = strings.ReplaceAll(s, "\r", "")
	if s != "" && !strings.HasSuffix(s, "\n") {
		s += "\n"
	}
	return s
}

// serviceInstalled 判断开机修改服务是否已安装。
func serviceInstalled() bool {
	text, err := runNssm("status", svcName)
	if strings.Contains(text, "Can't open service") {
		return false
	}
	if err != nil {
		return false
	}
	return true
}

// installService 安装开机自动修改服务，并创建桌面快捷方式。
// 服务启动时读取 %APPDATA%\HwName.json 应用全部名称。
func installService() (string, error) {
	target := savedExePath()
	if target == "" {
		return "", errors.New("%APPDATA% 未设置")
	}

	var out strings.Builder
	// 升级场景：先停掉旧服务，避免 %APPDATA%\HwName.exe 被占用而无法覆盖
	if serviceInstalled() {
		text, _ := runNssm("stop", svcName)
		out.WriteString(text)
	}

	if exe, err := os.Executable(); err == nil && !strings.EqualFold(filepath.Clean(exe), filepath.Clean(target)) {
		if err := copyFile(exe, target); err != nil && !fileExists(target) {
			return "", fmt.Errorf("复制程序到 %s 失败: %w", target, err)
		}
	}
	if !fileExists(target) {
		return "", fmt.Errorf("找不到程序文件 %s", target)
	}

	workDir, err := os.Getwd()
	if err != nil {
		workDir = exeDir()
	}

	if text, err := runNssm("install", svcName, target); err != nil && !strings.Contains(text, "already exists") {
		out.WriteString(text)
		return out.String(), fmt.Errorf("安装服务失败: %w", err)
	} else {
		out.WriteString(text)
	}

	// AppParameters 只传 "server"，服务启动时从配置文件读取要修改的项。
	// AppExit 设为 Exit，避免程序执行完退出后被 nssm 反复重启。
	for _, args := range [][]string{
		{"set", svcName, "AppDirectory", workDir},
		{"set", svcName, "AppParameters", "server"},
		{"set", svcName, "AppExit", "Default", "Exit"},
		{"set", svcName, "AppExit", "0", "Exit"},
		{"set", svcName, "Start", "SERVICE_AUTO_START"},
	} {
		text, _ := runNssm(args...)
		out.WriteString(text)
	}

	if err := createShortcut(desktopShortcutPath(), target, "Hardware Name Modifier"); err != nil {
		out.WriteString("创建桌面快捷方式失败: " + err.Error() + "\n")
	}

	if !serviceInstalled() {
		return out.String(), errors.New("服务安装失败，请检查是否有权限或查看上方信息")
	}
	return out.String(), nil
}

// uninstallService 停止并移除开机修改服务。
func uninstallService() (string, error) {
	var out strings.Builder
	for _, args := range [][]string{
		{"stop", svcName},
		{"remove", svcName, "confirm"},
	} {
		text, _ := runNssm(args...)
		out.WriteString(text)
	}
	if serviceInstalled() {
		return out.String(), errors.New("服务移除失败，请查看上方信息")
	}
	return out.String(), nil
}

// desktopShortcutPath 返回桌面快捷方式路径。
func desktopShortcutPath() string {
	home := os.Getenv("USERPROFILE")
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	return filepath.Join(home, "Desktop", shortcutName)
}
