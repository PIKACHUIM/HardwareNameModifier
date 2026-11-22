package main

// 使用 Win32 API 实现的界面：设备列表 + 名称编辑 + 应用/恢复/开机自动修改。

import (
	"fmt"
	"math"
	"runtime"
	"strings"
	"syscall"
	"unsafe"
)

var (
	procRegisterClassExW   = modUser32.NewProc("RegisterClassExW")
	procCreateWindowExW    = modUser32.NewProc("CreateWindowExW")
	procDefWindowProcW     = modUser32.NewProc("DefWindowProcW")
	procShowWindow         = modUser32.NewProc("ShowWindow")
	procUpdateWindow       = modUser32.NewProc("UpdateWindow")
	procGetMessageW        = modUser32.NewProc("GetMessageW")
	procTranslateMessage   = modUser32.NewProc("TranslateMessage")
	procDispatchMessageW   = modUser32.NewProc("DispatchMessageW")
	procIsDialogMessageW   = modUser32.NewProc("IsDialogMessageW")
	procPostQuitMessage    = modUser32.NewProc("PostQuitMessage")
	procSendMessageW       = modUser32.NewProc("SendMessageW")
	procSetWindowTextW     = modUser32.NewProc("SetWindowTextW")
	procGetWindowTextW     = modUser32.NewProc("GetWindowTextW")
	procGetWindowTextLen   = modUser32.NewProc("GetWindowTextLengthW")
	procDestroyWindow      = modUser32.NewProc("DestroyWindow")
	procAdjustWindowRectEx = modUser32.NewProc("AdjustWindowRectEx")
	procGetSystemMetrics   = modUser32.NewProc("GetSystemMetrics")
	procGetSysColorBrush   = modUser32.NewProc("GetSysColorBrush")
	procSetBkMode          = modGdi32.NewProc("SetBkMode")
	procInitCommonControls = modComctl32.NewProc("InitCommonControlsEx")
	procSetTimer           = modUser32.NewProc("SetTimer")
	procImageListCreate    = modComctl32.NewProc("ImageList_Create")
)

const (
	windowTitle = "硬件名称修改工具 for Windows"
	classTitle  = "HwNameWindowClass"

	winWidthDIP     = 780
	winHeightDIP    = 520
	headerHeightDIP = 76

	// 列表卡片与列表控件（DIP）
	listCardX, listCardY, listCardW, listCardH = 24, 92, 732, 300
	dipListX, dipListY, dipListW, dipListH     = 26, 94, 728, 296

	// 底部编辑区（DIP）
	dipNameLblX, dipNameLblY, dipNameLblW, dipNameLblH = 24, 412, 52, 30
	dipEditX, dipEditY, dipEditW, dipEditH             = 80, 410, 676, 30

	dipBtnY, dipBtnH         = 458, 36
	dipApplyX, dipApplyW     = 24, 116
	dipRestoreX, dipRestoreW = 148, 124
	dipServiceX, dipServiceW = 446, 190
	dipRefreshX, dipRefreshW = 644, 112
)

const (
	idList       = 1001
	idEdit       = 1002
	idBtnApply   = 1003
	idBtnRestore = 1004
	idBtnService = 1005
	idBtnRefresh = 1006
)

