package crossplatform

import (
	"fmt"
	"math"
	"time"
	"unsafe"

	"github.com/MarcosTypeAP/go-assert"
	"github.com/ebitengine/purego"
	rl "github.com/gen2brain/raylib-go/raylib"
)

func CStringToUTF8String(ptr *byte) string {
	assert.NotNil(ptr)
	data := unsafe.Slice(ptr, math.MaxInt64)

	n := 0
	for data[n] != 0 {
		n++
	}
	return string(data[:n])
}

const XKeycodeOffset = 8

func keycodeToKey(keycode uint8) Key {
	return Key(keycode - XKeycodeOffset)
}

func keyToKeycode(key Key) uint8 {
	return uint8(key + XKeycodeOffset)
}

const None = 0

const (
	RevertToNone        = 0
	RevertToPointerRoot = 1
	RevertToParent      = 2
)

const NoSymbol = 0

const (
	Success     = 0 // everything's okay
	BadRequest  = 1 // bad request code
	BadValue    = 2 // int parameter out of range
	BadWindow   = 3 // parameter not a Window
	BadPixmap   = 4 // parameter not a Pixmap
	BadAtom     = 5 // parameter not an Atom
	BadCursor   = 6 // parameter not a Cursor
	BadFont     = 7 // parameter not a Font
	BadMatch    = 8 // parameter mismatch
	BadDrawable = 9 // parameter not a Pixmap or Window
	// depending on context:
	// - key/button already grabbed
	// - attempt to free an illegal
	//   cmap entry
	// - attempt to store into a read-only
	//   color map entry.
	// - attempt to modify the access control
	//   list from other than the local host.
	BadAccess         = 10
	BadAlloc          = 11 // insufficient resources
	BadColor          = 12 // no such colormap
	BadGC             = 13 // parameter not a GC
	BadIDChoice       = 14 // choice not in range or already used
	BadName           = 15 // font or color name doesn't exist
	BadLength         = 16 // Request length incorrect
	BadImplementation = 17 // server is defective

	FirstExtensionError = 128
	LastExtensionError  = 255
)

const XI_RawKeyPress = 13
const XI_RawKeyRelease = 14

const XIAllMasterDevices = 1

const GenericEvent = 35

type Display struct{}

type Status = int32

type Window = uint64

type XIEventMask struct {
	deviceid int32
	mask_len int32
	mask     *byte
}

type XGenericEventCookie struct {
	type_      int32    /* of event. Always GenericEvent */
	serial     uint64   /* # of last request processed */
	send_event Bool     /* true if from SendEvent request */
	display    *Display /* Display the event was read from */
	extension  int32    /* major opcode of extension that caused the event */
	evtype     int32    /* actual event type. */
	cookie     uint32
	data       uintptr
}

type XEvent struct {
	xcookie XGenericEventCookie
	_pad    [136]byte
}

type Time = uint64

type XIValuatorState struct {
	mask_len int32
	mask     *byte
	values   *float64
}

type XIRawEvent struct {
	type_      int32    /* GenericEvent */
	serial     uint64   /* # of last request processed by server */
	send_event bool     /* true if this came from a SendEvent request */
	display    *Display /* Display the event was read from */
	extension  int32    /* XI extension offset */
	evtype     int32    /* XI_RawKeyPress, XI_RawKeyRelease, etc. */
	time       Time
	deviceid   int32
	sourceid   int32 /* Bug: Always 0. https://bugs.freedesktop.org//show_bug.cgi?id=34240 */
	detail     int32
	flags      int32
	valuators  XIValuatorState
	raw_values *float64
}

type Atom = uint64

type Bool = int32

type XClientMessageEvent struct {
	type_        int32
	serial       uint64   /* # of last request processed by server */
	send_event   Bool     /* true if this came from a SendEvent request */
	display      *Display /* Display the event was read from */
	window       Window
	message_type Atom
	format       int32
	_pad         [4]byte
	data         [40]byte
}

type KeyCode = uint8

type KeySym = uint64

func XISetMask(mask []byte, event byte) {
	mask[event>>3] |= 1 << (event & 7)
}

var XOpenDisplay func(name *byte) *Display
var XCloseDisplay func(dpy *Display) Status
var XDefaultRootWindow func(dpy *Display) Window
var XNextEvent func(dpy *Display, event_return *XEvent) int
var XPeekEvent func(dpy *Display, event_return *XEvent) int
var XPending func(dpy *Display) int32
var XGetEventData func(dpy *Display, cookie *XGenericEventCookie) bool
var XFreeEventData func(dpy *Display, cookie *XGenericEventCookie)
var XGetInputFocus func(dpy *Display, focus *Window, revert_to *int) int
var XkbKeycodeToKeysym func(dpy *Display, kc KeyCode, group, level int32) KeySym
var XKeysymToString func(keysym KeySym) *byte

var XIQueryVersion func(dpy *Display, major_version_input, minor_version_input *int32) Status
var XISelectEvents func(dpy *Display, window Window, masks *XIEventMask, num_masks int32) Status

const libX11SharedObjectPath = "/usr/lib/x86_64-linux-gnu/libX11.so.6"
const libXiSharedObjectPath = "/usr/lib/x86_64-linux-gnu/libXi.so.6"

var libX11 uintptr
var libXi uintptr

