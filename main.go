package main

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/MarcosTypeAP/soundpad/internal/crossplatform"

	"github.com/MarcosTypeAP/go-assert"
	gui "github.com/MarcosTypeAP/go-rlgui"
	"github.com/MarcosTypeAP/go-rnnoise"

	rl "github.com/gen2brain/raylib-go/raylib"
	"github.com/gordonklaus/portaudio"
)

var StorageConfigFilePath string
var StorageTracksDirPath string
var StorageImagesDirPath string

func init() {
	configPath, err := os.UserConfigDir()
	assert.NoError(err, "WTF WHAT KIND OF OS ARE YOU ON???")

	appConfigDir := path.Join(configPath, "soundpad")
	err = os.MkdirAll(appConfigDir, 0755)
	if err != nil {
		logAndOpenLoadingErrorWindow(fmt.Errorf("Error: could not create the directory %q: %w\n", appConfigDir, err))
		os.Exit(1)
	}

	StorageTracksDirPath = path.Join(appConfigDir, "tracks")
	err = os.MkdirAll(StorageTracksDirPath, 0755)
	if err != nil {
		logAndOpenLoadingErrorWindow(fmt.Errorf("Error: could not create the directory %q: %w\n", StorageTracksDirPath, err))
		os.Exit(1)
	}

	StorageImagesDirPath = path.Join(appConfigDir, "images")
	err = os.MkdirAll(StorageImagesDirPath, 0755)
	if err != nil {
		logAndOpenLoadingErrorWindow(fmt.Errorf("Error: could not create the directory %q: %w\n", StorageImagesDirPath, err))
		os.Exit(1)
	}

	StorageConfigFilePath = path.Join(appConfigDir, "config.json")
}

type ID = uint64

const (
	IDUnset ID = 0
	IDDummy ID = 1
)

var _generateIDMutex sync.Mutex
var _lastGeneratedID ID

func GenerateID() ID {
	_generateIDMutex.Lock()
	id := uint64(time.Now().UnixNano())
	for id == _lastGeneratedID {
		id = uint64(time.Now().UnixNano())
	}
	_generateIDMutex.Unlock()
	return id
}

func ExportImageResize(src, dst string) error {
	assert.True(strings.HasSuffix(dst, ".png"))

	extension := filepath.Ext(src)
	if extension == "" {
		return fmt.Errorf("missing extension: %q", src)
	}

	srcData, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("reading source file: %w", err)
	}

	const NewImageSize = 128

	img := rl.LoadImageFromMemory(extension, srcData, int32(len(srcData)))
	if !rl.IsImageValid(img) {
		return fmt.Errorf("invalid image: %q", src)
	}

	imgSize := gui.Vec2(float32(img.Width), float32(img.Height))
	if imgSize.X/imgSize.Y > 1 { // landscape
		rl.ImageCrop(img, gui.Rect((imgSize.X-imgSize.Y)/2, 0, imgSize.Y, imgSize.Y))
	} else {
		rl.ImageCrop(img, gui.Rect(0, (imgSize.Y-imgSize.X)/2, imgSize.X, imgSize.X))
	}
	rl.ImageResize(img, NewImageSize, NewImageSize)

	newData := rl.ExportImageToMemory(*img, ".png")
	assert.NotEqual(len(newData), 0)

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	_, err = dstFile.Write(newData)
	if err != nil {
		return err
	}

	raylibTraceLog(rl.LogInfo, "Saved image: "+dst)
	return nil
}

type Track struct {
	ID   ID
	Name string
	Gain float32

	trackPath string
	imagePath string

	samples Samples
}

func (t *Track) IsLoaded() bool {
	return t.samples != nil && t.samples.len() > 0
}

func (t *Track) HasImage() bool {
	return t.imagePath != ""
}

func (t *Track) HasTrack() bool {
	return t.trackPath != ""
}

func (t *Track) LoadSamples() error {
	var err error
	t.samples, err = LoadSamplesFromTrackFile(t.trackPath)
	if err != nil {
		return err
	}
	return nil
}

func (t *Track) UnloadSamples() {
	t.samples = nil
}

func (t *Track) IsHeavy() bool {
	return t.samples != nil && t.samples.len()*t.samples.sampleSize() > 1*1024*1024 // 1 MiB
}

type KeyKind uint8

const (
	KeyKindUnset KeyKind = iota
	KeyKindKeyboard
	KeyKeypad
)

type Key uint32

const KeyUnset Key = 0

func NewKey(key uint32, kind KeyKind) Key {
	var k Key
	k.SetKey(key)
	k.SetKind(kind)
	return k
}

func (k Key) IsSet() bool {
	return k != KeyUnset
}

func (k Key) IsValid() bool {
	key := k.GetKey()
	if key == 0 {
		return false
	}

	switch k.GetKind() {
	case KeyKindKeyboard:
		return crossplatform.Key(key).IsValid()

	case KeyKeypad:
		panic("unimplemented")
	}

	panic("unimplemented")
}

func (k Key) GetKind() KeyKind {
	return KeyKind(k >> 24)
}

func (k *Key) SetKind(kind KeyKind) {
	*k &^= 0xFF << 24
	*k |= Key(uint64(kind) << 24)
}

func (k Key) GetKey() uint32 {
	return uint32(k &^ (0xFF << 24))
}

func (k *Key) SetKey(key uint32) {
	*k &= Key(0xFF << 24)
	*k |= Key(key)
}

func (k Key) String() string {
	if !k.IsSet() {
		return "UNSET"
	}

	if !k.IsValid() {
		return "INVALID"
	}

	switch k.GetKind() {
	case KeyKindKeyboard:
		key := crossplatform.Key(k.GetKey())
		assert.True(key.IsValid())
		return strings.ToUpper(strings.ReplaceAll(crossplatform.GetKeyName(key), "_", " "))

	case KeyKeypad:
		panic("unimplemented")
	}

	panic("unimplemented")
}

type KeyCombo [4]Key

