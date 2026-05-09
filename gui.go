package main

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/MarcosTypeAP/go-assert"
	"github.com/MarcosTypeAP/go-rlgui"
	"github.com/MarcosTypeAP/soundpad/internal/crossplatform"
	"github.com/gordonklaus/portaudio"

	rl "github.com/gen2brain/raylib-go/raylib"

	_ "embed"
)

//go:embed THIRD_PARTY_LICENSES
var licensesString string

type Scene struct {
	rootSubWindow *gui.SubWindow

	popupSubWindow *gui.SubWindow
	popupState     PopupState
	popupOnClose   func()

	ctxMenuSubWindow *gui.SubWindow
	ctxMenuState     CtxMenuState
	ctxMenuOnClose   func()

	errorPopupSubWindow *gui.SubWindow
	errorPopupErrMsg    string
	errorPopupIsFatal   bool

	profileToEdit *Profile

	trackIDToDelete ID

	addTrackImagePath string
	addTrackFilePath  string
	addTrackSamples   SamplesFloat32

	selectedBinding                    *Binding
	isSelectedBindingListeningKeyCombo bool

	selectedTrack            *Track
	selectedTrackTestSoundID SoundID

	isSettingsPopupListeningKeyCombo bool

	haveDisplayedDefaultDevicesWarning bool
	havePromptedToCleanGarbageFiles    bool
}

func NewScene() *Scene {
	scene := &Scene{
		rootSubWindow: gui.AddSubWindow(gui.NewSubWindow(gui.SubWindowProps{
			DebugID: "root",
			SizingX: gui.Grow(),
			SizingY: gui.Grow(),
			ZIndex:  gui.ZIndexRoot,
		}), gui.Vec2(0, 0)),

		popupSubWindow: gui.AddSubWindow(gui.NewSubWindow(gui.SubWindowProps{
			DebugID: "popup",
			SizingX: gui.Grow(),
			SizingY: gui.Grow(),
			Hidden:  true,
			ZIndex:  gui.ZIndexPopup,
		}), gui.Vec2(0, 0)),

		errorPopupSubWindow: gui.AddSubWindow(gui.NewSubWindow(gui.SubWindowProps{
			DebugID: "error popup",
			SizingX: gui.Grow(),
			SizingY: gui.Grow(),
			Hidden:  true,
			ZIndex:  gui.ZIndexPopup,
		}), gui.Vec2(0, 0)),

		ctxMenuSubWindow: gui.AddSubWindow(gui.NewSubWindow(gui.SubWindowProps{
			DebugID: "ctx menu",
			Hidden:  true,
			ZIndex:  gui.ZIndexEphemeral,
		}), gui.Vec2(0, 0)),
	}
	return scene
}

func (s *Scene) OpenErrorPopup(msg string, isFatal bool) {
	raylibTraceLog(rl.LogError, msg)

	if s.errorPopupErrMsg == "" {
		s.errorPopupErrMsg = msg
	} else {
		s.errorPopupErrMsg += "\n\n" + msg
	}
	if isFatal {
		s.errorPopupIsFatal = isFatal
	}
	s.errorPopupSubWindow.Show()

	s.ClosePopup()
	s.CloseCtxMenu()
}

func (s *Scene) CloseErrorPopup() {
	s.errorPopupErrMsg = ""
	s.errorPopupIsFatal = false
	s.errorPopupSubWindow.Hide()
}

func (s *Scene) OpenPopup(state PopupState) {
	assert.NotEqual(state, PopupNone)
	s.popupState = state
	s.popupSubWindow.ResetIDs()
	s.popupSubWindow.Show()
	s.popupOnClose = nil
}

func (s *Scene) ClosePopup() {
	s.popupState = PopupNone
	s.popupSubWindow.Hide()

	if s.popupOnClose != nil {
		s.popupOnClose()
	}
	s.popupOnClose = nil
}

func (s *Scene) OpenCtxMenu(state CtxMenuState, position *rl.Vector2) {
	assert.NotEqual(state, CtxMenuNone)
	s.ctxMenuState = state
	s.ctxMenuSubWindow.ResetIDs()
	s.ctxMenuOnClose = nil

	if position != nil {
		s.ctxMenuSubWindow.Move(*position)
	} else {
		s.ctxMenuSubWindow.Move(rl.Vector2SubtractValue(rl.GetMousePosition(), 10))
	}

	s.ctxMenuSubWindow.Show()
}

func (s *Scene) CloseCtxMenu() {
	s.ctxMenuState = CtxMenuNone
	s.ctxMenuSubWindow.Hide()

	if s.ctxMenuOnClose != nil {
		s.ctxMenuOnClose()
	}
	s.ctxMenuOnClose = nil
}

func (s *Scene) Run(storage *Storage, keyListener *KeyListener, audioPlayer *AudioPlayer) (shouldExitProgram bool) {
	//// Build Layout ////
	gui.ResetLayout()

	if storage.IsDefaultDevicesWarningEnabled && !s.haveDisplayedDefaultDevicesWarning {
		s.haveDisplayedDefaultDevicesWarning = true
		s.OpenPopup(PopupDefaultDevicesWarning)
	}
	if s.popupState == PopupDefaultDevicesWarning {
		s.BuildPopupDefaultDevicesWarning(storage)
	}

	if len(storage.garbageFiles) > 0 && !s.havePromptedToCleanGarbageFiles {
		s.havePromptedToCleanGarbageFiles = true
		s.OpenPopup(PopupPromptToCleanGarbageFiles)
	}
	if s.popupState == PopupPromptToCleanGarbageFiles {
		s.BuildPopupPromptToCleanGarbageFiles(storage)
	}

	root := s.rootSubWindow.SetRoot(gui.NewBox(gui.BoxProps{
		SizingX:     gui.Grow(),
		SizingY:     gui.Grow(),
		Orientation: gui.Vertical,
		ChildGap:    theme.childGap,
		Padding:     theme.padding,
		BgColor:     theme.bg1,
	}))

	headerBox := gui.AddChild(root, gui.NewBox(gui.BoxProps{
		SizingX:  gui.Grow(),
		ChildGap: theme.childGap,
	}))
	headerBoxLeft := gui.AddChild(headerBox, gui.NewBox(gui.BoxProps{
		SizingX:     gui.Grow(),
		SizingY:     gui.Grow(),
		ChildAlignX: gui.Start,
		ChildAlignY: gui.Center,
		ChildGap:    theme.childGap,
	}))
	headerBoxCenter := gui.AddChild(headerBox, gui.NewBox(gui.BoxProps{
		SizingX:     gui.Grow(),
		SizingY:     gui.Grow(),
		ChildAlignX: gui.Center,
		ChildAlignY: gui.Center,
		ChildGap:    2,
	}))
	headerBoxRight := gui.AddChild(headerBox, gui.NewBox(gui.BoxProps{
		SizingX:     gui.Grow(),
		SizingY:     gui.Grow(),
		ChildAlignX: gui.End,
		ChildAlignY: gui.Center,
		ChildGap:    theme.childGap,
	}))

	addTrackBtn := gui.AddChild(headerBoxLeft, NewPrimaryButton("plus.png", "Add Track", theme.secondary1))
	gui.AddPostUpdate(func() {
		if addTrackBtn.IsLeftButtonPressed() {
			selectedFile, ok, err := crossplatform.OpenFileDialog("Select Track", "MP3 (*.mp3) | *.mp3")
			if err != nil {
				s.OpenErrorPopup(fmt.Sprintf("Could not open the file selecting dialog: %s", err), false)
				return
			}
			if ok {
				wave := rl.LoadWave(selectedFile)
				defer func() { rl.UnloadWave(wave) }()
				rl.WaveFormat(&wave, SampleRate, 32, 1)
				if !rl.IsWaveValid(wave) {
					s.OpenErrorPopup(fmt.Sprintf("Could not load the track %q: invalid audio file (see logs)", selectedFile), false)
					return
				}
				s.addTrackFilePath = selectedFile
				s.addTrackSamples = LoadMonoWaveSamplesFloat32(wave)
				s.addTrackImagePath = ""
				audioPlayer.ClearSounds()
				s.OpenPopup(PopupAddTrack)
			}
		}
	})

	if s.popupState == PopupAddTrack {
		s.BuildPopupAddTrack(storage, audioPlayer)
	}

	profileDropdown := gui.AddChild(headerBoxCenter, gui.NewDropdown(gui.DropdownProps{
		BoxProps: gui.BoxProps{
			ID:           gui.NewID("profile dropdown"),
			BgColor:      theme.primary1,
			Padding:      theme.padding,
			CornerRadius: theme.radius,
		},
		FontConfigProps: gui.FontConfigProps{
			Font:    theme.fontBold,
			FgColor: theme.bg1,
		},
		OptionsCornerRadius: theme.radius,
	}, "Select Profile", storage.GetProfileNames(), storage.GetCurrentProfileIdx()))

	gui.AddChild(headerBoxCenter, gui.NewSpacerX(gui.NewSizingFixed(3)))

	gui.AddPostUpdate(func() {
		if profileDropdown.HasChanged() {
			if idx := profileDropdown.GetSelectedIdx(); idx != -1 {
				profileID := storage.GetProfileByIndex(idx).ID
				storage.SetCurrentProfileID(profileID)
				if err := storage.Save(); err != nil {
					s.OpenErrorPopup(fmt.Sprintf("Could not save config: %s", err), false)
					return
				}
			}
		}
	})

	if profileDropdown.GetSelectedIdx() != -1 {
		deleteProfileBtn := gui.AddChild(headerBoxCenter, gui.NewButton(gui.ButtonProps{
			BoxProps: gui.BoxProps{
				SizingY:      gui.Grow(),
				ChildAlignY:  gui.Center,
				Padding:      gui.Padding(10, 10, 10, 12),
				BgColor:      theme.error1,
				CornerRadius: gui.RadiusOverride(theme.radius, -1, 0, 0, -1),
			},
			OnHover: gui.EffectBrighten,
		}))
		gui.AddChild(deleteProfileBtn, gui.NewBoxImage(gui.BoxProps{
			SizingX:     gui.Fixed(24),
			SizingY:     gui.Fixed(24),
			TextureTint: theme.bg1,
		}, "assets/icons/trash.png", iconsFS))

		renameProfileBtn := gui.AddChild(headerBoxCenter, gui.NewButton(gui.ButtonProps{
			BoxProps: gui.BoxProps{
				SizingY:     gui.Grow(),
				ChildAlignY: gui.Center,
				Padding:     gui.Padding(10, 10, 10, 12),
				BgColor:     theme.primary1,
			},
			OnHover: gui.EffectBrighten,
		}))
		gui.AddChild(renameProfileBtn, gui.NewBoxImage(gui.BoxProps{
			SizingX:     gui.Fixed(24),
			SizingY:     gui.Fixed(24),
			TextureTint: theme.bg1,
		}, "assets/icons/rename.png", iconsFS))

		gui.AddPostUpdate(func() {
			if deleteProfileBtn.IsLeftButtonPressed() {
				s.OpenPopup(PopupRemoveProfileConfirmation)
			}

			if renameProfileBtn.IsLeftButtonPressed() {
				profile := storage.GetCurrentProfile()
				assert.NotNil(profile)

				s.profileToEdit = profile
				s.OpenPopup(PopupEditProfile)
			}
		})

		if s.popupState == PopupRemoveProfileConfirmation {
			s.BuildPopupRemoveProfileConfirmation(storage, profileDropdown)
		}
	}

	addProfileBtn := gui.AddChild(headerBoxCenter, gui.NewButton(gui.ButtonProps{
		BoxProps: gui.BoxProps{
			SizingY:      gui.Grow(),
			ChildAlignY:  gui.Center,
			Padding:      gui.Padding(12),
			CornerRadius: gui.Ternary(profileDropdown.HasSelection(), gui.RadiusOverride(theme.radius, 0, -1, -1, 0), theme.radius),
			BgColor:      theme.primary1,
		},
		OnHover: gui.EffectBrighten,
	}))
	gui.AddChild(addProfileBtn, gui.NewBoxImage(gui.BoxProps{
		SizingX:     gui.Fixed(20),
		SizingY:     gui.Fixed(20),
		TextureTint: theme.bg1,
	}, "assets/icons/plus.png", iconsFS))

	gui.AddPostUpdate(func() {
		if addProfileBtn.IsLeftButtonPressed() {
			s.OpenPopup(PopupAddProfile)
		}
	})

	if s.popupState == PopupAddProfile {
		s.profileToEdit = nil
		s.BuildPopupAddOrEditProfile(storage, profileDropdown)
	}
	if s.popupState == PopupEditProfile {
		assert.NotNil(s.profileToEdit)
		s.BuildPopupAddOrEditProfile(storage, profileDropdown)
	}

	settingsBtn := gui.AddChild(headerBoxRight, NewPrimaryButton("settings.png", "", theme.primary1))

	gui.AddPostUpdate(func() {
		if settingsBtn.IsLeftButtonPressed() {
			s.OpenPopup(PopupSettings)
		}
	})

	if s.popupState == PopupSettings {
		s.BuildPopupSettions(storage, audioPlayer, keyListener)
	}

	if s.popupState == PopupLicenses {
		s.BuildPopupLicenses()
	}

	bodyBox := gui.AddChild(root, gui.NewBox(gui.BoxProps{
		SizingX:  gui.Grow(),
		SizingY:  gui.Grow(),
		ChildGap: theme.childGap,
	}))

	s.BuildBindings(storage, audioPlayer, keyListener, bodyBox, profileDropdown)
	if s.ctxMenuState == CtxMenuBinding {
		s.BuildCtxMenuBinding(storage, audioPlayer, keyListener, profileDropdown)
	}

	s.BuildTrackList(storage, audioPlayer, bodyBox, profileDropdown)
	if s.ctxMenuState == CtxMenuTrack {
		s.BuildCtxMenuTrack(storage, audioPlayer)
	}
	if s.popupState == PopupRemoveTrackConfirmation {
		s.BuildPopupRemoveTrackConfirmation(storage)
	}

	playerBarBox := gui.AddChild(root, gui.NewBox(gui.BoxProps{
		SizingX:     gui.Grow(),
		ChildGap:    theme.childGap,
		ChildAlignY: gui.Center,
	}))

	stopPlayingAudioBtn := gui.AddChild(playerBarBox, NewPrimaryButton("stop.png", "Stop Playing", theme.error1))

	gui.AddPostUpdate(func() {
		if stopPlayingAudioBtn.IsLeftButtonPressed() {
			audioPlayer.ClearSounds()
		}
	})

	gui.AddChild(playerBarBox, gui.NewSpacerX())

	muteMicBtn := gui.AddChild(playerBarBox, gui.TernaryLazy(storage.IsInputMuted,
		func() *gui.Button { return NewPrimaryButton("mic-muted.png", "", theme.error1) },
		func() *gui.Button { return NewPrimaryButton("mic.png", "", theme.primary1) },
	))
	openGainMenuBtn := gui.AddChild(playerBarBox, NewPrimaryButton("sliders.png", "", theme.primary1))

	gui.AddPostUpdate(func() {
		if muteMicBtn.IsLeftButtonPressed() {
			storage.IsInputMuted = !storage.IsInputMuted
			audioPlayer.SetInputMuted(storage.IsInputMuted)
			if storage.IsInputMuted {
				audioPlayer.AddSamples(muteSoundSamples, 1, IDUnset, true)
			} else {
				audioPlayer.AddSamples(unmuteSoundSamples, 1, IDUnset, true)
			}
			if err := storage.Save(); err != nil {
				s.OpenErrorPopup(fmt.Sprintf("Could not mute/unmute the input: %s", err), false)
				return
			}
		}

		if openGainMenuBtn.IsLeftButtonPressed() {
			pos := gui.GetScreenSize()
			s.OpenCtxMenu(CtxMenuGain, &pos)
		}
	})

	if s.ctxMenuState == CtxMenuGain {
		s.BuildCtxMenuGain(storage, audioPlayer)
	}

	if s.errorPopupErrMsg != "" {
		s.BuildErrorPopup(&shouldExitProgram)
	}

	//// Compute Layout ////
	gui.ComputeLayout()

	//// Update ////
	gui.Update()

	if (s.ctxMenuSubWindow.IsHidden() && s.popupSubWindow.IsHidden()) || rl.IsKeyPressed(rl.KeyEscape) {
		s.CloseCtxMenu()
		s.ClosePopup()
		keyListener.IsGrabbedByScene = false
	}

	if s.ctxMenuState != CtxMenuNone || s.popupState != PopupNone {
		keyListener.IsGrabbedByScene = true
	}

	//// Render ////
	gui.Render()

	return shouldExitProgram
}

