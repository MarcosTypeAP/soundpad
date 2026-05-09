package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"
	"unsafe"

	"github.com/MarcosTypeAP/go-assert"
	"github.com/MarcosTypeAP/go-rnnoise"
	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/gordonklaus/portaudio"
)

const (
	FileExtensionMP3 = ".mp3"
	FileExtensionWAV = ".wav"
	FileExtensionPNG = ".png"
	FileExtensionJPG = ".jpg"
)

const SampleRate = 48000

const FramesPerBufferVirtualMicAndInput = rnnoise.FrameSize
const FramesPerBufferOutput = 4096

func linearToExponentialGain(x float32) float32 {
	out := _linearToExponentialGain(x)
	assert.EqualApprox(_exponentialToLinearGain(out), x, 0.0001)
	return out
}

func _linearToExponentialGain(x float32) float32 {
	// https://www.dr-lex.be/info-stuff/volumecontrols.html
	assert.InRange(x, 0, 1)
	const maxOutGain = 2.
	const amplitude30dB = 31.622776   // math.Pow(10, 40/20)
	const logAmplitude30dB = 3.453877 // math.Log(amplitude40dB)

	switch x {
	case 0:
		return 0
	case 1:
		return maxOutGain
	}

	ampl := (maxOutGain / amplitude30dB) * float32(math.Exp(float64(logAmplitude30dB*x)))
	assert.InRange(ampl, 0, maxOutGain)
	return ampl
}

func exponentialToLinearGain(x float32) float32 {
	out := _exponentialToLinearGain(x)
	assert.EqualApprox(_linearToExponentialGain(out), x, 0.0001)
	return out
}

func _exponentialToLinearGain(x float32) float32 {
	// https://www.dr-lex.be/info-stuff/volumecontrols.html
	const maxOutGain = 2.
	assert.InRange(x, 0, maxOutGain)
	const amplitude30dB = 31.622776   // math.Pow(10, 40/20)
	const logAmplitude30dB = 3.453877 // math.Log(amplitude40dB)

	if x == 0 {
		return 0
	} else if x > maxOutGain-0.0001 {
		return 1
	}

	linear := float32(math.Log(float64(x/(maxOutGain/amplitude30dB))) / logAmplitude30dB)
	assert.InRange(linear, 0, 1)
	return linear
}


func NewWave(sampleCount, sampleRate, sampleSize, channels int, data unsafe.Pointer) rl.Wave {
	return rl.Wave{
		FrameCount: uint32(sampleCount),
		SampleRate: uint32(sampleRate),
		SampleSize: uint32(sampleSize),
		Channels:   uint32(channels),
		Data:       data,
	}
}

func NewWaveFromMonoSamples(samples Samples) rl.Wave {
	switch samples := samples.(type) {
	case SamplesInt8:
		data := make([]uint8, len(samples))
		for i := range data {
			data[i] = uint8(int(samples[i]) + 128)
		}
		return NewWave(len(samples), SampleRate, 8, 1, unsafe.Pointer(&data[0]))

	case SamplesInt16:
		data := make([]int16, len(samples))
		copy(data, samples)
		return NewWave(len(samples), SampleRate, 16, 1, unsafe.Pointer(&data[0]))

	case SamplesFloat32:
		data := make([]float32, len(samples))
		copy(data, samples)
		return NewWave(len(samples), SampleRate, 32, 1, unsafe.Pointer(&data[0]))
	}

	assert.Unreachable()
	return rl.Wave{}
}