func (c KeyCombo) Equal(keys ...Key) bool {
	assert.InRange(len(keys), 1, len(KeyCombo{}))

	var cc KeyCombo
	copy(cc[:], keys)
	return cc == c
}

func (c KeyCombo) IsSet() bool {
	return c[0].IsSet()
}

func (c KeyCombo) IsValid() bool {
	for _, key := range c {
		if !key.IsSet() {
			return true
		}
		if !key.IsValid() {
			return false
		}
	}
	return true
}

var _KeyComboStringBuilder = &strings.Builder{}

func (c KeyCombo) String() string {
	if !c.IsValid() {
		return "INVALID"
	}

	lastIdx := -1
	for i, key := range c {
		if !key.IsSet() {
			break
		}
		lastIdx = i
	}

	if lastIdx == -1 {
		return "UNSET"
	}

	out := _KeyComboStringBuilder
	out.Reset()

	for i := 0; i < lastIdx; i++ {
		out.WriteString(c[i].String())
		out.WriteByte('+')
	}
	out.WriteString(c[lastIdx].String())

	return out.String()
}

type Binding struct {
	TrackID   ID
	Keys      KeyCombo
	IsEnabled bool
	keysCache string
}

func (b Binding) GetKeysString() string {
	if b.keysCache != "" {
		return b.keysCache
	}

	b.keysCache = b.Keys.String()
	return b.keysCache
}

type Profile struct {
	ID       ID
	Name     string
	Bindings []Binding
}

type KeyListener struct {
	keyEventsCh         <-chan crossplatform.KeyEvent
	eventsBuf           []crossplatform.KeyEvent
	isRecording         bool
	isKeyComboAvailable bool
	IsGrabbedByScene    bool
}

func NewKeyListener(keyEventsCh <-chan crossplatform.KeyEvent) KeyListener {
	return KeyListener{
		keyEventsCh: keyEventsCh,
	}
}

func (l *KeyListener) getCurrentKeyCombo() KeyCombo {
	var keys KeyCombo

	for i := range l.eventsBuf {
		keys[i] = NewKey(uint32(l.eventsBuf[i].Key), KeyKindKeyboard)
	}
	slices.Sort(keys[:len(l.eventsBuf)])

	return keys
}

func (l *KeyListener) StartRecording() {
	l.eventsBuf = l.eventsBuf[:0]
	l.Flush()
	l.isRecording = true
}

func (l *KeyListener) StopRecording() KeyCombo {
	if !l.isRecording {
		return KeyCombo{}
	}

	l.eventsBuf = l.eventsBuf[:0]
	l.Flush()
	l.isRecording = false

	assert.InRange(len(l.eventsBuf), 0, len(KeyCombo{}))

	return l.getCurrentKeyCombo()
}

func (l *KeyListener) IsRecordingFinished() (KeyCombo, bool) {
	if len(l.eventsBuf) == 0 {
		return KeyCombo{}, false
	}

	for _, event := range l.eventsBuf {
		if event.IsPressed {
			return KeyCombo{}, false
		}
	}

	return l.getCurrentKeyCombo(), true
}

func (l *KeyListener) GetRecordingKeyCombo() KeyCombo {
	return l.getCurrentKeyCombo()
}

func (l *KeyListener) GetKeyCombo() (keys KeyCombo, ok bool) {
	if !l.isKeyComboAvailable {
		return KeyCombo{}, false
	}

	assert.InRange(len(l.eventsBuf), 1, len(KeyCombo{}))

	for i := range l.eventsBuf {
		keys[i] = NewKey(uint32(l.eventsBuf[i].Key), KeyKindKeyboard)
	}
	slices.Sort(keys[:len(l.eventsBuf)])

	return keys, true
}

func (l *KeyListener) Update() {
	l.isKeyComboAvailable = false

	if runtime.GOOS == "linux" && !crossplatform.HasX11Focus() {
		l.eventsBuf = l.eventsBuf[:0]
		return
	}

	var event crossplatform.KeyEvent
	select {
	case event = <-l.keyEventsCh:
	default:
		return
	}

	if l.isRecording {
		if event.IsPressed {
			l.eventsBuf = slices.DeleteFunc(l.eventsBuf, func(e crossplatform.KeyEvent) bool { return !e.IsPressed })

			l.eventsBuf = l.eventsBuf[:min(len(l.eventsBuf), len(KeyCombo{})-1)]
			l.eventsBuf = append(l.eventsBuf, event)
		} else {
			for i := range l.eventsBuf {
				if l.eventsBuf[i].Key == event.Key {
					l.eventsBuf[i] = event
				}
			}
		}

	} else {
		eventIdx := slices.IndexFunc(l.eventsBuf, func(e crossplatform.KeyEvent) bool { return e.Key == event.Key })

		if !event.IsPressed {
			if eventIdx != -1 {
				l.eventsBuf = slices.Delete(l.eventsBuf, eventIdx, eventIdx+1)
			}
			return
		}

		if eventIdx == -1 {
			l.eventsBuf = append(l.eventsBuf, event)
		} else {
			return
		}

		if len(l.eventsBuf) == 0 || len(l.eventsBuf) > len(KeyCombo{}) {
			return
		}

		l.isKeyComboAvailable = true
	}
}

func (l *KeyListener) Flush() {
	for len(l.keyEventsCh) > 0 {
		<-l.keyEventsCh
	}
	l.eventsBuf = l.eventsBuf[:0]
}

var ErrInvalidConfig = errors.New("the config file is malformed")

const ConfigVersion uint = 0

type JSONStorageHeader struct {
	ConfigVersion uint
}

type JSONStorage struct {
	JSONStorageHeader

	Tracks []Track

	Profiles         []Profile
	CurrentProfileID ID

	TracksGain float32
	OutputGain float32
	InputGain  float32

	WindowSize rl.Vector2

	IsTracksMuted  bool
	IsMonitorMuted bool
	IsInputMuted   bool

	IsDenoiseEnabled bool

	IsPauseRenderEnabled bool

	IsDefaultDevicesWarningEnabled bool

	SelectedInputName  string
	SelectedOutputName string

	ClearSoundsKeyCombo KeyCombo
}