func ValidateProfileName(name string) (errMsg string) {
	if name == "" {
		return "The profile must have a name"
	}
	if len(name) > 40 {
		return "The maximum length is 40"
	}
	return ""
}

func ValidateTrackName(name string) (errMsg string) {
	if name == "" {
		return "The track must have a name"
	}
	if len(name) > 60 {
		return "The maximum length is 60"
	}
	return ""
}

type PopupState uint8

const (
	PopupNone PopupState = iota
	PopupAddTrack
	PopupAddProfile
	PopupEditProfile
	PopupRemoveTrackConfirmation
	PopupRemoveProfileConfirmation
	PopupSettings
	PopupPromptToCleanGarbageFiles
	PopupDefaultDevicesWarning
	PopupLicenses
)

func NewPopup(subWindow *gui.SubWindow, title string, addContent func(body, buttons *gui.Box)) {
	root := subWindow.SetRoot(gui.NewBox(gui.BoxProps{
		SizingX:     gui.Grow(),
		SizingY:     gui.Grow(),
		ChildAlignX: gui.Center,
		ChildAlignY: gui.Center,
		BgColor:     rl.ColorAlpha(theme.bg1, 0.5),
	}))
	bodyBox := gui.AddChild(root, gui.NewBox(gui.BoxProps{
		Orientation:  gui.Vertical,
		Padding:      theme.popupPadding,
		ChildGap:     theme.popupChildGap,
		BorderWidth:  theme.border,
		BorderColor:  theme.fg3,
		CornerRadius: theme.radius,
		BgColor:      theme.bg1,
	}))

	if title != "" {
		titleBox := gui.AddChild(bodyBox, gui.NewBox(gui.BoxProps{
			SizingX:     gui.Grow(),
			ChildAlignX: gui.Center,
		}))
		gui.AddChild(titleBox, gui.NewText(gui.TextProps{
			FontConfigProps: gui.FontConfigProps{
				FontSize: 28,
				Font:     theme.fontBold,
			},
		}, title))
	}

	contentBox := gui.NewBox(gui.BoxProps{
		SizingX:     gui.Grow(),
		SizingY:     gui.Grow(),
		Orientation: gui.Vertical,
		ChildGap:    theme.popupChildGap,
	})
	buttonsBox := gui.NewBox(gui.BoxProps{
		SizingX:  gui.Grow(),
		ChildGap: theme.popupChildGap,
	})

	addContent(contentBox, buttonsBox)

	if contentBox.GetComputedChildCount() > 0 {
		gui.AddChild(bodyBox, contentBox)
	}
	gui.AddChild(bodyBox, buttonsBox)
}

type CtxMenuState uint8

const (
	CtxMenuNone CtxMenuState = iota
	CtxMenuTrack
	CtxMenuBinding
	CtxMenuGain
)

func NewCtxMenu(subWindow *gui.SubWindow, addContent func(content *gui.Box)) {
	root := subWindow.SetRoot(gui.NewBox(gui.BoxProps{
		Orientation:  gui.Vertical,
		Padding:      theme.padding,
		CornerRadius: theme.radius,
		BorderWidth:  theme.border,
		BorderColor:  theme.fg3,
		BgColor:      theme.bg2,
		ChildGap:     theme.childGap,
	}))

	addContent(root)
}

func NewPrimaryButton(icon, text string, color rl.Color, sizingXY ...gui.SizingProp) *gui.Button {
	assert.InRange(len(sizingXY), 0, 2)

	var sizingX, sizingY gui.SizingProp
	switch len(sizingXY) {
	case 1:
		sizingX = sizingXY[0]
	case 2:
		sizingX = sizingXY[0]
		sizingY = sizingXY[1]
	}

	button := gui.NewButton(gui.ButtonProps{
		BoxProps: gui.BoxProps{
			SizingX:      sizingX,
			SizingY:      sizingY,
			ChildAlignY:  gui.Center,
			ChildAlignX:  gui.Center,
			Padding:      gui.Ternary(text != "", gui.Padding(12, 16, 12, 12), gui.Padding(12)),
			ChildGap:     10,
			CornerRadius: theme.radius,
			BgColor:      color,
		},
		OnHover: gui.EffectBrighten,
	})
	if icon != "" {
		gui.AddChild(button, gui.NewBoxImage(gui.BoxProps{
			SizingX:     gui.Fixed(20),
			SizingY:     gui.Fixed(20),
			TextureTint: theme.bg1,
		}, "assets/icons/"+icon, iconsFS))
	}
	if text != "" {
		gui.AddChild(button, gui.NewText(gui.TextProps{
			FontConfigProps: gui.FontConfigProps{
				Font:    theme.fontBold,
				FgColor: theme.bg1,
			},
		}, text))
	}
	return button
}

func NewSecondaryButton(icon, text string, sizingXY ...gui.SizingProp) *gui.Button {
	assert.InRange(len(sizingXY), 0, 2)

	var sizingX, sizingY gui.SizingProp
	switch len(sizingXY) {
	case 1:
		sizingX = sizingXY[0]
	case 2:
		sizingX = sizingXY[0]
		sizingY = sizingXY[1]
	}

	button := gui.NewButton(gui.ButtonProps{
		BoxProps: gui.BoxProps{
			SizingX:      sizingX,
			SizingY:      sizingY,
			ChildAlignY:  gui.Center,
			ChildAlignX:  gui.Center,
			Padding:      gui.Padding(12, 16, 12, 12),
			ChildGap:     10,
			CornerRadius: theme.radius,
			BgColor:      theme.bg3,
		},
		OnHover: gui.EffectBrighten,
	})
	if icon != "" {
		gui.AddChild(button, gui.NewBoxImage(gui.BoxProps{
			SizingX:     gui.Fixed(20),
			SizingY:     gui.Fixed(20),
			TextureTint: theme.fg2,
		}, "assets/icons/"+icon, iconsFS))
	}
	gui.AddChild(button, gui.NewText(gui.TextProps{
		FontConfigProps: gui.FontConfigProps{
			Font:    theme.fontBold,
			FgColor: theme.fg2,
		},
	}, text))
	return button
}

func NewCtxMenuButton(icon, text string, fg, bg rl.Color) *gui.Button {
	button := gui.NewButton(gui.ButtonProps{
		BoxProps: gui.BoxProps{
			SizingX:      gui.Grow(),
			ChildAlignY:  gui.Center,
			ChildAlignX:  gui.Start,
			Padding:      gui.PaddingOverride(theme.padding, -1, theme.padding.Right+4, -1, -1),
			ChildGap:     10,
			CornerRadius: theme.radius,
			BgColor:      bg,
		},
		OnHover: gui.EffectBrighten,
	})
	gui.AddChild(button, gui.NewText(gui.TextProps{
		FontConfigProps: gui.FontConfigProps{
			Font:    theme.fontBold,
			FgColor: fg,
		},
	}, text))
	gui.AddChild(button, gui.NewSpacerX())
	gui.AddChild(button, gui.NewBoxImage(gui.BoxProps{
		SizingX:     gui.Fixed(25),
		SizingY:     gui.Fixed(25),
		TextureTint: fg,
	}, "assets/icons/"+icon, iconsFS))
	return button
}

