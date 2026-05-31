package ffmpeg

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
)

//go:embed bin/ffmpeg.gz
var ffmpegBinaryGzip []byte

func InstallFFmpeg() {
	_ffmpegPath = ""

	tmpDir := os.TempDir()
	ffmpegPath := filepath.Join(tmpDir, "tmp-soundpad-ffmpeg")

	if err := installCompressedFFmpeg(ffmpegBinaryGzip, ffmpegPath); err != nil {
		panic(fmt.Errorf("could not create temporary ffmpeg: %w", err))
	}

	_ffmpegPath = ffmpegPath
}
