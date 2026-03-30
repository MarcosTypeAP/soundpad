package crossplatform

import (
	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/gordonklaus/portaudio"
)

func raylibTraceLog(level rl.TraceLogLevel, msg string) {
	rl.TraceLog(level, "CROSSPLATFORM: "+msg)
}

const (
	AliasDefaultInput  = "Default Input"
	AliasDefaultOutput = "Default Output"
)

type AudioDevice struct {
	Alias string
	Info  *portaudio.DeviceInfo
}

type KeyEvent struct {
	Key       Key
	IsPressed bool
}

var keyEventsCh = make(chan KeyEvent, 1)

type Key uint8

func (k Key) IsValid() bool {
	return k != KeyUnset
}

// X11 Keys (latam1)
const (
	KeyUnset Key = 0

	KeyEscape Key = 1
	//
	KeyKPEnter      Key = 96
	KeyControlRight Key = 97
	KeyKPDivide     Key = 98
	KeyPrint        Key = 99
	KeyAltRight     Key = 100
	//
	KeyHome   Key = 102
	KeyUp     Key = 103
	KeyPrior  Key = 104
	KeyLeft   Key = 105
	KeyRight  Key = 106
	KeyEnd    Key = 107
	KeyDown   Key = 108
	KeyNext   Key = 109
	KeyInsert Key = 110
	KeyDelete Key = 111
	//
	KeyPause Key = 119
	//
	KeySuperLeft  Key = 125
	KeySuperRight Key = 126
	KeyMenu       Key = 127
)