type Storage struct {
	JSONStorage

	garbageFiles []string

	tracksCache map[ID]*Track

	profileNamesBuf []string

	keyComboToTrackID map[KeyCombo]ID

	inputDevices     []crossplatform.AudioDevice
	inputDeviceNames []string
	selectedInputIdx int

	outputDevices     []crossplatform.AudioDevice
	outputDeviceNames []string
	selectedOutputIdx int
}

func NewStorage() *Storage {
	return &Storage{
		tracksCache:       make(map[ID]*Track),
		keyComboToTrackID: make(map[KeyCombo]ID),

		selectedInputIdx:  -1,
		selectedOutputIdx: -1,

		JSONStorage: JSONStorage{
			JSONStorageHeader: JSONStorageHeader{
				ConfigVersion: ConfigVersion,
			},

			WindowSize: gui.Vec2(800, 600),

			TracksGain: linearToExponentialGain(1),
			OutputGain: linearToExponentialGain(1),
			InputGain:  linearToExponentialGain(1),

			IsPauseRenderEnabled: true,

			IsDefaultDevicesWarningEnabled: true,
		},
	}
}

func (s *Storage) SetClearSoundsKeyCombo(keys KeyCombo) error {
	for i := range s.Profiles {
		for j := range s.Profiles[i].Bindings {
			if s.Profiles[i].Bindings[j].Keys == (KeyCombo{}) {
				continue
			}
			assert.NotEqual(s.Profiles[i].Bindings[j].Keys, KeyCombo{})

			if s.Profiles[i].Bindings[j].Keys == keys {
				track := s.GetTrackByID(s.Profiles[i].Bindings[j].TrackID)
				return fmt.Errorf("Key combo %q already in use with binding %q in profile %q", keys, track.Name, s.Profiles[i].Name)
			}
		}
	}

	s.ClearSoundsKeyCombo = keys

	if err := s.Save(); err != nil {
		return err
	}
	return nil
}

func (s *Storage) GetInputDeviceInfoByName(name string) *portaudio.DeviceInfo {
	for _, d := range s.inputDevices {
		if d.Alias == name {
			return d.Info
		}
	}
	return nil
}

func (s *Storage) GetOutputDeviceInfoByName(name string) *portaudio.DeviceInfo {
	for _, d := range s.outputDevices {
		if d.Alias == name {
			return d.Info
		}
	}
	return nil
}

func (s *Storage) GetInputDeviceInfo(idx int) (*portaudio.DeviceInfo, error) {
	if idx >= 0 {
		assert.Less(idx, len(s.inputDevices))
		return s.inputDevices[idx].Info, nil
	}

	if s.selectedInputIdx >= 0 {
		assert.Less(s.selectedInputIdx, len(s.inputDevices))
		return s.inputDevices[s.selectedInputIdx].Info, nil
	}

	if s.SelectedInputName == "" {
		defaultInput, err := portaudio.DefaultInputDevice()
		if err != nil {
			return nil, fmt.Errorf("default input device: %w", err)
		}
		assert.NotNil(defaultInput)

		idx := slices.IndexFunc(s.inputDevices, func(d crossplatform.AudioDevice) bool { return d.Info == defaultInput })
		if idx == -1 {
			return nil, fmt.Errorf("the configured default input device is invalid")
		}
		s.selectedInputIdx = idx
		return defaultInput, nil
	}

	defaultInput := s.GetInputDeviceInfoByName(s.SelectedInputName)
	if defaultInput == nil {
		s.SelectedInputName = ""
		if err := s.Save(); err != nil {
			return nil, err
		}
		return s.GetInputDeviceInfo(-1)
	}
	return defaultInput, nil
}

func (s *Storage) GetOutputDeviceInfo(idx int) (*portaudio.DeviceInfo, error) {
	if idx >= 0 {
		assert.Less(idx, len(s.outputDevices))
		return s.outputDevices[idx].Info, nil
	}

	if s.selectedOutputIdx >= 0 {
		assert.Less(s.selectedOutputIdx, len(s.outputDevices))
		return s.outputDevices[s.selectedOutputIdx].Info, nil
	}

	if s.SelectedOutputName == "" {
		defaultOutput, err := portaudio.DefaultOutputDevice()
		if err != nil {
			return nil, fmt.Errorf("default output device: %w", err)
		}
		assert.NotNil(defaultOutput)

		idx := slices.IndexFunc(s.outputDevices, func(d crossplatform.AudioDevice) bool { return d.Info == defaultOutput })
		if idx == -1 {
			return nil, fmt.Errorf("the configured default output device is invalid")
		}
		s.selectedOutputIdx = idx
		return defaultOutput, nil
	}

	defaultOutput := s.GetOutputDeviceInfoByName(s.SelectedOutputName)
	if defaultOutput == nil {
		s.SelectedOutputName = ""
		if err := s.Save(); err != nil {
			return nil, err
		}
		return s.GetOutputDeviceInfo(-1)
	}
	return defaultOutput, nil
}

func (s *Storage) SetSelectedInputDevice(idx int) error {
	if idx < 0 {
		s.selectedInputIdx = -1
		s.SelectedInputName = ""
	} else {
		device, err := s.GetInputDeviceInfo(idx)
		assert.NoError(err)
		s.selectedInputIdx = idx
		s.SelectedInputName = device.Name
	}

	if err := s.Save(); err != nil {
		return err
	}
	return nil
}

func (s *Storage) SetSelectedOutputDevice(idx int) error {
	if idx < 0 {
		s.selectedOutputIdx = -1
		s.SelectedOutputName = ""
	} else {
		device, err := s.GetOutputDeviceInfo(idx)
		assert.NoError(err)
		s.selectedOutputIdx = idx
		s.SelectedOutputName = device.Name
	}

	if err := s.Save(); err != nil {
		return err
	}
	return nil
}