const (
	wsChild        = 0x40000000
	wsVisible      = 0x10000000
	wsTabStop      = 0x00010000
	wsBorder       = 0x00800000
	wsCaption      = 0x00C00000
	wsSysMenu      = 0x00080000
	wsMinimizeBox  = 0x00020000
	wsClipChildren = 0x02000000
	wsExClientEdge = 0x00000200

	esAutoHScroll = 0x0080

	ssLeft      = 0x00000000
	bsOwnerDraw = 0x0000000B

	lvsReport      = 0x0001
	lvsSingleSel   = 0x0004
	lvsShowSelOnly = 0x0008

	iccListViewClasses = 0x00000001

	lvmFirst                = 0x1000
	lvmSetImageList         = lvmFirst + 3
	lvmDeleteAllItems       = lvmFirst + 9
	lvmInsertItem           = lvmFirst + 77 // LVM_INSERTITEMW
	lvmInsertColumn         = lvmFirst + 97 // LVM_INSERTCOLUMNW
	lvmSetItemText          = lvmFirst + 116
	lvmGetNextItem          = lvmFirst + 12
	lvmSetItemState         = lvmFirst + 43
	lvmSetExtendedListStyle = lvmFirst + 54
	lvmSetColumnWidth       = lvmFirst + 30

	lvsilSmall = 1

	lvsExFullRowSelect = 0x00000020
	lvsExDoubleBuffer  = 0x00010000

	lvifText = 0x0001
	lvcfFmt  = 0x0001
	lvcfCx   = 0x0002
	lvcfText = 0x0004

	lvniSelected = 0x0002
	lvisSelected = 0x0002
	lvisFocused  = 0x0001

	wmCreate         = 0x0001
	wmDestroy        = 0x0002
	wmPaint          = 0x000F
	wmClose          = 0x0010
	wmEraseBkgnd     = 0x0014
	wmSetFont        = 0x0030
	wmDrawItem       = 0x002B
	wmCommand        = 0x0111
	wmTimer          = 0x0113
	wmCtlColorStatic = 0x0138

	swShow = 5

	colorWindow      = 5
	defaultCharset   = 1
	cleartypeQuality = 5
	fwNormal         = 400
	idcArrow         = 32512

	timerPollSelection = 1
	pollIntervalMs     = 200
)

type wndClassExW struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     syscall.Handle
	hIcon         syscall.Handle
	hCursor       syscall.Handle
	hbrBackground syscall.Handle
	lpszMenuName  *uint16
	lpszClassName *uint16
	hIconSm       syscall.Handle
}

type rectT struct {
	left, top, right, bottom int32
}

type msgT struct {
	hwnd    syscall.Handle
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      struct{ x, y int32 }
	priv    uint32
}

type lvColumn struct {
	mask       uint32
	fmt        int32
	cx         int32
	pszText    *uint16
	cchTextMax int32
	iSubItem   int32
	iImage     int32
	iOrder     int32
	cxMin      int32
	cxMax      int32
	cxDefault  int32
	cxIdeal    int32
}

type lvItem struct {
	mask       uint32
	iItem      int32
	iSubItem   int32
	state      uint32
	stateMask  uint32
	pszText    *uint16
	cchTextMax int32
	iImage     int32
	lParam     uintptr
	iIndent    int32
	iGroupID   int32
	cColumns   uint32
	puColumns  *uint32
	piColFmt   *int32
	iGroup     int32
}

// deviceRow 列表中的一行。
type deviceRow struct {
	isCPU bool
	kind  string
	label string
	id    string
	name  string
	dev   device
}

// app 保存界面和状态。
type app struct {
	scale      float64
	hwnd       syscall.Handle
	hFont      uintptr
	fonts      uiFonts
	bgBrush    uintptr
	himl       uintptr
	listHwnd   syscall.Handle
	editHwnd   syscall.Handle
	btnApply   syscall.Handle
	btnRestore syscall.Handle
	btnService syscall.Handle
	btnRefresh syscall.Handle
	rows       []deviceRow
	lastSel    int
	installed  bool
	subtitle   string
}

var (
	currentApp *app
	wndProcCb  = syscall.NewCallback(wndProc)
)

func (a *app) dip(v int) int {
	return int(math.Round(float64(v) * a.scale))
}

func runGUI() {
	currentApp = &app{scale: initDPI(), lastSel: -1}
	if err := currentApp.run(); err != nil {
		messageBox(0, "启动失败", err.Error(), mbOK|mbIconError)
	}
}

