package main

// 注册表读写封装，行为对应原 WinReg.py。

import (
	"fmt"
	"runtime"
	"strings"
	"syscall"
	"unsafe"
)

const (
	hkeyLocalMachine = 0x80000002

	keyRead      = 0x20019 // KEY_READ
	keyReadWrite = 0x2001B // KEY_READ | KEY_SET_VALUE

	regSZ       = 1
	regExpandSZ = 2
	regMultiSZ  = 7

	errorMoreData = 234
)

var (
	procRegOpenKeyExW    = modAdvapi32.NewProc("RegOpenKeyExW")
	procRegCloseKey      = modAdvapi32.NewProc("RegCloseKey")
	procRegQueryInfoKeyW = modAdvapi32.NewProc("RegQueryInfoKeyW")
	procRegEnumKeyExW    = modAdvapi32.NewProc("RegEnumKeyExW")
	procRegEnumValueW    = modAdvapi32.NewProc("RegEnumValueW")
	procRegQueryValueExW = modAdvapi32.NewProc("RegQueryValueExW")
	procRegSetValueExW   = modAdvapi32.NewProc("RegSetValueExW")
	procRegDeleteValueW  = modAdvapi32.NewProc("RegDeleteValueW")
)

// Key 对应一个打开的 HKLM 子键。
type Key struct {
	handle syscall.Handle
	path   string
	access uint32
}

// regValue 对应 winreg.EnumValue 的结果。
type regValue struct {
	Name string
	Type uint32
	Data []byte
}

// openKey 打开 HKLM 下的相对路径。
func openKey(path string, access uint32) (*Key, error) {
	name := utf16Ptr(path)
	var handle syscall.Handle
	ret, _, _ := procRegOpenKeyExW.Call(
		uintptr(hkeyLocalMachine),
		uintptr(unsafe.Pointer(name)),
		0,
		uintptr(access),
		uintptr(unsafe.Pointer(&handle)),
	)
	runtime.KeepAlive(name)
	if ret != 0 {
		return nil, fmt.Errorf("无法打开注册表项 %s: %w", path, syscall.Errno(ret))
	}
	return &Key{handle: handle, path: path, access: access}, nil
}

func (k *Key) Close() {
	if k != nil && k.handle != 0 {
		procRegCloseKey.Call(uintptr(k.handle))
		k.handle = 0
	}
}

// info 返回子键数量、值数量等信息。
func (k *Key) info() (subKeys, values uint32, err error) {
	var maxSubKeyLen, maxClassLen, maxValueNameLen, maxValueLen uint32
	ret, _, _ := procRegQueryInfoKeyW.Call(
		uintptr(k.handle),
		0, 0, 0,
		uintptr(unsafe.Pointer(&subKeys)),
		uintptr(unsafe.Pointer(&maxSubKeyLen)),
		uintptr(unsafe.Pointer(&maxClassLen)),
		uintptr(unsafe.Pointer(&values)),
		uintptr(unsafe.Pointer(&maxValueNameLen)),
		uintptr(unsafe.Pointer(&maxValueLen)),
		0, 0,
	)
	if ret != 0 {
		return 0, 0, fmt.Errorf("无法读取注册表项信息 %s: %w", k.path, syscall.Errno(ret))
	}
	return subKeys, values, nil
}

// SubKeys 列出所有子项名称。
func (k *Key) SubKeys() ([]string, error) {
	count, _, err := k.info()
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, count)
	for i := uint32(0); i < count; i++ {
		size := uint32(512)
		buf := make([]uint16, size)
		for {
			n := size
			ret, _, _ := procRegEnumKeyExW.Call(
				uintptr(k.handle),
				uintptr(i),
				uintptr(unsafe.Pointer(&buf[0])),
				uintptr(unsafe.Pointer(&n)),
				0, 0, 0, 0,
			)
			if ret == errorMoreData {
				size *= 2
				buf = make([]uint16, size)
				continue
			}
			if ret != 0 {
				return names, fmt.Errorf("枚举子项失败 %s: %w", k.path, syscall.Errno(ret))
			}
			names = append(names, syscall.UTF16ToString(buf[:n]))
			break
		}
	}
	return names, nil
}