func (s *Storage) UpdateAudioDevices() error {
	devices, err := crossplatform.GetUserDevices()
	if err != nil {
		return err
	}

	s.inputDevices = s.inputDevices[:0]
	s.inputDeviceNames = s.inputDeviceNames[:0]

	s.outputDevices = s.outputDevices[:0]
	s.outputDeviceNames = s.outputDeviceNames[:0]

	s.selectedInputIdx = -1
	s.selectedOutputIdx = -1

	for _, device := range devices {
		if device.Info.MaxInputChannels > 0 && device.Info.MaxOutputChannels == 0 {
			s.inputDevices = append(s.inputDevices, device)
			s.inputDeviceNames = append(s.inputDeviceNames, device.Alias)
			if device.Info.Name == s.SelectedInputName {
				s.selectedInputIdx = len(s.inputDeviceNames) - 1
			}
		}
		if device.Info.MaxOutputChannels > 0 && device.Info.MaxInputChannels == 0 {
			s.outputDevices = append(s.outputDevices, device)
			s.outputDeviceNames = append(s.outputDeviceNames, device.Alias)
			if device.Info.Name == s.SelectedOutputName {
				s.selectedOutputIdx = len(s.outputDeviceNames) - 1
			}
		}
	}

	if s.selectedInputIdx == -1 {
		s.SelectedInputName = ""
	}
	if s.selectedOutputIdx == -1 {
		s.SelectedOutputName = ""
	}

	if len(s.inputDevices) == 0 {
		return fmt.Errorf("could not get any input device")
	}
	if len(s.outputDevices) == 0 {
		return fmt.Errorf("could not get any output device")
	}
	return nil
}

func (s *Storage) GetTrackByID(id ID) *Track {
	if track, ok := s.tracksCache[id]; ok {
		assert.Equal(track.ID, id)
		return track
	}

	idx := slices.IndexFunc(s.Tracks, func(t Track) bool { return t.ID == id })
	assert.NotEqual(idx, -1)

	s.tracksCache[id] = &s.Tracks[idx]
	return &s.Tracks[idx]
}

func (s *Storage) GetSamplesFromTrackID(trackID ID) (Samples, error) {
	track := s.GetTrackByID(trackID)
	if !track.HasTrack() {
		return nil, fmt.Errorf("track has no sound file assigned (track id = %d)", trackID)
	}

	if !track.IsLoaded() {
		if err := track.LoadSamples(); err != nil {
			return nil, err
		}
	}

	return track.samples, nil
}

func (s *Storage) AddTrack(name, imagePath string, samples Samples, gain float32) error {
	track := Track{
		ID:      GenerateID(),
		Name:    name,
		Gain:    gain,
		samples: samples,
	}
	track.trackPath = path.Join(StorageTracksDirPath, fmt.Sprint(track.ID)+FileExtensionWAV)

	wave := NewWaveFromMonoSamples(samples)
	rl.ExportWave(wave, track.trackPath)
	raylibTraceLog(rl.LogInfo, "Saved track: "+name+" ("+track.trackPath+")")

	if imagePath != "" {
		track.imagePath = path.Join(StorageImagesDirPath, fmt.Sprint(track.ID)+FileExtensionPNG)

		err := ExportImageResize(imagePath, track.imagePath)
		if err != nil {
			return fmt.Errorf("copying image: %w", err)
		}
	}

	s.Tracks = append(s.Tracks, track)
	clear(s.tracksCache)

	if err := s.Save(); err != nil {
		return err
	}

	return nil
}

func (s *Storage) RemoveTrack(id ID) error {
	idx := slices.IndexFunc(s.Tracks, func(t Track) bool { return t.ID == id })
	assert.NotEqual(idx, -1)

	track := s.Tracks[idx]

	s.Tracks = slices.Delete(s.Tracks, idx, idx+1)
	clear(s.tracksCache)

	for i := range s.Profiles {
		s.Profiles[i].Bindings = slices.DeleteFunc(s.Profiles[i].Bindings, func(b Binding) bool { return b.TrackID == id })
	}
	s.RemakeBindingTable()

	if track.HasTrack() {
		err := os.Remove(track.trackPath)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("deleting track file: %w", err)
		}
		raylibTraceLog(rl.LogInfo, "Deleted track file: "+track.Name+" ("+track.trackPath+")")
	}

	if track.HasImage() {
		err := os.Remove(track.imagePath)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("deleting image file file: %w", err)
		}
		raylibTraceLog(rl.LogInfo, "Deleted track image file: "+track.Name+" ("+track.imagePath+")")
	}

	if err := s.Save(); err != nil {
		return err
	}
	return nil
}

func (s *Storage) RemoveTrackImage(trackID ID) error {
	track := s.GetTrackByID(trackID)
	assert.True(track.HasImage())

	if err := os.Remove(track.imagePath); err != nil {
		assert.False(errors.Is(err, os.ErrNotExist))
	}
	gui.UnloadImageTexture(track.imagePath, nil)

	track.imagePath = ""

	if err := s.Save(); err != nil {
		return err
	}
	return nil
}

func (s *Storage) SetTrackImage(trackID ID, imagePath string) error {
	track := s.GetTrackByID(trackID)

	newImagePath := path.Join(StorageImagesDirPath, fmt.Sprint(track.ID)+FileExtensionPNG)

	if err := ExportImageResize(imagePath, newImagePath); err != nil {
		return fmt.Errorf("copying image: %w", err)
	}

	gui.UnloadImageTexture(newImagePath, nil)
	track.imagePath = newImagePath

	return nil
}

func (s *Storage) UnloadHeavyTracks() (unloaded bool) {
	for i := range s.Tracks {
		if s.Tracks[i].IsHeavy() {
			s.Tracks[i].UnloadSamples()
			unloaded = true
		}
	}
	return unloaded
}

func (s *Storage) AddProfile(name string) error {
	profile := Profile{
		ID:   GenerateID(),
		Name: name,
	}
	s.Profiles = append(s.Profiles, profile)

	s.SetCurrentProfileID(profile.ID)

	if err := s.Save(); err != nil {
		return err
	}
	return nil
}