func LoadSharedLibs() error {
	assert.Equal(libX11, 0, "the shared libraries have already been loaded")

	var loadErr error
	defer func() {
		if loadErr == nil {
			return
		}
		if libX11 != 0 {
			purego.Dlclose(libX11)
			raylibTraceLog(rl.LogInfo, "Unloaded libX11")
		}
		if libXi != 0 {
			purego.Dlclose(libXi)
			raylibTraceLog(rl.LogInfo, "Unloaded libXi")
		}
	}()

	libX11, loadErr = purego.Dlopen(libX11SharedObjectPath, purego.RTLD_LAZY|purego.RTLD_LOCAL)
	if loadErr != nil {
		return fmt.Errorf("loading %s: %w", libX11SharedObjectPath, loadErr)
	}
	raylibTraceLog(rl.LogInfo, "Loaded libX11")

	purego.RegisterLibFunc(&XOpenDisplay, libX11, "XOpenDisplay")
	purego.RegisterLibFunc(&XCloseDisplay, libX11, "XCloseDisplay")
	purego.RegisterLibFunc(&XDefaultRootWindow, libX11, "XDefaultRootWindow")
	purego.RegisterLibFunc(&XNextEvent, libX11, "XNextEvent")
	purego.RegisterLibFunc(&XPeekEvent, libX11, "XPeekEvent")
	purego.RegisterLibFunc(&XPending, libX11, "XPending")
	purego.RegisterLibFunc(&XGetEventData, libX11, "XGetEventData")
	purego.RegisterLibFunc(&XFreeEventData, libX11, "XFreeEventData")
	purego.RegisterLibFunc(&XGetInputFocus, libX11, "XGetInputFocus")
	purego.RegisterLibFunc(&XkbKeycodeToKeysym, libX11, "XkbKeycodeToKeysym")
	purego.RegisterLibFunc(&XKeysymToString, libX11, "XKeysymToString")

	libXi, loadErr = purego.Dlopen(libXiSharedObjectPath, purego.RTLD_LAZY|purego.RTLD_LOCAL)
	if loadErr != nil {
		return fmt.Errorf("loading %s: %w", libXiSharedObjectPath, loadErr)
	}
	raylibTraceLog(rl.LogInfo, "Loaded libXi")

	purego.RegisterLibFunc(&XIQueryVersion, libXi, "XIQueryVersion")
	purego.RegisterLibFunc(&XISelectEvents, libXi, "XISelectEvents")

	return nil
}

func UnloadSharedLibs() {
	const errMsg = "the shared libraries have not been loaded"
	assert.NotEqual(libX11, 0, errMsg)
	assert.NotEqual(libXi, 0, errMsg)

	purego.Dlclose(libX11)
	raylibTraceLog(rl.LogInfo, "Unloaded libX11")

	purego.Dlclose(libXi)
	raylibTraceLog(rl.LogInfo, "Unloaded libXi")
}

var display *Display

var stopEventLoopCh = make(chan struct{})

func BeginListeningKeyPresses() (<-chan KeyEvent, error) {
	assert.Nil(display, "it is already listening")

	display = XOpenDisplay(nil)
	assert.NotNil(display, "cannot open display")
	raylibTraceLog(rl.LogInfo, "Opened X11 display")

	var major, minor int32 = 2, 0
	status := XIQueryVersion(display, &major, &minor)
	assert.Equal(status, Success, "XI2 not available")

	root := XDefaultRootWindow(display)

	maskData := make([]byte, (XI_RawKeyRelease>>3)+1) // big enough to set the (1 << XI_RawKeyRelease) bit
	XISetMask(maskData, XI_RawKeyPress)
	XISetMask(maskData, XI_RawKeyRelease)

	mask := XIEventMask{
		deviceid: XIAllMasterDevices,
		mask:     &maskData[0],
		mask_len: int32(len(maskData)),
	}

	status = XISelectEvents(display, root, &mask, 1)
	assert.Equal(status, Success)

	go func() {
		for {
			if XPending(display) > 0 {
				var ev XEvent
				XNextEvent(display, &ev)

				if ev.xcookie.type_ == GenericEvent {
					if XGetEventData(display, &ev.xcookie) {
						var re = (*XIRawEvent)(unsafe.Pointer(ev.xcookie.data))

						keyEvent := KeyEvent{
							Key:       keycodeToKey(uint8(re.detail & 0xFF)),
							IsPressed: re.evtype == XI_RawKeyPress,
						}

						if keyEvent.Key == KeySuperLeft {
							goto DropEvent
						}

						select {
						case keyEventsCh <- keyEvent:
						case <-stopEventLoopCh:
						}

					DropEvent:
						XFreeEventData(display, &ev.xcookie)
					}
				}
			}

			select {
			case <-stopEventLoopCh:
				return
			default:
			}

			time.Sleep(50 * time.Millisecond)
		}
	}()

	raylibTraceLog(rl.LogInfo, "Key listening initialized")
	return keyEventsCh, nil
}

func EndListeningKeyPresses() {
	assert.NotNil(display, "not listening")

	stopEventLoopCh <- struct{}{}

	status := XCloseDisplay(display)
	assert.Equal(status, Success)
	raylibTraceLog(rl.LogInfo, "Closed X11 display")

	raylibTraceLog(rl.LogInfo, "Key listening deinitialized")
}

func HasX11Focus() bool {
	assert.NotNil(display)

	var focus Window
	var revertTo int
	status := XGetInputFocus(display, &focus, &revertTo)
	assert.Equal(status, 1) // hardcoded
	return focus != None
}

func GetKeyName(key Key) string {
	assert.NotNil(display)
	keycode := keyToKeycode(key)

	keysym := XkbKeycodeToKeysym(display, keycode, 0, 0)
	if keysym == NoSymbol {
		return ""
	}

	cKeyname := XKeysymToString(keysym)
	if cKeyname == nil {
		return ""
	}
	return CStringToUTF8String(cKeyname)
}
