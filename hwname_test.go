package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDecodeUTF16(t *testing.T) {
	// NssmUI.exe 输出为 UTF-16LE
	raw := []byte{'C', 0, 'P', 0, 'U', 0}
	if got := decodeUTF16(raw); got != "CPU" {
		t.Fatalf("decodeUTF16(utf16) = %q", got)
	}
	if got := decodeUTF16([]byte("SERVICE_RUNNING")); got != "SERVICE_RUNNING" {
		t.Fatalf("decodeUTF16(ansi) = %q", got)
	}
	if got := decodeUTF16(nil); got != "" {
		t.Fatalf("decodeUTF16(nil) = %q", got)
	}
}

func TestQuoteArgs(t *testing.T) {
	cases := map[string][]string{
		"":                    nil,
		"server":              {"server"},
		`server "Intel 0000"`: {"server", "Intel 0000"},
	}
	for want, args := range cases {
		if got := quoteArgs(args); got != want {
			t.Errorf("quoteArgs(%v) = %q, want %q", args, got, want)
		}
	}
}

func TestNormalizeOutput(t *testing.T) {
	if got := normalizeOutput("a\r\nb\r\n"); got != "a\nb\n" {
		t.Fatalf("normalizeOutput = %q", got)
	}
	if got := normalizeOutput(""); got != "" {
		t.Fatalf("normalizeOutput(\"\") = %q", got)
	}
}

func TestReadCPUName(t *testing.T) {
	name, err := readCPUName()
	if err != nil {
		t.Fatalf("readCPUName: %v", err)
	}
	if name == "" {
		t.Fatal("CPU 名称为空")
	}
	t.Logf("当前 CPU 名称: %s", name)
}

func TestCreateShortcut(t *testing.T) {
	path := filepath.Join(os.TempDir(), "hwname-test.lnk")
	_ = os.Remove(path)
	if err := createShortcut(path, `C:\Windows\System32\notepad.exe`, "Hardware Name Modifier"); err != nil {
		t.Fatalf("createShortcut: %v", err)
	}
	defer os.Remove(path)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("快捷方式为空")
	}
}

func TestServiceStatus(t *testing.T) {
	text, err := runNssm("status", svcName)
	t.Logf("服务状态: %q err=%v installed=%v", text, err, serviceInstalled())
}