func NewGainSlider(
	sliderOut **gui.Slider, sliderID gui.NodeID, initGain float32, trackColor, textColor rl.Color,
	muteBtnOut **gui.Button, isMicrophone, isMuted bool, buttonColor rl.Color,
) *gui.Box {

	assert.NotNil(sliderOut)

	gainBox := gui.NewBox(gui.BoxProps{
		SizingX:      gui.Grow(),
		Orientation:  gui.Vertical,
		ChildGap:     theme.childGap / 2,
		Padding:      theme.padding,
		CornerRadius: theme.radius,
		BgColor:      theme.bg3,
	})

	var outBox *gui.Box
	if muteBtnOut != nil {
		outBox = gui.NewBox(gui.BoxProps{
			SizingX:  gui.Grow(),
			ChildGap: theme.childGap,
		})
		gui.AddChild(outBox, gainBox)
		if isMicrophone {
			if isMuted {
				*muteBtnOut = gui.AddChild(outBox, NewPrimaryButton("mic-muted.png", "", theme.error1, gui.Shrink(), gui.Grow()))
			} else {
				*muteBtnOut = gui.AddChild(outBox, NewPrimaryButton("mic.png", "", buttonColor, gui.Shrink(), gui.Grow()))
			}
		} else {
			if isMuted {
				*muteBtnOut = gui.AddChild(outBox, NewPrimaryButton("speaker-muted.png", "", theme.error1, gui.Shrink(), gui.Grow()))
			} else {
				*muteBtnOut = gui.AddChild(outBox, NewPrimaryButton("speaker.png", "", buttonColor, gui.Shrink(), gui.Grow()))
			}
		}
	} else {
		outBox = gainBox
	}

	*sliderOut = gui.NewSlider(gui.SliderProps{
		BoxProps: gui.BoxProps{
			ID:      sliderID,
			SizingX: gui.Grow(200),
		},

		ThumbCornerRadius: gui.Radius(69),
		ThumbColor:        theme.fg1,

		TrackCornerRadius:  gui.Radius(69),
		TrackActiveColor:   trackColor,
		TrackInactiveColor: theme.fg3,
	}, 0, 1, exponentialToLinearGain(initGain))
	gui.AddChild(gainBox, gui.NewText(gui.TextProps{
		FontConfigProps: gui.FontConfigProps{
			FgColor: textColor,
		},
	}, fmt.Sprintf("Volume %d%%", int(linearToExponentialGain((*sliderOut).GetProgress())*100))))
	gui.AddChild(gainBox, *sliderOut)

	return outBox
}

func (s *Scene) BuildPopupDefaultDevicesWarning(storage *Storage) {
	NewPopup(s.popupSubWindow, "Warning", func(body, buttons *gui.Box) {
		gui.AddChild(body, gui.NewText(gui.TextProps{
			BoxProps: gui.BoxProps{
				SizingX:     gui.Grow(),
				ChildAlignX: gui.Center,
			},
			FontConfigProps: gui.FontConfigProps{
				FontSize: 22,
			},
			Wrapping: gui.Wrap,
		}, `Please check if you have set virtual devices (containing "Soundpad" or "CABLE" in the name) as the default option, if so, set another devices as the default or manually choose which device to use in the soundpad settings.`))

		imagesBox := gui.AddChild(body, gui.NewBox(gui.BoxProps{
			SizingX:     gui.Grow(400),
			ChildAlignX: gui.Center,
			ChildGap:    theme.childGap,
		}))
		gui.AddChild(imagesBox, gui.NewBoxImage(gui.BoxProps{
			SizingX: gui.Fixed(300),
			SizingY: gui.AspectRatio(1.17),
		}, "assets/images/default-input.png", imagesFS))
		gui.AddChild(imagesBox, gui.NewBoxImage(gui.BoxProps{
			SizingX: gui.Fixed(300),
			SizingY: gui.AspectRatio(1.17),
		}, "assets/images/default-output.png", imagesFS))

		gui.AddChild(buttons, gui.NewSpacerX())

		dontShowAgainBtn := gui.AddChild(buttons, NewSecondaryButton("cross.png", "Don't Show Again"))
		closeBtn := gui.AddChild(buttons, NewPrimaryButton("cross.png", "Close", theme.primary1))

		gui.AddPostUpdate(func() {
			if dontShowAgainBtn.IsLeftButtonPressed() {
				storage.IsDefaultDevicesWarningEnabled = false
				if err := storage.Save(); err != nil {
					s.OpenErrorPopup(fmt.Sprintf("Could not save preferences to the config file: %s", err), false)
				}
				s.ClosePopup()
				return
			}

			if closeBtn.IsLeftButtonPressed() {
				s.ClosePopup()
				return
			}
		})
	})
}

func (s *Scene) BuildPopupPromptToCleanGarbageFiles(storage *Storage) {
	NewPopup(s.popupSubWindow, "Clean garbage files?", func(body, buttons *gui.Box) {
		gui.AddChild(body, gui.NewText(gui.TextProps{
			BoxProps: gui.BoxProps{
				SizingX:     gui.Grow(),
				ChildAlignX: gui.Center,
			},
			FontConfigProps: gui.FontConfigProps{
				FontSize: 22,
			},
			Wrapping: gui.Wrap,
		}, "The following files were found in the configuration directory and are not in use:"))

		fileList := gui.AddChild(body, gui.NewScrollBox(gui.ScrollBoxProps{
			BoxProps: gui.BoxProps{
				ID:          s.popupSubWindow.GetAutoID(),
				SizingX:     gui.Grow(),
				SizingY:     gui.Fixed(min(200, float32(len(storage.garbageFiles))*gui.DefaultFontSize*1.1)),
				Orientation: gui.Vertical,
				ChildGap:    theme.childGap,
			},
			ThumbColor:        theme.fg3,
			ThumbCornerRadius: 69,
			ScrollOrientation: gui.Vertical,
		}))

		gui.AddChild(fileList, gui.NewText(gui.TextProps{
			BoxProps: gui.BoxProps{
				SizingX:     gui.Grow(),
				SizingY:     gui.Grow(),
				ChildAlignX: gui.Center,
			},
			FontConfigProps: gui.FontConfigProps{
				FgColor: theme.fg2,
			},
		}, strings.Join(storage.garbageFiles, "\n")))

		gui.AddChild(buttons, gui.NewSpacerX())

		closeBtn := gui.AddChild(buttons, NewSecondaryButton("cross.png", "Close"))
		removeBtn := gui.AddChild(buttons, NewPrimaryButton("trash.png", "Delete All", theme.error1))

		gui.AddPostUpdate(func() {
			if closeBtn.IsLeftButtonPressed() {
				s.ClosePopup()
				return
			}

			if removeBtn.IsLeftButtonPressed() {
				if err := storage.RemoveGarbageFiles(); err != nil {
					s.OpenErrorPopup(fmt.Sprintf("Could not delete the garbage files: %s", err), false)
				}
				s.ClosePopup()
				return
			}
		})
	})
}

func (s *Scene) BuildPopupAddTrack(storage *Storage, audioPlayer *AudioPlayer) {
	NewPopup(s.popupSubWindow, "Add Track", func(body, buttons *gui.Box) {
		configBox := gui.AddChild(body, gui.NewBox(gui.BoxProps{
			SizingX:  gui.Grow(),
			ChildGap: theme.childGap,
		}))

		var _trackImgTexture rl.Texture2D
		if s.addTrackImagePath != "" {
			var err error
			_trackImgTexture, err = gui.LoadImageTexture(s.addTrackImagePath, nil)
			if err != nil {
				s.OpenErrorPopup(fmt.Sprintf("Could not load the track image (%s): %s", s.addTrackImagePath, err), false)
				s.addTrackImagePath = ""
			}
		}
		trackAddImgBtn := gui.AddChild(configBox, gui.NewButton(gui.ButtonProps{
			BoxProps: gui.BoxProps{
				SizingX:      gui.Fixed(80),
				SizingY:      gui.Fixed(80),
				ChildAlignX:  gui.Center,
				ChildAlignY:  gui.Center,
				CornerRadius: gui.Radius(16),
				BorderWidth:  gui.Border(3),
				BorderColor:  theme.fg1,
				BgColor:      gui.Ternary(s.addTrackImagePath != "", rl.Blank, theme.secondary1),
				Texture:      _trackImgTexture,
			},
			OnHover: func(box *gui.Box) func() {
				img := box.GetChild(0).(*gui.Box)
				prevTexture := img.Texture
				img.Texture = gui.Must(gui.LoadImageTexture("assets/icons/image-plus-256.png", iconsFS))
				img.Invisible = false
				return func() {
					img.Texture = prevTexture
					img.Invisible = s.addTrackImagePath != ""
				}
			},
		}))
		gui.AddChild(trackAddImgBtn, gui.NewBoxImage(gui.BoxProps{
			SizingX:     gui.Percentage(85),
			SizingY:     gui.AspectRatio(1),
			TextureTint: theme.fg1,
			Invisible:   s.addTrackImagePath != "",
		}, "assets/icons/image-256.png", iconsFS))

		configHBox := gui.AddChild(configBox, gui.NewBox(gui.BoxProps{
			SizingX:  gui.Grow(),
			SizingY:  gui.Grow(),
			ChildGap: theme.childGap,
		}))
		configLeftVBox := gui.AddChild(configHBox, gui.NewBox(gui.BoxProps{
			SizingX:     gui.Grow(),
			SizingY:     gui.Grow(),
			Orientation: gui.Vertical,
		}))
		configRightVBox := gui.AddChild(configHBox, gui.NewBox(gui.BoxProps{
			SizingX:     gui.Grow(),
			SizingY:     gui.Grow(),
			Orientation: gui.Vertical,
		}))

		gui.AddChild(configLeftVBox, gui.NewText(gui.TextProps{
			FontConfigProps: gui.FontConfigProps{
				FgColor: theme.fg2,
			},
		}, filepath.Base(s.addTrackFilePath)))
		gui.AddChild(configLeftVBox, gui.NewSpacerY())
		nameInput := gui.AddChild(configLeftVBox, gui.NewTextInput(gui.TextInputProps{
			BoxProps: gui.BoxProps{
				ID:           s.popupSubWindow.GetAutoID(),
				SizingX:      gui.Grow(300),
				Padding:      theme.padding,
				BgColor:      theme.bg2,
				CornerRadius: theme.radius,
			},
		}, "Track Name", ""))

		gainBox := gui.AddChild(configRightVBox, gui.NewBox(gui.BoxProps{
			SizingX:     gui.Grow(),
			Orientation: gui.Vertical,
			ChildGap:    theme.childGap / 3,
		}))
		gainSlider := gui.NewSlider(gui.SliderProps{
			BoxProps: gui.BoxProps{
				ID:      s.popupSubWindow.GetAutoID(),
				SizingX: gui.Grow(200),
			},

			ThumbCornerRadius: gui.Radius(69),
			ThumbColor:        theme.fg1,

			TrackCornerRadius:  gui.Radius(69),
			TrackActiveColor:   theme.secondary1,
			TrackInactiveColor: theme.fg3,
		}, 0, 1, exponentialToLinearGain(storage.NewTrackGain))
		gui.AddChild(gainBox, gui.NewText(gui.TextProps{
			FontConfigProps: gui.FontConfigProps{
				FgColor: theme.fg2,
			},
		}, fmt.Sprintf("Volume %d%%", int(linearToExponentialGain(gainSlider.GetProgress())*100))))
		gui.AddChild(gainBox, gainSlider)

		gui.AddChild(configRightVBox, gui.NewSpacerY())

		qualityBox := gui.AddChild(configRightVBox, gui.NewBox(gui.BoxProps{
			ChildAlignY: gui.Center,
			ChildGap:    theme.childGap,
		}))
		gui.AddChild(qualityBox, gui.NewText(gui.TextProps{
			FontConfigProps: gui.FontConfigProps{
				FgColor: theme.fg2,
			},
		}, "Quality"))
		qualityDropdown := gui.AddChild(qualityBox, gui.NewDropdown(gui.DropdownProps{
			BoxProps: gui.BoxProps{
				ID:           s.popupSubWindow.GetAutoID(),
				BgColor:      theme.bg3,
				CornerRadius: theme.radius,
				Padding:      gui.PaddingOverride(theme.padding, 5, -1, 5, -1),
			},
			FontConfigProps: gui.FontConfigProps{
				FgColor: theme.fg2,
			},
			OptionsCornerRadius: theme.radius,
		}, "", []string{"Low", "Medium", "High"}, 0))

		waveCutter := gui.AddChild(body, NewWaveCutter(WaveCutterProps{
			BoxProps: gui.BoxProps{
				ID:      s.popupSubWindow.GetAutoID(),
				SizingX: gui.Grow(600),
				SizingY: gui.Fixed(100),
				Padding: gui.Padding(0, 10),
			},
			CutSamplesGain:      storage.NewTrackGain,
			SamplesColor:        theme.secondary1,
			CutBarsColor:        theme.fg1,
			RemovedSegmentColor: rl.ColorAlpha(theme.bg1, 0.5),
		}, s.addTrackSamples, audioPlayer))

		saveBtn := gui.AddChild(buttons, NewPrimaryButton("save.png", "Save", theme.secondary1, gui.Grow()))
		playBtn := gui.AddChild(buttons, gui.TernaryLazy(waveCutter.IsPlaying(),
			func() *gui.Button { return NewPrimaryButton("stop.png", "Stop", theme.error1, gui.Grow()) },
			func() *gui.Button { return NewPrimaryButton("play.png", "Play", theme.primary1, gui.Grow()) }),
		)
		cancelBtn := gui.AddChild(buttons, NewSecondaryButton("cross.png", "Cancel", gui.Grow()))

		gui.AddPostUpdate(func() {
			if playBtn.IsLeftButtonPressed() {
				if waveCutter.IsPlaying() {
					waveCutter.StopPlaying()
				} else {
					quality := SampleQuality(qualityDropdown.GetSelectedIdx())
					waveCutter.PlayCutSamples(quality)
				}
			}

			if gainSlider.IsChanging() {
				gain := linearToExponentialGain(gainSlider.GetProgress())
				waveCutter.SetCutSamplesGain(gain)
			}

			if trackAddImgBtn.IsLeftButtonPressed() {
				filePath, ok, err := crossplatform.OpenFileDialog("Select Image", "Image (*.png, *.jpg) | *.png;*.jpg")
				if err != nil {
					s.OpenErrorPopup(fmt.Sprintf("Could not open the file selecting dialog: %s", err), false)
					return
				}
				if ok {
					s.addTrackImagePath = filePath
				}
			}

			if nameInput.ErrorMessage != "" && nameInput.HasChanged() {
				name := strings.TrimSpace(nameInput.Value())
				nameInput.ErrorMessage = ValidateTrackName(name)
			}

			if qualityDropdown.HasChanged() {
				quality := SampleQuality(qualityDropdown.GetSelectedIdx())
				waveCutter.ChangePlayingSoundQuality(quality)
			}

			if cancelBtn.IsLeftButtonPressed() {
				audioPlayer.ClearSounds()
				s.ClosePopup()
			}

			if saveBtn.IsLeftButtonPressed() {
				name := strings.TrimSpace(nameInput.Value())

				if errMsg := ValidateTrackName(name); errMsg != "" {
					nameInput.ErrorMessage = errMsg
				} else {
					quality := SampleQuality(qualityDropdown.GetSelectedIdx())
					samples := waveCutter.GetCutSamples(quality)
					gain := linearToExponentialGain(gainSlider.GetProgress())
					storage.NewTrackGain = gain
					if err := storage.AddTrack(name, s.addTrackImagePath, samples, gain); err != nil {
						s.ClosePopup()
						s.OpenErrorPopup(fmt.Sprintf("Could not create the track: %s", err), false)
						return
					}
					audioPlayer.ClearSounds()
					s.ClosePopup()
				}
			}
		})
	})
}

