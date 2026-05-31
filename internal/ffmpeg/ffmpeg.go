package ffmpeg

import (
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"unsafe"
)

func init() {
	go InstallFFmpeg()
}

var _ffmpegPath string

func GetFFmpegPath() string {
	if _ffmpegPath != "" {
		return _ffmpegPath
	}

	InstallFFmpeg()
	return _ffmpegPath
}

func installCompressedFFmpeg(gzipData []byte, dstPath string) error {
	gzipReader, err := gzip.NewReader(bytes.NewReader(gzipData))
	if err != nil {
		return fmt.Errorf("could not create gzip reader: %w", err)
	}
	defer gzipReader.Close()

	dstFile, err := os.Create(dstPath)
	if err != nil {
		return fmt.Errorf("could not create destination file: %w", err)
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, gzipReader)
	if err != nil {
		return fmt.Errorf("could not copy decompressed data: %w", err)
	}

	err = dstFile.Chmod(0o750)
	if err != nil {
		panic(fmt.Errorf("could not change destination permissions: %w", err))
	}

	return nil
}

func loadMP3Samples(ffmpegArgs ...string) ([]byte, error) {
	haveReinstalled := false
Retry:
	cmd := exec.Command(GetFFmpegPath(), ffmpegArgs...)

	var outBuffer bytes.Buffer
	var errBuffer bytes.Buffer
	cmd.Stdout = &outBuffer
	cmd.Stderr = &errBuffer

	if err := cmd.Run(); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			if haveReinstalled {
				return nil, fmt.Errorf("could not re-install ffmpeg")
			}
			InstallFFmpeg()
			haveReinstalled = true
			goto Retry
		}
		return nil, fmt.Errorf("ffmpeg failed: %w: %s", err, errBuffer.String())
	}

	rawBytes := outBuffer.Bytes()
	if len(rawBytes) == 0 {
		return nil, fmt.Errorf("no data decoded")
	}
	return rawBytes, nil
}

func LoadMP3SamplesInt8(filePath string, sampleRate, channels uint) ([]int8, error) {
	rawBytes, err := loadMP3Samples(
		"-v", "error",
		"-i", filePath,
		"-f", "s8",
		"-acodec", "pcm_s8",
		"-ac", fmt.Sprint(channels),
		"-ar", fmt.Sprint(sampleRate),
		"pipe:1",
	)
	if err != nil {
		return nil, err
	}

	samplesCount := len(rawBytes)
	samples := unsafe.Slice((*int8)(unsafe.Pointer(&rawBytes[0])), samplesCount)

	return samples, nil
}

func LoadMP3SamplesInt16(filePath string, sampleRate, channels uint) ([]int16, error) {
	rawBytes, err := loadMP3Samples(
		"-v", "error",
		"-i", filePath,
		"-f", "s16le",
		"-acodec", "pcm_s16le",
		"-ac", fmt.Sprint(channels),
		"-ar", fmt.Sprint(sampleRate),
		"pipe:1",
	)
	if err != nil {
		return nil, err
	}

	if len(rawBytes)%2 != 0 {
		return nil, fmt.Errorf("corrupted byte length, not divisible by 2")
	}
	samplesCount := len(rawBytes) / 2
	samples := unsafe.Slice((*int16)(unsafe.Pointer(&rawBytes[0])), samplesCount)

	return samples, nil
}

func LoadMP3SamplesFloat32(filePath string, sampleRate, channels uint) ([]float32, error) {
	rawBytes, err := loadMP3Samples(
		"-v", "error",
		"-i", filePath,
		"-f", "f32le",
		"-acodec", "pcm_f32le",
		"-ac", fmt.Sprint(channels),
		"-ar", fmt.Sprint(sampleRate),
		"pipe:1",
	)
	if err != nil {
		return nil, err
	}

	if len(rawBytes)%4 != 0 {
		return nil, fmt.Errorf("corrupted byte length, not divisible by 4")
	}
	samplesCount := len(rawBytes) / 4
	samples := unsafe.Slice((*float32)(unsafe.Pointer(&rawBytes[0])), samplesCount)

	for i, sample := range samples {
		samples[i] = float32(math.Tanh(float64(sample))) // soft-clip
	}

	return samples, nil
}

func SaveMP3SamplesFloat32(samples []float32, sampleRate, channels uint, filePath string) error {
	haveReinstalled := false
Retry:
	cmd := exec.Command(GetFFmpegPath(),
		"-v", "error",
		"-f", "f32le",
		"-ar", fmt.Sprint(sampleRate),
		"-ac", fmt.Sprint(channels),
		"-i", "pipe:0",
		"-acodec", "libmp3lame",
		"-aq", "2",
		"-y",
		filePath,
	)

	var errBuffer bytes.Buffer
	cmd.Stderr = &errBuffer

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("could not create stdin pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			if haveReinstalled {
				return fmt.Errorf("could not re-install ffmpeg")
			}
			InstallFFmpeg()
			haveReinstalled = true
			goto Retry
		}
		_ = stdin.Close()
		return fmt.Errorf("could not start ffmpeg: %w", err)
	}

	bytesLength := len(samples) * 4
	rawBytes := unsafe.Slice((*byte)(unsafe.Pointer(&samples[0])), bytesLength)

	if _, err = stdin.Write(rawBytes); err != nil {
		_ = stdin.Close()
		_ = cmd.Wait()
		return fmt.Errorf("could not write to ffmpeg stdin: %w", err)
	}

	stdin.Close()

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("ffmpeg failed: %w: %s", err, errBuffer.String())
	}
	return nil
}

func ConvertToMP3(sourcePath, targetPath string) error {
	haveReinstalled := false
Retry:
	cmd := exec.Command(GetFFmpegPath(),
		"-v", "error",
		"-i", sourcePath,
		"-acodec", "libmp3lame",
		"-aq", "2",
		"-y",
		targetPath,
	)

	var errBuffer bytes.Buffer
	cmd.Stderr = &errBuffer

	if err := cmd.Run(); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			if haveReinstalled {
				return fmt.Errorf("could not re-install ffmpeg")
			}
			InstallFFmpeg()
			haveReinstalled = true
			goto Retry
		}
		return fmt.Errorf("ffmpeg failed: %w: %s", err, errBuffer.String())
	}

	return nil
}
