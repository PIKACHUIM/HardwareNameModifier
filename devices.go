package main

// 设备名称修改：显卡 / 硬盘 / 声卡 / 显示器。
//
// 原理：Windows 设备管理器、设置等界面显示的名称来自 PnP 设备实例键
// HKLM\SYSTEM\CurrentControlSet\Enum\<枚举器>\<设备>\<实例> 下的 FriendlyName。
// 写入该值即可改变显示名称；删除则恢复系统默认（DeviceDesc）。
// 显卡额外写入 HKLM\SYSTEM\CurrentControlSet\Control\Class\{4d36e968-...}\<nnnn>\DriverDesc，
// 因为部分程序（如 WMI 的 Win32_VideoController）取的是该值。

import (
	"sort"
	"strings"
)

// 设备类别（ClassGUID）
type devKindDef struct {
	Key   string
	Label string
	GUID  string
}

var deviceKinds = []devKindDef{
	{"GPU", "显卡", "{4d36e968-e325-11ce-bfc1-08002be10318}"},
	{"Disk", "硬盘", "{4d36e967-e325-11ce-bfc1-08002be10318}"},
	{"Audio", "声卡", "{4d36e96c-e325-11ce-bfc1-08002be10318}"},
	{"Monitor", "显示器", "{4d36e96e-e325-11ce-bfc1-08002be10318}"},
}

const enumRootPath = `SYSTEM\CurrentControlSet\Enum`

// device 表示一个可改名的 PnP 设备实例。
type device struct {
	Kind      string // GPU / Disk / Audio / Monitor
	KindLabel string // 显卡 / 硬盘 ...
	ID        string // 设备实例 ID
	EnumPath  string // Enum 下的注册表路径
	ClassPath string // Control\Class 下的驱动键（可能为空）
	Name      string // 当前显示名称
}

func kindByGUID(guid string) (devKindDef, bool) {
	guid = strings.ToLower(strings.TrimSpace(guid))
	for _, k := range deviceKinds {
		if strings.ToLower(k.GUID) == guid {
			return k, true
		}
	}
	return devKindDef{}, false
}

func kindLabel(key string) string {
	for _, k := range deviceKinds {
		if k.Key == key {
			return k.Label
		}
	}
	return key
}

// parseDesc 处理 "@oem13.inf,%amd67ef.9%;Radeon RX 560 Series" 这类间接字符串。
func parseDesc(s string) string {
	s = strings.TrimSpace(strings.TrimRight(s, "\x00"))
	if s == "" {
		return ""
	}
	if strings.HasPrefix(s, "@") {
		if i := strings.LastIndex(s, ";"); i >= 0 && i+1 < len(s) {
			return strings.TrimSpace(s[i+1:])
		}
		return s
	}
	// REG_MULTI_SZ 只取第一段
	if i := strings.IndexByte(s, 0); i >= 0 {
		return s[:i]
	}
	return s
}

