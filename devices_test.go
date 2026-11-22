package main

import (
	"os"
	"strings"
	"testing"
	"time"
	"unsafe"
)

var procPostMessage = modUser32.NewProc("PostMessageW")

func TestParseDesc(t *testing.T) {
	cases := map[string]string{
		"@oem13.inf,%amd67ef.9%;Radeon RX 560 Series": "Radeon RX 560 Series",
		"@disk.inf,%disk_devdesc%;Disk drive":         "Disk drive",
		"Fanxiang P761 2TB":                           "Fanxiang P761 2TB",
		"GenDisk\x00Other":                            "GenDisk",
		"":                                            "",
	}
	for in, want := range cases {
		if got := parseDesc(in); got != want {
			t.Errorf("parseDesc(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestEnumerateDevices(t *testing.T) {
	devs := enumerateDevices()
	if len(devs) == 0 {
		t.Fatal("没有枚举到任何可改名的设备")
	}

	counts := map[string]int{}
	for _, d := range devs {
		counts[d.KindLabel]++
		if d.ID == "" || d.EnumPath == "" || d.Name == "" {
			t.Errorf("设备信息不完整: %+v", d)
		}
		if d.Kind == "GPU" && d.ClassPath == "" {
			t.Errorf("显卡缺少 Control\\Class 路径: %s", d.ID)
		}
	}
	t.Logf("共 %d 个设备: %v", len(devs), counts)
	for i, d := range devs {
		if i >= 12 {
			break
		}
		t.Logf("  [%s] %s -> %s", d.KindLabel, d.ID, d.Name)
	}
}

func TestResolveDevice(t *testing.T) {
	devs := enumerateDevices()
	if len(devs) == 0 {
		t.Skip("没有可测试的设备")
	}
	d, ok := resolveDevice(devs[0].ID)
	if !ok {
		t.Fatalf("无法通过 ID 解析设备: %s", devs[0].ID)
	}
	if d.Name != devs[0].Name {
		t.Errorf("名称不一致: %q != %q", d.Name, devs[0].Name)
	}
	friendly, _, err := captureOriginals(d)
	if err != nil {
		t.Fatalf("captureOriginals: %v", err)
	}
	t.Logf("原始 FriendlyName = %q", friendly)
}

func TestConfigRoundTrip(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())

	cfg := loadConfig()
	if !cfg.empty() {
		t.Fatalf("新配置应为空: %+v", cfg)
	}

	cfg.CPU = "Test CPU"
	cfg.CPUOriginal = "Origin CPU"
	cfg.setDevice(deviceOverride{ID: `PCI\VEN_0000&DEV_0000\1&2&3`, Kind: "GPU", Name: "Test GPU", OrigFriendly: "", OrigDriver: "Origin GPU"})
	if err := saveConfig(cfg); err != nil {
		t.Fatalf("saveConfig: %v", err)
	}

	reloaded := loadConfig()
	if reloaded.CPU != "Test CPU" || reloaded.CPUOriginal != "Origin CPU" {
		t.Fatalf("CPU 未正确保存: %+v", reloaded)
	}
	e, ok := reloaded.findDevice(`PCI\VEN_0000&DEV_0000\1&2&3`)
	if !ok || e.Name != "Test GPU" || e.OrigDriver != "Origin GPU" {
		t.Fatalf("设备记录不正确: %+v", e)
	}
	if !strings.Contains(describeConfig(reloaded), "显卡") {
		t.Errorf("describeConfig 输出异常: %s", describeConfig(reloaded))
	}

	// 同一 ID 再次写入应覆盖而非追加
	reloaded.setDevice(deviceOverride{ID: `PCI\VEN_0000&DEV_0000\1&2&3`, Kind: "GPU", Name: "Test GPU 2"})
	if len(reloaded.Devices) != 1 || reloaded.Devices[0].Name != "Test GPU 2" {
		t.Fatalf("setDevice 覆盖失败: %+v", reloaded.Devices)
	}

	if !reloaded.removeDevice(`PCI\VEN_0000&DEV_0000\1&2&3`) || len(reloaded.Devices) != 0 {
		t.Fatal("removeDevice 失败")
	}

	// 配置文件应真实落盘
	if _, err := os.Stat(configPath()); err != nil {
		t.Fatalf("配置文件不存在: %v", err)
	}
}

// TestGUISmoke 会短暂弹出窗口，仅本地调试时通过 HWN_TEST_GUI=1 运行。
func TestGUISmoke(t *testing.T) {
	if os.Getenv("HWN_TEST_GUI") != "1" {
		t.Skip("设置 HWN_TEST_GUI=1 后运行界面冒烟测试")
	}

	done := make(chan error, 1)
	go func() {
		currentApp = &app{scale: initDPI(), lastSel: -1}
		done <- currentApp.run()
	}()

	time.Sleep(2 * time.Second)
	a := currentApp
	if a == nil || a.listHwnd == 0 {
		t.Fatal("列表控件未创建")
	}

	count, _, _ := procSendMessageW.Call(uintptr(a.listHwnd), lvmFirst+4, 0, 0)
	if count < 2 {
		t.Errorf("列表行数异常: %d", count)
	}
	t.Logf("列表行数 = %d，首行类型 = %q，编辑框 = %q", count, listText(uintptr(a.listHwnd), 0, 0), a.editText())

	procPostMessage.Call(uintptr(a.hwnd), wmClose, 0, 0)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("GUI 错误: %v", err)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("窗口未关闭")
	}
}

func listText(hwnd uintptr, row, sub int) string {
	buf := make([]uint16, 512)
	item := lvItem{
		mask:       lvifText,
		iItem:      int32(row),
		iSubItem:   int32(sub),
		pszText:    &buf[0],
		cchTextMax: int32(len(buf)),
	}
	msg := uintptr(lvmFirst + 115) // LVM_GETITEMTEXTW
	if sub == 0 {
		msg = uintptr(lvmFirst + 75) // LVM_GETITEMW
	}
	procSendMessageW.Call(hwnd, msg, uintptr(row), uintptr(unsafe.Pointer(&item)))
	out := make([]rune, 0, len(buf))
	for _, c := range buf {
		if c == 0 {
			break
		}
		out = append(out, rune(c))
	}
	return string(out)
}