func LoadSamplesFromTrackFile(trackPath string) (Samples, error) {
	if filepath.Ext(trackPath) != FileExtensionWAV {
		return nil, fmt.Errorf("file has wrong extension, must be "+FileExtensionWAV+": %s", trackPath)
	}

	file, err := os.Open(trackPath)
	if err != nil {
		return nil, fmt.Errorf("opening file: %w", err)
	}
	defer file.Close()

	const AudioFormatPCMInteger = 1
	const AudioFormatIEEE754Float = 3

	// https://en.wikipedia.org/wiki/WAV
	type WavFormat struct {
		// [Master RIFF chunk]
		FileChunkID  [4]byte // "RIFF"
		FileSize     uint32  // minus 8 bytes
		FileFormatID [4]byte // "WAVE"

		// [Chunk describing the data format]
		FormatChunkID [4]byte // "fmt "
		FormatSize    uint32  // minus 8 bytes (16 bytes)
		AudioFormat   uint16  // 1 = PCM integer, 3 = IEEE 754 float
		Channels      uint16
		SampleRate    uint32
		ByteRate      uint32
		FrameSize     uint16
		BitsPerSample uint16

		// [Chunk containing the sampled data]
		DataChunkID [4]byte // "data"
		DataSize    uint32
		// Data        []byte
	}

	wavData := WavFormat{}
	err = binary.Read(file, binary.LittleEndian, &wavData)
	if err != nil {
		return nil, fmt.Errorf("parsing file: %w", err)
	}

	errMalformed := fmt.Errorf("malformed track")

	if wavData.FileChunkID != [4]byte([]byte("RIFF")) {
		return nil, errMalformed
	}
	if wavData.FileFormatID != [4]byte([]byte("WAVE")) {
		return nil, errMalformed
	}
	if wavData.FormatChunkID != [4]byte([]byte("fmt ")) {
		return nil, errMalformed
	}
	if wavData.AudioFormat != AudioFormatPCMInteger && wavData.AudioFormat != AudioFormatIEEE754Float {
		return nil, fmt.Errorf("only 'PCM integer' and 'IEEE 754 float' are supported")
	}
	if wavData.Channels != 1 {
		return nil, fmt.Errorf("only mono tracks are supported")
	}
	if wavData.SampleRate != SampleRate {
		return nil, fmt.Errorf("only %dHz tracks are supported", SampleRate)
	}
	if wavData.FrameSize != wavData.BitsPerSample/8 { // 1 channel frames
		return nil, fmt.Errorf("%w: frame size (%d) != sample size (%d)", errMalformed, wavData.FrameSize, wavData.BitsPerSample/8)
	}
	if wavData.BitsPerSample != 8 && wavData.BitsPerSample != 16 && wavData.BitsPerSample != 32 {
		return nil, fmt.Errorf("only uint8, int16 or float32 samples are supported")
	}
	if wavData.DataChunkID != [4]byte([]byte("data")) {
		return nil, errMalformed
	}
	if wavData.DataSize%uint32(wavData.BitsPerSample/8) != 0 {
		return nil, errMalformed
	}

	var outSamples Samples

	data, err := io.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("reading file's data chunk: %w", err)
	}
	if wavData.DataSize > uint32(len(data)) {
		return nil, fmt.Errorf("%w: data size (%d) > data read (%d)", errMalformed, wavData.DataSize, len(data))
	}
	data = data[:wavData.DataSize]

	switch wavData.BitsPerSample {
	case 8:
		if wavData.AudioFormat != AudioFormatPCMInteger {
			return nil, errMalformed
		}
		out := SamplesInt8(unsafe.Slice((*int8)(unsafe.Pointer(&data[0])), len(data)))
		for i := range data {
			out[i] = int8(data[i] - 128)
		}
		outSamples = out

	case 16:
		if wavData.AudioFormat != AudioFormatPCMInteger {
			return nil, errMalformed
		}
		outSamples = SamplesInt16(unsafe.Slice((*int16)(unsafe.Pointer(&data[0])), len(data)/2))

	case 32:
		if wavData.AudioFormat != AudioFormatIEEE754Float {
			return nil, errMalformed
		}
		outSamples = SamplesFloat32(unsafe.Slice((*float32)(unsafe.Pointer(&data[0])), len(data)/4))

	default:
		assert.Unreachable()
	}

	return outSamples, nil
}

func LoadMonoWaveSamplesFloat32(wave rl.Wave) SamplesFloat32 {
	assert.Equal(wave.Channels, 1)

	out := make(SamplesFloat32, wave.FrameCount)

	switch wave.SampleSize {
	case 8:
		samplesUint8 := unsafe.Slice((*uint8)(wave.Data), wave.FrameCount)
		for i := range out {
			out[i] = (float32(samplesUint8[i]) - 128) / 127
		}

	case 16:
		samplesInt16 := unsafe.Slice((*int16)(wave.Data), wave.FrameCount)
		copySamples(out, SamplesInt16(samplesInt16))

	case 32:
		samplesFloat32 := unsafe.Slice((*float32)(wave.Data), wave.FrameCount)
		copy(out, samplesFloat32)

	default:
		assert.Unreachable()
	}

	raylibTraceLog(rl.LogInfo, "Loaded wave samples (mono float32)")
	return out
}

