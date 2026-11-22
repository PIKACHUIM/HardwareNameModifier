package main

// 通过 COM（IShellLinkW + IPersistFile）创建 .lnk 快捷方式，对应原 winshell 的用法。

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"unsafe"
)

var (
	procCoInitializeEx   = modOle32.NewProc("CoInitializeEx")
	procCoUninitialize   = modOle32.NewProc("CoUninitialize")
	procCoCreateInstance = modOle32.NewProc("CoCreateInstance")
)

type guid struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

var (
	clsidShellLink = guid{0x00021401, 0x0000, 0x0000,
		[8]byte{0xC0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46}}
	iidShellLinkW = guid{0x000214F9, 0x0000, 0x0000,
		[8]byte{0xC0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46}}
	iidPersistFile = guid{0x0000010B, 0x0000, 0x0000,
		[8]byte{0xC0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46}}
)

// COM 接口虚表索引（前 3 项为 IUnknown）
const (
	vtblQueryInterface = 0
	vtblRelease        = 2

	vtblSetDescription = 7
	vtblSetShowCmd     = 15
	vtblSetPath        = 20

	vtblSave = 6 // IPersistFile::Save
)

// comIface 只用于按索引读取虚表函数指针，COM 内存不归 Go 管理。
type comIface struct {
	vtbl *[64]uintptr
}

// comCall 调用 COM 对象的第 index 个虚表方法。
func comCall(obj unsafe.Pointer, index int, args ...uintptr) uintptr {
	o := (*comIface)(obj)
	fn := o.vtbl[index]
	call := make([]uintptr, 0, len(args)+1)
	call = append(call, uintptr(obj))
	call = append(call, args...)
	ret, _, _ := syscall.SyscallN(fn, call...)
	return ret
}

func comRelease(obj unsafe.Pointer) {
	if obj != nil {
		comCall(obj, vtblRelease)
	}
}

func hrFailed(hr uintptr) bool {
	return int32(hr) < 0
}

// createShortcut 在 lnkPath 处创建指向 target 的快捷方式。
func createShortcut(lnkPath, target, description string) error {
	if dir := filepath.Dir(lnkPath); dir != "" {
		_ = os.MkdirAll(dir, 0o755)
	}

	// COINIT_APARTMENTTHREADED = 2
	hr, _, _ := procCoInitializeEx.Call(0, 2)
	initialized := !hrFailed(hr)
	const rpcEChangedMode = 0x80010106
	if initialized {
		defer procCoUninitialize.Call()
	} else if uint32(hr) != rpcEChangedMode {
		return fmt.Errorf("CoInitializeEx 失败: 0x%08X", uint32(hr))
	}

	var link unsafe.Pointer
	hr, _, _ = procCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsidShellLink)),
		0,
		1, // CLSCTX_INPROC_SERVER
		uintptr(unsafe.Pointer(&iidShellLinkW)),
		uintptr(unsafe.Pointer(&link)),
	)
	if hrFailed(hr) || link == nil {
		return fmt.Errorf("创建 ShellLink 失败: 0x%08X", uint32(hr))
	}
	defer comRelease(link)

	targetPtr := utf16Ptr(target)
	descPtr := utf16Ptr(description)
	defer runtime.KeepAlive(targetPtr)
	defer runtime.KeepAlive(descPtr)

	comCall(link, vtblSetPath, uintptr(unsafe.Pointer(targetPtr)))
	comCall(link, vtblSetDescription, uintptr(unsafe.Pointer(descPtr)))
	comCall(link, vtblSetShowCmd, 1) // SW_SHOWNORMAL

	var persist unsafe.Pointer
	hr = comCall(link, vtblQueryInterface,
		uintptr(unsafe.Pointer(&iidPersistFile)),
		uintptr(unsafe.Pointer(&persist)))
	if hrFailed(hr) || persist == nil {
		return fmt.Errorf("获取 IPersistFile 失败: 0x%08X", uint32(hr))
	}
	defer comRelease(persist)

	lnkPtr := utf16Ptr(lnkPath)
	defer runtime.KeepAlive(lnkPtr)
	hr = comCall(persist, vtblSave, uintptr(unsafe.Pointer(lnkPtr)), 1) // STGM_READWRITE
	if hrFailed(hr) {
		return fmt.Errorf("保存快捷方式失败: 0x%08X", uint32(hr))
	}
	return nil
}
