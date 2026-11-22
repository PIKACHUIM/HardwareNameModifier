package main

// 通用的 Win32 辅助函数：DLL 句柄、字符串转换、消息框、权限提升、DPI。

import (
	"errors"
	"os"
	"runtime"
	"strings"
	"syscall"
	"unicode/utf16"
	"unsafe"
)

var (
	modUser32   = syscall.NewLazyDLL("user32.dll")
	modGdi32    = syscall.NewLazyDLL("gdi32.dll")
	modKernel32 = syscall.NewLazyDLL("kernel32.dll")
	modAdvapi32 = syscall.NewLazyDLL("advapi32.dll")
	modShell32  = syscall.NewLazyDLL("shell32.dll")
	modShcore   = syscall.NewLazyDLL("shcore.dll")
	modOle32    = syscall.NewLazyDLL("ole32.dll")
	modComctl32 = syscall.NewLazyDLL("comctl32.dll")
)

var (
	procGetModuleHandleW  = modKernel32.NewProc("GetModuleHandleW")
	procCloseHandle       = modKernel32.NewProc("CloseHandle")
	procGetCurrentProcess = modKernel32.NewProc("GetCurrentProcess")

	procMessageBoxW     = modUser32.NewProc("MessageBoxW")
	procLoadCursorW     = modUser32.NewProc("LoadCursorW")
	procGetDC           = modUser32.NewProc("GetDC")
	procReleaseDC       = modUser32.NewProc("ReleaseDC")
	procGetDpiForSystem = modUser32.NewProc("GetDpiForSystem")

	procCreateFontW   = modGdi32.NewProc("CreateFontW")
	procGetDeviceCaps = modGdi32.NewProc("GetDeviceCaps")

	procOpenProcessToken    = modAdvapi32.NewProc("OpenProcessToken")
	procGetTokenInformation = modAdvapi32.NewProc("GetTokenInformation")

	procShellExecuteW      = modShell32.NewProc("ShellExecuteW")
	procSetProcessDpiAware = modShcore.NewProc("SetProcessDpiAwareness")
)

const (
	mbOK              = 0x00000000
	mbIconError       = 0x00000010
	mbIconWarning     = 0x00000030
	mbIconInformation = 0x00000040
	mbSetForeground   = 0x00010000

	tokenQuery     = 0x00000008
	tokenElevation = 20

	logPixelsX = 88
)

// utf16Ptr 返回以 0 结尾的 UTF-16 指针，调用方需在系统调用后 KeepAlive。
func utf16Ptr(s string) *uint16 {
	p, err := syscall.UTF16PtrFromString(s)
	if err != nil {
		p, _ = syscall.UTF16PtrFromString("")
	}
	return p
}

// utf16WithNull 返回带结尾 0 的 UTF-16 编码，用于写注册表。
func utf16WithNull(s string) []uint16 {
	u := utf16.Encode([]rune(s))
	out := make([]uint16, len(u)+1)
	copy(out, u)
	return out
}

// decodeUTF16 智能解码：数据看起来像 UTF-16LE 就按 UTF-16 解码，否则按原始字节。
func decodeUTF16(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	if len(b)%2 != 0 {
		return string(b)
	}
	zeros := 0
	for i := 1; i < len(b); i += 2 {
		if b[i] == 0 {
			zeros++
		}
	}
	if zeros*2 < len(b)/2 {
		return string(b)
	}
	u := make([]uint16, 0, len(b)/2)
	for i := 0; i+1 < len(b); i += 2 {
		u = append(u, uint16(b[i])|uint16(b[i+1])<<8)
	}
	s := string(utf16.Decode(u))
	return strings.TrimPrefix(s, "\ufeff")
}

func messageBox(owner uintptr, title, text string, flags uint32) {
	t := utf16Ptr(title)
	m := utf16Ptr(text)
	procMessageBoxW.Call(owner,
		uintptr(unsafe.Pointer(m)),
		uintptr(unsafe.Pointer(t)),
		uintptr(flags|mbSetForeground))
	runtime.KeepAlive(t)
	runtime.KeepAlive(m)
}

// isElevated 判断当前进程是否具有管理员权限。
func isElevated() bool {
	var token syscall.Handle
	cur, _, _ := procGetCurrentProcess.Call()
	ok, _, _ := procOpenProcessToken.Call(cur, tokenQuery, uintptr(unsafe.Pointer(&token)))
	if ok == 0 {
		return false
	}
	defer procCloseHandle.Call(uintptr(token))

	var elevated uint32
	var size uint32 = 4
	ok, _, _ = procGetTokenInformation.Call(uintptr(token), tokenElevation,
		uintptr(unsafe.Pointer(&elevated)), uintptr(size), uintptr(unsafe.Pointer(&size)))
	if ok == 0 {
		return false
	}
	return elevated != 0
}

// quoteArgs 把参数列表拼成 Windows 命令行。
func quoteArgs(args []string) string {
	var b strings.Builder
	for i, a := range args {
		if i > 0 {
			b.WriteByte(' ')
		}
		if a == "" || strings.ContainsAny(a, " \t\"") {
			b.WriteByte('"')
			b.WriteString(strings.ReplaceAll(a, `"`, `\"`))
			b.WriteByte('"')
		} else {
			b.WriteString(a)
		}
	}
	return b.String()
}

// relaunchElevated 以管理员身份重新启动自己，返回是否成功拉起。
func relaunchElevated() bool {
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	verb := utf16Ptr("runas")
	file := utf16Ptr(exe)
	var paramsPtr *uint16
	params := quoteArgs(os.Args[1:])
	if params != "" {
		paramsPtr = utf16Ptr(params)
	}
	ret, _, _ := procShellExecuteW.Call(0,
		uintptr(unsafe.Pointer(verb)),
		uintptr(unsafe.Pointer(file)),
		uintptr(unsafe.Pointer(paramsPtr)),
		0, 1)
	runtime.KeepAlive(verb)
	runtime.KeepAlive(file)
	runtime.KeepAlive(paramsPtr)
	return ret > 32
}

// initDPI 设置进程 DPI 感知并返回缩放比例（96 DPI 时为 1.0）。
func initDPI() float64 {
	if procSetProcessDpiAware.Find() == nil {
		// PROCESS_SYSTEM_DPI_AWARE
		procSetProcessDpiAware.Call(1)
	}
	dpi := uintptr(96)
	if procGetDpiForSystem.Find() == nil {
		if v, _, _ := procGetDpiForSystem.Call(); v > 0 {
			dpi = v
		}
	} else if hdc, _, _ := procGetDC.Call(0); hdc != 0 {
		if v, _, _ := procGetDeviceCaps.Call(hdc, logPixelsX); v > 0 {
			dpi = v
		}
		procReleaseDC.Call(0, hdc)
	}
	return float64(dpi) / 96.0
}

func fileExists(path string) bool {
	st, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !st.IsDir()
}

func copyFile(src, dst string) error {
	if !fileExists(src) {
		return errors.New("找不到文件: " + src)
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}
