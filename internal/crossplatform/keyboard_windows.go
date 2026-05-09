package crossplatform

import (
	"fmt"
	"runtime"
	"syscall"
	"unsafe"

	"github.com/MarcosTypeAP/go-assert"
	rl "github.com/gen2brain/raylib-go/raylib"
	"golang.org/x/sys/windows"
)

var (
	user32                  = windows.NewLazySystemDLL("user32.dll")
	procSetWindowsHookEx    = user32.NewProc("SetWindowsHookExW")
	procCallNextHookEx      = user32.NewProc("CallNextHookEx")
	procUnhookWindowsHookEx = user32.NewProc("UnhookWindowsHookEx")
	procGetMessage          = user32.NewProc("GetMessageW")
	procPostThreadMessage   = user32.NewProc("PostThreadMessageW")
	procGetKeyNameText      = user32.NewProc("GetKeyNameTextW")
	// procGetMessageExtraInfo = user32.NewProc("GetMessageExtraInfo")
)

const (
	WHKeyboard   = 2
	WHKeyboardLL = 13

	WMKeydown    = 256
	WMKeyup      = 257
	WMSysKeydown = 260
	WMSysKeyup   = 261

	WMQuit = 18

	LLKHFExtended = 1
	LLKHFUp       = 128

	PMRemove = 1
)

type KBDLLHOOKSTRUCT struct {
	vkCode      uint32
	scanCode    uint32
	flags       uint32
	time        uint32
	dwExtraInfo uintptr
}

type MSG struct {
	hwnd     uintptr
	message  uint32
	wParam   uintptr
	lParam   uintptr
	time     uint32
	pt       struct{ x, y int32 }
	lPrivate uint32
}

func LoadSharedLibs() error { return nil }

func UnloadSharedLibs() {}

var hook uintptr

var _keyPressed [256]bool

var hookProc = syscall.NewCallback(func(nCode int, wParam uintptr, lParam uintptr) uintptr {
	if nCode >= 0 && (wParam == WMKeydown || wParam == WMKeyup) {
		kbd := (*KBDLLHOOKSTRUCT)(unsafe.Pointer(lParam))
		isPressed := kbd.flags&LLKHFUp != LLKHFUp

		key := scanCodeToKey(uint8(kbd.scanCode), kbd.flags&LLKHFExtended == LLKHFExtended)

		if isPressed && _keyPressed[key] {
			goto DropEvent
		}
		_keyPressed[key] = isPressed

		if key == KeySuperLeft {
			goto DropEvent
		}

		keyEventsCh <- KeyEvent{
			Key:       key,
			IsPressed: isPressed,
		}
	}

DropEvent:
	ret, _, _ := procCallNextHookEx.Call(0, uintptr(nCode), wParam, lParam)
	return ret
})

var msgLoopThreadID uint32

func BeginListeningKeyPresses() (<-chan KeyEvent, error) {
	assert.Equal(hook, 0, "it is already listening")

	ready := make(chan struct{})

	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		var err error
		hook, _, err = procSetWindowsHookEx.Call(WHKeyboardLL, hookProc, 0, 0)
		assert.NotEqual(hook, 0, err)

		msgLoopThreadID = windows.GetCurrentThreadId()
		close(ready)

		for {
			var msg MSG
			ret, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
			if ret == 0 { // WM_QUIT
				return
			}
			if int32(ret) == -1 { // the return type is a BOOL, in other words, an int32, according to Microsoft
				raylibTraceLog(rl.LogError, fmt.Sprintf("procGetMessage() failed: %s", windows.GetLastError()))
				return
			}
		}
	}()

	<-ready

	raylibTraceLog(rl.LogInfo, "Key listening initialized")
	return keyEventsCh, nil
}

func EndListeningKeyPresses() {
	assert.NotEqual(hook, 0, "not listening")

	procPostThreadMessage.Call(uintptr(msgLoopThreadID), WMQuit, 0, 0)

	for len(keyEventsCh) > 0 {
		select {
		case <-keyEventsCh:
		default:
		}
	}

	ret, _, err := procUnhookWindowsHookEx.Call(hook)
	assert.NotEqual(ret, 0, err)
	hook = 0

	raylibTraceLog(rl.LogInfo, "Key listening deinitialized")
}

func GetKeyName(key Key) string {
	if !key.IsValid() {
		return ""
	}

	scanCode, extended := keyToScanCode(key)

	var lparam = uintptr(scanCode) << 16
	if extended {
		lparam |= LLKHFExtended << 24
	}

	keynameBuf := make([]uint16, 32)
	nameLen, _, _ := procGetKeyNameText.Call(lparam, uintptr(unsafe.Pointer(&keynameBuf[0])), uintptr(len(keynameBuf)))
	if nameLen == 0 {
		return ""
	} else {
		return syscall.UTF16ToString(keynameBuf)
	}
}

func scanCodeToKey(scanCode uint8, extended bool) Key {
	if scanCode>>7 == 1 {
		return 0 // too crazy key
	}
	if extended {
		scanCode |= FlagExtended
	}
	return scanCodeToKeyMapping[scanCode]
}

const FlagExtended = 1 << 7

func keyToScanCode(key Key) (scanCode uint8, extended bool) {
	return keyToScanCodeMapping[key&^FlagExtended], key&FlagExtended == FlagExtended
}

