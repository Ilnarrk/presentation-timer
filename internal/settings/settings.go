package settings

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

type Settings struct {
	TalkMinutes                int      `json:"talkMinutes"`
	TalkSeconds                int      `json:"talkSeconds"`
	QuestionsMinutes           int      `json:"questionsMinutes"`
	QuestionsSeconds           int      `json:"questionsSeconds"`
	ReminderMinutes            int      `json:"reminderMinutes"`
	ReminderSeconds            int      `json:"reminderSeconds"`
	SoundID                    string   `json:"soundId"`
	ReminderSoundID            string   `json:"reminderSoundId"`
	QuestionsSoundID           string   `json:"questionsSoundId"`
	NextSoundID                string   `json:"nextSoundId"`
	DeviceID                   string   `json:"deviceId"`
	Volume                     float64  `json:"volume"`
	MuteConferenceSound        bool     `json:"muteConferenceSound"`
	MuteConferenceReceive      bool     `json:"muteConferenceReceive"`
	ConferenceCameraEnabled    bool     `json:"conferenceCameraEnabled"`
	SessionTotalMinutes        int      `json:"sessionTotalMinutes"`
	SessionTotalSeconds        int      `json:"sessionTotalSeconds"`
	SessionSpeakerCount        int      `json:"sessionSpeakerCount"`
	SessionSpeakerNames        []string `json:"sessionSpeakerNames"`
	SessionTalkMinutes         int      `json:"sessionTalkMinutes"`
	SessionTalkSeconds         int      `json:"sessionTalkSeconds"`
	SessionQuestionsMinutes    int      `json:"sessionQuestionsMinutes"`
	SessionQuestionsSeconds    int      `json:"sessionQuestionsSeconds"`
	SessionUseDefaultTalk      bool     `json:"sessionUseDefaultTalk"`
	SessionUseDefaultQuestions bool     `json:"sessionUseDefaultQuestions"`
	TimerScalePercent          int      `json:"timerScalePercent"`
	TimerDisplayMode           string   `json:"timerDisplayMode"`
	TimerFont                  string   `json:"timerFont"`
	WidgetPlacement            string   `json:"widgetPlacement"`
	AppTheme                   string   `json:"appTheme"`
	WidgetTheme                string   `json:"widgetTheme"`
	WidgetShape                string   `json:"widgetShape"`
	WidgetBackgroundTransparency int    `json:"widgetBackgroundTransparency"`
	WidgetFreeX                int      `json:"widgetFreeX"`
	WidgetFreeY                int      `json:"widgetFreeY"`
	WidgetFreeWidth            int      `json:"widgetFreeWidth"`
	WidgetFreeHeight           int      `json:"widgetFreeHeight"`
	WidgetColorIdle            string   `json:"widgetColorIdle"`
	WidgetColorRunning         string   `json:"widgetColorRunning"`
	WidgetColorPaused          string   `json:"widgetColorPaused"`
	WidgetColorOvertime        string   `json:"widgetColorOvertime"`
	WidgetQuickPresets         []DurationPreset `json:"widgetQuickPresets"`
	MainWindowX                int      `json:"mainWindowX"`
	MainWindowY                int      `json:"mainWindowY"`
	MainWindowWidth            int      `json:"mainWindowWidth"`
	MainWindowHeight           int      `json:"mainWindowHeight"`
}

const (
	MinTimerScalePercent     = 80
	MaxTimerScalePercent     = 140
	DefaultTimerScalePercent = 115

	WidgetPlacementTopRight = "topRight"
	WidgetPlacementTopCenter = "topCenter"
	WidgetPlacementTopLeft  = "topLeft"
	WidgetPlacementFree     = "free"
	AppThemeDark            = "dark"
	AppThemeLight           = "light"
	WidgetThemeDark         = "dark"
	WidgetThemeLight        = "light"
	WidgetThemeGreen        = "green"
	WidgetThemeTransparent  = "transparent"
	WidgetShapeRounded      = "rounded"
	WidgetShapeRectangular  = "rectangular"
	TimerDisplayModeRing    = "ring"
	TimerDisplayModeDigital = "digital"
	TimerFontSystem         = "system"
	TimerFontDigital        = "digital"
	MinWidgetBackgroundTransparency = 0
	MaxWidgetBackgroundTransparency = 100
	DefaultWidgetBackgroundTransparency = 0
	WidgetQuickPresetCount = 4
	MaxTalkDurationMinutes = 180
)

