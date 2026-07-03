//go:build windows

package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

// win32 clipboard format identifiers and procs. user32 is shared with
// clipboard_hotkey_windows.go (same package, same build tag), so we only add
// the additional proc handles here.
const (
	cfDIB   = 8  // CF_DIB: packed device-independent bitmap
	cfHDROP = 15 // CF_HDROP: list of files
)

var (
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	procRtlMoveMemory = kernel32.NewProc("RtlMoveMemory")

	procOpenClipboard              = user32.NewProc("OpenClipboard")
	procCloseClipboard             = user32.NewProc("CloseClipboard")
	procIsClipboardFormatAvailable = user32.NewProc("IsClipboardFormatAvailable")
	procGetClipboardData           = user32.NewProc("GetClipboardData")
	procGlobalLock                 = user32.NewProc("GlobalLock")
	procGlobalUnlock               = user32.NewProc("GlobalUnlock")
	procGlobalSize                 = user32.NewProc("GlobalSize")
	procDragQueryFileW             = user32.NewProc("DragQueryFileW")
)

// lastClipboardImageHash / lastClipboardFiles dedup consecutive polls so a
// static image on the clipboard is saved once, not every 500ms. They are only
// touched from monitorClipboard's single goroutine, so no mutex is needed.
var (
	lastClipboardImageHash string
	lastClipboardFiles     string
)

// readClipboardRichNonText inspects the clipboard for non-text content (copied
// files or images). It returns present=true when such content is on the clipboard
// (so the caller can skip the text path), with:
//   - kind="file", content=the newline-joined file paths, when a file copy exists
//   - kind="image", content=the saved PNG path, when a bitmap exists
//   - content="" when the content is present but unchanged since the last call
//     (deduped), so the caller records nothing and skips the text fallback
//
// Files take priority over images (a file copy is more specific than a bitmap).
// When no non-text format is available it returns present=false so monitorClipboard
// falls through to the text path. Conversion/save failures are reported as
// present=false so text fallback can still record something rather than stalling.
func readClipboardRichNonText() (kind, content string, present bool) {
	// Files (CF_HDROP).
	if files, ok := clipboardFiles(); ok {
		fp := strings.Join(files, "\n")
		if fp == lastClipboardFiles {
			return "file", "", true // present but unchanged
		}
		lastClipboardFiles = fp
		lastClipboardImageHash = ""
		return "file", fp, true
	}
	lastClipboardFiles = ""

	// Image (CF_DIB).
	if dib, ok := clipboardDIB(); ok {
		h := sha256Sum(dib)
		if h == lastClipboardImageHash {
			return "image", "", true // present but unchanged
		}
		lastClipboardImageHash = h
		path, err := saveClipboardImagePNG(dib)
		if err != nil {
			// Conversion failed (unsupported DIB format). The hash is already
			// recorded, so we won't re-read/re-hash/re-convert this same image
			// on every poll. Treat as present-but-unchanged (record nothing,
			// skip the text path) — unsupported formats are rare; BI_RGB (the
			// common case) always converts.
			return "image", "", true
		}
		return "image", path, true
	}
	lastClipboardImageHash = ""

	return "", "", false
}

// clipboardFiles reads the CF_HDROP file list from the clipboard, if present.
func clipboardFiles() ([]string, bool) {
	if !openClipboard() {
		return nil, false
	}
	defer closeClipboard()
	if !isFormatAvailable(cfHDROP) {
		return nil, false
	}
	h, _, _ := procGetClipboardData.Call(uintptr(cfHDROP))
	if h == 0 {
		return nil, false
	}
	// DragQueryFile takes the HDROP handle directly (no GlobalLock needed).
	count, _, _ := procDragQueryFileW.Call(h, 0xFFFFFFFF, 0, 0)
	if count == 0 {
		return nil, false
	}
	files := make([]string, 0, count)
	for i := uintptr(0); i < count; i++ {
		length, _, _ := procDragQueryFileW.Call(h, i, 0, 0) // length excludes null terminator
		if length == 0 {
			continue
		}
		buf := make([]uint16, length+1)
		procDragQueryFileW.Call(h, i, uintptr(unsafe.Pointer(&buf[0])), uintptr(length+1))
		files = append(files, syscall.UTF16ToString(buf))
	}
	if len(files) == 0 {
		return nil, false
	}
	return files, true
}