func (s *Storage) EditProfile(id ID, newName string) error {
	profile := s.GetProfileByID(id)
	assert.NotNil(profile)

	profile.Name = newName

	if err := s.Save(); err != nil {
		return err
	}
	return nil
}

func (s *Storage) RemoveProfile(id ID) error {
	s.Profiles = slices.DeleteFunc(s.Profiles, func(p Profile) bool { return p.ID == id })

	s.SetCurrentProfileID(0)

	if err := s.Save(); err != nil {
		return err
	}
	return nil
}

func (s *Storage) GetProfileByIndex(idx int) *Profile {
	assert.InRange(idx, 0, len(s.Profiles)-1)
	profile := &s.Profiles[idx]
	assert.NotEqual(profile.ID, 0)
	return profile
}

func (s *Storage) GetProfileByID(id ID) *Profile {
	idx := slices.IndexFunc(s.Profiles, func(p Profile) bool { return p.ID == id })
	assert.NotEqual(idx, -1)

	return &s.Profiles[idx]
}

func (s *Storage) SetCurrentProfileID(profileID ID) {
	s.CurrentProfileID = profileID
	s.RemakeBindingTable()
}

func (s *Storage) GetCurrentProfileIdx() int16 {
	return int16(slices.IndexFunc(s.Profiles, func(p Profile) bool { return p.ID == s.CurrentProfileID }))
}

func (s *Storage) GetCurrentProfile() *Profile {
	idx := slices.IndexFunc(s.Profiles, func(p Profile) bool { return p.ID == s.CurrentProfileID })
	if idx == -1 {
		return nil
	}
	return &s.Profiles[idx]
}

func (s *Storage) GetProfileNames() []string {
	s.profileNamesBuf = s.profileNamesBuf[:0]

	for i := range s.Profiles {
		s.profileNamesBuf = append(s.profileNamesBuf, s.Profiles[i].Name)
	}
	return s.profileNamesBuf
}

func (s *Storage) AddBinding(profile *Profile, trackID ID) error {
	if slices.ContainsFunc(profile.Bindings, func(b Binding) bool { return b.TrackID == trackID }) {
		return nil
	}

	profile.Bindings = append(profile.Bindings, Binding{
		TrackID:   trackID,
		IsEnabled: true,
	})

	s.RemakeBindingTable()

	if err := s.Save(); err != nil {
		return err
	}
	return nil
}

func (s *Storage) RemoveBinding(profile *Profile, trackID ID) error {
	profile.Bindings = slices.DeleteFunc(profile.Bindings, func(b Binding) bool { return b.TrackID == trackID })

	s.RemakeBindingTable()

	if err := s.Save(); err != nil {
		return err
	}
	return nil
}

func (s *Storage) SetBindingKeyCombo(binding *Binding, keys KeyCombo) error {
	if trackID, ok := s.keyComboToTrackID[keys]; ok {
		track := s.GetTrackByID(trackID)
		return fmt.Errorf("Key combo %q already in use with binding %q", keys, track.Name)
	}
	if s.ClearSoundsKeyCombo != (KeyCombo{}) && s.ClearSoundsKeyCombo == keys {
		return fmt.Errorf("Key combo %q already in use for \"Clear sounds\" shortcut (see settings)", keys)
	}

	binding.Keys = keys
	s.RemakeBindingTable()

	if err := s.Save(); err != nil {
		return err
	}
	return nil
}

func (s *Storage) SetBindingEnabled(binding *Binding, enabled bool) error {
	binding.IsEnabled = enabled
	s.RemakeBindingTable()

	if err := s.Save(); err != nil {
		return err
	}
	return nil
}

func (s *Storage) GetTrackIDByKeyCombo(keys KeyCombo) (ID, bool) {
	for i, key := range keys {
		if !key.IsSet() {
			break
		}
		assert.True(key.IsValid(), keys, i)
	}
	id, ok := s.keyComboToTrackID[keys]
	return id, ok
}

func (s *Storage) RemakeBindingTable() {
	clear(s.keyComboToTrackID)

	profile := s.GetCurrentProfile()
	if profile == nil {
		return
	}

	for _, binding := range profile.Bindings {
		if !binding.Keys.IsSet() || !binding.IsEnabled {
			continue
		}
		s.keyComboToTrackID[binding.Keys] = binding.TrackID
	}
}

func (s *Storage) RemoveGarbageFiles() error {
	for _, filePath := range s.garbageFiles {
		if err := os.Remove(filePath); err != nil {
			raylibTraceLog(rl.LogError, fmt.Sprintf("Failed to remove %q: %s", filePath, err))
			return err
		}
		raylibTraceLog(rl.LogInfo, fmt.Sprintf("Removed %s", filePath))
	}
	return nil
}