type DurationPreset struct {
	Minutes int `json:"minutes"`
	Seconds int `json:"seconds"`
}

var timerFontIDPattern = regexp.MustCompile(`^[a-z0-9_-]{1,32}$`)

func NormalizeWidgetPlacement(placement string) string {
	switch placement {
	case WidgetPlacementTopLeft, WidgetPlacementTopCenter, WidgetPlacementFree:
		return placement
	default:
		return WidgetPlacementTopRight
	}
}

func Default() Settings {
	return Settings{
		TalkMinutes:                10,
		TalkSeconds:                0,
		QuestionsMinutes:           5,
		QuestionsSeconds:           0,
		ReminderMinutes:            2,
		ReminderSeconds:            0,
		SoundID:                    "",
		ReminderSoundID:            "",
		QuestionsSoundID:           "",
		NextSoundID:                "",
		DeviceID:                   "default",
		Volume:                     0.85,
		MuteConferenceSound:        false,
		MuteConferenceReceive:      true,
		ConferenceCameraEnabled:    true,
		SessionUseDefaultTalk:      true,
		SessionUseDefaultQuestions: true,
		TimerScalePercent:          DefaultTimerScalePercent,
		TimerDisplayMode:           TimerDisplayModeRing,
		TimerFont:                  TimerFontDigital,
		WidgetPlacement:            WidgetPlacementFree,
		AppTheme:                   AppThemeDark,
		WidgetTheme:                WidgetThemeTransparent,
		WidgetShape:                WidgetShapeRounded,
		WidgetBackgroundTransparency: DefaultWidgetBackgroundTransparency,
		WidgetFreeX:                0,
		WidgetFreeY:                0,
		WidgetQuickPresets:         DefaultWidgetQuickPresets(),
	}
}

func DefaultWidgetQuickPresets() []DurationPreset {
	return []DurationPreset{
		{Minutes: 5, Seconds: 0},
		{Minutes: 10, Seconds: 0},
		{Minutes: 15, Seconds: 0},
		{Minutes: 20, Seconds: 0},
	}
}

type Store struct {
	mu       sync.RWMutex
	path     string
	settings Settings
}

func NewMemoryStore() *Store {
	return &Store{settings: Default()}
}

func NewMemoryStoreWithDefaults(defaults Settings) *Store {
	return &Store{settings: normalize(defaults, defaults)}
}

func NewStore() (*Store, error) {
	return NewStoreWithDefaults(Default())
}

// NewStoreWithDefaults loads saved values over defaults. Unmarshalling into
// the pre-populated value is intentional: fields absent from older JSON files
// retain current defaults, while explicitly saved empty sound IDs stay empty.
func NewStoreWithDefaults(defaults Settings) (*Store, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}

	appDir := filepath.Join(dir, "presentation-timer")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		return nil, err
	}

	store := &Store{
		path:     filepath.Join(appDir, "settings.json"),
		settings: normalize(defaults, Default()),
	}

	if err := store.load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	return store, nil
}

func (s *Store) Get() Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.settings
}

func (s *Store) PersistsToDisk() bool {
	return s.path != ""
}

func (s *Store) Save(settings Settings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.settings = normalize(settings, s.settings)
	return s.saveLocked()
}

// KeepSession copies session template fields from stored settings onto input
// so a regular settings save cannot wipe a template the user stored separately.
func KeepSession(input, stored Settings) Settings {
	input.SessionTotalMinutes = stored.SessionTotalMinutes
	input.SessionTotalSeconds = stored.SessionTotalSeconds
	input.SessionSpeakerCount = stored.SessionSpeakerCount
	input.SessionSpeakerNames = append([]string(nil), stored.SessionSpeakerNames...)
	input.SessionTalkMinutes = stored.SessionTalkMinutes
	input.SessionTalkSeconds = stored.SessionTalkSeconds
	input.SessionQuestionsMinutes = stored.SessionQuestionsMinutes
	input.SessionQuestionsSeconds = stored.SessionQuestionsSeconds
	input.SessionUseDefaultTalk = stored.SessionUseDefaultTalk
	input.SessionUseDefaultQuestions = stored.SessionUseDefaultQuestions
	input.WidgetFreeX = stored.WidgetFreeX
	input.WidgetFreeY = stored.WidgetFreeY
	input.WidgetFreeWidth = stored.WidgetFreeWidth
	input.WidgetFreeHeight = stored.WidgetFreeHeight
	input.MainWindowX = stored.MainWindowX
	input.MainWindowY = stored.MainWindowY
	input.MainWindowWidth = stored.MainWindowWidth
	input.MainWindowHeight = stored.MainWindowHeight
	return input
}