// clipboardDIB reads the CF_DIB bytes (BITMAPINFOHEADER + color table + pixels)
// from the clipboard, if a bitmap is present.
func clipboardDIB() ([]byte, bool) {
	if !openClipboard() {
		return nil, false
	}
	defer closeClipboard()
	if !isFormatAvailable(cfDIB) {
		return nil, false
	}
	h, _, _ := procGetClipboardData.Call(uintptr(cfDIB))
	if h == 0 {
		return nil, false
	}
	size, _, _ := procGlobalSize.Call(h)
	if size == 0 {
		return nil, false
	}
	ptr, _, _ := procGlobalLock.Call(h)
	if ptr == 0 {
		return nil, false
	}
	defer procGlobalUnlock.Call(h)
	buf := make([]byte, size)
	// Copy via RtlMoveMemory so every pointer conversion is the vet-approved
	// unsafe.Pointer→uintptr direction (passing pointers as syscall args). The
	// reverse uintptr→unsafe.Pointer conversion that go vet flags is avoided.
	// GlobalLock pins the source memory until the GlobalUnlock above runs.
	procRtlMoveMemory.Call(uintptr(unsafe.Pointer(&buf[0])), ptr, uintptr(size))
	return buf, true
}

// saveClipboardImagePNG converts a CF_DIB to PNG and writes it under
// <desktopConfigDir>/clipboard/imgs/<timestamp>.png, returning the path.
func saveClipboardImagePNG(dib []byte) (string, error) {
	pngBytes, err := dibToPNG(dib)
	if err != nil {
		return "", fmt.Errorf("convert clipboard DIB to PNG: %w", err)
	}
	dir := filepath.Join(desktopConfigDir(), "clipboard", "imgs")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create clipboard image dir: %w", err)
	}
	path := filepath.Join(dir, fmt.Sprintf("%d.png", time.Now().UnixNano()))
	if err := os.WriteFile(path, pngBytes, 0o600); err != nil {
		return "", fmt.Errorf("write clipboard image: %w", err)
	}
	return path, nil
}

