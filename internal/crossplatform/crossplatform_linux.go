package crossplatform

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/MarcosTypeAP/go-assert"
	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/gordonklaus/portaudio"
)

func GetClipboardImage() (*rl.Image, error) {
	var getClipboardImage func(mimetype string) ([]byte, error)

	switch {
	case os.Getenv("WAYLAND_DISPLAY") != "":
		getClipboardImage = getClipboardImageWayland

	case os.Getenv("DISPLAY") != "":
		getClipboardImage = getClipboardImageX11

	default:
		return nil, fmt.Errorf("missing $DISPLAY and $WAYLAND_DISPLAY environment variables")
	}

	{
		data, err := getClipboardImage("image/png")
		if err != nil {
			return nil, err
		}
		if data != nil {
			image := rl.LoadImageFromMemory(".png", data, int32(len(data)))
			if !rl.IsImageValid(image) {
				return nil, fmt.Errorf("invalid PNG image")
			}
			return image, nil
		}
	}

	{
		data, err := getClipboardImage("image/jpg")
		if err != nil {
			return nil, err
		}
		if data != nil {
			image := rl.LoadImageFromMemory(".jpg", data, int32(len(data)))
			if !rl.IsImageValid(image) {
				return nil, fmt.Errorf("invalid JPG image")
			}
			return image, nil
		}
	}

	clipboardText := rl.GetClipboardText()
	if filepath.VolumeName(clipboardText) != "" {
		image := rl.LoadImage(clipboardText)
		if !rl.IsImageValid(image) {
			return nil, fmt.Errorf("unsupported image file: %s", clipboardText)
		}
		return image, nil
	}

	return nil, fmt.Errorf("no supported image found in clipboard")
}

func getClipboardImageX11(mimetype string) ([]byte, error) {
	xclipAbsPath, err := exec.LookPath("xclip")
	if err != nil {
		return nil, fmt.Errorf(`missing dependency "xclip" (can be installed in debian-based systems with "apt install xclip")`)
	}

	var stdout, stderr bytes.Buffer

	cmd := exec.Command(xclipAbsPath, "-selection", "clipboard", "-target", mimetype, "-out")
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	if err != nil {
		stderrString := stderr.String()
		if strings.Contains(stderrString, "target "+mimetype+" not available") {
			return nil, nil
		}
		rl.TraceLog(rl.LogError, stderrString)
		return nil, fmt.Errorf("pasting image: %w", err)
	}
	return stdout.Bytes(), nil
}

func getClipboardImageWayland(mimetype string) ([]byte, error) {
	wlpasteAbsPath, err := exec.LookPath("wl-paste")
	if err != nil {
		return nil, fmt.Errorf(`missing dependency "wl-paste" (can be installed in debian-based systems with "apt install wl-clipboard")`)
	}

	var stdout, stderr bytes.Buffer

	cmd := exec.Command(wlpasteAbsPath, "--type", mimetype)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	if err != nil {
		stderrString := stderr.String()
		if strings.Contains(stderrString, `Clipboard content is not available as requested type "`+mimetype+`"`) {
			return nil, nil
		}
		rl.TraceLog(rl.LogError, stderrString)
		return nil, fmt.Errorf("pasting image: %w", err)
	}
	return stdout.Bytes(), nil
}

const VirtualMicSinkName = "SoundpadSink"
const VirtualMicSinkMonitorName = "SoundpadSink.monitor"
const VirtualMicSourceName = "SoundpadMic"

func GetVirtualMicSinkInfo() (sinkInfo *portaudio.DeviceInfo, needsDrivers bool, err error) {
	sinkInfo, err = getDeviceByName(VirtualMicSinkName)
	if err != nil {
		return nil, false, fmt.Errorf("getting devices: %w", err)
	}

	if sinkInfo == nil {
		if err := createVirtualMic(); err != nil {
			return nil, false, fmt.Errorf("creating virtual microphone: %w", err)
		}

		if sinkInfo, err = getDeviceByName(VirtualMicSinkName); err != nil {
			return nil, false, fmt.Errorf("getting virtual microphone: %w", err)
		}
	}

	assert.NotNil(sinkInfo)
	return sinkInfo, false, nil
}

func GetVirtualMicSourceInfo() (sourceInfo *portaudio.DeviceInfo, needsDrivers bool, err error) {
	sourceInfo, err = getDeviceByName(VirtualMicSinkMonitorName)
	if err != nil {
		return nil, false, fmt.Errorf("getting virtual microphone monitor: %w", err)
	}

	if sourceInfo == nil {
		if err := createVirtualMic(); err != nil {
			return nil, false, fmt.Errorf("creating virtual microphone: %w", err)
		}

		if sourceInfo, err = getDeviceByName(VirtualMicSinkMonitorName); err != nil {
			return nil, false, fmt.Errorf("getting virtual microphone monitor: %w", err)
		}
	}

	assert.NotNil(sourceInfo)
	return sourceInfo, false, nil
}

