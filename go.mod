module github.com/MarcosTypeAP/soundpad

go 1.26.0

require (
	github.com/MarcosTypeAP/go-assert v1.4.0
	github.com/MarcosTypeAP/go-rlgui v1.0.0
	github.com/MarcosTypeAP/go-rnnoise v1.1.0
	github.com/ebitengine/purego v0.9.1
	github.com/gen2brain/raylib-go/raylib v0.55.1
	github.com/gordonklaus/portaudio v0.0.0-20250206071425-98a94950218b
	golang.org/x/sys v0.39.0
)

require golang.org/x/exp v0.0.0-20251219203646-944ab1f22d93 // indirect

// replace github.com/MarcosTypeAP/go-rlgui => ../rlgui
// replace github.com/MarcosTypeAP/go-assert => ../assert
// replace github.com/MarcosTypeAP/go-rnnoise => ../rnnoise