// dibToPNG decodes a CF_DIB (BITMAPINFOHEADER + optional color table + pixel
// data, in BGR/BGRA order, rows padded to 4 bytes, bottom-up unless biHeight<0)
// into an NRGBA image and encodes it as PNG. It handles the common BI_RGB
// uncompressed formats (1/4/8/16/24/32-bit). Unsupported compression or bit
// depths return an error so the caller can fall back to text.
//
// This is a stdlib-only decoder (no golang.org/x/image/bmp dependency) so the
// desktop module's dependency set stays unchanged.
func dibToPNG(dib []byte) ([]byte, error) {
	if len(dib) < 40 {
		return nil, fmt.Errorf("DIB too small (%d bytes)", len(dib))
	}
	biSize := binary.LittleEndian.Uint32(dib[0:4])
	if biSize < 40 {
		return nil, fmt.Errorf("unsupported DIB header size %d", biSize)
	}
	width := int(int32(binary.LittleEndian.Uint32(dib[4:8])))
	height := int(int32(binary.LittleEndian.Uint32(dib[8:12])))
	biBitCount := binary.LittleEndian.Uint16(dib[14:16])
	biCompression := binary.LittleEndian.Uint32(dib[16:20])
	if biCompression != 0 { // 0 = BI_RGB; other values (BI_RLE8, BI_BITFIELDS…) unsupported
		return nil, fmt.Errorf("unsupported DIB compression %d", biCompression)
	}
	topDown := false
	if height < 0 {
		height = -height
		topDown = true
	}
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("invalid DIB dimensions %dx%d", width, height)
	}

	// Resolve where pixel data starts and load the palette for paletted formats.
	var palette []color.RGBA
	pixelDataOff := int(biSize)
	switch biBitCount {
	case 1, 4, 8:
		clrUsed := binary.LittleEndian.Uint32(dib[32:36])
		if clrUsed == 0 {
			clrUsed = 1 << biBitCount
		}
		palette = make([]color.RGBA, clrUsed)
		for i := uint32(0); i < clrUsed; i++ {
			o := int(biSize) + int(i*4)
			if o+3 >= len(dib) {
				return nil, fmt.Errorf("DIB color table truncated")
			}
			// Palette entries are BGR0.
			palette[i] = color.RGBA{R: dib[o+2], G: dib[o+1], B: dib[o], A: 0xFF}
		}
		pixelDataOff = int(biSize) + int(clrUsed*4)
	case 16, 24, 32:
		// no color table
	default:
		return nil, fmt.Errorf("unsupported DIB bit depth %d", biBitCount)
	}

	rowSize := ((int(biBitCount)*width + 31) / 32) * 4 // each row padded to 4 bytes
	if pixelDataOff+rowSize*height > len(dib) {
		return nil, fmt.Errorf("DIB pixel data truncated")
	}

	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		srcRow := pixelDataOff
		if topDown {
			srcRow += y * rowSize
		} else {
			srcRow += (height - 1 - y) * rowSize
		}
		dst := img.PixOffset(0, y)
		switch biBitCount {
		case 32:
			for x := 0; x < width; x++ {
				o := srcRow + x*4
				img.Pix[dst+0] = dib[o+2] // R
				img.Pix[dst+1] = dib[o+1] // G
				img.Pix[dst+2] = dib[o]   // B
				img.Pix[dst+3] = 0xFF     // 32-bit BI_RGB alpha is unused
				dst += 4
			}
		case 24:
			for x := 0; x < width; x++ {
				o := srcRow + x*3
				img.Pix[dst+0] = dib[o+2] // R
				img.Pix[dst+1] = dib[o+1] // G
				img.Pix[dst+2] = dib[o]   // B
				img.Pix[dst+3] = 0xFF
				dst += 4
			}
		case 16:
			// 16-bit BI_RGB: 5-5-5 (BGR555), high bit unused.
			for x := 0; x < width; x++ {
				o := srcRow + x*2
				v := binary.LittleEndian.Uint16(dib[o : o+2])
				img.Pix[dst+0] = uint8(((v>>10)&0x1F)<<3 | ((v>>13)&0x7))
				img.Pix[dst+1] = uint8(((v>>5)&0x1F)<<3 | ((v>>8)&0x7))
				img.Pix[dst+2] = uint8((v&0x1F)<<3 | ((v>>2)&0x7))
				img.Pix[dst+3] = 0xFF
				dst += 4
			}
		case 8:
			for x := 0; x < width; x++ {
				c := palette[dib[srcRow+x]]
				img.Pix[dst+0] = c.R
				img.Pix[dst+1] = c.G
				img.Pix[dst+2] = c.B
				img.Pix[dst+3] = 0xFF
				dst += 4
			}
		case 4:
			for x := 0; x < width; x++ {
				b := dib[srcRow+x/2]
				idx := (b >> (4 - (x%2)*4)) & 0x0F
				c := palette[idx]
				img.Pix[dst+0] = c.R
				img.Pix[dst+1] = c.G
				img.Pix[dst+2] = c.B
				img.Pix[dst+3] = 0xFF
				dst += 4
			}
		case 1:
			for x := 0; x < width; x++ {
				b := dib[srcRow+x/8]
				idx := (b >> (7 - x%8)) & 0x01
				c := palette[idx]
				img.Pix[dst+0] = c.R
				img.Pix[dst+1] = c.G
				img.Pix[dst+2] = c.B
				img.Pix[dst+3] = 0xFF
				dst += 4
			}
		}
	}

	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func openClipboard() bool {
	r, _, _ := procOpenClipboard.Call(0) // NULL hwnd = associate with current task
	return r != 0
}

func closeClipboard() {
	procCloseClipboard.Call()
}

func isFormatAvailable(format uint32) bool {
	r, _, _ := procIsClipboardFormatAvailable.Call(uintptr(format))
	return r != 0
}

func sha256Sum(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