func (s *Store) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, &s.settings); err != nil {
		backupPath := s.path + ".bak"
		_ = os.Remove(backupPath)
		if renameErr := os.Rename(s.path, backupPath); renameErr != nil {
			return err
		}
		return os.ErrNotExist
	}
	s.settings = normalize(s.settings, Default())
	return nil
}

func normalize(value, fallback Settings) Settings {
	if value.ReminderMinutes < 0 || value.ReminderSeconds < 0 ||
		value.ReminderMinutes == 0 && value.ReminderSeconds == 0 {
		value.ReminderMinutes = fallback.ReminderMinutes
		value.ReminderSeconds = fallback.ReminderSeconds
		if value.ReminderMinutes == 0 && value.ReminderSeconds == 0 {
			value.ReminderMinutes = Default().ReminderMinutes
			value.ReminderSeconds = Default().ReminderSeconds
		}
	}
	if value.DeviceID == "" {
		value.DeviceID = "default"
	}
	if value.SoundID == "" {
		value.SoundID = fallback.SoundID
	}
	if value.Volume < 0 {
		value.Volume = 0
	}
	if value.Volume > 1 {
		value.Volume = 1
	}
	if value.SessionTotalMinutes < 0 {
		value.SessionTotalMinutes = 0
	}
	if value.SessionTotalSeconds < 0 {
		value.SessionTotalSeconds = 0
	}
	if value.SessionSpeakerCount < 0 {
		value.SessionSpeakerCount = 0
	}
	if value.SessionSpeakerCount > 50 {
		value.SessionSpeakerCount = 50
	}
	if value.SessionSpeakerNames == nil {
		value.SessionSpeakerNames = []string{}
	}
	if value.SessionTalkMinutes < 0 {
		value.SessionTalkMinutes = 0
	}
	if value.SessionTalkSeconds < 0 {
		value.SessionTalkSeconds = 0
	}
	if value.SessionQuestionsMinutes < 0 {
		value.SessionQuestionsMinutes = 0
	}
	if value.SessionQuestionsSeconds < 0 {
		value.SessionQuestionsSeconds = 0
	}
	if !value.SessionUseDefaultTalk && value.SessionTalkMinutes == 0 && value.SessionTalkSeconds == 0 {
		value.SessionUseDefaultTalk = true
	}
	if !value.SessionUseDefaultQuestions && value.SessionQuestionsMinutes == 0 && value.SessionQuestionsSeconds == 0 {
		value.SessionUseDefaultQuestions = true
	}
	if value.TimerScalePercent < MinTimerScalePercent {
		if value.TimerScalePercent == 0 {
			value.TimerScalePercent = fallback.TimerScalePercent
			if value.TimerScalePercent == 0 {
				value.TimerScalePercent = DefaultTimerScalePercent
			}
		} else {
			value.TimerScalePercent = MinTimerScalePercent
		}
	}
	if value.TimerScalePercent > MaxTimerScalePercent {
		value.TimerScalePercent = MaxTimerScalePercent
	}
	switch value.TimerDisplayMode {
	case TimerDisplayModeDigital:
	default:
		value.TimerDisplayMode = TimerDisplayModeRing
	}
	value.TimerFont = normalizeTimerFont(value.TimerFont)
	value.WidgetColorIdle = normalizeWidgetColor(value.WidgetColorIdle)
	value.WidgetColorRunning = normalizeWidgetColor(value.WidgetColorRunning)
	value.WidgetColorPaused = normalizeWidgetColor(value.WidgetColorPaused)
	value.WidgetColorOvertime = normalizeWidgetColor(value.WidgetColorOvertime)
	switch value.WidgetPlacement {
	case WidgetPlacementTopLeft, WidgetPlacementTopCenter, WidgetPlacementFree:
	default:
		if value.WidgetPlacement == "" {
			value.WidgetPlacement = fallback.WidgetPlacement
			if value.WidgetPlacement == "" {
				value.WidgetPlacement = WidgetPlacementFree
			}
		} else {
			value.WidgetPlacement = WidgetPlacementTopRight
		}
	}
	switch value.AppTheme {
	case AppThemeLight:
	default:
		value.AppTheme = AppThemeDark
	}
	switch value.WidgetTheme {
	case "violet":
		value.WidgetTheme = WidgetThemeTransparent
	case WidgetThemeDark, WidgetThemeLight, WidgetThemeGreen, WidgetThemeTransparent:
	default:
		value.WidgetTheme = fallback.WidgetTheme
		if value.WidgetTheme == "" {
			value.WidgetTheme = WidgetThemeTransparent
		}
	}
	if value.WidgetBackgroundTransparency < MinWidgetBackgroundTransparency {
		value.WidgetBackgroundTransparency = MinWidgetBackgroundTransparency
	}
	if value.WidgetBackgroundTransparency > MaxWidgetBackgroundTransparency {
		value.WidgetBackgroundTransparency = MaxWidgetBackgroundTransparency
	}
	switch value.WidgetShape {
	case WidgetShapeRectangular:
	case "square":
		value.WidgetShape = WidgetShapeRectangular
	default:
		value.WidgetShape = WidgetShapeRounded
	}
	value.WidgetQuickPresets = normalizeWidgetQuickPresets(value.WidgetQuickPresets)
	return value
}