func (s *Scene) BuildPopupRemoveProfileConfirmation(storage *Storage, profileDropdown *gui.Dropdown) {
	NewPopup(s.popupSubWindow, "Confirm Deletion", func(body, buttons *gui.Box) {
		profile := storage.GetCurrentProfile()
		assert.NotNil(profile)
		gui.AddChild(body, gui.NewText(gui.TextProps{
			FontConfigProps: gui.FontConfigProps{
				FgColor: theme.fg2,
			},
		}, "Are you sure you want to delete \""+profile.Name+"\" profile?"))

		deleteBtn := gui.AddChild(buttons, NewPrimaryButton("trash.png", "Delete", theme.error1, gui.Grow()))
		cancelBtn := gui.AddChild(buttons, NewSecondaryButton("cross.png", "Cancel", gui.Grow()))

		gui.AddPostUpdate(func() {
			if deleteBtn.IsLeftButtonPressed() {
				if err := storage.RemoveProfile(profile.ID); err != nil {
					s.OpenErrorPopup(fmt.Sprintf("Could not remove the profile: %s", err), false)
					return
				}
				gui.RemoveNodeFromCache(profileDropdown.ID())
				s.ClosePopup()
			}

			if cancelBtn.IsLeftButtonPressed() {
				s.ClosePopup()
			}
		})
	})
}

func (s *Scene) BuildPopupAddOrEditProfile(storage *Storage, profileDropdown *gui.Dropdown) {
	defaultInputValue := ""
	if s.profileToEdit != nil {
		defaultInputValue = s.profileToEdit.Name
	}

	NewPopup(s.popupSubWindow, gui.Ternary(s.profileToEdit != nil, "Edit Profile", "Add Profile"), func(body, buttons *gui.Box) {
		profileNameInput := gui.AddChild(body, gui.NewTextInput(gui.TextInputProps{
			BoxProps: gui.BoxProps{
				ID:           s.popupSubWindow.GetAutoID(),
				SizingX:      gui.Fixed(300),
				Padding:      theme.padding,
				BgColor:      theme.bg3,
				CornerRadius: theme.radius,
			},
			PlaceholderColor: theme.fg3,
		}, "Name", defaultInputValue))
		gui.AddChild(body, gui.NewSpacerY(gui.Fixed(0)))

		saveBtn := gui.AddChild(buttons, NewPrimaryButton("save.png", "Save", theme.primary1, gui.Grow()))
		cancelBtn := gui.AddChild(buttons, NewSecondaryButton("cross.png", "Cancel", gui.Grow()))

		gui.AddPostUpdate(func() {
			if saveBtn.IsLeftButtonPressed() {
				name := strings.TrimSpace(profileNameInput.Value())
				if errMsg := ValidateProfileName(name); errMsg != "" {
					profileNameInput.ErrorMessage = errMsg
					return
				}
				if s.profileToEdit != nil {
					if err := storage.EditProfile(s.profileToEdit.ID, name); err != nil {
						s.OpenErrorPopup(fmt.Sprintf("Could not create the profile: %s", err), false)
						return
					}
				} else {
					if err := storage.AddProfile(name); err != nil {
						s.OpenErrorPopup(fmt.Sprintf("Could not create the profile: %s", err), false)
						return
					}
				}
				gui.RemoveNodeFromCache(profileDropdown.ID())
				s.ClosePopup()
				s.profileToEdit = nil
			}

			if cancelBtn.IsLeftButtonPressed() {
				s.ClosePopup()
				s.profileToEdit = nil
			}
		})
	})
}

func (s *Scene) BuildPopupSettions(storage *Storage, audioPlayer *AudioPlayer, keyListener *KeyListener) {
	s.popupOnClose = func() {
		s.isSettingsPopupListeningKeyCombo = false
		keyListener.StopRecording()
	}

	NewPopup(s.popupSubWindow, "Settings", func(body, buttons *gui.Box) {
		devicesBox := gui.AddChild(body, gui.NewBox(gui.BoxProps{
			SizingX:  gui.Grow(),
			ChildGap: theme.childGap,
		}))

		inputDeviceBox := gui.AddChild(devicesBox, gui.NewBox(gui.BoxProps{
			Orientation: gui.Vertical,
			ChildGap:    4,
			ChildAlignX: gui.Center,
		}))
		gui.AddChild(inputDeviceBox, gui.NewText(gui.TextProps{}, "Input Device"))
		inputDeviceDropdown := gui.AddChild(inputDeviceBox, gui.NewDropdown(gui.DropdownProps{
			BoxProps: gui.BoxProps{
				ID:           s.popupSubWindow.GetAutoID(),
				SizingX:      gui.Grow(),
				BgColor:      theme.primary1,
				Padding:      theme.padding,
				CornerRadius: theme.radius,
			},
			FontConfigProps: gui.FontConfigProps{
				Font:    theme.fontBold,
				FgColor: theme.bg1,
			},
			OptionsCornerRadius: theme.radius,
		}, "Select Input Device", storage.inputDeviceNames, int16(storage.selectedInputIdx)))

		outputDeviceBox := gui.AddChild(devicesBox, gui.NewBox(gui.BoxProps{
			Orientation: gui.Vertical,
			ChildGap:    4,
			ChildAlignX: gui.Center,
		}))
		gui.AddChild(outputDeviceBox, gui.NewText(gui.TextProps{}, "Output Device"))
		outputDeviceDropdown := gui.AddChild(outputDeviceBox, gui.NewDropdown(gui.DropdownProps{
			BoxProps: gui.BoxProps{
				ID:           s.popupSubWindow.GetAutoID(),
				SizingX:      gui.Grow(),
				BgColor:      theme.primary1,
				Padding:      theme.padding,
				CornerRadius: theme.radius,
			},
			FontConfigProps: gui.FontConfigProps{
				Font:    theme.fontBold,
				FgColor: theme.bg1,
			},
			OptionsCornerRadius: theme.radius,
		}, "Select Output Device", storage.outputDeviceNames, int16(storage.selectedOutputIdx)))

		refreshDevices := gui.AddChild(body, NewPrimaryButton("reload.png", "Refresh Devices", theme.primary1))

		clearSoundsKeyComboBox := gui.AddChild(body, gui.NewBox(gui.BoxProps{
			ChildGap:    10,
			ChildAlignY: gui.Center,
		}))
		gui.AddChild(clearSoundsKeyComboBox, gui.NewText(gui.TextProps{}, "Clear sounds"))
		var setClearSoundsKeyComboBtn *gui.Button
		{
			var btnText string
			if s.isSettingsPopupListeningKeyCombo {
				keys := keyListener.GetRecordingKeyCombo()
				if keys == (KeyCombo{}) {
					btnText = "Listening..."
				} else {
					btnText = keys.String()
				}
			} else {
				btnText = storage.ClearSoundsKeyCombo.String()
			}
			setClearSoundsKeyComboBtn = gui.AddChild(clearSoundsKeyComboBox, NewSecondaryButton("keyboard.png", btnText))
		}

		if DevMode {
			debugModeToggleBox := gui.AddChild(body, gui.NewBox(gui.BoxProps{
				ChildGap:    theme.childGap,
				ChildAlignY: gui.Center,
			}))
			debugModeToggle := gui.AddChild(debugModeToggleBox, gui.NewToggle(gui.ToggleProps{
				BoxProps: gui.BoxProps{
					ID:           s.popupSubWindow.GetAutoID(),
					SizingX:      gui.Fixed(50),
					SizingY:      gui.Fixed(25),
					CornerRadius: gui.Radius(69),
					BgColor:      theme.bg3,
				},
				OnColor:  theme.primary1,
				OffColor: theme.fg2,
			}, gui.Debug))
			gui.AddChild(debugModeToggleBox, gui.NewText(gui.TextProps{
				FontConfigProps: gui.FontConfigProps{
					FgColor: theme.fg2,
				},
			}, "GUI Debug Mode"))

			gui.AddPostUpdate(func() {
				if debugModeToggle.HasChanged() {
					gui.Debug = debugModeToggle.GetValue()
				}
			})
		}

		pauseRenderToggleBox := gui.AddChild(body, gui.NewBox(gui.BoxProps{
			ChildGap:    theme.childGap,
			ChildAlignY: gui.Center,
		}))
		pauseRenderToggle := gui.AddChild(pauseRenderToggleBox, gui.NewToggle(gui.ToggleProps{
			BoxProps: gui.BoxProps{
				ID:           s.popupSubWindow.GetAutoID(),
				SizingX:      gui.Fixed(50),
				SizingY:      gui.Fixed(25),
				CornerRadius: gui.Radius(69),
				BgColor:      theme.bg3,
			},
			OnColor:  theme.primary1,
			OffColor: theme.fg2,
		}, storage.IsPauseRenderEnabled))
		gui.AddChild(pauseRenderToggleBox, gui.NewText(gui.TextProps{
			FontConfigProps: gui.FontConfigProps{
				FgColor: theme.fg2,
			},
		}, "Pause rendering when not in focus (saves CPU and GPU)"))

		licensesBtn := gui.AddChild(body, NewPrimaryButton("license.png", "Licenses", theme.primary1))

		gui.AddChild(buttons, gui.NewSpacerX())
		closeBtn := gui.AddChild(buttons, NewSecondaryButton("cross.png", "Close"))

		gui.AddPostUpdate(func() {
			if pauseRenderToggle.HasChanged() {
				storage.IsPauseRenderEnabled = pauseRenderToggle.GetValue()
				if err := storage.Save(); err != nil {
					s.OpenErrorPopup(fmt.Sprintf("Could not toggle \"Pause render\": %s", err), true)
					return
				}
			}

			if licensesBtn.IsLeftButtonPressed() {
				s.OpenPopup(PopupLicenses)
			}

			if closeBtn.IsLeftButtonPressed() {
				s.ClosePopup()
			}
		})

		startDeviceStream := func(
			deviceInfo *portaudio.DeviceInfo,
			startStreamFunc func(*portaudio.DeviceInfo) error,
			setDeviceFunc func(int) error,
			getDeviceFunc func(int) (*portaudio.DeviceInfo, error),
			deviceType, streamType string,
		) bool {
			if err := startStreamFunc(deviceInfo); err != nil {
				s.OpenErrorPopup(fmt.Sprintf("Could not start the "+streamType+" stream (fallback to default): %s", err), false)

				if err := setDeviceFunc(-1); err != nil {
					s.OpenErrorPopup(fmt.Sprintf("Could not reset the "+deviceType+" device selection: %s", err), true)
					return false
				}

				if defaultDevice, err := getDeviceFunc(-1); err != nil {
					s.OpenErrorPopup(fmt.Sprintf("Could not get info about the default "+deviceType+" device: %s", err), true)
					return false
				} else {
					if err := startStreamFunc(defaultDevice); err != nil {
						s.OpenErrorPopup(fmt.Sprintf("Could not start the "+streamType+" stream with default: %s", err), true)
						return false
					}
				}
			}
			return true
		}

		gui.AddPostUpdate(func() {
			if inputDeviceDropdown.HasChanged() {
				idx := inputDeviceDropdown.GetSelectedIdx()
				assert.InRange(idx, 0, len(storage.inputDevices)-1)

				newInput, err := storage.GetInputDeviceInfo(idx)
				assert.NoError(err)
				assert.NotNil(newInput)
				if err := storage.SetSelectedInputDevice(idx); err != nil {
					s.OpenErrorPopup(fmt.Sprintf("Could not change the input device: %s", err), false)
					return
				}

				if err := audioPlayer.CloseVirtualMicStream(); err != nil {
					s.OpenErrorPopup(fmt.Sprintf("Could not close the virtual microphone and input stream: %s", err), true)
					return
				}
				if !startDeviceStream(newInput, audioPlayer.StartVirtualMicStream, storage.SetSelectedInputDevice, storage.GetInputDeviceInfo, "input", "virtual microphone and input") {
					return
				}
			}

			if outputDeviceDropdown.HasChanged() {
				idx := outputDeviceDropdown.GetSelectedIdx()
				assert.InRange(idx, 0, len(storage.outputDevices)-1)

				newOutput, err := storage.GetOutputDeviceInfo(idx)
				assert.NoError(err)
				assert.NotNil(newOutput)
				if err := storage.SetSelectedOutputDevice(idx); err != nil {
					s.OpenErrorPopup(fmt.Sprintf("Could not change the output device: %s", err), false)
					return
				}

				if err := audioPlayer.CloseOutputStream(); err != nil {
					s.OpenErrorPopup(fmt.Sprintf("Could not close the output stream: %s", err), true)
					return
				}
				if !startDeviceStream(newOutput, audioPlayer.StartOutputStream, storage.SetSelectedOutputDevice, storage.GetOutputDeviceInfo, "output", "output") {
					return
				}
			}

			if refreshDevices.IsLeftButtonPressed() {
				if err := audioPlayer.CloseVirtualMicStream(); err != nil {
					s.OpenErrorPopup(fmt.Sprintf("Could not close the virtual microphone and input stream: %s", err), true)
					return
				}
				if err := audioPlayer.CloseOutputStream(); err != nil {
					s.OpenErrorPopup(fmt.Sprintf("Could not close the output stream: %s", err), true)
					return
				}

				if err := ReinitializePortaudio(); err != nil {
					s.OpenErrorPopup(fmt.Sprintf("Could not reinitialize the audio: %s", err), true)
					return
				}
				if err := storage.UpdateAudioDevices(); err != nil {
					s.OpenErrorPopup(fmt.Sprintf("Could not update the audio devices: %s", err), true)
					return
				}

				inputDevice, err := storage.GetInputDeviceInfo(-1)
				if err != nil {
					s.OpenErrorPopup(fmt.Sprintf("Could not get info about input device: %s", err), true)
					return
				}
				assert.NotNil(inputDevice)
				if !startDeviceStream(inputDevice, audioPlayer.StartVirtualMicStream, storage.SetSelectedInputDevice, storage.GetInputDeviceInfo, "input", "virtual microphone and input") {
					return
				}

				outputDevice, err := storage.GetOutputDeviceInfo(-1)
				if err != nil {
					s.OpenErrorPopup(fmt.Sprintf("Could not get info about output device: %s", err), true)
					return
				}
				assert.NotNil(outputDevice)
				if !startDeviceStream(outputDevice, audioPlayer.StartOutputStream, storage.SetSelectedOutputDevice, storage.GetOutputDeviceInfo, "output", "output") {
					return
				}

				gui.RemoveNodeFromCache(inputDeviceDropdown.ID())
				gui.RemoveNodeFromCache(outputDeviceDropdown.ID())
			}

			if setClearSoundsKeyComboBtn.IsLeftButtonPressed() {
				keyListener.StartRecording()
				s.isSettingsPopupListeningKeyCombo = true
			}
			if s.isSettingsPopupListeningKeyCombo {
				keys, ok := keyListener.IsRecordingFinished()
				if !ok {
					return
				}

				s.isSettingsPopupListeningKeyCombo = false

				if !keys.IsValid() {
					return
				}

				if keys.Equal(NewKey(uint32(crossplatform.KeyEscape), KeyKindKeyboard)) {
					keys = KeyCombo{}
				}

				if err := storage.SetClearSoundsKeyCombo(keys); err != nil {
					s.OpenErrorPopup(fmt.Sprintf("Could not set the key combo %q: %s", keys, err), false)
					return
				}
			}
		})
	})
}

