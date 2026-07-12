//go:build windows

package main

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"syscall"
	"unsafe"
)

// Windows constants
const (
	srccopy         = 0x00CC0020
	dibRGBColors    = 0
	biRGB           = 0
	smCXScreen      = 0
	smCYScreen      = 1
	smXVirtualScreen = 76
	smYVirtualScreen = 77
	smCXVirtualScreen = 78
	smCYVirtualScreen = 79
)

var (
	user32 = syscall.NewLazyDLL("user32.dll")
	gdi32  = syscall.NewLazyDLL("gdi32.dll")

	procGetDC            = user32.NewProc("GetDC")
	procReleaseDC        = user32.NewProc("ReleaseDC")
	procGetSystemMetrics = user32.NewProc("GetSystemMetrics")

	procCreateCompatibleDC  = gdi32.NewProc("CreateCompatibleDC")
	procCreateCompatibleBitmap = gdi32.NewProc("CreateCompatibleBitmap")
	procSelectObject        = gdi32.NewProc("SelectObject")
	procDeleteObject        = gdi32.NewProc("DeleteObject")
	procDeleteDC            = gdi32.NewProc("DeleteDC")
	procBitBlt              = gdi32.NewProc("BitBlt")
	procGetDIBits           = gdi32.NewProc("GetDIBits")
)

// bitmapInfoHeader mirrors Windows BITMAPINFOHEADER. It is also the first
// field of BITMAPINFO, so a pointer to this struct serves as LPBITMAPINFO
// when the color table is unused (32-bit DIBs).
type bitmapInfoHeader struct {
	BiSize          uint32
	BiWidth         int32
	BiHeight        int32
	BiPlanes        uint16
	BiBitCount      uint16
	BiCompression   uint32
	BiSizeImage     uint32
	BiXPelsPerMeter int32
	BiYPelsPerMeter int32
	BiClrUsed       uint32
	BiClrImportant  uint32
}

// screenshotDisplay captures the given display (or a sub-region of it) and
// returns the PNG-encoded bytes plus the effective width/height.
func screenshotDisplay(display int, region *rect) ([]byte, int, int, error) {
	var hdc, originX, originY, screenW, screenH uintptr

	if display == 0 {
		// Primary monitor.
		hdc = getDC(0)
		screenW = getSystemMetrics(smCXScreen)
		screenH = getSystemMetrics(smCYScreen)
		originX, originY = 0, 0
	} else {
		// Virtual screen (all monitors combined). CreateDCW("DISPLAY")
		// would also work, but GetDC(0) on the virtual screen coordinates
		// covers non-primary monitors when BitBlt source uses virtual origin.
		hdc = getDC(0)
		originX = getSystemMetrics(smXVirtualScreen)
		originY = getSystemMetrics(smYVirtualScreen)
		screenW = getSystemMetrics(smCXVirtualScreen)
		screenH = getSystemMetrics(smCYVirtualScreen)
	}
	if hdc == 0 {
		return nil, 0, 0, fmt.Errorf("GetDC failed")
	}
	defer releaseDC(0, hdc)

	capX, capY, capW, capH := int(originX), int(originY), int(screenW), int(screenH)
	if region != nil {
		capX = int(originX) + region.X
		capY = int(originY) + region.Y
		capW = region.W
		capH = region.H
	}
	if capW <= 0 || capH <= 0 {
		return nil, 0, 0, fmt.Errorf("invalid capture dimensions %dx%d", capW, capH)
	}

	hdcMem := createCompatibleDC(hdc)
	if hdcMem == 0 {
		return nil, 0, 0, fmt.Errorf("CreateCompatibleDC failed")
	}
	defer deleteDC(hdcMem)

	hBmp := createCompatibleBitmap(hdc, capW, capH)
	if hBmp == 0 {
		return nil, 0, 0, fmt.Errorf("CreateCompatibleBitmap failed")
	}
	defer deleteObject(hBmp)

	hOld := selectObject(hdcMem, hBmp)
	if hOld == 0 {
		return nil, 0, 0, fmt.Errorf("SelectObject failed")
	}
	defer selectObject(hdcMem, hOld)

	// BitBlt from screen DC (at capX, capY) into memory DC (0,0).
	ok := bitBlt(hdcMem, 0, 0, capW, capH, hdc, capX, capY, srccopy)
	if !ok {
		return nil, 0, 0, fmt.Errorf("BitBlt failed")
	}

	// Pull pixels as top-down 32-bit BGRA.
	bmi := bitmapInfoHeader{
		BiSize:        uint32(unsafe.Sizeof(bitmapInfoHeader{})),
		BiWidth:       int32(capW),
		BiHeight:      int32(-capH), // negative => top-down
		BiPlanes:      1,
		BiBitCount:    32,
		BiCompression: biRGB,
		BiSizeImage:   uint32(capW * capH * 4),
	}
	buf := make([]byte, capW*capH*4)
	ret, _, _ := procGetDIBits.Call(
		uintptr(hdcMem),
		uintptr(hBmp),
		0,
		uintptr(capH),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&bmi)),
		uintptr(dibRGBColors),
	)
	if ret == 0 {
		return nil, 0, 0, fmt.Errorf("GetDIBits failed")
	}

	// Convert BGRA -> RGBA for image.RGBA.
	rgba := image.NewRGBA(image.Rect(0, 0, capW, capH))
	for i := 0; i < capW*capH; i++ {
		b := buf[i*4+0]
		g := buf[i*4+1]
		r := buf[i*4+2]
		// buf[i*4+3] is reserved (alpha of DIB), force opaque.
		rgba.Pix[i*4+0] = r
		rgba.Pix[i*4+1] = g
		rgba.Pix[i*4+2] = b
		rgba.Pix[i*4+3] = 0xFF
	}

	var out bytes.Buffer
	if err := png.Encode(&out, rgba); err != nil {
		return nil, 0, 0, fmt.Errorf("png encode: %w", err)
	}
	return out.Bytes(), capW, capH, nil
}

// --- thin syscall wrappers ---

func getDC(hwnd uintptr) uintptr {
	r, _, _ := procGetDC.Call(hwnd)
	return r
}

func releaseDC(hwnd, hdc uintptr) {
	procReleaseDC.Call(hwnd, hdc)
}

func getSystemMetrics(idx int) uintptr {
	r, _, _ := procGetSystemMetrics.Call(uintptr(idx))
	return r
}

func createCompatibleDC(hdc uintptr) uintptr {
	r, _, _ := procCreateCompatibleDC.Call(hdc)
	return r
}

func createCompatibleBitmap(hdc uintptr, w, h int) uintptr {
	r, _, _ := procCreateCompatibleBitmap.Call(hdc, uintptr(w), uintptr(h))
	return r
}

func selectObject(hdc, hgdiobj uintptr) uintptr {
	r, _, _ := procSelectObject.Call(hdc, hgdiobj)
	return r
}

func deleteObject(hgdiobj uintptr) {
	procDeleteObject.Call(hgdiobj)
}

func deleteDC(hdc uintptr) {
	procDeleteDC.Call(hdc)
}

func bitBlt(hdcDst uintptr, xDst, yDst, w, h int, hdcSrc uintptr, xSrc, ySrc int, rop uint32) bool {
	r, _, _ := procBitBlt.Call(
		hdcDst,
		uintptr(xDst), uintptr(yDst),
		uintptr(w), uintptr(h),
		hdcSrc,
		uintptr(xSrc), uintptr(ySrc),
		uintptr(rop),
	)
	return r != 0
}