func sampleSoftClip(sample float32) float32 {
	return float32(math.Tanh(float64(sample)))
}

type Samples interface {
	int8() []int8
	int16() []int16
	float32() []float32
	sampleSize() int
	len() int
}

type SamplesInt8 []int8

func (s SamplesInt8) int8() []int8       { return s }
func (s SamplesInt8) int16() []int16     { assert.Unreachable(); return nil }
func (s SamplesInt8) float32() []float32 { assert.Unreachable(); return nil }
func (s SamplesInt8) sampleSize() int    { return 1 }
func (s SamplesInt8) len() int           { return len(s) }

type SamplesInt16 []int16

func (s SamplesInt16) int8() []int8       { assert.Unreachable(); return nil }
func (s SamplesInt16) int16() []int16     { return s }
func (s SamplesInt16) float32() []float32 { assert.Unreachable(); return nil }
func (s SamplesInt16) sampleSize() int    { return 2 }
func (s SamplesInt16) len() int           { return len(s) }

type SamplesFloat32 []float32

func (s SamplesFloat32) int8() []int8       { assert.Unreachable(); return nil }
func (s SamplesFloat32) int16() []int16     { assert.Unreachable(); return nil }
func (s SamplesFloat32) float32() []float32 { return s }
func (s SamplesFloat32) sampleSize() int    { return 4 }
func (s SamplesFloat32) len() int           { return len(s) }

func copySamples(dst, src Samples) {
	assert.NotNil(src)
	assert.NotNil(dst)
	assert.GreaterEqual(dst.len(), src.len())

	switch dst := dst.(type) {
	case SamplesInt8:
		switch src := src.(type) {
		case SamplesInt8:
			copy(dst, src)

		case SamplesInt16:
			for i := range dst {
				dst[i] = int8(src[i] / 256)
			}

		case SamplesFloat32:
			for i := range dst {
				dst[i] = int8(src[i] * 127)
			}

		default:
			assert.Unreachable()
		}

	case SamplesInt16:
		switch src := src.(type) {
		case SamplesInt8:
			for i := range dst {
				dst[i] = int16(src[i]) * 256
			}

		case SamplesInt16:
			copy(dst, src)

		case SamplesFloat32:
			for i := range dst {
				dst[i] = int16(src[i] * 32767)
			}

		default:
			assert.Unreachable()
		}

	case SamplesFloat32:
		switch src := src.(type) {
		case SamplesInt8:
			for i := range dst {
				dst[i] = float32(src[i]) / 127
			}

		case SamplesInt16:
			for i := range dst {
				dst[i] = float32(src[i]) / 32767
			}

		case SamplesFloat32:
			copy(dst, src)

		default:
			assert.Unreachable()
		}

	default:
		assert.Unreachable()
	}
}

type SampleQuality uint8

const (
	SampleQualityInt8 SampleQuality = iota
	SampleQualityInt16
	SampleQualityFloat32
)

type SoundID = uint32

type Sound struct {
	samples Samples
	cursor  uint32
	gain    float32
	id      SoundID

	trackID      ID
	isOutputOnly bool
}

func NewSound(id SoundID, samples Samples, gain float32, trackID ID, isOutputOnly bool) Sound {
	return Sound{
		id:           id,
		samples:      samples,
		gain:         gain,
		trackID:      trackID,
		isOutputOnly: isOutputOnly,
	}
}

func (s *Sound) IsTrack() bool {
	return s.trackID != IDUnset
}

func (s *Sound) IsConsumed() bool {
	assert.NotNil(s.samples)
	return int(s.cursor) >= s.samples.len()
}

func (s *Sound) ConsumeSamples(count uint32) {
	assert.NotNil(s.samples)
	s.cursor = min(s.cursor+count, uint32(s.samples.len()))
}

func (s *Sound) MixIntoBuffer(out []float32, channels int, gain float32) {
	assert.NotNil(s.samples)
	count := min(len(out)/channels, s.samples.len()-int(s.cursor))
	assert.GreaterEqual(count, 0)

	switch samples := s.samples.(type) {
	case SamplesInt8:
		for i := range count {
			sampleFloat32 := float32(samples[int(s.cursor)+i]) / 127

			for j := range channels {
				out[i*channels+j] += sampleFloat32 * s.gain * gain
			}
		}
	case SamplesInt16:
		for i := range count {
			sampleFloat32 := float32(samples[int(s.cursor)+i]) / 32767

			for j := range channels {
				out[i*channels+j] += sampleFloat32 * s.gain * gain
			}
		}
	case SamplesFloat32:
		for i := range count {
			for j := range channels {
				out[i*channels+j] += samples[int(s.cursor)+i] * s.gain * gain
			}
		}
	}
	s.cursor += uint32(count)

	assert.LessEqual(s.cursor, uint32(s.samples.len()))
}