func (s *Scene) BuildPopupLicenses() {
	NewPopup(s.popupSubWindow, "Third-Party Licenses", func(body, buttons *gui.Box) {
		scrollBox := gui.AddChild(body, gui.NewScrollBox(gui.ScrollBoxProps{
			BoxProps: gui.BoxProps{
				ID:          s.popupSubWindow.GetAutoID(),
				SizingX:     gui.Fixed(600),
				SizingY:     gui.Fixed(300),
				Orientation: gui.Vertical,
				ChildGap:    theme.childGap,
			},
			ThumbColor:        theme.fg3,
			ThumbCornerRadius: 69,
			ScrollOrientation: gui.Vertical,
		}))
		gui.AddChild(scrollBox, gui.NewText(gui.TextProps{
			BoxProps: gui.BoxProps{
				SizingX: gui.Grow(),
				SizingY: gui.Grow(),
			},
			FontConfigProps: gui.FontConfigProps{
				FontSize: 22,
			},
			Wrapping: gui.Wrap,
		}, licensesString))

		gui.AddChild(buttons, gui.NewSpacerX())
		closeBtn := gui.AddChild(buttons, NewSecondaryButton("cross.png", "Close"))

		gui.AddPostUpdate(func() {
			if closeBtn.IsLeftButtonPressed() {
				s.OpenPopup(PopupSettings) // go back
			}
		})
	})
}

func (s *Scene) BuildBindings(storage *Storage, audioPlayer *AudioPlayer, keyListener *KeyListener, bodyBox *gui.Box, profileDropdown *gui.Dropdown) {
	bindingsScrollBox := gui.AddChild(bodyBox, gui.NewScrollBox(gui.ScrollBoxProps{
		BoxProps: gui.BoxProps{
			ID:           gui.NewID("bindings scroll list"),
			SizingX:      gui.Grow(),
			SizingY:      gui.Grow(),
			ChildWrap:    true,
			Padding:      theme.padding,
			ChildGap:     theme.childGap,
			CornerRadius: theme.radius,
			BgColor:      theme.bg2,
		},
		ThumbColor:        theme.fg3,
		ThumbCornerRadius: 69,
		ScrollOrientation: gui.Vertical,
	}))

	if profileDropdown.HasSelection() {
		profile := storage.GetCurrentProfile()
		assert.NotNil(profile)

		if len(profile.Bindings) == 0 {
			gui.AddChild(bindingsScrollBox, gui.NewText(gui.TextProps{
				BoxProps: gui.BoxProps{
					SizingX:     gui.Grow(),
					SizingY:     gui.Grow(),
					ChildAlignX: gui.Center,
					ChildAlignY: gui.Center,
				},
				FontConfigProps: gui.FontConfigProps{
					FontSize: 32,
					Font:     theme.fontBold,
					FgColor:  theme.fg3,
				},
			}, "No bindings yet"))
		}

		for i, binding := range profile.Bindings {
			track := storage.GetTrackByID(binding.TrackID)
			var trackImg rl.Texture2D
			if track.HasImage() {
				var err error
				if trackImg, err = gui.LoadImageTexture(track.imagePath, nil); err != nil {
					s.OpenErrorPopup(fmt.Sprintf("Could not load the binding track image [ID=%d NAME=%q] (%s): %s", track.ID, track.Name, track.imagePath, err), false)
					track.imagePath = ""
				}
			}
			bindingBtn := gui.AddChild(bindingsScrollBox, gui.NewButton(gui.ButtonProps{
				BoxProps: gui.BoxProps{
					SizingX:      gui.Fixed(160),
					SizingY:      gui.Fixed(80),
					ChildAlignX:  gui.Center,
					ChildAlignY:  gui.End,
					CornerRadius: theme.radius,
					BgColor:      gui.Ternary(track.HasImage(), rl.Blank, theme.bg3),
					Texture:      trackImg,
					TextureTint:  gui.Ternary(binding.IsEnabled, rl.ColorAlpha(rl.White, 0.9), rl.ColorAlpha(rl.White, 0.6)),
					BorderWidth:  gui.Border(5),
					Orientation:  gui.Vertical,
				},
				OnHover: func(box *gui.Box) func() {
					if rl.IsTextureValid(box.Texture) {
						box.TextureTint = rl.White
					} else {
						box.BgColor = rl.ColorLerp(box.BgColor, rl.White, 0.2)
					}
					return nil
				},
				OnPress: func(box *gui.Box) func() {
					if binding.IsEnabled {
						box.BorderColor = theme.secondary1
					}
					return nil
				},
			}))
			gui.AddChild(bindingBtn, gui.NewText(gui.TextProps{
				BoxProps: gui.BoxProps{
					SizingX:      gui.Grow(),
					ChildAlignX:  gui.Center,
					Padding:      gui.Padding(2, 10),
					BgColor:      gui.Ternary(track.HasImage(), rl.ColorAlpha(theme.bg3, 0.6), rl.ColorAlpha(rl.White, 0.5)),
					CornerRadius: gui.RadiusOverride(theme.radius, -1, -1, 0, 0),
				},
				FontConfigProps: gui.FontConfigProps{
					Font:     theme.fontBold,
					FontSize: 20,
					FgColor:  theme.fg2,
				},
				Wrapping: gui.EllipsisOverflow,
			}, gui.Ternary(binding.IsEnabled, binding.GetKeysString(), "DISABLED")))
			gui.AddChild(bindingBtn, gui.NewSpacerY())
			gui.AddChild(bindingBtn, gui.NewText(gui.TextProps{
				BoxProps: gui.BoxProps{
					SizingX:      gui.Grow(),
					ChildAlignX:  gui.Center,
					Padding:      gui.Padding(2, 10),
					BgColor:      gui.Ternary(track.HasImage(), rl.ColorAlpha(theme.bg3, 0.6), rl.ColorAlpha(rl.White, 0.5)),
					CornerRadius: gui.RadiusOverride(theme.radius, 0, 0, -1, -1),
				},
				FontConfigProps: gui.FontConfigProps{
					Font:     theme.fontBold,
					FontSize: 24,
				},
				Wrapping: gui.EllipsisOverflow,
			}, track.Name))

			gui.AddPostUpdate(func() {
				if binding.IsEnabled && bindingBtn.IsLeftButtonPressed() {
					if _, err := audioPlayer.AddTrack(storage, binding.TrackID, false); err != nil {
						track := storage.GetTrackByID(binding.TrackID)
						assert.NotNil(track)
						s.OpenErrorPopup(fmt.Sprintf("Could not play track [ID=%d NAME=%q]: %s", track.ID, track.Name, err), false)
						return
					}
				}
				if bindingBtn.IsRightButtonPressed() {
					s.selectedBinding = &profile.Bindings[i]
					s.OpenCtxMenu(CtxMenuBinding, nil)
				}
			})
		}
	} else {
		gui.AddChild(bindingsScrollBox, gui.NewText(gui.TextProps{
			BoxProps: gui.BoxProps{
				SizingX:     gui.Grow(),
				SizingY:     gui.Grow(),
				ChildAlignX: gui.Center,
				ChildAlignY: gui.Center,
			},
			FontConfigProps: gui.FontConfigProps{
				FontSize: 32,
				Font:     theme.fontBold,
				FgColor:  theme.fg3,
			},
		}, "No profile selected"))
	}
}