var pulseaudioNameRegex = regexp.MustCompile(`^[^\.]+\.(.+)(\.[^\.]+)+$`)
var pulseaudioUsbNameRegex = regexp.MustCompile(`^[^\.]+\.usb-(.+)-\d\d(\.[^\.]+)+$`)
var pulseaudioBluezMacRegex = regexp.MustCompile(`^bluez_\w+\.(\w\w_\w\w_\w\w_\w\w_\w\w_\w\w)(\.[^\.]+)+$`)

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
		if strings.HasSuffix(device.Name, ".monitor") || strings.Contains(device.Name, "Soundpad") {
			continue
		}

		audioDevice := AudioDevice{
			Info: device,
		}

		if device == defaultInput {
			audioDevice.Alias = AliasDefaultInput

		} else if device == defaultOutput {
			audioDevice.Alias = AliasDefaultOutput

		} else if matches := pulseaudioUsbNameRegex.FindSubmatch([]byte(device.Name)); matches != nil {
			name := matches[1]
			for i, char := range name {
				if char == '-' || char == '_' || char == '.' {
					name[i] = ' '
				}
			}
			audioDevice.Alias = string(name)

		} else if matches := pulseaudioBluezMacRegex.FindStringSubmatch(device.Name); matches != nil {
			mac := matches[1]
			name, err := queryBluetoothDeviceNameByMac(mac)
			if err != nil {
				return nil, fmt.Errorf("querying bluetooth device name: %w", err)
			}
			audioDevice.Alias = name

		} else if matches := pulseaudioNameRegex.FindSubmatch([]byte(device.Name)); matches != nil {
			name := matches[1]
			for i, char := range name {
				if char == '-' || char == '_' || char == '.' {
					name[i] = ' '
				}
			}
			audioDevice.Alias = string(name)

		} else {
			audioDevice.Alias = device.Name
		}

		out = append(out, audioDevice)
	}

	return out, nil
}

var bluetoothctlNameRegex = regexp.MustCompile(`\s+Name: ([^\n]+)\n`)
var bluetoothMacToNameCache = make(map[string]string)

func queryBluetoothDeviceNameByMac(mac string) (string, error) {
	if name, ok := bluetoothMacToNameCache[mac]; ok {
		return name, nil
	}

	bluetoothctlAbsPath, err := exec.LookPath("bluetoothctl")
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			raylibTraceLog(rl.LogWarning, "Missing optional dependency: bluetoothctl (bluez)")
			return "", nil
		}
		return "", err
	}

	var stdout, stderr bytes.Buffer

	cmd := exec.Command(bluetoothctlAbsPath, "info", strings.ReplaceAll(mac, "_", ":"))
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	if err != nil {
		rl.TraceLog(rl.LogError, stderr.String())
		return "", fmt.Errorf("looking up mac: %w", err)
	}

	var name string
	if matches := bluetoothctlNameRegex.FindStringSubmatch(stdout.String()); matches != nil {
		name = matches[1]
	}

	bluetoothMacToNameCache[mac] = name
	return name, nil
}

func getDeviceByName(name string) (*portaudio.DeviceInfo, error) {
	devices, err := portaudio.Devices()
	if err != nil {
		return nil, fmt.Errorf("getting devices: %w", err)
	}

	for _, d := range devices {
		if d.Name == name {
			return d, nil
		}
	}
	return nil, nil
}

func createVirtualMic() error {
	pactlAbsPath, err := exec.LookPath("pactl")
	if err != nil {
		return fmt.Errorf(`missing dependency "pactl" (can be installed in debian-based systems with "apt install pulseaudio-utils")`)
	}

	cmd := exec.Command(
		pactlAbsPath,
		"load-module",
		"module-null-sink",
		"sink_name='"+VirtualMicSinkName+"'",
		"sink_properties=device.description='Soundpad Virtual Sink'",
	)
	err = cmd.Run()
	if err != nil {
		return fmt.Errorf("creating sink: %w", err)
	}

	cmd = exec.Command(
		pactlAbsPath,
		"load-module",
		"module-virtual-source",
		"source_name='"+VirtualMicSourceName+"'",
		"master='"+VirtualMicSinkMonitorName+"'",
		"source_properties=device.description='Soundpad Virtual Mic'",
	)
	err = cmd.Run()
	if err != nil {
		return fmt.Errorf("creating source: %w", err)
	}

	err = portaudio.Terminate()
	if err != nil && !errors.Is(err, portaudio.NotInitialized) {
		return fmt.Errorf("portaudio reinitialization: terminating: %w", err)
	}
	if err == nil {
		if err = portaudio.Initialize(); err != nil {
			return fmt.Errorf("portaudio reinitialization: initializing: %w", err)
		}
	}

	raylibTraceLog(rl.LogInfo, "Created virtual microphone")
	return nil
}

// fileter = "PATTERN1;PATTERN2 ..."
// fileter = "NAME | PATTERN1;PATTERN2 ..."
func OpenFileDialog(title string, filters ...string) (filePath string, ok bool, err error) {
	zenityAbsPath, err := exec.LookPath("zenity")
	if err != nil {
		return "", false, fmt.Errorf(`missing dependency "zenity" (can be installed in debian-based systems with "apt install zenity")`)
	}

	args := make([]string, 0, 1+len(filters))

	args = append(args, "--title="+title, "--file-selection")

	for _, filter := range filters {
		args = append(args, "--file-filter="+strings.ReplaceAll(filter, ";", " "))
	}

	var stdout, stderr bytes.Buffer

	cmd := exec.Command(zenityAbsPath, args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			if exitErr.ExitCode() == 1 {
				return "", false, nil
			}
		}
		rl.TraceLog(rl.LogError, stderr.String())
		return "", false, fmt.Errorf("opening file dialog: %w", err)
	}

	filePath = strings.TrimSpace(stdout.String())
	assert.Equal(filePath[0], '/')

	return filePath, true, nil
}