type AudioPlayer struct {
	virtualMicSounds []Sound
	outputSounds     []Sound
	idCounter        SoundID

	TracksGain float32
	InputGain  float32
	OutputGain float32

	isTracksMuted  bool
	isInputMuted   bool
	isMonitorMuted bool

	isDenoiseEnabled bool

	tracksMutedStartTime time.Time

	virtualMicAndInputStream *portaudio.Stream
	outputStream             *portaudio.Stream
	outputStreamChannels     uint8

	virtualMicBuf []float32
	inputBuf      []float32
	outputBuf     []float32

	virtualMicInfo *portaudio.DeviceInfo

	virtualMicAndInputStreamMutex sync.Mutex
	outputStreamMutex             sync.Mutex

	audioMutex sync.Mutex
	audioCond  sync.Cond
}

func NewAudioPlayer(storage *Storage, virtualMicInfo *portaudio.DeviceInfo) *AudioPlayer {
	audioPlayer := &AudioPlayer{
		TracksGain:           storage.TracksGain,
		InputGain:            storage.InputGain,
		OutputGain:           storage.OutputGain,
		isTracksMuted:        storage.IsTracksMuted,
		isInputMuted:         storage.IsInputMuted,
		isMonitorMuted:       storage.IsMonitorMuted,
		isDenoiseEnabled:     storage.IsDenoiseEnabled,
		virtualMicInfo:       virtualMicInfo,
		tracksMutedStartTime: time.Now(),
	}
	audioPlayer.audioCond = sync.Cond{L: &audioPlayer.audioMutex}

	return audioPlayer
}

func (p *AudioPlayer) AddTrack(storage *Storage, trackID ID, isOutputOnly bool) (SoundID, error) {
	track := storage.GetTrackByID(trackID)
	samples, err := storage.GetSamplesFromTrackID(trackID)
	if err != nil {
		return 0, err
	}
	soundID := p.AddSamples(samples, track.Gain, track.ID, isOutputOnly)
	raylibTraceLog(rl.LogInfo, "Playing track: "+track.Name)
	return soundID, nil
}

func (p *AudioPlayer) AddSamples(samples Samples, gain float32, trackID ID, isOutputOnly bool) SoundID {
	p.audioMutex.Lock()
	defer p.audioMutex.Unlock()

	p.idCounter++
	soundID := p.idCounter
	sound := NewSound(soundID, samples, gain, trackID, isOutputOnly)

	if !isOutputOnly {
		p.virtualMicSounds = append(p.virtualMicSounds, sound)
	}
	p.outputSounds = append(p.outputSounds, sound)

	p.audioCond.Broadcast()

	return soundID
}

func (p *AudioPlayer) RemoveSound(id SoundID) {
	p.audioMutex.Lock()
	for i := range p.virtualMicSounds {
		if p.virtualMicSounds[i].id == id {
			p.virtualMicSounds[i] = p.virtualMicSounds[len(p.virtualMicSounds)-1]
			p.virtualMicSounds = p.virtualMicSounds[:len(p.virtualMicSounds)-1]
			break
		}
	}
	for i := range p.outputSounds {
		if p.outputSounds[i].id == id {
			p.outputSounds[i] = p.outputSounds[len(p.outputSounds)-1]
			p.outputSounds = p.outputSounds[:len(p.outputSounds)-1]
			break
		}
	}
	p.audioMutex.Unlock()
}

func (p *AudioPlayer) SetProgress(id SoundID, progress float32) {
	p.audioMutex.Lock()
	defer p.audioMutex.Unlock()

	if idx := slices.IndexFunc(p.virtualMicSounds, func(s Sound) bool { return s.id == id }); idx != -1 {
		sound := &p.virtualMicSounds[idx]
		assert.NotNil(sound.samples)
		sound.cursor = uint32(float32(sound.samples.len()) * progress)
	}
	if idx := slices.IndexFunc(p.outputSounds, func(s Sound) bool { return s.id == id }); idx != -1 {
		sound := &p.outputSounds[idx]
		assert.NotNil(sound.samples)
		sound.cursor = uint32(float32(sound.samples.len()) * progress)
	}
}