var scanCodeToKeyMapping = [256]Key{
	1:  KeyEscape,
	2:  2,
	3:  3,
	4:  4,
	5:  5,
	6:  6,
	7:  7,
	8:  8,
	9:  9,
	10: 10,
	11: 11,
	12: 12,
	13: 13,
	14: 14,
	15: 15,
	16: 16,
	17: 17,
	18: 18,
	19: 19,
	20: 20,
	21: 21,
	22: 22,
	23: 23,
	24: 24,
	25: 25,
	26: 26,
	27: 27,
	28: 28,
	29: 29,
	30: 30,
	31: 31,
	32: 32,
	33: 33,
	34: 34,
	35: 35,
	36: 36,
	37: 37,
	38: 38,
	39: 39,
	40: 40,
	41: 41,
	42: 42,
	43: 43,
	44: 44,
	45: 45,
	46: 46,
	47: 47,
	48: 48,
	49: 49,
	50: 50,
	51: 51,
	52: 52,
	53: 53,
	//
	55: 55,
	56: 56,
	57: 57,
	58: 0, // no caps lock
	59: 59,
	60: 60,
	61: 61,
	62: 62,
	63: 63,
	64: 64,
	65: 65,
	66: 66,
	67: 67,
	68: 68,
	69: KeyPause,
	70: 70,
	71: 71,
	72: 72,
	73: 73,
	74: 74,
	75: 75,
	76: 76,
	77: 77,
	78: 78,
	79: 79,
	80: 80,
	81: 81,
	82: 82,
	83: 83,
	//
	86: 86,
	87: 87,
	88: 88,

	// Extended
	FlagExtended | 28: KeyKPEnter,
	FlagExtended | 29: KeyControlRight,
	//
	FlagExtended | 53: KeyKPDivide,
	FlagExtended | 54: 54,
	FlagExtended | 55: KeyPrint,
	FlagExtended | 56: KeyAltRight,
	//
	FlagExtended | 69: 0,
	//
	FlagExtended | 71: KeyHome,
	FlagExtended | 72: KeyUp,
	FlagExtended | 73: KeyPrior,
	//
	FlagExtended | 75: KeyLeft,
	//
	FlagExtended | 77: KeyRight,
	//
	FlagExtended | 79: KeyEnd,
	FlagExtended | 80: KeyDown,
	FlagExtended | 81: KeyNext,
	FlagExtended | 82: KeyInsert,
	FlagExtended | 83: KeyDelete,
	//
	FlagExtended | 91: KeySuperLeft,
	FlagExtended | 92: KeySuperRight,
	FlagExtended | 93: KeyMenu,
}

var keyToScanCodeMapping = [256]uint8{
	1:  1,
	2:  2,
	3:  3,
	4:  4,
	5:  5,
	6:  6,
	7:  7,
	8:  8,
	9:  9,
	10: 10,
	11: 11,
	12: 12,
	13: 13,
	14: 14,
	15: 15,
	16: 16,
	17: 17,
	18: 18,
	19: 19,
	20: 20,
	21: 21,
	22: 22,
	23: 23,
	24: 24,
	25: 25,
	26: 26,
	27: 27,
	28: 28,
	29: 29,
	30: 30,
	31: 31,
	32: 32,
	33: 33,
	34: 34,
	35: 35,
	36: 36,
	37: 37,
	38: 38,
	39: 39,
	40: 40,
	41: 41,
	42: 42,
	43: 43,
	44: 44,
	45: 45,
	46: 46,
	47: 47,
	48: 48,
	49: 49,
	50: 50,
	51: 51,
	52: 52,
	53: 53,
	//
	55: 55,
	56: 56,
	57: 57,
	// 0:     58, // no caps lock, already removed
	59:       59,
	60:       60,
	61:       61,
	62:       62,
	63:       63,
	64:       64,
	65:       65,
	66:       66,
	67:       67,
	68:       68,
	KeyPause: 69,
	70:       70,
	71:       71,
	72:       72,
	73:       73,
	74:       74,
	75:       75,
	76:       76,
	77:       77,
	78:       78,
	79:       79,
	80:       80,
	81:       81,
	82:       82,
	83:       83,
	//
	86: 86,
	87: 87,
	88: 88,

	// Extended
	KeyKPEnter:      28 | FlagExtended,
	KeyControlRight: 29 | FlagExtended,
	//
	KeyKPDivide: 53 | FlagExtended,

	// GetKeyNameTextW() is too stupid to understand its own scancodes,
	// so the extended flag of the right shift must be removed to avoid an error.
	54: 54,

	KeyPrint:    55 | FlagExtended,
	KeyAltRight: 56 | FlagExtended,
	//
	// 0: 69 | FlagExtended, // already removed
	//
	KeyHome:  71 | FlagExtended,
	KeyUp:    72 | FlagExtended,
	KeyPrior: 73 | FlagExtended,
	//
	KeyLeft: 75 | FlagExtended,
	//
	KeyRight: 77 | FlagExtended,
	//
	KeyEnd:    79 | FlagExtended,
	KeyDown:   80 | FlagExtended,
	KeyNext:   81 | FlagExtended,
	KeyInsert: 82 | FlagExtended,
	KeyDelete: 83 | FlagExtended,
	//
	KeySuperLeft:  91 | FlagExtended,
	KeySuperRight: 92 | FlagExtended,
	KeyMenu:       93 | FlagExtended,
}
