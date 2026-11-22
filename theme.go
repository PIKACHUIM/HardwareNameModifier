package main

// 界面配色与 GDI 绘制工具：标题栏、圆角按钮、文字绘制。

import (
	"runtime"
	"syscall"
	"unsafe"
)

var (
	procBeginPaint     = modUser32.NewProc("BeginPaint")
	procEndPaint       = modUser32.NewProc("EndPaint")
	procGetClientRect  = modUser32.NewProc("GetClientRect")
	procFillRect       = modUser32.NewProc("FillRect")
	procDrawTextW      = modUser32.NewProc("DrawTextW")
	procInvalidateRect = modUser32.NewProc("InvalidateRect")

	procCreateSolidBrush = modGdi32.NewProc("CreateSolidBrush")
	procCreatePen        = modGdi32.NewProc("CreatePen")
	procSelectObject     = modGdi32.NewProc("SelectObject")
	procDeleteObject     = modGdi32.NewProc("DeleteObject")
	procRoundRect        = modGdi32.NewProc("RoundRect")
	procSetTextColor     = modGdi32.NewProc("SetTextColor")
	procGetStockObject   = modGdi32.NewProc("GetStockObject")
)

type paintStruct struct {
	hdc         syscall.Handle
	fErase      int32
	rcPaint     rectT
	fRestore    int32
	fIncUpdate  int32
	rgbReserved [32]byte
}

// drawItemStruct 对应 Win32 的 DRAWITEMSTRUCT（按钮自绘回调参数）。
type drawItemStruct struct {
	ctlType    uint32
	ctlID      uint32
	itemID     uint32
	itemAction uint32
	itemState  uint32
	hwndItem   syscall.Handle
	hDC        syscall.Handle
	rcItem     rectT
	itemData   uintptr
}

// 自绘按钮状态
const odsSelected = 0x0001

// DrawText 标志
const (
	dtLeft        = 0x0000
	dtCenter      = 0x0001
	dtRight       = 0x0002
	dtVCenter     = 0x0004
	dtSingleLine  = 0x0020
	dtEndEllipsis = 0x8000
)

// 颜色（COLORREF = 0x00BBGGRR）
func rgb(r, g, b uintptr) uintptr {
	return r | g<<8 | b<<16
}

var (
	colBg          = rgb(244, 246, 249) // 页面背景
	colCard        = rgb(255, 255, 255) // 卡片/按钮底色
	colBorder      = rgb(226, 230, 236) // 卡片描边
	colText        = rgb(23, 28, 38)    // 主文字
	colSubText     = rgb(120, 128, 142) // 次要文字
	colWhite       = rgb(255, 255, 255)
	colPrimary     = rgb(45, 106, 235) // 主按钮
	colPrimaryDown = rgb(33, 84, 200)
	colDanger      = rgb(214, 48, 49) // 危险操作文字
	colDangerLine  = rgb(240, 180, 180)
	colSecondDown  = rgb(238, 241, 245)
)

// uiFonts 保存界面字体。
type uiFonts struct {
	title    uintptr
	subtitle uintptr
	body     uintptr
	button   uintptr
}

func (a *app) createFontEx(sizeDIP, weight int) uintptr {
	face := utf16Ptr("Microsoft YaHei UI")
	font, _, _ := procCreateFontW.Call(
		uintptr(uint32(int32(-a.dip(sizeDIP)))),
		0, 0, 0,
		uintptr(weight),
		0, 0, 0,
		defaultCharset,
		0, 0,
		cleartypeQuality,
		0,
		uintptr(unsafe.Pointer(face)),
	)
	runtime.KeepAlive(face)
	return font
}

func (a *app) createFonts() {
	a.fonts = uiFonts{
		title:    a.createFontEx(20, 600),
		subtitle: a.createFontEx(13, 400),
		body:     a.createFontEx(15, 400),
		button:   a.createFontEx(15, 400),
	}
}

func fillRectColor(hdc uintptr, rc rectT, color uintptr) {
	brush, _, _ := procCreateSolidBrush.Call(color)
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&rc)), brush)
	procDeleteObject.Call(brush)
}

// drawRounded 画圆角矩形（border 为 0 时不描边）。
func drawRounded(hdc uintptr, rc rectT, radius int, fill, border uintptr) {
	brush, _, _ := procCreateSolidBrush.Call(fill)

	var pen uintptr
	if border == 0 {
		pen, _, _ = procGetStockObject.Call(8) // NULL_PEN
	} else {
		pen, _, _ = procCreatePen.Call(0, 1, border) // PS_SOLID
	}

	oldBrush, _, _ := procSelectObject.Call(hdc, brush)
	oldPen, _, _ := procSelectObject.Call(hdc, pen)
	d := uintptr(radius * 2)
	procRoundRect.Call(hdc,
		uintptr(rc.left), uintptr(rc.top), uintptr(rc.right), uintptr(rc.bottom), d, d)
	procSelectObject.Call(hdc, oldBrush)
	procSelectObject.Call(hdc, oldPen)

	procDeleteObject.Call(brush)
	if border != 0 {
		procDeleteObject.Call(pen)
	}
}