// readDevice 读取单个设备实例；不属于支持的类别时返回 false。
func readDevice(id string) (device, bool) {
	enumPath := enumRootPath + `\` + id
	k, err := openKey(enumPath, keyRead)
	if err != nil {
		return device{}, false
	}
	defer k.Close()

	guid, _ := k.GetString("ClassGUID")
	kind, ok := kindByGUID(guid)
	if !ok {
		return device{}, false
	}

	d := device{
		Kind:      kind.Key,
		KindLabel: kind.Label,
		ID:        id,
		EnumPath:  enumPath,
	}

	friendly, _ := k.GetString("FriendlyName")
	if strings.TrimSpace(friendly) != "" {
		d.Name = parseDesc(friendly)
	}
	if d.Name == "" {
		desc, _ := k.GetString("DeviceDesc")
		d.Name = parseDesc(desc)
	}
	if d.Name == "" {
		d.Name = id
	}

	if drv, err := k.GetString("Driver"); err == nil {
		drv = strings.TrimSpace(strings.TrimRight(drv, "\x00"))
		if drv != "" {
			d.ClassPath = `SYSTEM\CurrentControlSet\Control\Class\` + drv
		}
	}
	return d, true
}

// resolveDevice 通过实例 ID 找到设备。
func resolveDevice(id string) (device, bool) {
	return readDevice(id)
}

// enumerateDevices 扫描 Enum 树，返回所有可改名的设备（同一型号多个实例都会列出）。
func enumerateDevices() []device {
	root, err := openKey(enumRootPath, keyRead)
	if err != nil {
		return nil
	}
	defer root.Close()

	enumerators, err := root.SubKeys()
	if err != nil {
		return nil
	}

	var out []device
	for _, enumName := range enumerators {
		node, err := root.OpenSub(enumName)
		if err != nil {
			continue
		}
		deviceNames, err := node.SubKeys()
		if err != nil {
			node.Close()
			continue
		}
		for _, devName := range deviceNames {
			devKey, err := node.OpenSub(devName)
			if err != nil {
				continue
			}
			instances, err := devKey.SubKeys()
			if err != nil {
				devKey.Close()
				continue
			}
			for _, inst := range instances {
				id := enumName + `\` + devName + `\` + inst
				if d, ok := readDevice(id); ok {
					out = append(out, d)
				}
			}
			devKey.Close()
		}
		node.Close()
	}

	order := func(kind string) int {
		for i, k := range deviceKinds {
			if k.Key == kind {
				return i
			}
		}
		return len(deviceKinds)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return order(out[i].Kind) < order(out[j].Kind)
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// shortDeviceID 生成列表里显示的简短设备标识（保留可区分性）。
func shortDeviceID(id string) string {
	parts := strings.Split(id, `\`)
	if len(parts) < 3 {
		return id
	}
	enumName, deviceName, instance := parts[0], parts[1], parts[len(parts)-1]

	switch enumName {
	case "SCSI", "STORAGE", "NVME":
		deviceName = strings.TrimPrefix(deviceName, "Disk&Ven_")
		deviceName = strings.ReplaceAll(deviceName, "&Prod_", " ")
		deviceName = strings.ReplaceAll(deviceName, "_", " ")
	case "PCI":
		if i := strings.Index(deviceName, "&SUBSYS"); i > 0 {
			deviceName = deviceName[:i]
		}
	}

	suffix := instance
	if len(suffix) > 4 {
		suffix = suffix[len(suffix)-4:]
	}
	return strings.TrimSpace(enumName + " " + deviceName + " #" + suffix)
}

// captureOriginals 记录修改前的原始名称，用于“恢复默认”。
func captureOriginals(d device) (friendly, driver string, err error) {
	k, err := openKey(d.EnumPath, keyRead)
	if err != nil {
		return "", "", err
	}
	friendly, _ = k.GetString("FriendlyName")
	k.Close()

	if d.Kind == "GPU" && d.ClassPath != "" {
		if ck, err := openKey(d.ClassPath, keyRead); err == nil {
			driver, _ = ck.GetString("DriverDesc")
			ck.Close()
		}
	}
	return friendly, driver, nil
}

// applyDeviceName 写入自定义名称。
func applyDeviceName(d device, name string) error {
	k, err := openKey(d.EnumPath, keyReadWrite)
	if err != nil {
		return err
	}
	err = k.SetString("FriendlyName", name)
	k.Close()
	if err != nil {
		return err
	}

	if d.Kind == "GPU" && d.ClassPath != "" {
		if ck, err := openKey(d.ClassPath, keyReadWrite); err == nil {
			werr := ck.SetString("DriverDesc", name)
			ck.Close()
			if werr != nil {
				return werr
			}
		}
	}
	return nil
}

// restoreDeviceName 还原为修改前的名称。
func restoreDeviceName(d device, origFriendly, origDriver string) error {
	k, err := openKey(d.EnumPath, keyReadWrite)
	if err != nil {
		return err
	}
	if strings.TrimSpace(origFriendly) == "" {
		_ = k.DeleteValue("FriendlyName")
	} else {
		if err := k.SetString("FriendlyName", origFriendly); err != nil {
			k.Close()
			return err
		}
	}
	k.Close()

	if d.Kind == "GPU" && d.ClassPath != "" && origDriver != "" {
		if ck, err := openKey(d.ClassPath, keyReadWrite); err == nil {
			_ = ck.SetString("DriverDesc", origDriver)
			ck.Close()
		}
	}
	return nil
}