func (s *Scene) BuildCtxMenuBinding(storage *Storage, audioPlayer *AudioPlayer, keyListener *KeyListener, profileDropdown *gui.Dropdown) {
	assert.NotNil(s.selectedBinding)

	s.ctxMenuOnClose = func() {
		keyListener.StopRecording()
		s.isSelectedBindingListeningKeyCombo = false
		s.selectedBinding = nil
	}

	NewCtxMenu(s.ctxMenuSubWindow, func(content *gui.Box) {
		assert.NotEqual(s.selectedBinding.TrackID, 0)
		track := storage.GetTrackByID(s.selectedBinding.TrackID)

		gui.AddChild(content, gui.NewText(gui.TextProps{
			BoxProps: gui.BoxProps{
				SizingX:     gui.Grow(),
				ChildAlignX: gui.Center,
			},
			FontConfigProps: gui.FontConfigProps{
				FontSize: 24,
				Font:     theme.fontBold,
			},
			Wrapping: gui.EllipsisOverflow,
		}, track.Name))

		if s.selectedBinding.Keys.IsSet() || s.isSelectedBindingListeningKeyCombo {
			var keyComboString string

			if s.isSelectedBindingListeningKeyCombo {
				keyComboString = keyListener.GetRecordingKeyCombo().String()
			} else {
				keyComboString = s.selectedBinding.GetKeysString()
			}

			gui.AddChild(content, gui.NewText(gui.TextProps{
				BoxProps: gui.BoxProps{
					SizingX:     gui.Grow(),
					ChildAlignX: gui.Center,
				},
				FontConfigProps: gui.FontConfigProps{
					FontSize: 24,
					Font:     theme.fontBold,
					FgColor:  theme.fg3,
				},
				Wrapping: gui.EllipsisOverflow,
			}, keyComboString))
		}

		changeKeyBtn := gui.AddChild(content, NewCtxMenuButton("keyboard.png", gui.Ternary(s.isSelectedBindingListeningKeyCombo, "Listening...", "Change Keys"), theme.fg2, theme.bg3))
		disableBtn := gui.AddChild(content, NewCtxMenuButton("disabled.png", gui.Ternary(s.selectedBinding.IsEnabled, "Disable", "Enable"), theme.fg2, theme.bg3))
		removeBtn := gui.AddChild(content, NewCtxMenuButton("trash.png", "Remove", theme.fg2, theme.error2))

		gui.AddPostUpdate(func() {
			if disableBtn.IsLeftButtonPressed() {
				if err := storage.SetBindingEnabled(s.selectedBinding, !s.selectedBinding.IsEnabled); err != nil {
					s.OpenErrorPopup(fmt.Sprintf("Could not enable/disable the binding: %s", err), false)
					return
				}
			}

			if removeBtn.IsLeftButtonPressed() {
				profileIdx := profileDropdown.GetSelectedIdx()
				assert.NotEqual(profileIdx, -1)
				profile := storage.GetProfileByIndex(profileIdx)
				if err := storage.RemoveBinding(profile, track.ID); err != nil {
					s.OpenErrorPopup(fmt.Sprintf("Could not remove the binding: %s", err), false)
					return
				}
				s.CloseCtxMenu()
			}

			if changeKeyBtn.IsLeftButtonPressed() {
				s.isSelectedBindingListeningKeyCombo = true
				keyListener.StartRecording()
			}
			if s.isSelectedBindingListeningKeyCombo {
				keys, ok := keyListener.IsRecordingFinished()
				if !ok {
					return
				}

				s.isSelectedBindingListeningKeyCombo = false

				if !keys.IsValid() {
					return
				}

				if keys.Equal(NewKey(uint32(crossplatform.KeyEscape), KeyKindKeyboard)) {
					keys = KeyCombo{}
				}

				if err := storage.SetBindingKeyCombo(s.selectedBinding, keys); err != nil {
					s.OpenErrorPopup(fmt.Sprintf("Could not set the key binding: %s", err), false)
					return
				}
			}
		})
	})
}

func (s *Scene) BuildTrackList(storage *Storage, audioPlayer *AudioPlayer, bodyBox *gui.Box, profileDropdown *gui.Dropdown) {
	trackList := gui.AddChild(bodyBox, gui.NewScrollBox(gui.ScrollBoxProps{
		BoxProps: gui.BoxProps{
			ID:          gui.NewID("tracks scroll list"),
			SizingX:     gui.Shrink(300),
			SizingY:     gui.Grow(),
			Orientation: gui.Vertical,
			ChildGap:    theme.childGap,
		},
		ThumbColor:        theme.fg3,
		ThumbCornerRadius: 69,
		ScrollOrientation: gui.Vertical,
	}))
	if len(storage.Tracks) == 0 {
		gui.AddChild(trackList, gui.NewText(gui.TextProps{
			BoxProps: gui.BoxProps{
				SizingX:     gui.Grow(),
				SizingY:     gui.Grow(),
				ChildAlignX: gui.Center,
				ChildAlignY: gui.Center,
			},
			FontConfigProps: gui.FontConfigProps{
				Font:     theme.fontBold,
				FontSize: 32,
				FgColor:  theme.fg3,
			},
		}, "No tracks yet"))
	}
	for i, track := range slices.Backward(storage.Tracks) {
		trackItemBtn := gui.AddChild(trackList, gui.NewButton(gui.ButtonProps{
			BoxProps: gui.BoxProps{
				SizingX:      gui.Grow(),
				ChildAlignY:  gui.Center,
				Padding:      theme.padding,
				ChildGap:     theme.childGap,
				CornerRadius: theme.radius,
				BgColor:      gui.Ternary(!track.HasTrack(), theme.error2, theme.bg3),
			},
		}))

		gui.AddPostUpdate(func() {
			if trackItemBtn.IsLeftButtonPressed() {
				if track.HasTrack() {
					if _, err := audioPlayer.AddTrack(storage, track.ID, false); err != nil {
						s.OpenErrorPopup(fmt.Sprintf("Could not play track [ID=%d NAME=%q]: %s", track.ID, track.Name, err), false)
						return
					}
				}
			}

			if trackItemBtn.IsRightButtonPressed() {
				s.selectedTrack = storage.GetTrackByID(track.ID)
				s.selectedTrackTestSoundID = 0
				s.OpenCtxMenu(CtxMenuTrack, nil)
			}
		})

		addBindingBtn := gui.AddChild(trackItemBtn, gui.NewButton(gui.ButtonProps{
			BoxProps: gui.BoxProps{
				SizingX:      gui.Fixed(50),
				SizingY:      gui.Fixed(50),
				CornerRadius: theme.radius,
				BgColor:      theme.bg2,
				Padding:      theme.padding,
			},
			OnHover: gui.EffectBrighten,
		}))
		gui.AddChild(addBindingBtn, gui.NewBoxImage(gui.BoxProps{
			SizingX:     gui.Grow(),
			SizingY:     gui.Grow(),
			TextureTint: theme.fg2,
		}, "assets/icons/arrow-left.png", iconsFS))

		gui.AddPostUpdate(func() {
			if addBindingBtn.IsLeftButtonPressed() {
				if profileDropdown.HasSelection() {
					profile := storage.GetCurrentProfile()
					assert.NotNil(profile)

					if err := storage.AddBinding(profile, track.ID); err != nil {
						s.OpenErrorPopup(fmt.Sprintf("Could not add the binding: %s", err), false)
						return
					}
				} else {
					if profileDropdown.GetNumberOfOptions() > 0 {
						s.OpenErrorPopup("Must select a profile first", false)
					} else {
						s.OpenErrorPopup("Must create a profile first", false)
					}
					return
				}
			}
		})

		if track.HasImage() {
			imageTexture, err := gui.LoadImageTexture(track.imagePath, nil)
			if err != nil {
				s.OpenErrorPopup(fmt.Sprintf("Could not load track image [ID=%s NAME=%q] (%s): %s", track.ID, track.Name, track.imagePath, err), false)
				track.imagePath = ""
				storage.Tracks[i].imagePath = ""
			}
			gui.AddChild(trackItemBtn, gui.NewBox(gui.BoxProps{
				SizingX:      gui.Fixed(50),
				SizingY:      gui.Fixed(50),
				BorderWidth:  theme.border,
				BorderColor:  theme.fg1,
				CornerRadius: theme.radius,
				Texture:      imageTexture,
			}))
		} else {
			imgBgBox := gui.AddChild(trackItemBtn, gui.NewBox(gui.BoxProps{
				SizingX:      gui.Fixed(50),
				SizingY:      gui.Fixed(50),
				ChildAlignX:  gui.Center,
				ChildAlignY:  gui.Center,
				BorderWidth:  theme.border,
				BorderColor:  theme.fg1,
				CornerRadius: theme.radius,
				BgColor:      theme.secondary1,
			}))
			gui.AddChild(imgBgBox, gui.NewBoxImage(gui.BoxProps{
				SizingX:     gui.Percentage(50),
				SizingY:     gui.AspectRatio(1),
				TextureTint: theme.fg1,
			}, "assets/icons/music-note-256.png", iconsFS))
		}

		textBox := gui.AddChild(trackItemBtn, gui.NewBox(gui.BoxProps{
			SizingX:     gui.Grow(),
			SizingY:     gui.Grow(),
			Orientation: gui.Vertical,
			ChildAlignY: gui.Center,
			ChildGap:    3,
			Padding:     gui.Padding(0, theme.padding.Right, 0, 0),
		}))
		gui.AddChild(textBox, gui.NewText(gui.TextProps{
			BoxProps: gui.BoxProps{
				SizingX: gui.Grow(),
			},
			FontConfigProps: gui.FontConfigProps{
				FgColor: gui.Ternary(track.HasTrack(), theme.fg1, theme.fg2),
			},
			Wrapping: gui.EllipsisOverflow,
		}, track.Name))
		if !track.HasTrack() {
			gui.AddChild(textBox, gui.NewText(gui.TextProps{
				BoxProps: gui.BoxProps{
					SizingX: gui.Grow(),
				},
				FontConfigProps: gui.FontConfigProps{
					FgColor: theme.error1,
				},
				Wrapping: gui.EllipsisOverflow,
			}, "Missing track"))
		}
	}
}

