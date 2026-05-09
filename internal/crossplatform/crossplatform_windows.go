package crossplatform

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"syscall"
	"unicode/utf16"
	"unsafe"

	"github.com/MarcosTypeAP/go-assert"
	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/gordonklaus/portaudio"
	"golang.org/x/sys/windows"
)

func GetClipboardImage() (*rl.Image, error) {
	image := rl.GetClipboardImage()
	if !rl.IsImageValid(&image) {
		clipboardText := rl.GetClipboardText()
		if filepath.VolumeName(clipboardText) != "" {
			image := rl.LoadImage(clipboardText)
			if !rl.IsImageValid(image) {
				return nil, fmt.Errorf("image format not supported: %s", clipboardText)
			}
			return image, nil
		}
		return nil, fmt.Errorf("no supported image found in clipboard (File Explorer does not copy the image, you should copy its path instead)")
	}
	return &image, nil
}

func HasX11Focus() bool {
	assert.Unreachable("should not be called")
	return false
}

const VirtualMicSinkName = "CABLE Input (VB-Audio Virtual C"

func GetVirtualMicSinkInfo() (sinkInfo *portaudio.DeviceInfo, needsDrivers bool, err error) {
	devices, err := portaudio.Devices()
	if err != nil {
		return nil, false, fmt.Errorf("getting devices: %w", err)
	}

	for _, d := range devices {
		if d.Name == VirtualMicSinkName {
			sinkInfo = d
		}
	}
	if sinkInfo == nil {
		return nil, true, nil
	}
	return sinkInfo, false, nil
}

func GetUserDevices() ([]AudioDevice, error) {
	devices, err := portaudio.Devices()
	if err != nil {
		return nil, fmt.Errorf("getting devices: %w", err)
	}

	defaultInput, err := portaudio.DefaultInputDevice()
	if err != nil {
		return nil, fmt.Errorf("getting default input: %w", err)
	}
	defaultOutput, err := portaudio.DefaultOutputDevice()
	if err != nil {
		return nil, fmt.Errorf("getting default output: %w", err)
	}

	out := make([]AudioDevice, 0, len(devices))

	for _, device := range devices {
		if strings.Contains(device.Name, "VB-Audio") {
			continue
		}

		audioDevice := AudioDevice{
			Info: device,
		}

		switch device {
		case defaultInput:
			audioDevice.Alias = AliasDefaultInput

		case defaultOutput:
			audioDevice.Alias = AliasDefaultOutput

		default:
			audioDevice.Alias = device.Name
		}

		out = append(out, audioDevice)
	}

	return out, nil
}

type OPENFILENAMEW struct {
	lStructSize       uint32
	hwndOwner         syscall.Handle
	hInstance         syscall.Handle
	lpstrFilter       *uint16
	lpstrCustomFilter *uint16
	nMaxCustFilter    uint32
	nFilterIndex      uint32
	lpstrFile         *uint16
	nMaxFile          uint32
	lpstrFileTitle    *uint16
	nMaxFileTitle     uint32
	lpstrInitialDir   *uint16
	lpstrTitle        *uint16
	Flags             uint32
	nFileOffset       uint16
	nFileExtension    uint16
	lpstrDefExt       *uint16
	lCustData         uintptr
	lpfnHook          syscall.Handle
	lpTemplateName    *uint16
	pvReserved        unsafe.Pointer
	dwReserved        uint32
	FlagsEx           uint32
}

var comdlg32DLL = windows.NewLazySystemDLL("comdlg32.dll")
var getOpenFileNameW = comdlg32DLL.NewProc("GetOpenFileNameW")

// fileter = "PATTERN1;PATTERN2 ..."
// fileter = "NAME | PATTERN1;PATTERN2 ..."
func OpenFileDialog(title string, filters ...string) (filePath string, ok bool, err error) {
	filePathBuf := make([]uint16, 260) // 260 = MAX_PATH

	// const filterText = "MP3 Files (*.mp3)\x00*.mp3\x00" + "All Files (*.*)\x00*.*\x00"
	filterText := ""
	for _, filter := range filters {
		if name, patterns, found := strings.Cut(filter, "|"); found {
			filterText += strings.TrimSpace(name) + "\x00" + strings.TrimSpace(patterns) + "\x00"
		} else {
			filterText += filter + "\x00" + filter + "\x00"
		}
	}
	filterUTF16 := utf16.Encode([]rune(filterText))

	dialogTitleUTF16, _ := syscall.UTF16PtrFromString("Select Source Code")

	const (
		OFN_EXPLORER        = 0x00080000
		OFN_FILEMUSTEXIST   = 0x00001000
		OFN_HIDEREADONLY    = 0x00000004
		OFN_NONETWORKBUTTON = 0x00020000
	)

	ofn := OPENFILENAMEW{
		lStructSize:  uint32(unsafe.Sizeof(OPENFILENAMEW{})),
		lpstrFile:    &filePathBuf[0],
		nMaxFile:     uint32(len(filePathBuf)),
		lpstrFilter:  &filterUTF16[0],
		nFilterIndex: 1, // 1-indexed
		lpstrTitle:   dialogTitleUTF16,
		Flags:        OFN_EXPLORER | OFN_FILEMUSTEXIST | OFN_HIDEREADONLY | OFN_NONETWORKBUTTON,
	}

	ret, _, err := getOpenFileNameW.Call(uintptr(unsafe.Pointer(&ofn)))
	if ret == 0 {
		var errno windows.Errno
		if errors.As(err, &errno); errno != windows.Errno(0) {
			return "", false, fmt.Errorf("opening file dialog: errno: %w", err)
		}
		return "", false, nil
	}
	filePath = strings.TrimSpace(syscall.UTF16ToString(filePathBuf))
	return filePath, true, nil
}