func (a *app) run() error {
	runtime.LockOSThread()

	var icc struct {
		size uint32
		icc  uint32
	}
	icc.size = uint32(unsafe.Sizeof(icc))
	icc.icc = iccListViewClasses
	procInitCommonControls.Call(uintptr(unsafe.Pointer(&icc)))

	hInst, _, _ := procGetModuleHandleW.Call(0)
	className := utf16Ptr(classTitle)

	cursor, _, _ := procLoadCursorW.Call(0, idcArrow)
	wc := wndClassExW{
		cbSize:        uint32(unsafe.Sizeof(wndClassExW{})),
		lpfnWndProc:   wndProcCb,
		hInstance:     syscall.Handle(hInst),
		hCursor:       syscall.Handle(cursor),
		hbrBackground: syscall.Handle(colorWindow + 1),
		lpszClassName: className,
	}
	if ret, _, errno := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); ret == 0 {
		return fmt.Errorf("无法注册窗口类: %v", errno)
	}

	a.createFonts()
	a.hFont = a.fonts.body
	a.bgBrush, _, _ = procCreateSolidBrush.Call(colBg)

	style := uintptr(wsCaption | wsSysMenu | wsMinimizeBox | wsClipChildren)
	rc := rectT{right: int32(a.dip(winWidthDIP)), bottom: int32(a.dip(winHeightDIP))}
	procAdjustWindowRectEx.Call(uintptr(unsafe.Pointer(&rc)), style, 0, 0)
	winW := rc.right - rc.left
	winH := rc.bottom - rc.top

	screenW, _, _ := procGetSystemMetrics.Call(0)
	screenH, _, _ := procGetSystemMetrics.Call(1)
	posX := (int(screenW) - int(winW)) / 2
	posY := (int(screenH) - int(winH)) / 2

	title := utf16Ptr(windowTitle)
	hwnd, _, errno := procCreateWindowExW.Call(0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(title)),
		style,
		uintptr(posX), uintptr(posY), uintptr(winW), uintptr(winH),
		0, 0, hInst, 0)
	runtime.KeepAlive(className)
	runtime.KeepAlive(title)
	if hwnd == 0 {
		return fmt.Errorf("无法创建窗口: %v", errno)
	}
	a.hwnd = syscall.Handle(hwnd)

	a.subtitle = "CPU · 显卡 · 硬盘 · 声卡 · 显示器"
	a.createControls()
	procShowWindow.Call(hwnd, swShow)
	procUpdateWindow.Call(hwnd)

	a.reload()
	a.refreshServiceButton()
	procSetTimer.Call(hwnd, timerPollSelection, pollIntervalMs, 0)

	var m msgT
	for {
		ret, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(ret) <= 0 {
			break
		}
		// 让 Tab 键可以在控件间切换焦点
		if handled, _, _ := procIsDialogMessageW.Call(hwnd, uintptr(unsafe.Pointer(&m))); handled != 0 {
			continue
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
	return nil
}

func (a *app) createControl(class, text string, style uintptr, exStyle uintptr,
	x, y, w, h int, id uintptr) syscall.Handle {
	cls := utf16Ptr(class)
	txt := utf16Ptr(text)
	hwnd, _, _ := procCreateWindowExW.Call(
		exStyle,
		uintptr(unsafe.Pointer(cls)),
		uintptr(unsafe.Pointer(txt)),
		style,
		uintptr(a.dip(x)), uintptr(a.dip(y)), uintptr(a.dip(w)), uintptr(a.dip(h)),
		uintptr(a.hwnd), id, 0, 0)
	runtime.KeepAlive(cls)
	runtime.KeepAlive(txt)
	if hwnd != 0 {
		procSendMessageW.Call(hwnd, wmSetFont, a.hFont, 1)
	}
	return syscall.Handle(hwnd)
}

func (a *app) createButton(text string, x, y, w, h int, id uintptr) syscall.Handle {
	return a.createControl("BUTTON", text,
		wsChild|wsVisible|wsTabStop|bsOwnerDraw, 0, x, y, w, h, id)
}

func (a *app) createControls() {
	a.listHwnd = a.createControl("SysListView32", "",
		wsChild|wsVisible|wsTabStop|lvsReport|lvsSingleSel|lvsShowSelOnly, 0,
		dipListX, dipListY, dipListW, dipListH, idList)
	procSendMessageW.Call(uintptr(a.listHwnd), lvmSetExtendedListStyle, 0,
		lvsExFullRowSelect|lvsExDoubleBuffer)

	// 用一个 1xN 的空图像列表把行高撑起来，观感更舒展
	a.himl, _, _ = procImageListCreate.Call(1, uintptr(a.dip(28)), 0x20 /*ILC_COLOR32*/, 1, 1)
	if a.himl != 0 {
		procSendMessageW.Call(uintptr(a.listHwnd), lvmSetImageList, lvsilSmall, a.himl)
	}

	a.insertColumn(0, "类型", 88)
	a.insertColumn(1, "设备", 288)
	a.insertColumn(2, "当前名称", 340)
	a.fitColumns()

	a.createControl("STATIC", "名称：", wsChild|wsVisible|ssLeft, 0,
		dipNameLblX, dipNameLblY, dipNameLblW, dipNameLblH, 0)
	a.editHwnd = a.createControl("EDIT", "",
		wsChild|wsVisible|wsTabStop|esAutoHScroll, 0,
		dipEditX, dipEditY, dipEditW, dipEditH, idEdit)

	a.btnApply = a.createButton("应用名称", dipApplyX, dipBtnY, dipApplyW, dipBtnH, idBtnApply)
	a.btnRestore = a.createButton("恢复默认", dipRestoreX, dipBtnY, dipRestoreW, dipBtnH, idBtnRestore)
	a.btnService = a.createButton("设置开机自动修改", dipServiceX, dipBtnY, dipServiceW, dipBtnH, idBtnService)
	a.btnRefresh = a.createButton("刷新", dipRefreshX, dipBtnY, dipRefreshW, dipBtnH, idBtnRefresh)
}

func (a *app) insertColumn(index int, title string, width int) {
	text := utf16Ptr(title)
	col := lvColumn{
		mask:    lvcfFmt | lvcfCx | lvcfText,
		fmt:     0, // LVCFMT_LEFT
		cx:      int32(a.dip(width)),
		pszText: text,
	}
	procSendMessageW.Call(uintptr(a.listHwnd), lvmInsertColumn, uintptr(index), uintptr(unsafe.Pointer(&col)))
	runtime.KeepAlive(text)
}

// fitColumns 让列宽正好铺满列表（预留纵向滚动条宽度），避免出现横向滚动条。
func (a *app) fitColumns() {
	var rc rectT
	procGetClientRect.Call(uintptr(a.listHwnd), uintptr(unsafe.Pointer(&rc)))
	clientW := int(rc.right - rc.left)
	sb, _, _ := procGetSystemMetrics.Call(2) // SM_CXVSCROLL

	avail := clientW - int(sb) - 2
	w0 := a.dip(88)
	w1 := a.dip(288)
	w2 := avail - w0 - w1
	if w2 < a.dip(140) {
		w2 = a.dip(140)
	}
	a.setColumnWidth(0, w0)
	a.setColumnWidth(1, w1)
	a.setColumnWidth(2, w2)
}

func (a *app) setColumnWidth(index, width int) {
	procSendMessageW.Call(uintptr(a.listHwnd), lvmSetColumnWidth, uintptr(index), uintptr(width))
}

func (a *app) setItemText(row, sub int, text string) {
	ptr := utf16Ptr(text)
	item := lvItem{mask: lvifText, iSubItem: int32(sub), pszText: ptr}
	procSendMessageW.Call(uintptr(a.listHwnd), lvmSetItemText, uintptr(row), uintptr(unsafe.Pointer(&item)))
	runtime.KeepAlive(ptr)
}

func (a *app) insertRow(index int, r deviceRow) {
	text := utf16Ptr(r.label)
	item := lvItem{
		mask:    lvifText,
		iItem:   int32(index),
		pszText: text,
	}
	procSendMessageW.Call(uintptr(a.listHwnd), lvmInsertItem, 0, uintptr(unsafe.Pointer(&item)))
	runtime.KeepAlive(text)
	a.setItemText(index, 1, r.id)
	a.setItemText(index, 2, r.name)
}

func (a *app) selectRow(index int) {
	item := lvItem{
		state:     lvisSelected | lvisFocused,
		stateMask: lvisSelected | lvisFocused,
	}
	procSendMessageW.Call(uintptr(a.listHwnd), lvmSetItemState, uintptr(index), uintptr(unsafe.Pointer(&item)))
}

func (a *app) selectedRow() int {
	ret, _, _ := procSendMessageW.Call(uintptr(a.listHwnd), lvmGetNextItem, ^uintptr(0), lvniSelected)
	return int(int32(ret))
}

// reload 重新枚举设备并刷新列表。
func (a *app) reload() {
	keep := a.selectedRow()

	procSendMessageW.Call(uintptr(a.listHwnd), lvmDeleteAllItems, 0, 0)

	rows := make([]deviceRow, 0, 32)
	cpuName, _ := readCPUName()
	rows = append(rows, deviceRow{
		isCPU: true, kind: "CPU", label: "CPU",
		id: `CentralProcessor\0`, name: cpuName,
	})
	for _, d := range enumerateDevices() {
		rows = append(rows, deviceRow{
			kind: d.Kind, label: d.KindLabel, id: shortDeviceID(d.ID), name: d.Name, dev: d,
		})
	}

	a.rows = rows
	for i, r := range a.rows {
		a.insertRow(i, r)
	}

	a.subtitle = fmt.Sprintf("共 %d 项可修改 · 修改后请在设备管理器中刷新查看", len(rows))
	headerRect := a.rect(0, 0, winWidthDIP, headerHeightDIP)
	procInvalidateRect.Call(uintptr(a.hwnd), uintptr(unsafe.Pointer(&headerRect)), 0)

	a.lastSel = -1
	if len(a.rows) == 0 {
		return
	}
	if keep < 0 || keep >= len(a.rows) {
		keep = 0
	}
	a.lastSel = keep
	a.selectRow(keep)
	a.setEditText(a.rows[keep].name)
}

// refreshServiceButton 根据服务状态更新按钮文字。
func (a *app) refreshServiceButton() {
	a.installed = serviceInstalled()
	if a.installed {
		setWindowText(uintptr(a.btnService), "取消开机自动修改")
	} else {
		setWindowText(uintptr(a.btnService), "设置开机自动修改")
	}
	procInvalidateRect.Call(uintptr(a.btnService), 0, 1)
}

func wndProc(hwnd, uMsg, wParam uintptr, lParam unsafe.Pointer) uintptr {
	switch uMsg {
	case wmPaint:
		if currentApp != nil {
			currentApp.paintUI(hwnd)
			return 0
		}
	case wmEraseBkgnd:
		return 1
	case wmDrawItem:
		if currentApp != nil {
			currentApp.drawButton((*drawItemStruct)(lParam))
			return 1
		}
		return 0
	case wmCommand:
		if currentApp != nil {
			switch loword(wParam) {
			case idBtnApply:
				currentApp.onApply()
			case idBtnRestore:
				currentApp.onRestore()
			case idBtnService:
				currentApp.onToggleService()
			case idBtnRefresh:
				currentApp.reload()
				currentApp.refreshServiceButton()
			}
		}
		return 0
	case wmTimer:
		// 轮询列表选中项，比处理 LVN_ITEMCHANGED 通知更简单可靠
		if currentApp != nil {
			currentApp.onSelectionChanged()
		}
		return 0
	case wmClose:
		procDestroyWindow.Call(hwnd)
		return 0
	case wmDestroy:
		procPostQuitMessage.Call(0)
		return 0
	case wmCtlColorStatic:
		if currentApp != nil && currentApp.bgBrush != 0 {
			procSetBkMode.Call(wParam, 1) // TRANSPARENT
			return currentApp.bgBrush
		}
	}
	ret, _, _ := procDefWindowProcW.Call(hwnd, uMsg, wParam, uintptr(lParam))
	return ret
}

func loword(v uintptr) uintptr {
	return v & 0xffff
}

func (a *app) onSelectionChanged() {
	idx := a.selectedRow()
	if idx == a.lastSel {
		return
	}
	a.lastSel = idx
	if idx >= 0 && idx < len(a.rows) {
		a.setEditText(a.rows[idx].name)
	}
}

func (a *app) editText() string {
	n, _, _ := procGetWindowTextLen.Call(uintptr(a.editHwnd))
	if n == 0 {
		return ""
	}
	buf := make([]uint16, n+1)
	procGetWindowTextW.Call(uintptr(a.editHwnd), uintptr(unsafe.Pointer(&buf[0])), n+1)
	return syscall.UTF16ToString(buf)
}

func (a *app) setEditText(s string) {
	setWindowText(uintptr(a.editHwnd), s)
}

func setWindowText(hwnd uintptr, s string) {
	text := utf16Ptr(s)
	procSetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(text)))
	runtime.KeepAlive(text)
}