func (s *Scene) BuildCtxMenuTrack(storage *Storage, audioPlayer *AudioPlayer) {
	assert.NotNil(s.selectedTrack)

	NewCtxMenu(s.ctxMenuSubWindow, func(content *gui.Box) {
		nameInput := gui.AddChild(content, gui.NewTextInput(gui.TextInputProps{
			BoxProps: gui.BoxProps{
				ID:           s.ctxMenuSubWindow.GetAutoID(),
				SizingX:      gui.Grow(),
				SizingY:      gui.Shrink(25 + theme.padding.Y()),
				Padding:      theme.padding,
				BgColor:      theme.bg3,
				CornerRadius: theme.radius,
				BorderWidth:  theme.border,
				BorderColor:  rl.Blank,
			},
			PlaceholderColor: theme.fg3,
		}, "Name", s.selectedTrack.Name))

		changeImageBtn := gui.AddChild(content, NewCtxMenuButton("image-plus.png", "Change Image", theme.fg2, theme.bg3))

		var gainSlider *gui.Slider
		gui.AddChild(content, NewGainSlider(&gainSlider, s.ctxMenuSubWindow.GetAutoID(), s.selectedTrack.Gain, theme.secondary1, theme.fg2, nil, false, false, rl.Blank))

		testBtn := gui.AddChild(content, NewCtxMenuButton("play-monitor.png", "Test", theme.fg2, theme.bg3))
		playBtn := gui.AddChild(content, NewCtxMenuButton("play.png", "Play", theme.fg2, theme.bg3))
		deleteBtn := gui.AddChild(content, NewCtxMenuButton("trash.png", "Delete", theme.fg2, theme.error2))

		gui.AddChild(content, gui.NewText(gui.TextProps{
			BoxProps: gui.BoxProps{
				SizingX:     gui.Grow(),
				ChildAlignX: gui.Center,
			},
			FontConfigProps: gui.FontConfigProps{
				FgColor: theme.fg3,
			},
		}, fmt.Sprint(s.selectedTrack.ID)))

		gui.AddPostUpdate(func() {
			if nameInput.HasChanged() {
				name := strings.TrimSpace(nameInput.Value())

				if ValidateTrackName(name) != "" {
					nameInput.BorderColor = theme.error1
				} else {
					nameInput.BorderColor = rl.Blank
					s.selectedTrack.Name = name
					if err := storage.Save(); err != nil {
						s.OpenErrorPopup(fmt.Sprintf("Could not update the track name: %s", err), false)
						return
					}
				}
			}

			if changeImageBtn.IsLeftButtonPressed() {
				filePath, ok, err := crossplatform.OpenFileDialog("Select Image", "Image (*.png, *.jpg) | *.png;*.jpg")
				if err != nil {
					s.OpenErrorPopup(fmt.Sprintf("Could not open the file selecting dialog: %s", err), false)
					return
				}
				if ok {
					if err := storage.SetTrackImage(s.selectedTrack.ID, filePath); err != nil {
						s.OpenErrorPopup(fmt.Sprintf("Could not set the track image: %s", err), false)
						return
					}
				}
			}

			if testBtn.IsLeftButtonPressed() {
				if s.selectedTrackTestSoundID != 0 {
					audioPlayer.RemoveSound(s.selectedTrackTestSoundID)
				}
				soundID, err := audioPlayer.AddTrack(storage, s.selectedTrack.ID, true)
				if err != nil {
					s.OpenErrorPopup(fmt.Sprintf("Could not create the track: %s", err), false)
					return
				}
				s.selectedTrackTestSoundID = soundID
			}

			if playBtn.IsLeftButtonPressed() {
				if s.selectedTrackTestSoundID != 0 {
					audioPlayer.RemoveSound(s.selectedTrackTestSoundID)
					s.selectedTrackTestSoundID = 0
				}
				if _, err := audioPlayer.AddTrack(storage, s.selectedTrack.ID, false); err != nil {
					s.OpenErrorPopup(fmt.Sprintf("Could not play track [ID=%d NAME=%q]: %s", s.selectedTrack.ID, s.selectedTrack.Name, err), false)
					return
				}
			}

			if gainSlider.IsChanging() {
				gain := linearToExponentialGain(gainSlider.GetProgress())
				if s.selectedTrackTestSoundID != 0 {
					audioPlayer.SetSoundGain(s.selectedTrackTestSoundID, gain)
				} else {
					audioPlayer.SetTrackGain(s.selectedTrack.ID, gain)
				}
			}

			if gainSlider.HasChanged() {
				s.selectedTrack.Gain = linearToExponentialGain(gainSlider.GetProgress())
				if err := storage.Save(); err != nil {
					s.OpenErrorPopup(fmt.Sprintf("Update the track gain: %s", err), false)
					return
				}
			}

			if deleteBtn.IsLeftButtonPressed() {
				s.trackIDToDelete = s.selectedTrack.ID
				s.CloseCtxMenu()
				s.OpenPopup(PopupRemoveTrackConfirmation)
			}
		})
	})
}

func (s *Scene) BuildPopupRemoveTrackConfirmation(storage *Storage) {
	assert.NotEqual(s.trackIDToDelete, 0)
	track := storage.GetTrackByID(s.trackIDToDelete)

	NewPopup(s.popupSubWindow, "Confirm Deletion", func(body, buttons *gui.Box) {
		gui.AddChild(body, gui.NewText(gui.TextProps{
			FontConfigProps: gui.FontConfigProps{
				FgColor: theme.fg2,
			},
		}, "Are you sure you want to delete \""+track.Name+"\" track?"))

		deleteBtn := gui.AddChild(buttons, NewPrimaryButton("trash.png", "Delete", theme.error1, gui.Grow()))
		cancelBtn := gui.AddChild(buttons, NewSecondaryButton("cross.png", "Cancel", gui.Grow()))

		gui.AddPostUpdate(func() {
			if deleteBtn.IsLeftButtonPressed() {
				if err := storage.RemoveTrack(s.trackIDToDelete); err != nil {
					s.OpenErrorPopup(fmt.Sprintf("Could not remove the track: %s", err), false)
					return
				}
				s.ClosePopup()
			}

			if cancelBtn.IsLeftButtonPressed() {
				s.ClosePopup()
			}
		})
	})
}

func (s *Scene) BuildCtxMenuGain(storage *Storage, audioPlayer *AudioPlayer) {
	NewCtxMenu(s.ctxMenuSubWindow, func(content *gui.Box) {
		gui.AddChild(content, gui.NewText(gui.TextProps{
			BoxProps: gui.BoxProps{
				SizingX:     gui.Grow(),
				ChildAlignX: gui.Center,
			},
			FontConfigProps: gui.FontConfigProps{
				FgColor:  theme.fg2,
				FontSize: 26,
				Font:     theme.fontBold,
			},
		}, "Microphone"))
		var micGainSlider *gui.Slider
		var micMuteBtn *gui.Button
		micBox := gui.AddChild(content, NewGainSlider(
			&micGainSlider, s.ctxMenuSubWindow.GetAutoID(), storage.InputGain, theme.primary1, theme.fg2,
			&micMuteBtn, true, storage.IsInputMuted, theme.primary1,
		))
		var denoiseBtn *gui.Button
		if storage.IsDenoiseEnabled {
			denoiseBtn = gui.AddChild(micBox, NewPrimaryButton("denoise.png", "", theme.primary1, gui.Shrink(), gui.Grow()))
		} else {
			denoiseBtn = gui.AddChild(micBox, NewPrimaryButton("denoise-disabled.png", "", theme.error1, gui.Shrink(), gui.Grow()))
		}

		gui.AddChild(content, gui.NewText(gui.TextProps{
			BoxProps: gui.BoxProps{
				SizingX:     gui.Grow(),
				ChildAlignX: gui.Center,
			},
			FontConfigProps: gui.FontConfigProps{
				FgColor:  theme.fg2,
				FontSize: 26,
				Font:     theme.fontBold,
			},
		}, "Tracks"))
		var tracksGainSlider *gui.Slider
		var tracksMuteBtn *gui.Button
		gui.AddChild(content, NewGainSlider(
			&tracksGainSlider, s.ctxMenuSubWindow.GetAutoID(), storage.TracksGain, theme.primary1, theme.fg2,
			&tracksMuteBtn, true, storage.IsTracksMuted, theme.primary1,
		))

		gui.AddChild(content, gui.NewText(gui.TextProps{
			BoxProps: gui.BoxProps{
				SizingX:     gui.Grow(),
				ChildAlignX: gui.Center,
			},
			FontConfigProps: gui.FontConfigProps{
				FgColor:  theme.fg2,
				FontSize: 26,
				Font:     theme.fontBold,
			},
		}, "Monitor"))
		var monitorGainSlider *gui.Slider
		var monitorMuteBtn *gui.Button
		gui.AddChild(content, NewGainSlider(
			&monitorGainSlider, s.ctxMenuSubWindow.GetAutoID(), storage.OutputGain, theme.primary1, theme.fg2,
			&monitorMuteBtn, false, storage.IsMonitorMuted, theme.primary1,
		))

		gui.AddPostUpdate(func() {
			if micGainSlider.IsChanging() {
				audioPlayer.InputGain = linearToExponentialGain(micGainSlider.GetProgress())
			}
			if micGainSlider.HasChanged() {
				storage.InputGain = linearToExponentialGain(micGainSlider.GetProgress())
				if err := storage.Save(); err != nil {
					s.OpenErrorPopup(fmt.Sprintf("Could not update the input gain: %s", err), false)
					return
				}
			}
			if micMuteBtn.IsLeftButtonPressed() {
				storage.IsInputMuted = !storage.IsInputMuted
				audioPlayer.SetInputMuted(storage.IsInputMuted)
				if storage.IsInputMuted {
					audioPlayer.AddSamples(muteSoundSamples, 1, IDUnset, true)
				} else {
					audioPlayer.AddSamples(unmuteSoundSamples, 1, IDUnset, true)
				}
				if err := storage.Save(); err != nil {
					s.OpenErrorPopup(fmt.Sprintf("Could not mute/unmute the input: %s", err), false)
					return
				}
			}

			if denoiseBtn.IsLeftButtonPressed() {
				storage.IsDenoiseEnabled = !storage.IsDenoiseEnabled
				audioPlayer.SetDenoiseEnabled(storage.IsDenoiseEnabled)
				if err := storage.Save(); err != nil {
					s.OpenErrorPopup(fmt.Sprintf("Could not enable/disable denoise: %s", err), false)
					return
				}
			}

			if tracksGainSlider.IsChanging() {
				audioPlayer.TracksGain = linearToExponentialGain(tracksGainSlider.GetProgress())
			}
			if tracksGainSlider.HasChanged() {
				storage.TracksGain = linearToExponentialGain(tracksGainSlider.GetProgress())
				if err := storage.Save(); err != nil {
					s.OpenErrorPopup(fmt.Sprintf("Could not update the tracks gain: %s", err), false)
					return
				}
			}
			if tracksMuteBtn.IsLeftButtonPressed() {
				storage.IsTracksMuted = !storage.IsTracksMuted
				audioPlayer.SetTracksMuted(storage.IsTracksMuted)
				if err := storage.Save(); err != nil {
					s.OpenErrorPopup(fmt.Sprintf("Could not mute/unmute the tracks: %s", err), false)
					return
				}
			}

			if monitorGainSlider.IsChanging() {
				audioPlayer.OutputGain = linearToExponentialGain(monitorGainSlider.GetProgress())
			}
			if monitorGainSlider.HasChanged() {
				storage.OutputGain = linearToExponentialGain(monitorGainSlider.GetProgress())
				if err := storage.Save(); err != nil {
					s.OpenErrorPopup(fmt.Sprintf("Could not update the monitor gain: %s", err), false)
					return
				}
			}
			if monitorMuteBtn.IsLeftButtonPressed() {
				storage.IsMonitorMuted = !storage.IsMonitorMuted
				audioPlayer.SetMonitorMuted(storage.IsMonitorMuted)
				if err := storage.Save(); err != nil {
					s.OpenErrorPopup(fmt.Sprintf("Could not mute/unmute the monitor: %s", err), false)
					return
				}
			}
		})
	})
}

func (s *Scene) BuildErrorPopup(shouldExitProgram *bool) {
	root := s.errorPopupSubWindow.SetRoot(gui.NewBox(gui.BoxProps{
		SizingX:     gui.Grow(),
		SizingY:     gui.Grow(),
		ChildAlignX: gui.Center,
		ChildAlignY: gui.Center,
		BgColor:     rl.ColorAlpha(theme.bg1, 0.5),
	}))
	bodyBox := gui.AddChild(root, gui.NewBox(gui.BoxProps{
		Orientation:  gui.Vertical,
		Padding:      theme.popupPadding,
		ChildGap:     theme.popupChildGap,
		BorderWidth:  theme.border,
		BorderColor:  theme.error1,
		CornerRadius: theme.radius,
		BgColor:      theme.bg1,
	}))

	titleBox := gui.AddChild(bodyBox, gui.NewBox(gui.BoxProps{
		SizingX:     gui.Grow(),
		ChildAlignX: gui.Center,
		ChildAlignY: gui.Center,
		ChildGap:    theme.childGap,
	}))
	gui.AddChild(titleBox, gui.NewBoxImage(gui.BoxProps{
		SizingX: gui.Fixed(32),
		SizingY: gui.AspectRatio(1),
	}, "assets/icons/exclamation.png", iconsFS))
	gui.AddChild(titleBox, gui.NewText(gui.TextProps{
		FontConfigProps: gui.FontConfigProps{
			FontSize: 28,
			Font:     theme.fontBold,
		},
	}, gui.Ternary(s.errorPopupIsFatal, "Fatal Error", "Error")))

	gui.AddChild(bodyBox, gui.NewText(gui.TextProps{
		BoxProps: gui.BoxProps{
			SizingX: gui.Shrink(400),
		},
		FontConfigProps: gui.FontConfigProps{
			FontSize: 24,
		},
		Wrapping: gui.Wrap,
	}, s.errorPopupErrMsg))

	buttonsBox := gui.AddChild(bodyBox, gui.NewBox(gui.BoxProps{
		SizingX:     gui.Grow(),
		ChildGap:    theme.popupChildGap,
		ChildAlignX: gui.End,
	}))
	copyErrorBtn := gui.AddChild(buttonsBox, NewSecondaryButton("copy.png", "Copy error"))
	closeBtn := gui.AddChild(buttonsBox, NewPrimaryButton("cross.png", gui.Ternary(s.errorPopupIsFatal, "Exit program", "Close"), theme.error1))

	gui.AddPostUpdate(func() {
		if closeBtn.IsLeftButtonPressed() {
			if s.errorPopupIsFatal {
				*shouldExitProgram = true
			} else {
				s.CloseErrorPopup()
			}
		}

		if copyErrorBtn.IsLeftButtonPressed() {
			rl.SetClipboardText(s.errorPopupErrMsg)
		}
	})
}