func (p *AudioPlayer) SetSoundGain(id SoundID, gain float32) {
	p.audioMutex.Lock()
	defer p.audioMutex.Unlock()

	if idx := slices.IndexFunc(p.virtualMicSounds, func(s Sound) bool { return s.id == id }); idx != -1 {
		p.virtualMicSounds[idx].gain = gain
	}
	if idx := slices.IndexFunc(p.outputSounds, func(s Sound) bool { return s.id == id }); idx != -1 {
		p.outputSounds[idx].gain = gain
	}
}

func (p *AudioPlayer) SetTrackGain(trackID ID, gain float32) {
	p.audioMutex.Lock()
	defer p.audioMutex.Unlock()

	if idx := slices.IndexFunc(p.virtualMicSounds, func(s Sound) bool { return s.trackID == trackID }); idx != -1 {
		p.virtualMicSounds[idx].gain = gain
	}
	if idx := slices.IndexFunc(p.outputSounds, func(s Sound) bool { return s.trackID == trackID }); idx != -1 {
		p.outputSounds[idx].gain = gain
	}
}

func (p *AudioPlayer) GetProgress(id SoundID) float32 {
	p.audioMutex.Lock()
	defer p.audioMutex.Unlock()

	if idx := slices.IndexFunc(p.virtualMicSounds, func(s Sound) bool { return s.id == id }); idx != -1 {
		sound := p.virtualMicSounds[idx]
		assert.NotNil(sound.samples)
		return float32(sound.cursor) / float32(sound.samples.len())
	}
	if idx := slices.IndexFunc(p.outputSounds, func(s Sound) bool { return s.id == id }); idx != -1 {
		sound := p.outputSounds[idx]
		assert.NotNil(sound.samples)
		return float32(sound.cursor) / float32(sound.samples.len())
	}
	return -1
}

func (p *AudioPlayer) ReplaceSamples(id SoundID, newSamples Samples) {
	assert.NotNil(newSamples)

	p.audioMutex.Lock()
	defer p.audioMutex.Unlock()

	if idx := slices.IndexFunc(p.virtualMicSounds, func(s Sound) bool { return s.id == id }); idx != -1 {
		sound := &p.virtualMicSounds[idx]
		assert.NotNil(sound.samples)
		sound.samples = newSamples
		sound.cursor = min(sound.cursor, uint32(sound.samples.len()))
	}
	if idx := slices.IndexFunc(p.outputSounds, func(s Sound) bool { return s.id == id }); idx != -1 {
		sound := &p.outputSounds[idx]
		assert.NotNil(sound.samples)
		sound.samples = newSamples
		sound.cursor = min(sound.cursor, uint32(sound.samples.len()))
	}
}

func (p *AudioPlayer) ClearSounds() {
	p.audioMutex.Lock()
	p.virtualMicSounds = p.virtualMicSounds[:0]
	p.outputSounds = p.outputSounds[:0]
	p.audioMutex.Unlock()
	raylibTraceLog(rl.LogInfo, "Cleared sounds")
}

func (p *AudioPlayer) SetInputMuted(muted bool) error {
	p.audioMutex.Lock()
	defer p.audioMutex.Unlock()

	p.isInputMuted = muted

	if !p.isInputMuted {
		p.virtualMicAndInputStreamMutex.Lock()
		defer p.virtualMicAndInputStreamMutex.Unlock()

		p.audioCond.Broadcast()

		for n, err := p.virtualMicAndInputStream.AvailableToRead(); err != nil || n > len(p.inputBuf); n, err = p.virtualMicAndInputStream.AvailableToRead() {
			if err != nil {
				return fmt.Errorf("checking the amount of available samples to read from input: %w", err)
			}

			if err := p.virtualMicAndInputStream.Read(); err != nil {
				return fmt.Errorf("flushing (reading) samples from input: %w", err)
			}
		}
	}

	return nil
}

func (p *AudioPlayer) syncOutputTracksWithVirtualMic() {
OuterLoop:
	for i := range p.outputSounds {
		if !p.outputSounds[i].IsTrack() || p.outputSounds[i].isOutputOnly {
			continue
		}

		for j := range p.virtualMicSounds {
			if p.outputSounds[i].id == p.virtualMicSounds[j].id {
				p.outputSounds[i].cursor = p.virtualMicSounds[j].cursor
				continue OuterLoop
			}
		}

		// the virtual mic sound could have been removed first
		assert.Less(p.outputSounds[i].samples.len()-int(p.outputSounds[i].cursor), 0.5*SampleRate) // 0.5s time window
		p.outputSounds[i].cursor = uint32(p.outputSounds[i].samples.len())
	}
}