func (s *Storage) Load() error {
	file, err := os.Open(StorageConfigFilePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("opening config file: %w", err)
	}
	defer file.Close()

	decoder := json.NewDecoder(file)

	var dataHeader JSONStorageHeader
	if err := decoder.Decode(&dataHeader); err != nil {
		return fmt.Errorf("%w: header: %w", ErrInvalidConfig, err)
	}
	if dataHeader.ConfigVersion != ConfigVersion {
		// TODO: modify user config
		return fmt.Errorf("config version mismatch: expected %q, got %q", ConfigVersion, dataHeader.ConfigVersion)
	}
	assert.Equal(gui.Must(file.Seek(0, 0)), 0)

	data := s.JSONStorage
	if err = decoder.Decode(&data); err != nil {
		return fmt.Errorf("%w: content: %w", ErrInvalidConfig, err)
	}
	s.JSONStorage = data

	s.RemakeBindingTable()

	trackEntries, err := os.ReadDir(StorageTracksDirPath)
	if err != nil {
		return fmt.Errorf("listing saved tracks: %w", err)
	}
	imageEntries, err := os.ReadDir(StorageImagesDirPath)
	if err != nil {
		return fmt.Errorf("listing saved images: %w", err)
	}

	s.garbageFiles = s.garbageFiles[:0]
	indexedFiles := make(map[ID]struct{ trackPath, imagePath string }, max(len(trackEntries), len(imageEntries)))

	trackRegex := regexp.MustCompile(`^(\d+)\.wav$`)
	imageRegex := regexp.MustCompile(`^(\d+)\.png$`)

	for _, entry := range trackEntries {
		entryPath := path.Join(StorageTracksDirPath, entry.Name())

		if matches := trackRegex.FindStringSubmatch(entry.Name()); matches != nil {
			assert.Equal(len(matches), 2)
			idStr := matches[1]

			id, err := strconv.ParseUint(idStr, 10, 64)
			if err != nil {
				raylibTraceLog(rl.LogWarning, fmt.Sprintf("Parsing track file entry %q: %s", entryPath, err))
				goto DiscardTrack
			}
			tmp := indexedFiles[id]
			tmp.trackPath = entryPath
			indexedFiles[id] = tmp
			continue
		}

	DiscardTrack:
		s.garbageFiles = append(s.garbageFiles, entryPath)
	}

	for _, entry := range imageEntries {
		entryPath := path.Join(StorageImagesDirPath, entry.Name())

		if matches := imageRegex.FindStringSubmatch(entry.Name()); matches != nil {
			assert.Equal(len(matches), 2)
			idStr := matches[1]

			id, err := strconv.ParseUint(idStr, 10, 64)
			if err != nil {
				raylibTraceLog(rl.LogWarning, fmt.Sprintf("Parsing track image file entry %q: %s", entryPath, err))
				goto DiscardImage
			}
			tmp := indexedFiles[id]
			tmp.imagePath = entryPath
			indexedFiles[id] = tmp
			continue
		}

	DiscardImage:
		s.garbageFiles = append(s.garbageFiles, entryPath)
	}

	for i := range s.Tracks {
		index := indexedFiles[s.Tracks[i].ID]

		// it's ok to not have a trackFile, the track will be displayed as broken
		s.Tracks[i].trackPath = index.trackPath
		s.Tracks[i].imagePath = index.imagePath
	}

	raylibTraceLog(rl.LogInfo, "Loaded config: "+StorageConfigFilePath)
	return nil
}

func (s *Storage) Save() error {
	buf := bytes.Buffer{}
	encoder := json.NewEncoder(&buf)

	encoder.SetIndent("", "\t")
	if err := encoder.Encode(s); err != nil {
		return fmt.Errorf("encoding config state: %w", err)
	}

	file, err := os.Create(StorageConfigFilePath)
	if err != nil {
		return fmt.Errorf("creating config file: %w", err)
	}
	defer file.Close()

	bufLen := buf.Len()
	n, err := file.ReadFrom(&buf)
	if err != nil {
		return fmt.Errorf("reading config file: %w", err)
	}
	assert.Equal(int(n), bufLen)

	raylibTraceLog(rl.LogInfo, "Saved config: "+StorageConfigFilePath)
	return nil
}

type Theme struct {
	primary1 rl.Color
	primary2 rl.Color
	primary3 rl.Color

	secondary1 rl.Color
	secondary2 rl.Color
	secondary3 rl.Color

	bg1 rl.Color
	bg2 rl.Color
	bg3 rl.Color

	fg1 rl.Color
	fg2 rl.Color
	fg3 rl.Color

	error1 rl.Color
	error2 rl.Color

	fontNormal rl.Font
	fontBold   rl.Font

	padding  gui.BoxSides
	childGap float32
	border   gui.BoxSides
	radius   gui.BoxCorners

	popupPadding  gui.BoxSides
	popupChildGap float32
}

var theme = Theme{
	primary1: gui.ColorHex(0x7ABAF4FF),
	primary2: gui.ColorHex(0x61B0F2FF),
	primary3: gui.ColorHex(0x42A7F0FF),

	secondary1: gui.ColorHex(0xF7A072FF),
	secondary2: gui.ColorHex(0xF68A51FF),
	secondary3: gui.ColorHex(0xF47E3EFF),

	bg1: gui.ColorHex(0x1A1F25FF),
	bg2: gui.ColorHex(0x2F3439FF),
	bg3: gui.ColorHex(0x464A4FFF),

	fg1: gui.ColorHex(0xF6FFFFFF),
	fg2: gui.ColorHex(0xCBDDDDFF),
	fg3: gui.ColorHex(0x707D7DFF),

	error1: gui.ColorHex(0xE56666FF),
	error2: gui.ColorHex(0xE56666AC),

	padding:  gui.Padding(12),
	childGap: 12,
	border:   gui.Border(2),
	radius:   gui.Radius(8),

	popupPadding:  gui.Padding(12 * 2),
	popupChildGap: 12 * 2,
}

var _unloadedHeavyTracks = false

func UnloadHeavyUnusedTracks(storage *Storage, audioPlayer *AudioPlayer) {
	if audioPlayer.IsPlayingSounds() {
		_unloadedHeavyTracks = false
		return
	}

	if _unloadedHeavyTracks {
		return
	}

	audioPlayer.audioMutex.Lock()
	defer audioPlayer.audioMutex.Unlock()

	clear(audioPlayer.virtualMicSounds[:cap(audioPlayer.virtualMicSounds)])
	clear(audioPlayer.outputSounds[:cap(audioPlayer.outputSounds)])

	if storage.UnloadHeavyTracks() {
		_unloadedHeavyTracks = true
		raylibTraceLog(rl.LogInfo, "Unloaded heavy unused tracks")
	}
}

//go:embed assets/fonts/roboto/static/Roboto-Medium.ttf
//go:embed assets/fonts/roboto/static/Roboto-SemiBold.ttf
var fontsFS embed.FS

//go:embed assets/icons/*
var iconsFS embed.FS

//go:embed assets/images/*
var imagesFS embed.FS

var muteSoundSamples SamplesInt8
var unmuteSoundSamples SamplesInt8