const WaveCutterMinSamples = SampleRate * 0.1

type WaveCutterProps struct {
	gui.BoxProps

	CutSamplesGain      float32
	SamplesColor        rl.Color
	CutBarsColor        rl.Color
	RemovedSegmentColor rl.Color
}

type WaveCutter struct {
	gui.Box
	gui.ChildlessNode

	SamplesColor        rl.Color
	CutBarsColor        rl.Color
	RemovedSegmentColor rl.Color

	audioPlayer      *AudioPlayer
	playingSoundID   SoundID
	playingSoundGain float32

	samples  []float32
	startCut int
	endCut   int

	isDraggingStartCut bool
	isDraggingEndCut   bool

	buf1 []int8
	buf2 []int8
}

func NewWaveCutter(props WaveCutterProps, samples []float32, audioPlayer *AudioPlayer) *WaveCutter {
	assert.NotEqual(props.ID, 0, "WaveCutter needs an ID")

	if node := gui.GetNodeFromCache[*WaveCutter](props.ID); node != nil {
		return node
	}

	assert.GreaterEqual(len(samples), WaveCutterMinSamples)
	const sampleCountToDraw = 500

	node := &WaveCutter{
		audioPlayer: audioPlayer,

		samples:  samples,
		startCut: 0,
		endCut:   len(samples),

		buf1: make([]int8, sampleCountToDraw),
		buf2: make([]int8, sampleCountToDraw),

		playingSoundGain: 1,
	}
	node.ApplyProps(props)

	gui.CacheNode(node)
	return node
}

func (n *WaveCutter) ApplyProps(props WaveCutterProps) {
	n.Box.ApplyProps(props.BoxProps)

	n.playingSoundGain = props.CutSamplesGain

	if props.SamplesColor == rl.Blank {
		props.SamplesColor = rl.LightGray
	}
	n.SamplesColor = props.SamplesColor

	if props.CutBarsColor == rl.Blank {
		props.CutBarsColor = rl.White
	}
	n.CutBarsColor = props.CutBarsColor

	n.RemovedSegmentColor = props.RemovedSegmentColor
}

func (n *WaveCutter) Update() {
	if gui.IsMouseButtonReleased(rl.MouseButtonLeft) {
		n.isDraggingStartCut = false
		n.isDraggingEndCut = false
	}

	if !n.isDraggingStartCut && !n.isDraggingEndCut && !gui.IsNodeHovered(n) {
		return
	}

	innerRect := n.InnerRect()

	mouseRelPos := rl.Vector2Subtract(rl.GetMousePosition(), gui.Vec2(innerRect.X, innerRect.Y))

	startCutRelPosX := innerRect.Width * (float32(n.startCut) / float32(len(n.samples)))
	endCutRelPosX := innerRect.Width * (float32(n.endCut) / float32(len(n.samples)))

	const HandleMouseWidth = 20

	startCutHandleRelRect := gui.Rect(startCutRelPosX-HandleMouseWidth/2, 0, HandleMouseWidth, innerRect.Height)
	endCutHandleRelRect := gui.Rect(endCutRelPosX-HandleMouseWidth/2, 0, HandleMouseWidth, innerRect.Height)

	if !n.isDraggingStartCut && !n.isDraggingEndCut {
		switch {
		case rl.CheckCollisionPointRec(mouseRelPos, startCutHandleRelRect):
			gui.SetMouseCursor(rl.MouseCursorPointingHand)
			if gui.IsMouseButtonPressedConsume(rl.MouseButtonLeft) {
				n.isDraggingStartCut = true
				if n.playingSoundID != 0 {
					n.audioPlayer.RemoveSound(n.playingSoundID)
					n.playingSoundID = 0
				}
			}

		case rl.CheckCollisionPointRec(mouseRelPos, endCutHandleRelRect):
			gui.SetMouseCursor(rl.MouseCursorPointingHand)
			if gui.IsMouseButtonPressedConsume(rl.MouseButtonLeft) {
				n.isDraggingEndCut = true
				if n.playingSoundID != 0 {
					n.audioPlayer.RemoveSound(n.playingSoundID)
					n.playingSoundID = 0
				}
			}

		case n.playingSoundID != 0:
			gui.SetMouseCursor(rl.MouseCursorPointingHand)
			if gui.IsMouseButtonPressedConsume(rl.MouseButtonLeft) {
				cutWidth := endCutRelPosX - startCutRelPosX
				progress := gui.Clamp((mouseRelPos.X-startCutRelPosX)/cutWidth, 0, 1)
				n.audioPlayer.SetProgress(n.playingSoundID, progress)
			}
		}
	}

	switch {
	case n.isDraggingStartCut:
		n.startCut = int((mouseRelPos.X / innerRect.Width) * float32(len(n.samples)))
		n.startCut = min(n.startCut, n.endCut-WaveCutterMinSamples)
		n.startCut = gui.Clamp(n.startCut, 0, len(n.samples)-1)

	case n.isDraggingEndCut:
		n.endCut = int((mouseRelPos.X / innerRect.Width) * float32(len(n.samples)))
		n.endCut = max(n.startCut+WaveCutterMinSamples, n.endCut)
		n.endCut = gui.Clamp(n.endCut, 0, len(n.samples))
	}
}

func (n *WaveCutter) Render() {
	if !gui.IsNodeVisible(n) {
		return
	}

	innerRect := n.InnerRect()

	if n.BgColor.A != 0 || (n.BorderColor.A != 0 && n.BorderWidth != (gui.BoxSides{})) {
		gui.DrawRectangle(innerRect, n.BorderWidth, n.CornerRadius, n.BorderColor, n.BgColor)
	}

	centerY := innerRect.Y + innerRect.Height/2

	sampleCountToDraw := min(len(n.samples), 500)

	distanceBetweenSamples := innerRect.Width / float32(sampleCountToDraw)
	jumpDistance := len(n.samples) / sampleCountToDraw

	assert.Equal(len(n.buf1), sampleCountToDraw)
	assert.Equal(len(n.buf1), len(n.buf2))

	// smoothing
	n.buf1 = n.buf1[:0]
	for i := 0; i < len(n.samples) && len(n.buf1) < cap(n.buf1); i += int(jumpDistance) {
		start := i
		end := min(start+3000, len(n.samples))

		maxSample := int8(slices.Max(n.samples[start:end]) * 127)
		n.buf1 = append(n.buf1, maxSample)
	}

	// blurring
	n.buf2 = n.buf2[:len(n.buf1)]
	for i := 2; i < len(n.buf1)-2; i++ {
		n.buf2[i] = int8((int(n.buf1[i-2]) + int(n.buf1[i-1]) + int(n.buf1[i]) + int(n.buf1[i+1]) + int(n.buf1[i+2])) / 5)
	}

	for i := 0; i < sampleCountToDraw; i += 6 {
		sample := float32(n.buf2[i]) / 128

		amplitude := sample*(innerRect.Height/2) + 2

		top := gui.Vec2(
			innerRect.X+distanceBetweenSamples*float32(i),
			centerY+amplitude,
		)
		bottom := gui.Vec2(
			innerRect.X+distanceBetweenSamples*float32(i),
			centerY-amplitude,
		)

		rl.DrawLineEx(top, bottom, 3, n.SamplesColor)
	}

	startCutPosX := innerRect.X + innerRect.Width*(float32(n.startCut)/float32(len(n.samples)))
	endCutPosX := innerRect.X + innerRect.Width*(float32(n.endCut)/float32(len(n.samples)))

	if n.playingSoundID != 0 {
		progress := n.audioPlayer.GetProgress(n.playingSoundID)
		if progress == 1 {
			n.playingSoundID = 0
		}
		progressPosX := startCutPosX + (endCutPosX-startCutPosX)*progress

		rl.DrawLineEx(gui.Vec2(progressPosX, innerRect.Y), gui.Vec2(progressPosX, innerRect.Y+innerRect.Height), 2, n.CutBarsColor)
	}

	rl.DrawRectangleRec(gui.Rect(innerRect.X-2, innerRect.Y-2, startCutPosX-innerRect.X+2, innerRect.Height+4), n.RemovedSegmentColor)
	rl.DrawRectangleRec(gui.Rect(endCutPosX, innerRect.Y-2, innerRect.X+innerRect.Width-endCutPosX+2, innerRect.Height+4), n.RemovedSegmentColor)

	const BarWidth = 3
	const HandleWidth = 8
	const HandleHeight = 32

	rl.DrawLineEx(gui.Vec2(startCutPosX, innerRect.Y), gui.Vec2(startCutPosX, innerRect.Y+innerRect.Height), BarWidth, n.CutBarsColor)
	rl.DrawRectangleRounded(gui.Rect(startCutPosX-HandleWidth/2, centerY-HandleHeight/2, HandleWidth, HandleHeight), 3, 2, n.CutBarsColor)

	rl.DrawLineEx(gui.Vec2(endCutPosX, innerRect.Y), gui.Vec2(endCutPosX, innerRect.Y+innerRect.Height), BarWidth, n.CutBarsColor)
	rl.DrawRectangleRounded(gui.Rect(endCutPosX-HandleWidth/2, centerY-HandleHeight/2, HandleWidth, HandleHeight), 3, 2, n.CutBarsColor)

	gui.DebuggingInfo(n)
}

func (n *WaveCutter) IsPlaying() bool {
	if n.playingSoundID != 0 {
		if progress := n.audioPlayer.GetProgress(n.playingSoundID); progress != -1 {
			return true
		}
		n.playingSoundID = 0
	}
	return false
}

func (n *WaveCutter) PlayCutSamples(quality SampleQuality) {
	if n.playingSoundID != 0 {
		n.audioPlayer.RemoveSound(n.playingSoundID)
	}

	samples := n.GetCutSamples(quality)
	n.playingSoundID = n.audioPlayer.AddSamples(samples, n.playingSoundGain, IDDummy, true)
}

func (n *WaveCutter) ChangePlayingSoundQuality(quality SampleQuality) {
	if n.playingSoundID == 0 {
		return
	}

	samples := n.GetCutSamples(quality)
	n.audioPlayer.ReplaceSamples(n.playingSoundID, samples)
}

func (n *WaveCutter) StopPlaying() {
	if n.playingSoundID == 0 {
		return
	}

	n.audioPlayer.RemoveSound(n.playingSoundID)
	n.playingSoundID = 0
}

func (n *WaveCutter) SetCutSamplesGain(gain float32) {
	n.playingSoundGain = gain
	if n.playingSoundID != 0 {
		n.audioPlayer.SetSoundGain(n.playingSoundID, n.playingSoundGain)
	}
}

func (n *WaveCutter) GetCutSamples(quality SampleQuality) Samples {
	assert.InRange(n.startCut, 0, n.endCut-WaveCutterMinSamples)
	assert.InRange(n.endCut, n.startCut+WaveCutterMinSamples, len(n.samples))

	cutSamples := SamplesFloat32(n.samples[n.startCut:n.endCut])
	var formatedSamples Samples

	switch quality {
	case SampleQualityInt8:
		formatedSamples = make(SamplesInt8, len(cutSamples))

	case SampleQualityInt16:
		formatedSamples = make(SamplesInt16, len(cutSamples))

	case SampleQualityFloat32:
		formatedSamples = make(SamplesFloat32, len(cutSamples))

	default:
		assert.Unreachable()
	}

	copySamples(formatedSamples, cutSamples)
	return formatedSamples
}