// drawTextIn 在矩形内绘制文字。
func drawTextIn(hdc uintptr, rc rectT, text string, color, font uintptr, flags uint32) {
	if font != 0 {
		procSelectObject.Call(hdc, font)
	}
	procSetBkMode.Call(hdc, 1) // TRANSPARENT
	procSetTextColor.Call(hdc, color)
	ptr := utf16Ptr(text)
	procDrawTextW.Call(hdc,
		uintptr(unsafe.Pointer(ptr)),
		^uintptr(0), // -1，字符串以 0 结尾
		uintptr(unsafe.Pointer(&rc)),
		uintptr(flags))
	runtime.KeepAlive(ptr)
}

// rect 返回一个矩形，参数为 DIP。
func (a *app) rect(x, y, w, h int) rectT {
	return rectT{
		left:   int32(a.dip(x)),
		top:    int32(a.dip(y)),
		right:  int32(a.dip(x + w)),
		bottom: int32(a.dip(y + h)),
	}
}

// paintUI 绘制整个窗口背景、顶部标题栏与列表边框。
func (a *app) paintUI(hwnd uintptr) {
	var ps paintStruct
	hdc, _, _ := procBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	if hdc == 0 {
		return
	}
	defer procEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))

	var client rectT
	procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&client)))

	// 页面背景
	fillRectColor(hdc, client, colBg)

	// 顶部标题栏
	header := a.rect(0, 0, winWidthDIP, headerHeightDIP)
	fillRectColor(hdc, header, colCard)
	fillRectColor(hdc, rectT{
		left:   header.left,
		top:    header.bottom - 1,
		right:  header.right,
		bottom: header.bottom,
	}, colBorder)

	drawTextIn(hdc, a.rect(24, 15, 420, 30), "硬件名称修改工具", colText, a.fonts.title,
		dtLeft|dtVCenter|dtSingleLine)
	drawTextIn(hdc, a.rect(24, 45, 560, 20), a.subtitle, colSubText,
		a.fonts.subtitle, dtLeft|dtVCenter|dtSingleLine|dtEndEllipsis)
	drawTextIn(hdc, a.rect(winWidthDIP-220, 15, 196, 30), "v"+buildVersion, colSubText,
		a.fonts.subtitle, dtRight|dtVCenter|dtSingleLine|dtEndEllipsis)

	// 列表边框
	borderRect := a.rect(listCardX, listCardY, listCardW, listCardH)
	drawRounded(hdc, borderRect, a.dip(6), colCard, colBorder)

	// 输入框边框（编辑框本身无边框，靠这里画出 1px 浅灰描边）
	editRect := a.rect(dipEditX-1, dipEditY-1, dipEditW+2, dipEditH+2)
	drawRounded(hdc, editRect, a.dip(5), colCard, colBorder)
}

// drawButton 自绘按钮。
func (a *app) drawButton(dis *drawItemStruct) {
	if dis == nil {
		return
	}
	hdc := uintptr(dis.hDC)
	rc := dis.rcItem
	pressed := dis.itemState&odsSelected != 0

	var fill, border, textColor uintptr
	textColor = colText
	switch dis.ctlID {
	case idBtnApply:
		fill, border, textColor = colPrimary, 0, colWhite
		if pressed {
			fill = colPrimaryDown
		}
	case idBtnService:
		fill, border = colCard, colBorder
		if a.installed {
			textColor, border = colDanger, colDangerLine
		}
		if pressed {
			fill = colSecondDown
		}
	default:
		fill, border = colCard, colBorder
		if pressed {
			fill = colSecondDown
		}
	}

	// 先铺一次页面背景，避免圆角外侧出现脏像素
	fillRectColor(hdc, rc, colBg)
	drawRounded(hdc, rc, a.dip(7), fill, border)
	drawTextIn(hdc, rc, windowText(dis.hwndItem), textColor, a.fonts.button,
		dtCenter|dtVCenter|dtSingleLine)
}

func windowText(hwnd syscall.Handle) string {
	n, _, _ := procGetWindowTextLen.Call(uintptr(hwnd))
	if n == 0 {
		return ""
	}
	buf := make([]uint16, n+1)
	procGetWindowTextW.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&buf[0])), n+1)
	return syscall.UTF16ToString(buf)
}