func (a *app) info(title, text string) {
	messageBox(uintptr(a.hwnd), title, text, mbOK|mbIconInformation)
}

func (a *app) warn(title, text string) {
	messageBox(uintptr(a.hwnd), title, text, mbOK|mbIconWarning)
}

func (a *app) fail(title, text string) {
	messageBox(uintptr(a.hwnd), title, text, mbOK|mbIconError)
}

// onApply 应用编辑框中的名称（立即写入注册表，并记入配置）。
func (a *app) onApply() {
	idx := a.selectedRow()
	if idx < 0 || idx >= len(a.rows) {
		a.warn("修改名称", "请先在设备列表中选择一项。")
		return
	}
	name := strings.TrimSpace(a.editText())
	if name == "" {
		a.warn("修改名称", "名称不能为空。")
		return
	}

	row := a.rows[idx]
	cfg := loadConfig()

	if row.isCPU {
		if cfg.CPUOriginal == "" {
			if cur, err := readCPUName(); err == nil {
				cfg.CPUOriginal = cur
			}
		}
		if err := applyCPUName(name); err != nil {
			a.fail("修改 CPU 名称", "修改失败：\n"+err.Error())
			return
		}
		cfg.CPU = name
	} else {
		entry, exists := cfg.findDevice(row.dev.ID)
		if !exists {
			friendly, driver, err := captureOriginals(row.dev)
			if err != nil {
				a.fail("修改设备名称", "读取原始名称失败：\n"+err.Error())
				return
			}
			entry = deviceOverride{
				ID:           row.dev.ID,
				Kind:         row.dev.Kind,
				OrigFriendly: friendly,
				OrigDriver:   driver,
			}
		}
		entry.Name = name
		if err := applyDeviceName(row.dev, name); err != nil {
			a.fail("修改设备名称", "修改失败：\n"+err.Error())
			return
		}
		cfg.setDevice(entry)
	}

	if err := saveConfig(cfg); err != nil {
		a.warn("保存配置", "名称已修改，但保存配置失败：\n"+err.Error())
	}

	a.reload()
	a.selectRow(idx)
	a.info("修改名称", "修改成功。\n设备管理器可点击“扫描检测硬件改动”或重新打开查看效果。")
}