func (p *AudioPlayer) SetTracksMuted(muted bool) {
	p.audioMutex.Lock()
	defer p.audioMutex.Unlock()

	prevMuted := p.isTracksMuted
	p.isTracksMuted = muted

	if !prevMuted && p.isTracksMuted {
		p.tracksMutedStartTime = time.Now()
		return
	}

	if prevMuted && !p.isTracksMuted {
		elapsedSamples := uint32(time.Since(p.tracksMutedStartTime).Seconds() * SampleRate)

		for i := range p.virtualMicSounds {
			p.virtualMicSounds[i].ConsumeSamples(elapsedSamples)
		}

		p.syncOutputTracksWithVirtualMic()
		p.audioCond.Broadcast()
	}
}

func (p *AudioPlayer) SetMonitorMuted(muted bool) {
	p.audioMutex.Lock()
	defer p.audioMutex.Unlock()

	prevMuted := p.isMonitorMuted
	p.isMonitorMuted = muted

	if prevMuted && !p.isMonitorMuted {
		p.syncOutputTracksWithVirtualMic()
		p.audioCond.Broadcast()
	}
}

func (p *AudioPlayer) SetDenoiseEnabled(enabled bool) {
	p.audioMutex.Lock()
	defer p.audioMutex.Unlock()

	p.isDenoiseEnabled = enabled
}

func (p *AudioPlayer) IsPlayingSounds() bool {
	p.audioMutex.Lock()
	defer p.audioMutex.Unlock()

	return len(p.virtualMicSounds)+len(p.outputSounds) > 0
}

func (p *AudioPlayer) ProcessVirtualSinkAndInput() error {
	p.audioMutex.Lock()

	if p.virtualMicAndInputStream == nil || ((len(p.virtualMicSounds) == 0 || p.isTracksMuted) && p.isInputMuted) {
		p.audioCond.Wait()
	}

	if p.virtualMicAndInputStream == nil {
		p.audioMutex.Unlock()
		return nil
	}

	assert.Equal(len(p.virtualMicBuf), FramesPerBufferVirtualMicAndInput*1)
	assert.Equal(len(p.inputBuf), FramesPerBufferVirtualMicAndInput*1)

	writeStream := false

	if !p.isInputMuted {
		if err := p.ReadInputStream(); err != nil {
			p.audioMutex.Unlock()
			return fmt.Errorf("reading input stream: %w", err)
		}

		assert.Equal(len(p.inputBuf), rnnoise.FrameSize)
		if p.isDenoiseEnabled {
			rnnoise.ProcessFrameNormalized(p.inputBuf, p.inputBuf)
		}

		for i := range p.virtualMicBuf {
			p.virtualMicBuf[i] = p.inputBuf[i] * p.InputGain
		}
		writeStream = true
	} else {
		for i := range p.virtualMicBuf {
			p.virtualMicBuf[i] = 0
		}
	}

	for i := 0; i < len(p.virtualMicSounds); i++ {
		if p.virtualMicSounds[i].IsConsumed() {
			p.virtualMicSounds[i] = p.virtualMicSounds[len(p.virtualMicSounds)-1]
			p.virtualMicSounds = p.virtualMicSounds[:len(p.virtualMicSounds)-1]
			i--
			continue
		}

		assert.True(p.virtualMicSounds[i].IsTrack())
		if p.isTracksMuted {
			continue
		}

		p.virtualMicSounds[i].MixIntoBuffer(p.virtualMicBuf, 1, p.TracksGain)
		writeStream = true
	}

	p.audioMutex.Unlock()

	if !writeStream {
		return nil
	}

	for i := range p.virtualMicBuf {
		if p.virtualMicBuf[i] == 0 {
			continue
		}
		p.virtualMicBuf[i] = sampleSoftClip(p.virtualMicBuf[i])
	}

	if err := p.WriteVirtualMicStream(); err != nil {
		return fmt.Errorf("writing to virtual mic stream: %w", err)
	}
	return nil
}