func init() {
	muteSoundSamples = make(SamplesInt8, SampleRate*0.1)
	for i := range muteSoundSamples {
		t := float64(i) / SampleRate
		sample := math.Sin(2 * math.Pi * 400 * t)
		muteSoundSamples[i] = int8(sample * 0.8 * 127)
	}
	unmuteSoundSamples = make(SamplesInt8, SampleRate*(0.1+0.1+0.1))
	copy(unmuteSoundSamples, muteSoundSamples)
	copy(unmuteSoundSamples[int(SampleRate*(0.1+0.1)):], muteSoundSamples)
}

var DevMode = os.Getenv("SOUNDPAD_DEV") == "1"
var KeyToggleGUIDebug = NewKey(59, KeyKindKeyboard) // F1

func raylibTraceLog(level rl.TraceLogLevel, msg string) {
	rl.TraceLog(level, "SOUNDPAD: "+msg)
}

func logAndOpenLoadingErrorWindow(err error) {
	raylibTraceLog(rl.LogError, err.Error())

	gui.InitWindow(gui.Vec2(500, 200), "Error")
	defer gui.CloseWindow()

	rl.SetTargetFPS(10)

	font := gui.Must(gui.LoadFontFS(fontsFS, "assets/fonts/roboto/static/Roboto-Medium.ttf", 64, 0))
	defer rl.UnloadFont(font)
	gui.SetDefaultFont(font)
	gui.DefaultTextColor = theme.fg1
	gui.DefaultFontSize = 22

	subWindow := gui.AddSubWindow(gui.NewSubWindow(gui.SubWindowProps{
		SizingX: gui.Grow(),
		SizingY: gui.Grow(),
	}), gui.Vec2(0, 0))

	for !rl.WindowShouldClose() {
		root := subWindow.SetRoot(gui.NewBox(gui.BoxProps{
			SizingX:     gui.Grow(),
			SizingY:     gui.Grow(),
			Padding:     theme.popupPadding,
			BgColor:     theme.bg1,
			Orientation: gui.Vertical,
		}))
		gui.AddChild(root, gui.NewText(gui.TextProps{
			BoxProps: gui.BoxProps{
				SizingX: gui.Grow(),
				SizingY: gui.Grow(),
			},
			Wrapping: gui.Wrap,
		}, err.Error()))
		buttonsBox := gui.AddChild(root, gui.NewBox(gui.BoxProps{
			SizingX:     gui.Grow(),
			ChildAlignX: gui.End,
			ChildGap:    theme.popupChildGap,
		}))
		copyBtn := gui.AddChild(buttonsBox, NewPrimaryButton("copy.png", "Copy error", theme.primary1))
		closeBtn := gui.AddChild(buttonsBox, NewPrimaryButton("cross.png", "Close", theme.error1))

		gui.ComputeLayout()

		gui.Update()

		if copyBtn.IsLeftButtonPressed() {
			rl.SetClipboardText(err.Error())
		}

		if closeBtn.IsLeftButtonPressed() {
			break
		}

		rl.BeginDrawing()
		gui.Render()
		rl.EndDrawing()
	}
}

const TargetFPS = 60

func main() {
	if err := Main(); err != nil {
		logAndOpenLoadingErrorWindow(err)
		os.Exit(1)
	}
}