// Values 列出所有键值对。
func (k *Key) Values() ([]regValue, error) {
	_, count, err := k.info()
	if err != nil {
		return nil, err
	}
	values := make([]regValue, 0, count)
	for i := uint32(0); i < count; i++ {
		nameSize := uint32(512)
		nameBuf := make([]uint16, nameSize)
		dataSize := uint32(2048)
		dataBuf := make([]byte, dataSize)
		var typ uint32
		var n, d uint32
		for {
			n = nameSize
			d = dataSize
			ret, _, _ := procRegEnumValueW.Call(
				uintptr(k.handle),
				uintptr(i),
				uintptr(unsafe.Pointer(&nameBuf[0])),
				uintptr(unsafe.Pointer(&n)),
				0,
				uintptr(unsafe.Pointer(&typ)),
				uintptr(unsafe.Pointer(&dataBuf[0])),
				uintptr(unsafe.Pointer(&d)),
			)
			if ret == errorMoreData {
				nameSize *= 2
				nameBuf = make([]uint16, nameSize)
				dataSize *= 2
				dataBuf = make([]byte, dataSize)
				continue
			}
			if ret != 0 {
				return values, fmt.Errorf("枚举键值失败 %s: %w", k.path, syscall.Errno(ret))
			}
			break
		}
		values = append(values, regValue{
			Name: syscall.UTF16ToString(nameBuf[:n]),
			Type: typ,
			Data: dataBuf[:d],
		})
	}
	return values, nil
}

// FindSub 返回名称包含指定子串的子项。
func (k *Key) FindSub(sub string) []string {
	names, err := k.SubKeys()
	if err != nil {
		return nil
	}
	var out []string
	for _, n := range names {
		if contains(n, sub) {
			out = append(out, n)
		}
	}
	return out
}

// FindVar 返回名称包含指定子串的键值。
func (k *Key) FindVar(sub string) []regValue {
	values, err := k.Values()
	if err != nil {
		return nil
	}
	var out []regValue
	for _, v := range values {
		if contains(v.Name, sub) {
			out = append(out, v)
		}
	}
	return out
}

// OpenSub 打开子项（按完整名称）。
func (k *Key) OpenSub(name string) (*Key, error) {
	return openKey(k.path+`\`+name, k.access)
}

// GetString 读取字符串值。
func (k *Key) GetString(name string) (string, error) {
	valName := utf16Ptr(name)
	var typ, size uint32
	ret, _, _ := procRegQueryValueExW.Call(
		uintptr(k.handle),
		uintptr(unsafe.Pointer(valName)),
		0,
		uintptr(unsafe.Pointer(&typ)),
		0,
		uintptr(unsafe.Pointer(&size)),
	)
	if ret != 0 {
		runtime.KeepAlive(valName)
		return "", fmt.Errorf("无法读取键值 %s: %w", name, syscall.Errno(ret))
	}
	buf := make([]byte, size+2)
	size = uint32(len(buf))
	ret, _, _ = procRegQueryValueExW.Call(
		uintptr(k.handle),
		uintptr(unsafe.Pointer(valName)),
		0,
		uintptr(unsafe.Pointer(&typ)),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&size)),
	)
	runtime.KeepAlive(valName)
	if ret != 0 {
		return "", fmt.Errorf("无法读取键值 %s: %w", name, syscall.Errno(ret))
	}
	switch typ {
	case regSZ, regExpandSZ, regMultiSZ:
		return strings.TrimRight(decodeUTF16(buf[:size]), "\x00"), nil
	default:
		return string(buf[:size]), nil
	}
}

// SetString 写入 REG_SZ 字符串值。
func (k *Key) SetString(name, value string) error {
	valName := utf16Ptr(name)
	data := utf16WithNull(value)
	ret, _, _ := procRegSetValueExW.Call(
		uintptr(k.handle),
		uintptr(unsafe.Pointer(valName)),
		0,
		regSZ,
		uintptr(unsafe.Pointer(&data[0])),
		uintptr(len(data)*2),
	)
	runtime.KeepAlive(valName)
	runtime.KeepAlive(data)
	if ret != 0 {
		return fmt.Errorf("无法写入键值 %s: %w", name, syscall.Errno(ret))
	}
	return nil
}

// DeleteValue 删除键值。
func (k *Key) DeleteValue(name string) error {
	valName := utf16Ptr(name)
	ret, _, _ := procRegDeleteValueW.Call(uintptr(k.handle), uintptr(unsafe.Pointer(valName)))
	runtime.KeepAlive(valName)
	if ret != 0 {
		return fmt.Errorf("无法删除键值 %s: %w", name, syscall.Errno(ret))
	}
	return nil
}

func contains(s, sub string) bool {
	return strings.Contains(s, sub)
}