func normalizeTimerFont(font string) string {
	if font == TimerFontSystem {
		return TimerFontSystem
	}
	if timerFontIDPattern.MatchString(font) && font != TimerFontSystem {
		return font
	}
	return TimerFontDigital
}

func normalizeWidgetQuickPresets(value []DurationPreset) []DurationPreset {
	defaults := DefaultWidgetQuickPresets()
	out := make([]DurationPreset, WidgetQuickPresetCount)
	for i := 0; i < WidgetQuickPresetCount; i++ {
		preset := defaults[i]
		if i < len(value) {
			preset = value[i]
		}
		out[i] = clampDurationPreset(preset)
	}
	return out
}

func clampDurationPreset(preset DurationPreset) DurationPreset {
	if preset.Minutes < 0 {
		preset.Minutes = 0
	}
	if preset.Minutes > MaxTalkDurationMinutes {
		preset.Minutes = MaxTalkDurationMinutes
	}
	if preset.Seconds < 0 {
		preset.Seconds = 0
	}
	if preset.Seconds > 59 {
		preset.Seconds = 59
	}
	if preset.Minutes == MaxTalkDurationMinutes {
		preset.Seconds = 0
	}
	if preset.Minutes == 0 && preset.Seconds == 0 {
		preset.Seconds = 1
	}
	return preset
}

func normalizeWidgetColor(color string) string {
	color = strings.TrimSpace(color)
	if color == "" {
		return ""
	}
	if !isValidHexColor(color) {
		return ""
	}
	return strings.ToLower(color)
}

func isValidHexColor(color string) bool {
	if len(color) != 4 && len(color) != 7 {
		return false
	}
	if color[0] != '#' {
		return false
	}
	for _, ch := range color[1:] {
		switch {
		case ch >= '0' && ch <= '9':
		case ch >= 'a' && ch <= 'f':
		case ch >= 'A' && ch <= 'F':
		default:
			return false
		}
	}
	return true
}

func (s *Store) saveLocked() error {
	if s.path == "" {
		return nil
	}
	data, err := json.MarshalIndent(s.settings, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(s.path)
	tmp, err := os.CreateTemp(dir, "settings-*.json")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	writeErr := func() error {
		if _, err := tmp.Write(data); err != nil {
			return err
		}
		return tmp.Close()
	}()
	if writeErr != nil {
		_ = os.Remove(tmpPath)
		return writeErr
	}
	if err := os.Rename(tmpPath, s.path); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}