func (p *AudioPlayer) ProcessOutput() error {
	p.audioMutex.Lock()

	hasOutputOnlySounds := false
	for i := range p.outputSounds {
		if p.outputSounds[i].isOutputOnly {
			hasOutputOnlySounds = true
			break
		}
	}

	if p.outputStream == nil || len(p.outputSounds) == 0 || (!hasOutputOnlySounds && (p.isTracksMuted || p.isMonitorMuted)) {
		p.audioCond.Wait()
	}

	if p.outputStream == nil {
		p.audioMutex.Unlock()
		return nil
	}

	assert.NotEqual(p.outputStreamChannels, 0)
	assert.Equal(len(p.outputBuf), FramesPerBufferOutput*int(p.outputStreamChannels))

	for i := range p.outputBuf {
		p.outputBuf[i] = 0
	}

	writeStream := false

	for i := 0; i < len(p.outputSounds); i++ {
		if p.outputSounds[i].IsConsumed() {
			p.outputSounds[i] = p.outputSounds[len(p.outputSounds)-1]
			p.outputSounds = p.outputSounds[:len(p.outputSounds)-1]
			i--
			continue
		}

		if p.outputSounds[i].IsTrack() {
			if p.isTracksMuted || p.isMonitorMuted {
				continue
			}
			p.outputSounds[i].MixIntoBuffer(p.outputBuf, int(p.outputStreamChannels), p.TracksGain)
		} else {
			p.outputSounds[i].MixIntoBuffer(p.outputBuf, int(p.outputStreamChannels), 1)
		}

		writeStream = true
	}

	p.audioMutex.Unlock()

	if !writeStream {
		return nil
	}

	for i := range p.outputBuf {
		if p.outputBuf[i] == 0 {
			continue
		}
		p.outputBuf[i] = sampleSoftClip(p.outputBuf[i] * p.OutputGain)
	}

	if err := p.WriteOutputStream(); err != nil {
		return fmt.Errorf("writing to output stream: %w", err)
	}
	return nil
}

func (p *AudioPlayer) StartVirtualMicStream(inputInfo *portaudio.DeviceInfo) error {
	p.audioMutex.Lock()
	defer p.audioMutex.Unlock()

	p.virtualMicAndInputStreamMutex.Lock()
	defer p.virtualMicAndInputStreamMutex.Unlock()

	if p.virtualMicAndInputStream != nil {
		err := p.lockedCloseVirtualMicStream()
		if err != nil {
			return err
		}
	}

	streamParams := portaudio.StreamParameters{
		Output: portaudio.StreamDeviceParameters{
			Device:   p.virtualMicInfo,
			Channels: 1,
			Latency:  p.virtualMicInfo.DefaultHighOutputLatency,
		},
		Input: portaudio.StreamDeviceParameters{
			Device:   inputInfo,
			Channels: 1,
			Latency:  inputInfo.DefaultHighInputLatency,
		},
		SampleRate:      SampleRate,
		FramesPerBuffer: FramesPerBufferVirtualMicAndInput,
	}

	if len(p.virtualMicBuf) != FramesPerBufferVirtualMicAndInput*streamParams.Output.Channels {
		p.virtualMicBuf = make([]float32, FramesPerBufferVirtualMicAndInput*streamParams.Output.Channels)
	}
	if len(p.inputBuf) != FramesPerBufferVirtualMicAndInput*streamParams.Input.Channels {
		p.inputBuf = make([]float32, FramesPerBufferVirtualMicAndInput*streamParams.Input.Channels)
	}

	stream, err := portaudio.OpenStream(streamParams, p.inputBuf, p.virtualMicBuf)
	if err != nil {
		return fmt.Errorf("opening stream: %w", err)
	}
	if err := stream.Start(); err != nil {
		return fmt.Errorf("starting stream: %w", err)
	}

	p.virtualMicAndInputStream = stream

	p.audioCond.Broadcast()

	raylibTraceLog(rl.LogInfo, "Started virtual microphone stream ("+inputInfo.Name+")")
	return nil
}

func (p *AudioPlayer) lockedCloseVirtualMicStream() error {
	assert.NotNil(p.virtualMicAndInputStream)

	defer func() {
		p.virtualMicAndInputStream = nil
	}()

	if err := p.virtualMicAndInputStream.Stop(); err != nil {
		assert.False(errors.Is(err, portaudio.StreamIsStopped))
		return fmt.Errorf("stopping stream: %w", err)
	}

	if err := p.virtualMicAndInputStream.Close(); err != nil {
		return fmt.Errorf("closing stream: %w", err)
	}

	raylibTraceLog(rl.LogInfo, "Closed virtual microphone stream")
	return nil
}