// onRestore 恢复为修改前的名称。
func (a *app) onRestore() {
	idx := a.selectedRow()
	if idx < 0 || idx >= len(a.rows) {
		a.warn("恢复默认", "请先在设备列表中选择一项。")
		return
	}

	row := a.rows[idx]
	cfg := loadConfig()

	if row.isCPU {
		if strings.TrimSpace(cfg.CPUOriginal) == "" {
			a.warn("恢复默认", "没有记录到原始 CPU 名称，无法恢复。")
			return
		}
		if err := applyCPUName(cfg.CPUOriginal); err != nil {
			a.fail("恢复默认", "恢复失败：\n"+err.Error())
			return
		}
		cfg.CPU = ""
		cfg.CPUOriginal = ""
	} else {
		entry, ok := cfg.findDevice(row.dev.ID)
		if !ok {
			a.warn("恢复默认", "该设备没有自定义名称记录。")
			return
		}
		if err := restoreDeviceName(row.dev, entry.OrigFriendly, entry.OrigDriver); err != nil {
			a.fail("恢复默认", "恢复失败：\n"+err.Error())
			return
		}
		cfg.removeDevice(row.dev.ID)
	}

	if err := saveConfig(cfg); err != nil {
		a.warn("保存配置", "已恢复名称，但保存配置失败：\n"+err.Error())
	}

	a.reload()
	a.selectRow(idx)
	a.info("恢复默认", "已恢复为原始名称。")
}

// onToggleService 安装/卸载开机自动修改服务。
func (a *app) onToggleService() {
	var (
		result string
		err    error
	)

	if a.installed {
		result, err = uninstallService()
	} else {
		cfg := loadConfig()
		if cfg.empty() {
			a.warn("开机自动修改", "请先修改并应用名称，再设置开机自动修改。")
			return
		}
		if applyErr := applyConfig(cfg); applyErr != nil {
			a.warn("开机自动修改", "应用配置时出现问题：\n"+applyErr.Error())
		}
		result, err = installService()
	}

	if strings.TrimSpace(result) == "" {
		if err != nil {
			result = err.Error()
		} else {
			result = "操作完成"
		}
	}

	if err != nil {
		a.fail("修改服务结果", strings.TrimSpace(result))
	} else {
		a.info("修改服务结果", strings.TrimSpace(result))
	}

	a.reload()
	a.refreshServiceButton()
}