func Main() error {
	defer raylibTraceLog(rl.LogInfo, "Terminated successfully")

	if runtime.GOOS == "linux" {
		// only x11 is supported at the moment
		os.Setenv("XDG_SESSION_TYPE", "x11")
		raylibTraceLog(rl.LogInfo, "Session type set to X11")
	}

	if err := rnnoise.Load(nil); err != nil {
		return fmt.Errorf("Loading rnnoise: %w", err)
	}
	raylibTraceLog(rl.LogInfo, "RNNoise global state initialized")

	if err := crossplatform.LoadSharedLibs(); err != nil {
		return fmt.Errorf("Loading shared libs: %w", err)
	}
	defer crossplatform.UnloadSharedLibs()

	keyPressesCh, err := crossplatform.BeginListeningKeyPresses()
	if err != nil {
		return fmt.Errorf("Setting the global keypress listener: %w", err)
	}
	defer crossplatform.EndListeningKeyPresses()

	keyListener := NewKeyListener(keyPressesCh)

	err = portaudio.Initialize()
	if err != nil {
		return fmt.Errorf("Initializing audio: %w", err)
	}
	raylibTraceLog(rl.LogInfo, "Portaudio initialized")
	defer func() {
		if err := portaudio.Terminate(); err != nil {
			raylibTraceLog(rl.LogError, fmt.Sprintf("Deinitializing audio: %s", err))
		} else {
			raylibTraceLog(rl.LogInfo, "Portaudio deinitialized")
		}
	}()

	virtualMicInfo, needsDrivers, err := crossplatform.GetVirtualMicSinkInfo()
	if err != nil {
		return fmt.Errorf("Getting information about the virtual microphone: %w", err)
	}
	if needsDrivers {
		return fmt.Errorf("Missing virtual microphone driver (VB-CABLE Driver). You can get it from here: https://vb-audio.com/Cable")
	}
	assert.GreaterEqual(virtualMicInfo.MaxOutputChannels, 1)

	storage := NewStorage()
	if err := storage.Load(); err != nil {
		return fmt.Errorf("Loading config: %w", err)
	}
	if err := storage.UpdateAudioDevices(); err != nil {
		return fmt.Errorf("Updating audio devices: %w", err)
	}

	inputInfo, err := storage.GetInputDeviceInfo(-1)
	if err != nil {
		return fmt.Errorf("Could not get information about the input device (reset to default): %w", err)
	}
	assert.NotNil(inputInfo)
	assert.GreaterEqual(inputInfo.MaxInputChannels, 1)

	outputInfo, err := storage.GetOutputDeviceInfo(-1)
	if err != nil {
		return fmt.Errorf("Could not get information about the output device (reset to default): %w", err)

	}
	assert.NotNil(outputInfo)
	assert.GreaterEqual(outputInfo.MaxOutputChannels, 1)

	audioPlayer := NewAudioPlayer(storage, virtualMicInfo)

	if err := audioPlayer.StartVirtualMicStream(inputInfo); err != nil {
		return fmt.Errorf("Could not open the virtual microphone and input stream: %w", err)
	}
	defer func() {
		if audioPlayer.virtualMicAndInputStream == nil {
			return
		}
		if err := audioPlayer.CloseVirtualMicStream(); err != nil {
			raylibTraceLog(rl.LogError, fmt.Sprintf("Could not close the virtual microphone and input stream: %s", err))
		}
	}()

	if err := audioPlayer.StartOutputStream(outputInfo); err != nil {
		return fmt.Errorf("Could not open the output stream: %w", err)
	}
	defer func() {
		if audioPlayer.outputStream == nil {
			return
		}
		if err := audioPlayer.CloseOutputStream(); err != nil {
			raylibTraceLog(rl.LogError, fmt.Sprintf("Could not close the output stream: %s", err))
		}
	}()

	go func() {
		ticker := time.NewTicker(10 * time.Second)
		for {
			<-ticker.C
			debug.FreeOSMemory()
		}
	}()

	rl.SetConfigFlags(rl.FlagMsaa4xHint | rl.FlagWindowResizable | rl.FlagWindowAlwaysRun)
	rl.SetTargetFPS(TargetFPS)

	gui.InitWindow(storage.WindowSize, "raylib-test")
	defer gui.CloseWindow()

	if DevMode {
		rl.SetExitKey(rl.KeyQ)
	} else {
		rl.SetExitKey(rl.KeyNull)
	}

	rl.SetWindowIcon(*gui.Must(gui.LoadImage("assets/icons/soundpad.png", iconsFS)))

	theme.fontNormal = gui.Must(gui.LoadFontFS(fontsFS, "assets/fonts/roboto/static/Roboto-Medium.ttf", 64, 0))
	defer rl.UnloadFont(theme.fontNormal)

	theme.fontBold = gui.Must(gui.LoadFontFS(fontsFS, "assets/fonts/roboto/static/Roboto-SemiBold.ttf", 64, 0))
	defer rl.UnloadFont(theme.fontBold)

	gui.SetDefaultFont(theme.fontNormal)
	gui.DefaultTextColor = theme.fg1
	gui.DefaultSliderThumbWidth = 20
	gui.DefaultSliderTrackWidth = 10

	scene := NewScene()

	go func() {
		for {
			if err := audioPlayer.ProcessVirtualSinkAndInput(); err != nil {
				msg := fmt.Sprintf("Processing the virtual microphone and input stream: %s", err)

				if errors.Is(err, portaudio.TimedOut) || errors.Is(err, portaudio.InputOverflowed) || errors.Is(err, portaudio.OutputUnderflowed) {
					raylibTraceLog(rl.LogWarning, msg)
					continue
				}
				scene.OpenErrorPopup(msg, true)
				return
			}
		}
	}()
	go func() {
		for {
			if err := audioPlayer.ProcessOutput(); err != nil {
				msg := fmt.Sprintf("Processing the output stream: %s", err)

				if errors.Is(err, portaudio.TimedOut) || errors.Is(err, portaudio.InputOverflowed) || errors.Is(err, portaudio.OutputUnderflowed) {
					raylibTraceLog(rl.LogWarning, msg)
					continue
				}
				scene.OpenErrorPopup(msg, true)
				return
			}
		}
	}()

	TimestampNever := int64(math.MaxInt64)
	nextWindowResizeSaveTimestamp := TimestampNever

	shouldExit := false
	isRenderingSuspended := false

	for !rl.WindowShouldClose() && !shouldExit {

		if !storage.IsPauseRenderEnabled || rl.IsWindowFocused() {

			if windowSize := gui.GetScreenSize(); windowSize != storage.WindowSize {
				storage.WindowSize = windowSize
				nextWindowResizeSaveTimestamp = time.Now().Add(2 * time.Second).Unix()
			}
			if time.Now().Unix() > nextWindowResizeSaveTimestamp {
				if err := storage.Save(); err != nil {
					return fmt.Errorf("Could not save window size: %w", err)
				}
				nextWindowResizeSaveTimestamp = TimestampNever
			}

			isRenderingSuspended = false
			rl.BeginDrawing()
			shouldExit = scene.Run(storage, &keyListener, audioPlayer)
			time.Sleep((1000 / TargetFPS) * time.Millisecond) // it's cheaper to sleep in Go
			rl.EndDrawing()

		} else {
			if !isRenderingSuspended {
				rl.BeginDrawing()
				rl.DrawRectangleV(gui.Vec2(0, 0), storage.WindowSize, rl.ColorAlpha(rl.Black, 0.2))
				rl.EndDrawing()
				isRenderingSuspended = true
			}
			time.Sleep(100 * time.Millisecond)
			rl.PollInputEvents()
		}

		keyListener.Update()

		if !keyListener.IsGrabbedByScene && !keyListener.isRecording {
			if keys, ok := keyListener.GetKeyCombo(); ok && keys.IsValid() {
				switch {
				case DevMode && keys.Equal(KeyToggleGUIDebug):
					gui.Debug = !gui.Debug

				case storage.ClearSoundsKeyCombo.IsSet() && keys == storage.ClearSoundsKeyCombo:
					audioPlayer.ClearSounds()

				default:
					if trackID, ok := storage.GetTrackIDByKeyCombo(keys); ok {
						if _, err := audioPlayer.AddTrack(storage, trackID, false); err != nil {
							track := storage.GetTrackByID(trackID)
							assert.NotNil(track)
							scene.OpenErrorPopup(fmt.Sprintf("Could not play track [ID=%d NAME=%q]: %s", track.ID, track.Name, err), false)
						}
					}
				}
			}
		}

		UnloadHeavyUnusedTracks(storage, audioPlayer)
	}

	return nil
}