func (p *AudioPlayer) CloseVirtualMicStream() error {
	p.audioMutex.Lock()
	defer p.audioMutex.Unlock()

	p.virtualMicAndInputStreamMutex.Lock()
	defer p.virtualMicAndInputStreamMutex.Unlock()

	return p.lockedCloseVirtualMicStream()
}

func (p *AudioPlayer) ReadInputStream() error {
	p.virtualMicAndInputStreamMutex.Lock()
	defer p.virtualMicAndInputStreamMutex.Unlock()

	if p.virtualMicAndInputStream == nil {
		return nil
	}

	return p.virtualMicAndInputStream.Read()
}

func (p *AudioPlayer) WriteVirtualMicStream() error {
	p.virtualMicAndInputStreamMutex.Lock()
	defer p.virtualMicAndInputStreamMutex.Unlock()

	if p.virtualMicAndInputStream == nil {
		return nil
	}

	return p.virtualMicAndInputStream.Write()
}

func (p *AudioPlayer) StartOutputStream(outputInfo *portaudio.DeviceInfo) error {
	p.audioMutex.Lock()
	defer p.audioMutex.Unlock()

	p.outputStreamMutex.Lock()
	defer p.outputStreamMutex.Unlock()

	if p.outputStream != nil {
		err := p.lockedCloseOutputStream()
		if err != nil {
			return err
		}
	}

	const f = 0x80000000
	const m = 0xFFFF0000
	const ff = 0x40

	streamParams := portaudio.StreamParameters{
		Output: portaudio.StreamDeviceParameters{
			Device:   outputInfo,
			Channels: min(2, outputInfo.MaxOutputChannels),
			Latency:  outputInfo.DefaultHighOutputLatency,
		},
		SampleRate:      SampleRate,
		FramesPerBuffer: FramesPerBufferOutput,
	}

	if len(p.outputBuf) != FramesPerBufferOutput*streamParams.Output.Channels {
		p.outputBuf = make([]float32, FramesPerBufferOutput*streamParams.Output.Channels)
	}

	stream, err := portaudio.OpenStream(streamParams, p.outputBuf)
	if err != nil {
		return fmt.Errorf("opening stream: %w", err)
	}
	if err := stream.Start(); err != nil {
		return fmt.Errorf("starting stream: %w", err)
	}

	p.outputStream = stream
	p.outputStreamChannels = uint8(streamParams.Output.Channels)

	p.audioCond.Broadcast()

	raylibTraceLog(rl.LogInfo, "Started output stream ("+outputInfo.Name+")")
	return nil
}

func (p *AudioPlayer) lockedCloseOutputStream() error {
	assert.NotNil(p.outputStream)

	defer func() {
		p.outputStream = nil
		p.outputStreamChannels = 0
	}()

	if err := p.outputStream.Stop(); err != nil {
		assert.False(errors.Is(err, portaudio.StreamIsStopped))
		return fmt.Errorf("stopping stream: %w", err)
	}

	if err := p.outputStream.Close(); err != nil {
		return fmt.Errorf("closing stream: %w", err)
	}

	raylibTraceLog(rl.LogInfo, "Closed output stream")
	return nil
}

func (p *AudioPlayer) CloseOutputStream() error {
	p.audioMutex.Lock()
	defer p.audioMutex.Unlock()

	p.outputStreamMutex.Lock()
	defer p.outputStreamMutex.Unlock()

	return p.lockedCloseOutputStream()
}

func (p *AudioPlayer) WriteOutputStream() error {
	p.outputStreamMutex.Lock()
	defer p.outputStreamMutex.Unlock()

	if p.outputStream == nil {
		return nil
	}

	return p.outputStream.Write()
}

func ReinitializePortaudio() error {
	if err := portaudio.Terminate(); err != nil && !errors.Is(err, portaudio.NotInitialized) {
		return fmt.Errorf("terminating portaudio: %w", err)
	}
	raylibTraceLog(rl.LogInfo, "Portaudio deinitialized")
	if err := portaudio.Initialize(); err != nil {
		return fmt.Errorf("initializing portaudio: %w", err)
	}
	raylibTraceLog(rl.LogInfo, "Portaudio initialized")
	return nil
}
