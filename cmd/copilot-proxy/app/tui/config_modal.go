package tui

import (
	"fmt"
	"reflect"
	"strings"

	bubblecursor "github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	config "copilot-proxy/cmd/copilot-proxy/app/settings"
	"copilot-proxy/cmd/copilot-proxy/app/tui/settingsform"
)

type ModalAction int

const (
	ModalActionNone ModalAction = iota
	ModalActionClose
	ModalActionSave
)

const (
	configModalPaddingY = 1
	configModalPaddingX = 2
	configModalWidth    = 88
	configModalLabelW   = 20

	kvCursorColKey   = 0
	kvCursorColValue = 1
	kvFieldIndent    = "                      "

	inputEmptyGlyph       = " "
	adaptiveInputWidthMin = 1
	adaptiveInputWidthMax = 64

	agentDetectionModeFieldKey     = "messages_agent_detection_request_mode"
	agentDetectionModeLabelPremium = "premium request"
	agentDetectionModeLabelSession = "session"
	defaultBoolLabelOn             = " ON"
	defaultBoolLabelOff            = "OFF"
)

type kvCursor struct {
	row int
	col int // 0=key, 1=value
}

type arrayCursor struct {
	row int
	col int
}

type kvInputRow struct {
	keyInput   *textinput.Model
	valueInput *textinput.Model
}

type arrayInputRow struct {
	fields map[string]*textinput.Model
}

type ConfigModal struct {
	open           bool
	specs          []settingsform.FieldSpec
	form           settingsform.SettingsForm
	baseForm       settingsform.SettingsForm
	kvCursors      map[string]kvCursor
	arrayCursors   map[string]arrayCursor
	scalarInputs   map[string]*textinput.Model
	keyValueInputs map[string][]kvInputRow
	arrayInputs    map[string][]arrayInputRow
	focus          int
	confirmDiscard bool
	errorMsg       string
}

var (
	configModalStyle = lipgloss.NewStyle().
				BorderStyle(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("39")).
				Padding(configModalPaddingY, configModalPaddingX).
				Width(configModalWidth)

	configModalTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("39"))

	configModalReadOnlyStyle = lipgloss.NewStyle().
					Foreground(lipgloss.Color("245"))

	configModalInputStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("252"))

	configModalInputFocusStyle = lipgloss.NewStyle().
					Foreground(lipgloss.Color("229")).
					Background(lipgloss.Color("57"))

	configModalInputReadOnlyStyle = lipgloss.NewStyle().
					Foreground(lipgloss.Color("243"))
)

func NewConfigModal() *ConfigModal {
	return &ConfigModal{
		open:  false,
		specs: nil,
		form: settingsform.SettingsForm{
			ScalarValues:      make(map[string]string),
			KeyValueValues:    make(map[string][]settingsform.HeaderKV),
			ObjectArrayValues: make(map[string][]map[string]string),
		},
		baseForm: settingsform.SettingsForm{
			ScalarValues:      make(map[string]string),
			KeyValueValues:    make(map[string][]settingsform.HeaderKV),
			ObjectArrayValues: make(map[string][]map[string]string),
		},
		kvCursors:      make(map[string]kvCursor),
		arrayCursors:   make(map[string]arrayCursor),
		scalarInputs:   make(map[string]*textinput.Model),
		keyValueInputs: make(map[string][]kvInputRow),
		arrayInputs:    make(map[string][]arrayInputRow),
		focus:          0,
		confirmDiscard: false,
		errorMsg:       "",
	}
}

func (m *ConfigModal) Open(settings *config.Settings) error {
	specs, err := settingsform.SettingsFieldSpecs()
	if err != nil {
		return fmt.Errorf("load settings field specs: %w", err)
	}
	form, err := settingsform.EncodeSettingsToForm(settings, specs)
	if err != nil {
		return fmt.Errorf("encode settings form: %w", err)
	}

	visibleSpecs := make([]settingsform.FieldSpec, 0, len(specs))
	kvCursors := make(map[string]kvCursor)
	arrayCursors := make(map[string]arrayCursor)
	scalarInputs := make(map[string]*textinput.Model)
	keyValueInputs := make(map[string][]kvInputRow)
	arrayInputs := make(map[string][]arrayInputRow)
	for i := range specs {
		spec := specs[i]
		if !spec.Visible {
			continue
		}
		visibleSpecs = append(visibleSpecs, spec)

		if spec.Widget == settingsform.WidgetKeyValue {
			rows := form.KeyValueValues[spec.Key]
			if len(rows) == 0 {
				rows = []settingsform.HeaderKV{{Key: "", Value: ""}}
				form.KeyValueValues[spec.Key] = rows
			}
			kvRows := make([]kvInputRow, 0, len(rows))
			for j := range rows {
				row := rows[j]
				kvRows = append(kvRows, kvInputRow{
					keyInput:   newModalTextInput(row.Key, ""),
					valueInput: newModalTextInput(row.Value, ""),
				})
			}
			keyValueInputs[spec.Key] = kvRows
			kvCursors[spec.Key] = kvCursor{row: 0, col: kvCursorColKey}
			continue
		}
		if spec.Widget == settingsform.WidgetArray {
			rows := form.ObjectArrayValues[spec.Key]
			columns := visibleArrayColumns(&spec)
			if len(rows) == 0 {
				emptyRow := make(map[string]string, len(columns))
				for _, col := range columns {
					emptyRow[col.Key] = ""
				}
				rows = []map[string]string{emptyRow}
				form.ObjectArrayValues[spec.Key] = rows
			}
			inputRows := make([]arrayInputRow, 0, len(rows))
			for _, row := range rows {
				inputMap := make(map[string]*textinput.Model, len(columns))
				for _, col := range columns {
					inputMap[col.Key] = newModalTextInput(row[col.Key], col.Placeholder)
				}
				inputRows = append(inputRows, arrayInputRow{fields: inputMap})
			}
			arrayInputs[spec.Key] = inputRows
			arrayCursors[spec.Key] = arrayCursor{row: 0, col: 0}
			continue
		}

		scalarInputs[spec.Key] = newModalTextInput(form.ScalarValues[spec.Key], spec.Placeholder)
	}

	m.open = true
	m.specs = visibleSpecs
	m.form = form.Clone()
	m.baseForm = form.Clone()
	m.kvCursors = kvCursors
	m.arrayCursors = arrayCursors
	m.scalarInputs = scalarInputs
	m.keyValueInputs = keyValueInputs
	m.arrayInputs = arrayInputs
	m.focus = m.firstFocusableIndex()
	m.confirmDiscard = false
	m.errorMsg = ""
	for i := range m.specs {
		spec := &m.specs[i]
		if spec.Widget == settingsform.WidgetArray {
			m.normalizeArrayRows(spec)
		}
	}
	m.baseForm = m.form.Clone()
	m.syncInputFocus()
	return nil
}

func (m *ConfigModal) Close() {
	m.open = false
	m.confirmDiscard = false
	m.errorMsg = ""
	m.blurAllInputs()
}

func (m *ConfigModal) IsOpen() bool {
	return m.open
}

func (m *ConfigModal) InDiscardConfirm() bool {
	return m.confirmDiscard
}

func (m *ConfigModal) IsDirty() bool {
	return !reflect.DeepEqual(m.baseForm, m.form)
}

func (m *ConfigModal) VisibleFieldKeys() []string {
	keys := make([]string, 0, len(m.specs))
	for i := range m.specs {
		keys = append(keys, m.specs[i].Key)
	}
	return keys
}

func (m *ConfigModal) CurrentFieldKey() string {
	if len(m.specs) == 0 || m.focus < 0 || m.focus >= len(m.specs) {
		return ""
	}
	return m.specs[m.focus].Key
}

func (m *ConfigModal) KeyValueRowCount(fieldKey string) int {
	return len(m.form.KeyValueValues[fieldKey])
}

func (m *ConfigModal) FieldValue(fieldKey string) string {
	return m.form.ScalarValues[fieldKey]
}

func (m *ConfigModal) SetError(message string) {
	m.errorMsg = message
}

func (m *ConfigModal) BuildCandidate(base *config.Settings) (config.Settings, error) {
	sanitizedForm := sanitizeFormForSave(m.specs, m.form)
	settings, err := settingsform.DecodeFormToSettings(base, m.specs, sanitizedForm)
	if err != nil {
		return config.Settings{}, fmt.Errorf("decode settings from form: %w", err)
	}
	clearEmptyKeyValueMaps(&settings, m.specs, sanitizedForm)
	return settings, nil
}

func (m *ConfigModal) HandleKey(msg tea.KeyMsg) ModalAction {
	if !m.open {
		return ModalActionNone
	}
	if m.confirmDiscard {
		return m.handleDiscardConfirmKey(msg)
	}

	switch {
	case keyMatches(msg, tea.KeyEsc, "esc"):
		if m.IsDirty() {
			m.confirmDiscard = true
			return ModalActionNone
		}
		return ModalActionClose
	case keyMatches(msg, tea.KeyCtrlS, "ctrl+s"):
		return ModalActionSave
	case keyMatches(msg, tea.KeyTab, "tab"):
		if m.handleColumnTab(1) {
			return ModalActionNone
		}
		m.moveFocus(1)
		m.syncInputFocus()
		return ModalActionNone
	case keyMatches(msg, tea.KeyShiftTab, "shift+tab"):
		if m.handleColumnTab(-1) {
			return ModalActionNone
		}
		m.moveFocus(-1)
		m.syncInputFocus()
		return ModalActionNone
	case keyMatches(msg, tea.KeyCtrlUp, "ctrl+up", "alt+up", "opt+up"):
		m.handleCollectionRowMove(-1)
		return ModalActionNone
	case keyMatches(msg, tea.KeyCtrlDown, "ctrl+down", "alt+down", "opt+down"):
		m.handleCollectionRowMove(1)
		return ModalActionNone
	case keyMatches(msg, tea.KeyUp, "up"):
		m.handleVerticalMove(-1)
		return ModalActionNone
	case keyMatches(msg, tea.KeyDown, "down"):
		m.handleVerticalMove(1)
		return ModalActionNone
	case keyMatches(msg, tea.KeyHome, "home"):
		m.moveFocusTo(0)
		m.syncInputFocus()
		return ModalActionNone
	case keyMatches(msg, tea.KeyEnd, "end"):
		m.moveFocusTo(len(m.specs) - 1)
		m.syncInputFocus()
		return ModalActionNone
	case keyMatches(msg, tea.KeyCtrlN, "ctrl+n"):
		m.handleCollectionAdd()
		return ModalActionNone
	case keyMatches(msg, tea.KeyCtrlD, "ctrl+d"):
		m.handleCollectionDelete()
		return ModalActionNone
	case keyMatches(msg, tea.KeyCtrlLeft, "ctrl+left", "alt+left", "opt+left"):
		m.handleCollectionColMove(-1)
		return ModalActionNone
	case keyMatches(msg, tea.KeyCtrlRight, "ctrl+right", "alt+right", "opt+right"):
		m.handleCollectionColMove(1)
		return ModalActionNone
	}

	m.handleInputKey(msg)
	return ModalActionNone
}

func (m *ConfigModal) handleDiscardConfirmKey(msg tea.KeyMsg) ModalAction {
	switch {
	case keyMatches(msg, tea.KeyEnter, "enter"):
		return ModalActionClose
	case keyMatches(msg, tea.KeyEsc, "esc"):
		m.confirmDiscard = false
		return ModalActionNone
	case msg.Type == tea.KeyRunes:
		if len(msg.Runes) == 0 {
			return ModalActionNone
		}
		switch strings.ToLower(string(msg.Runes[0])) {
		case "y":
			return ModalActionClose
		case "n":
			m.confirmDiscard = false
			return ModalActionNone
		}
	}
	return ModalActionNone
}

func (m *ConfigModal) moveFocus(step int) {
	if len(m.specs) == 0 {
		return
	}

	if !m.hasFocusableSpec() {
		next := m.focus + step
		if next < 0 {
			next = len(m.specs) - 1
		}
		if next >= len(m.specs) {
			next = 0
		}
		m.focus = next
		return
	}

	total := len(m.specs)
	next := m.focus
	for i := 0; i < total; i++ {
		next += step
		if next < 0 {
			next = total - 1
		}
		if next >= total {
			next = 0
		}
		if !m.specs[next].ReadOnly {
			m.focus = next
			return
		}
	}
}

func (m *ConfigModal) moveFocusTo(index int) {
	if len(m.specs) == 0 {
		return
	}
	if index < 0 {
		index = 0
	}
	if index >= len(m.specs) {
		index = len(m.specs) - 1
	}
	if m.specs[index].ReadOnly {
		if index == 0 {
			m.focus = m.firstFocusableIndex()
			return
		}
		if index == len(m.specs)-1 {
			m.focus = m.lastFocusableIndex()
			return
		}
		for i := index + 1; i < len(m.specs); i++ {
			if !m.specs[i].ReadOnly {
				m.focus = i
				return
			}
		}
		for i := index - 1; i >= 0; i-- {
			if !m.specs[i].ReadOnly {
				m.focus = i
				return
			}
		}
	}
	m.focus = index
}

func (m *ConfigModal) hasFocusableSpec() bool {
	for i := range m.specs {
		if !m.specs[i].ReadOnly {
			return true
		}
	}
	return false
}

func (m *ConfigModal) firstFocusableIndex() int {
	for i := range m.specs {
		if !m.specs[i].ReadOnly {
			return i
		}
	}
	if len(m.specs) == 0 {
		return 0
	}
	return 0
}

func (m *ConfigModal) lastFocusableIndex() int {
	for i := len(m.specs) - 1; i >= 0; i-- {
		if !m.specs[i].ReadOnly {
			return i
		}
	}
	if len(m.specs) == 0 {
		return 0
	}
	return len(m.specs) - 1
}

func keyMatches(msg tea.KeyMsg, keyType tea.KeyType, aliases ...string) bool {
	if msg.Type == keyType {
		return true
	}
	key := strings.ToLower(strings.TrimSpace(msg.String()))
	for _, alias := range aliases {
		if key == alias {
			return true
		}
	}
	return false
}

func (m *ConfigModal) handleVerticalMove(step int) {
	spec := m.currentSpec()
	if spec == nil {
		return
	}
	if spec.Widget != settingsform.WidgetKeyValue && spec.Widget != settingsform.WidgetArray {
		m.moveFocus(step)
		m.syncInputFocus()
		return
	}

	if spec.Widget == settingsform.WidgetArray {
		rows := m.form.ObjectArrayValues[spec.Key]
		if len(rows) == 0 {
			m.moveFocus(step)
			m.syncInputFocus()
			return
		}
		cursor := m.arrayCursors[spec.Key]
		nextRow := cursor.row + step
		if nextRow < 0 || nextRow >= len(rows) {
			m.moveFocus(step)
			m.syncInputFocus()
			return
		}
		cursor.row = nextRow
		m.arrayCursors[spec.Key] = cursor
		m.syncInputFocus()
		return
	}

	rows := m.form.KeyValueValues[spec.Key]
	if len(rows) == 0 {
		m.moveFocus(step)
		m.syncInputFocus()
		return
	}

	cursor := m.kvCursors[spec.Key]
	nextRow := cursor.row + step
	if nextRow < 0 || nextRow >= len(rows) {
		m.moveFocus(step)
		m.syncInputFocus()
		return
	}
	cursor.row = nextRow
	m.kvCursors[spec.Key] = cursor
	m.syncInputFocus()
}

func (m *ConfigModal) handleColumnTab(step int) bool {
	spec := m.currentSpec()
	if spec == nil || spec.ReadOnly {
		return false
	}
	if spec.Widget == settingsform.WidgetArray {
		columns := visibleArrayColumns(spec)
		if len(columns) == 0 {
			return false
		}
		cursor := m.arrayCursors[spec.Key]
		switch step {
		case 1:
			if cursor.col < len(columns)-1 {
				cursor.col++
				m.arrayCursors[spec.Key] = cursor
				m.syncInputFocus()
				return true
			}
		case -1:
			if cursor.col > 0 {
				cursor.col--
				m.arrayCursors[spec.Key] = cursor
				m.syncInputFocus()
				return true
			}
		}
		return false
	}
	if spec.Widget != settingsform.WidgetKeyValue {
		return false
	}

	cursor := m.kvCursors[spec.Key]
	switch step {
	case 1:
		if cursor.col == kvCursorColKey {
			cursor.col = kvCursorColValue
			m.kvCursors[spec.Key] = cursor
			m.syncInputFocus()
			return true
		}
	case -1:
		if cursor.col == kvCursorColValue {
			cursor.col = kvCursorColKey
			m.kvCursors[spec.Key] = cursor
			m.syncInputFocus()
			return true
		}
	}
	return false
}

func (m *ConfigModal) handleCollectionAdd() {
	spec := m.currentSpec()
	if spec == nil || spec.ReadOnly {
		return
	}
	if spec.Widget == settingsform.WidgetArray {
		columns := visibleArrayColumns(spec)
		if len(columns) == 0 {
			return
		}
		m.normalizeArrayRows(spec)
		rows := m.form.ObjectArrayValues[spec.Key]
		inputRows := m.arrayInputs[spec.Key]

		insertAt := len(rows)
		if insertAt > 0 && m.isArrayRowEmpty(spec, rows[len(rows)-1]) {
			insertAt = len(rows) - 1
		}

		newRow := m.buildEmptyArrayRow(spec)
		rows = append(rows, nil)
		copy(rows[insertAt+1:], rows[insertAt:])
		rows[insertAt] = newRow
		m.form.ObjectArrayValues[spec.Key] = rows

		inputRows = append(inputRows, arrayInputRow{})
		copy(inputRows[insertAt+1:], inputRows[insertAt:])
		inputRows[insertAt] = m.buildArrayInputRow(spec, newRow)
		m.arrayInputs[spec.Key] = inputRows

		cursor := m.arrayCursors[spec.Key]
		cursor.row = insertAt
		cursor.col = 0
		m.arrayCursors[spec.Key] = cursor
		m.appendTrailingBlankIfNeeded(spec)
		m.syncInputFocus()
		return
	}
	if spec.Widget != settingsform.WidgetKeyValue {
		return
	}

	rows := m.form.KeyValueValues[spec.Key]
	rows = append(rows, settingsform.HeaderKV{Key: "", Value: ""})
	m.form.KeyValueValues[spec.Key] = rows

	inputRows := m.keyValueInputs[spec.Key]
	inputRows = append(inputRows, kvInputRow{
		keyInput:   newModalTextInput("", ""),
		valueInput: newModalTextInput("", ""),
	})
	m.keyValueInputs[spec.Key] = inputRows

	cursor := m.kvCursors[spec.Key]
	cursor.row = len(rows) - 1
	cursor.col = kvCursorColKey
	m.kvCursors[spec.Key] = cursor
	m.syncInputFocus()
}

func (m *ConfigModal) handleCollectionDelete() {
	spec := m.currentSpec()
	if spec == nil || spec.ReadOnly {
		return
	}
	if spec.Widget == settingsform.WidgetArray {
		m.normalizeArrayRows(spec)
		rows := m.form.ObjectArrayValues[spec.Key]
		if len(rows) == 0 {
			return
		}
		inputRows := m.arrayInputs[spec.Key]
		cursor := m.arrayCursors[spec.Key]
		if cursor.row < 0 || cursor.row >= len(rows) {
			cursor.row = 0
		}
		rows = append(rows[:cursor.row], rows[cursor.row+1:]...)
		inputRows = append(inputRows[:cursor.row], inputRows[cursor.row+1:]...)
		m.form.ObjectArrayValues[spec.Key] = rows
		m.arrayInputs[spec.Key] = inputRows

		if cursor.row >= len(rows) {
			cursor.row = len(rows) - 1
		}
		m.arrayCursors[spec.Key] = cursor
		m.normalizeArrayRows(spec)
		m.syncInputFocus()
		return
	}
	if spec.Widget != settingsform.WidgetKeyValue {
		return
	}

	rows := m.form.KeyValueValues[spec.Key]
	if len(rows) == 0 {
		return
	}
	inputRows := m.keyValueInputs[spec.Key]

	cursor := m.kvCursors[spec.Key]
	if cursor.row < 0 || cursor.row >= len(rows) {
		cursor.row = 0
	}

	rows = append(rows[:cursor.row], rows[cursor.row+1:]...)
	inputRows = append(inputRows[:cursor.row], inputRows[cursor.row+1:]...)
	m.form.KeyValueValues[spec.Key] = rows
	m.keyValueInputs[spec.Key] = inputRows

	if len(rows) == 0 {
		cursor.row = 0
	} else if cursor.row >= len(rows) {
		cursor.row = len(rows) - 1
	}
	m.kvCursors[spec.Key] = cursor
	m.syncInputFocus()
}

func (m *ConfigModal) handleCollectionColMove(step int) {
	spec := m.currentSpec()
	if spec == nil {
		return
	}
	if spec.Widget == settingsform.WidgetArray {
		columns := visibleArrayColumns(spec)
		if len(columns) == 0 {
			return
		}
		cursor := m.arrayCursors[spec.Key]
		cursor.col += step
		if cursor.col < 0 {
			cursor.col = 0
		}
		if cursor.col >= len(columns) {
			cursor.col = len(columns) - 1
		}
		m.arrayCursors[spec.Key] = cursor
		m.syncInputFocus()
		return
	}
	if spec.Widget != settingsform.WidgetKeyValue {
		return
	}
	cursor := m.kvCursors[spec.Key]
	cursor.col += step
	if cursor.col < kvCursorColKey {
		cursor.col = kvCursorColKey
	}
	if cursor.col > kvCursorColValue {
		cursor.col = kvCursorColValue
	}
	m.kvCursors[spec.Key] = cursor
	m.syncInputFocus()
}

func (m *ConfigModal) handleCollectionRowMove(step int) {
	spec := m.currentSpec()
	if spec == nil || spec.ReadOnly || spec.Widget != settingsform.WidgetArray {
		return
	}

	m.normalizeArrayRows(spec)
	rows := m.form.ObjectArrayValues[spec.Key]
	if len(rows) <= 1 {
		return
	}
	inputRows := m.arrayInputs[spec.Key]
	cursor := m.arrayCursors[spec.Key]
	if cursor.row < 0 || cursor.row >= len(rows) {
		return
	}

	lastMovable := len(rows) - 1
	if m.isArrayRowEmpty(spec, rows[lastMovable]) {
		lastMovable--
	}
	if lastMovable < 0 || cursor.row > lastMovable {
		return
	}

	target := cursor.row + step
	if target < 0 || target > lastMovable {
		return
	}

	rows[cursor.row], rows[target] = rows[target], rows[cursor.row]
	if cursor.row < len(inputRows) && target < len(inputRows) {
		inputRows[cursor.row], inputRows[target] = inputRows[target], inputRows[cursor.row]
	}
	m.form.ObjectArrayValues[spec.Key] = rows
	m.arrayInputs[spec.Key] = inputRows
	cursor.row = target
	m.arrayCursors[spec.Key] = cursor
	m.syncInputFocus()
}

func (m *ConfigModal) handleInputKey(msg tea.KeyMsg) {
	spec := m.currentSpec()
	if spec == nil || spec.ReadOnly {
		return
	}

	if spec.Widget == settingsform.WidgetKeyValue {
		m.updateKeyValueInput(msg, spec)
		return
	}
	if spec.Widget == settingsform.WidgetArray {
		m.updateArrayInput(msg, spec)
		return
	}
	m.updateScalarInput(msg, spec)
}

func (m *ConfigModal) updateScalarInput(msg tea.KeyMsg, spec *settingsform.FieldSpec) {
	if spec == nil {
		return
	}
	if spec.Widget == settingsform.WidgetBool {
		if keyMatches(msg, tea.KeySpace, "space") || isSpaceRuneKey(msg) {
			m.toggleBoolField(spec)
		}
		return
	}
	if step, ok := enumCycleStep(msg); ok && len(spec.EnumValues) > 0 {
		nextValue := cycleEnumValue(m.form.ScalarValues[spec.Key], spec.EnumValues, step)
		m.form.ScalarValues[spec.Key] = nextValue
		if input, ok := m.scalarInputs[spec.Key]; ok && input != nil {
			input.SetValue(nextValue)
			syncTextInputWidth(input, input.Value(), input.Placeholder)
		}
		return
	}
	input, ok := m.scalarInputs[spec.Key]
	if !ok || input == nil {
		return
	}
	updatedInput, _ := input.Update(msg)
	*input = updatedInput
	syncTextInputWidth(input, input.Value(), input.Placeholder)
	m.form.ScalarValues[spec.Key] = input.Value()
}

func (m *ConfigModal) updateKeyValueInput(msg tea.KeyMsg, spec *settingsform.FieldSpec) {
	if spec == nil {
		return
	}
	rows := m.form.KeyValueValues[spec.Key]
	if len(rows) == 0 {
		return
	}

	cursor := m.kvCursors[spec.Key]
	if cursor.row < 0 || cursor.row >= len(rows) {
		return
	}
	inputRows := m.keyValueInputs[spec.Key]
	if len(inputRows) != len(rows) {
		return
	}
	rowInputs := inputRows[cursor.row]
	if rowInputs.keyInput == nil || rowInputs.valueInput == nil {
		return
	}

	switch cursor.col {
	case kvCursorColKey:
		updatedInput, _ := rowInputs.keyInput.Update(msg)
		*rowInputs.keyInput = updatedInput
		syncTextInputWidth(rowInputs.keyInput, rowInputs.keyInput.Value(), rowInputs.keyInput.Placeholder)
		rows[cursor.row].Key = rowInputs.keyInput.Value()
	case kvCursorColValue:
		updatedInput, _ := rowInputs.valueInput.Update(msg)
		*rowInputs.valueInput = updatedInput
		syncTextInputWidth(rowInputs.valueInput, rowInputs.valueInput.Value(), rowInputs.valueInput.Placeholder)
		rows[cursor.row].Value = rowInputs.valueInput.Value()
	default:
		return
	}

	inputRows[cursor.row] = rowInputs
	m.keyValueInputs[spec.Key] = inputRows
	m.form.KeyValueValues[spec.Key] = rows
}

func (m *ConfigModal) updateArrayInput(msg tea.KeyMsg, spec *settingsform.FieldSpec) {
	if spec == nil {
		return
	}
	rows := m.form.ObjectArrayValues[spec.Key]
	if len(rows) == 0 {
		return
	}
	columns := visibleArrayColumns(spec)
	if len(columns) == 0 {
		return
	}
	cursor := m.arrayCursors[spec.Key]
	if cursor.row < 0 || cursor.row >= len(rows) {
		return
	}
	if cursor.col < 0 || cursor.col >= len(columns) {
		return
	}
	col := columns[cursor.col]

	inputRows := m.arrayInputs[spec.Key]
	if len(inputRows) != len(rows) {
		return
	}
	rowInputs := inputRows[cursor.row]
	if rowInputs.fields == nil {
		return
	}
	input, ok := rowInputs.fields[col.Key]
	if !ok || input == nil {
		return
	}
	if step, ok := enumCycleStep(msg); ok && len(col.EnumValues) > 0 {
		nextValue := cycleEnumValue(rows[cursor.row][col.Key], col.EnumValues, step)
		input.SetValue(nextValue)
		syncTextInputWidth(input, input.Value(), input.Placeholder)
		rows[cursor.row][col.Key] = nextValue
		rowInputs.fields[col.Key] = input
		inputRows[cursor.row] = rowInputs
		m.arrayInputs[spec.Key] = inputRows
		m.form.ObjectArrayValues[spec.Key] = rows
		m.appendTrailingBlankIfNeeded(spec)
		m.normalizeArrayRows(spec)
		return
	}

	updatedInput, _ := input.Update(msg)
	*input = updatedInput
	syncTextInputWidth(input, input.Value(), input.Placeholder)
	rows[cursor.row][col.Key] = input.Value()
	rowInputs.fields[col.Key] = input
	inputRows[cursor.row] = rowInputs
	m.arrayInputs[spec.Key] = inputRows
	m.form.ObjectArrayValues[spec.Key] = rows
	m.appendTrailingBlankIfNeeded(spec)
	m.normalizeArrayRows(spec)
}

func (m *ConfigModal) currentSpec() *settingsform.FieldSpec {
	if len(m.specs) == 0 || m.focus < 0 || m.focus >= len(m.specs) {
		return nil
	}
	return &m.specs[m.focus]
}

func (m *ConfigModal) View() string {
	if !m.open {
		return ""
	}

	var sb strings.Builder
	sb.WriteString(configModalTitleStyle.Render("Settings"))
	sb.WriteString("\n")

	if m.confirmDiscard {
		sb.WriteString("\nDiscard unsaved changes?\n")
		sb.WriteString(DimStyle.Render("Press Enter/Y to discard, Esc/N to continue editing."))
		return configModalStyle.Render(sb.String())
	}

	sb.WriteString(
		DimStyle.Render(
			"Ctrl+S=save  Esc=close  Tab/↑↓=navigate  Space=toggle/cycle  Alt+Space=enum back  Ctrl+N/Ctrl+D=row  Ctrl/Alt/Opt+←/→=column  Ctrl/Alt/Opt+↑/↓=move row",
		),
	)
	sb.WriteString("\n\n")
	for i := range m.specs {
		sb.WriteString(m.renderFieldBlock(i))
		if i == m.focus {
			description := strings.TrimSpace(m.specs[i].Description)
			if description != "" {
				sb.WriteString("\n")
				sb.WriteString(DimStyle.Render("  " + description))
			}
		}
		if i < len(m.specs)-1 {
			sb.WriteString("\n")
		}
	}
	if m.errorMsg != "" {
		sb.WriteString("\n")
		sb.WriteString(ErrorStyle.Render(m.errorMsg))
	}

	return configModalStyle.Render(sb.String())
}

func (m *ConfigModal) renderFieldBlock(index int) string {
	spec := &m.specs[index]
	focused := index == m.focus

	label := spec.Label
	if spec.ReadOnly {
		label += " (read-only)"
		label = configModalReadOnlyStyle.Render(label)
	}

	if spec.Widget == settingsform.WidgetKeyValue {
		return m.renderKeyValueFieldBlock(spec, label, focused)
	}
	if spec.Widget == settingsform.WidgetArray {
		return m.renderObjectArrayFieldBlock(spec, label, focused)
	}
	value := m.renderScalarFieldValue(spec, focused)
	return fmt.Sprintf("%-*s : %s", configModalLabelW, label, value)
}

func (m *ConfigModal) renderScalarFieldValue(spec *settingsform.FieldSpec, focused bool) string {
	if spec == nil {
		return ""
	}
	if spec.Widget == settingsform.WidgetBool {
		return renderBoolValue(spec.Key, parseBoolScalarValue(m.form.ScalarValues[spec.Key]), focused, spec.ReadOnly)
	}
	if spec.ReadOnly {
		return configModalInputReadOnlyStyle.Render(wrapInputValue(m.form.ScalarValues[spec.Key]))
	}

	input, ok := m.scalarInputs[spec.Key]
	if !ok || input == nil {
		return configModalInputStyle.Render(wrapInputValue(m.form.ScalarValues[spec.Key]))
	}
	if focused {
		return configModalInputFocusStyle.Render("[" + input.View() + "]")
	}
	return configModalInputStyle.Render(wrapInputValue(input.Value()))
}

func (m *ConfigModal) renderKeyValueFieldBlock(spec *settingsform.FieldSpec, label string, focused bool) string {
	if spec == nil {
		return ""
	}
	rows := m.form.KeyValueValues[spec.Key]
	cursor := m.kvCursors[spec.Key]

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%-*s :", configModalLabelW, label))
	if len(rows) == 0 {
		sb.WriteString("\n")
		sb.WriteString(kvFieldIndent)
		sb.WriteString(DimStyle.Render("No rows. Press Ctrl+N to add."))
		return sb.String()
	}

	inputRows := m.keyValueInputs[spec.Key]
	for i := range rows {
		row := rows[i]
		var keyInput *textinput.Model
		var valueInput *textinput.Model
		if i < len(inputRows) {
			keyInput = inputRows[i].keyInput
			valueInput = inputRows[i].valueInput
		}
		rowFocused := focused && i == cursor.row
		keyActive := rowFocused && cursor.col == kvCursorColKey
		valueActive := rowFocused && cursor.col == kvCursorColValue

		keyBox := renderTextInputBox(keyInput, row.Key, keyActive, spec.ReadOnly)
		valueBox := renderTextInputBox(valueInput, row.Value, valueActive, spec.ReadOnly)

		sb.WriteString("\n")
		sb.WriteString(kvFieldIndent)
		sb.WriteString(fmt.Sprintf("%s : %s", keyBox, valueBox))
	}
	return sb.String()
}

func (m *ConfigModal) renderObjectArrayFieldBlock(spec *settingsform.FieldSpec, label string, focused bool) string {
	if spec == nil {
		return ""
	}
	rows := m.form.ObjectArrayValues[spec.Key]
	columns := visibleArrayColumns(spec)
	cursor := m.arrayCursors[spec.Key]

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%-*s :", configModalLabelW, label))
	if len(columns) == 0 {
		sb.WriteString("\n")
		sb.WriteString(kvFieldIndent)
		sb.WriteString(DimStyle.Render("No editable columns"))
		return sb.String()
	}
	headerParts := make([]string, 0, len(columns))
	for _, col := range columns {
		headerParts = append(headerParts, col.Label)
	}
	sb.WriteString("\n")
	sb.WriteString(kvFieldIndent)
	sb.WriteString(DimStyle.Render(strings.Join(headerParts, " | ")))

	if len(rows) == 0 {
		sb.WriteString("\n")
		sb.WriteString(kvFieldIndent)
		sb.WriteString(DimStyle.Render("No rows. Press Ctrl+N to add."))
		return sb.String()
	}

	inputRows := m.arrayInputs[spec.Key]
	for rowIndex, row := range rows {
		sb.WriteString("\n")
		sb.WriteString(kvFieldIndent)
		sb.WriteString(fmt.Sprintf("%d. ", rowIndex+1))

		cells := make([]string, 0, len(columns))
		for colIndex, col := range columns {
			var input *textinput.Model
			if rowIndex < len(inputRows) && inputRows[rowIndex].fields != nil {
				input = inputRows[rowIndex].fields[col.Key]
			}
			active := focused && rowIndex == cursor.row && colIndex == cursor.col
			value := row[col.Key]
			cells = append(cells, renderTextInputBox(input, value, active, spec.ReadOnly))
		}
		sb.WriteString(strings.Join(cells, " | "))
	}
	return sb.String()
}

func renderTextInputBox(input *textinput.Model, value string, focused, readOnly bool) string {
	if readOnly {
		return configModalInputReadOnlyStyle.Render(wrapInputValue(value))
	}
	if focused && input != nil {
		syncTextInputWidth(input, input.Value(), input.Placeholder)
		return configModalInputFocusStyle.Render("[" + input.View() + "]")
	}
	return configModalInputStyle.Render(wrapInputValue(value))
}

func renderBoolValue(fieldKey string, value bool, focused, readOnly bool) string {
	label := defaultBoolLabelOff
	if fieldKey == agentDetectionModeFieldKey {
		if value {
			label = agentDetectionModeLabelPremium
		} else {
			label = agentDetectionModeLabelSession
		}
	} else if value {
		label = defaultBoolLabelOn
	}
	box := "[" + label + "]"
	if readOnly {
		return configModalInputReadOnlyStyle.Render(box)
	}
	if focused {
		return configModalInputFocusStyle.Render(box)
	}
	return configModalInputStyle.Render(box)
}

func wrapInputValue(value string) string {
	content := value
	if content == "" {
		content = inputEmptyGlyph
	}
	return "[" + content + "]"
}

func isSpaceRuneKey(msg tea.KeyMsg) bool {
	return msg.Type == tea.KeyRunes && len(msg.Runes) == 1 && msg.Runes[0] == ' '
}

func isSpaceKey(msg tea.KeyMsg) bool {
	return msg.Type == tea.KeySpace || isSpaceRuneKey(msg)
}

func enumCycleStep(msg tea.KeyMsg) (int, bool) {
	key := strings.ToLower(msg.String())
	switch key {
	case "alt+ ", "opt+ ", "alt+space", "opt+space":
		return -1, true
	}
	if !isSpaceKey(msg) {
		return 0, false
	}
	if msg.Alt {
		return -1, true
	}
	return 1, true
}

func cycleEnumValue(current string, enumValues []string, step int) string {
	if len(enumValues) == 0 || step == 0 {
		return current
	}
	trimmed := strings.TrimSpace(current)
	if trimmed == "" {
		if step < 0 {
			return enumValues[len(enumValues)-1]
		}
		return enumValues[0]
	}

	index := -1
	for i := range enumValues {
		if enumValues[i] == trimmed {
			index = i
			break
		}
	}
	if index < 0 {
		if step < 0 {
			return enumValues[len(enumValues)-1]
		}
		return enumValues[0]
	}
	if step < 0 {
		index = (index - 1 + len(enumValues)) % len(enumValues)
	} else {
		index = (index + 1) % len(enumValues)
	}
	return enumValues[index]
}

func parseBoolScalarValue(raw string) bool {
	return strings.EqualFold(strings.TrimSpace(raw), "true")
}

func (m *ConfigModal) toggleBoolField(spec *settingsform.FieldSpec) {
	if spec == nil || spec.Widget != settingsform.WidgetBool || spec.ReadOnly {
		return
	}
	next := !parseBoolScalarValue(m.form.ScalarValues[spec.Key])
	newValue := "false"
	if next {
		newValue = "true"
	}
	m.form.ScalarValues[spec.Key] = newValue
	if input, ok := m.scalarInputs[spec.Key]; ok && input != nil {
		input.SetValue(newValue)
		syncTextInputWidth(input, input.Value(), input.Placeholder)
	}
}

func (m *ConfigModal) syncInputFocus() {
	m.blurAllInputs()

	spec := m.currentSpec()
	if spec == nil || spec.ReadOnly {
		return
	}

	if spec.Widget != settingsform.WidgetKeyValue && spec.Widget != settingsform.WidgetArray {
		input, ok := m.scalarInputs[spec.Key]
		if !ok || input == nil {
			return
		}
		_ = input.Focus()
		return
	}
	if spec.Widget == settingsform.WidgetArray {
		rows := m.arrayInputs[spec.Key]
		if len(rows) == 0 {
			return
		}
		columns := visibleArrayColumns(spec)
		if len(columns) == 0 {
			return
		}
		cursor := m.arrayCursors[spec.Key]
		if cursor.row < 0 {
			cursor.row = 0
		}
		if cursor.row >= len(rows) {
			cursor.row = len(rows) - 1
		}
		if cursor.col < 0 {
			cursor.col = 0
		}
		if cursor.col >= len(columns) {
			cursor.col = len(columns) - 1
		}
		m.arrayCursors[spec.Key] = cursor

		row := rows[cursor.row]
		if row.fields == nil {
			return
		}
		col := columns[cursor.col]
		input := row.fields[col.Key]
		if input == nil {
			return
		}
		_ = input.Focus()
		return
	}

	rows := m.keyValueInputs[spec.Key]
	if len(rows) == 0 {
		return
	}

	cursor := m.kvCursors[spec.Key]
	if cursor.row < 0 {
		cursor.row = 0
	}
	if cursor.row >= len(rows) {
		cursor.row = len(rows) - 1
	}
	m.kvCursors[spec.Key] = cursor

	row := rows[cursor.row]
	if row.keyInput == nil || row.valueInput == nil {
		return
	}
	switch cursor.col {
	case kvCursorColKey:
		_ = row.keyInput.Focus()
	case kvCursorColValue:
		_ = row.valueInput.Focus()
	default:
		_ = row.keyInput.Focus()
	}
}

func (m *ConfigModal) blurAllInputs() {
	for key := range m.scalarInputs {
		input := m.scalarInputs[key]
		if input == nil {
			continue
		}
		input.Blur()
	}
	for key := range m.keyValueInputs {
		rows := m.keyValueInputs[key]
		for i := range rows {
			if rows[i].keyInput != nil {
				rows[i].keyInput.Blur()
			}
			if rows[i].valueInput != nil {
				rows[i].valueInput.Blur()
			}
		}
	}
	for key := range m.arrayInputs {
		rows := m.arrayInputs[key]
		for i := range rows {
			for _, input := range rows[i].fields {
				if input != nil {
					input.Blur()
				}
			}
		}
	}
}

func newModalTextInput(value, placeholder string) *textinput.Model {
	input := textinput.New()
	input.Prompt = ""
	input.Placeholder = placeholder
	input.SetValue(value)
	input.CharLimit = 0
	syncTextInputWidth(&input, value, placeholder)
	input.TextStyle = lipgloss.NewStyle()
	input.PlaceholderStyle = DimStyle
	input.Cursor.SetMode(bubblecursor.CursorStatic)
	input.Blur()
	return &input
}

func syncTextInputWidth(input *textinput.Model, value, placeholder string) {
	if input == nil {
		return
	}
	input.Width = computeAdaptiveInputWidth(value, placeholder)
}

func computeAdaptiveInputWidth(value, placeholder string) int {
	content := value
	if strings.TrimSpace(content) == "" {
		content = placeholder
	}
	width := lipgloss.Width(content) + 1
	if width < adaptiveInputWidthMin {
		return adaptiveInputWidthMin
	}
	if width > adaptiveInputWidthMax {
		return adaptiveInputWidthMax
	}
	return width
}

func (m *ConfigModal) buildEmptyArrayRow(spec *settingsform.FieldSpec) map[string]string {
	columns := visibleArrayColumns(spec)
	row := make(map[string]string, len(columns))
	for _, col := range columns {
		row[col.Key] = ""
	}
	return row
}

func (m *ConfigModal) buildArrayInputRow(spec *settingsform.FieldSpec, row map[string]string) arrayInputRow {
	columns := visibleArrayColumns(spec)
	inputs := make(map[string]*textinput.Model, len(columns))
	for _, col := range columns {
		value := ""
		if row != nil {
			value = row[col.Key]
		}
		inputs[col.Key] = newModalTextInput(value, col.Placeholder)
	}
	return arrayInputRow{fields: inputs}
}

func (m *ConfigModal) isArrayRowEmpty(spec *settingsform.FieldSpec, row map[string]string) bool {
	if spec == nil {
		return true
	}
	columns := visibleArrayColumns(spec)
	if len(columns) == 0 {
		return true
	}
	for _, col := range columns {
		if strings.TrimSpace(row[col.Key]) != "" {
			return false
		}
	}
	return true
}

func (m *ConfigModal) appendTrailingBlankIfNeeded(spec *settingsform.FieldSpec) {
	if spec == nil || spec.Widget != settingsform.WidgetArray {
		return
	}
	rows := m.form.ObjectArrayValues[spec.Key]
	if len(rows) == 0 || !m.isArrayRowEmpty(spec, rows[len(rows)-1]) {
		rows = append(rows, m.buildEmptyArrayRow(spec))
		m.form.ObjectArrayValues[spec.Key] = rows
		inputRows := m.arrayInputs[spec.Key]
		inputRows = append(inputRows, m.buildArrayInputRow(spec, rows[len(rows)-1]))
		m.arrayInputs[spec.Key] = inputRows
	}
}

func (m *ConfigModal) normalizeArrayRows(spec *settingsform.FieldSpec) {
	if spec == nil || spec.Widget != settingsform.WidgetArray {
		return
	}
	columns := visibleArrayColumns(spec)
	rows := m.form.ObjectArrayValues[spec.Key]
	if len(rows) == 0 {
		rows = []map[string]string{m.buildEmptyArrayRow(spec)}
	}

	for i := range rows {
		if rows[i] == nil {
			rows[i] = m.buildEmptyArrayRow(spec)
			continue
		}
		normalized := make(map[string]string, len(columns))
		for _, col := range columns {
			normalized[col.Key] = rows[i][col.Key]
		}
		rows[i] = normalized
	}

	m.form.ObjectArrayValues[spec.Key] = rows
	m.appendTrailingBlankIfNeeded(spec)
	rows = m.form.ObjectArrayValues[spec.Key]
	cursor := m.arrayCursors[spec.Key]
	for len(rows) > 1 {
		last := len(rows) - 1
		penultimate := last - 1
		if !m.isArrayRowEmpty(spec, rows[last]) || !m.isArrayRowEmpty(spec, rows[penultimate]) {
			break
		}
		// Keep the penultimate row while the user is actively editing it.
		if cursor.row == penultimate {
			break
		}
		rows = append(rows[:penultimate], rows[last])
		if cursor.row > penultimate {
			cursor.row--
		}
	}
	m.form.ObjectArrayValues[spec.Key] = rows
	rows = m.form.ObjectArrayValues[spec.Key]

	existingInputs := m.arrayInputs[spec.Key]
	inputRows := make([]arrayInputRow, len(rows))
	for rowIndex := range rows {
		reconstructed := m.buildArrayInputRow(spec, rows[rowIndex])
		if rowIndex < len(existingInputs) && existingInputs[rowIndex].fields != nil {
			for colKey, input := range existingInputs[rowIndex].fields {
				if input == nil {
					continue
				}
				if _, exists := reconstructed.fields[colKey]; !exists {
					continue
				}
				input.SetValue(rows[rowIndex][colKey])
				syncTextInputWidth(input, input.Value(), input.Placeholder)
				reconstructed.fields[colKey] = input
			}
		}
		inputRows[rowIndex] = reconstructed
	}
	m.arrayInputs[spec.Key] = inputRows

	if cursor.row < 0 {
		cursor.row = 0
	}
	if cursor.row >= len(rows) {
		cursor.row = len(rows) - 1
	}
	if cursor.col < 0 {
		cursor.col = 0
	}
	if len(columns) == 0 {
		cursor.col = 0
	} else if cursor.col >= len(columns) {
		cursor.col = len(columns) - 1
	}
	m.arrayCursors[spec.Key] = cursor
}

func sanitizeFormForSave(specs []settingsform.FieldSpec, form settingsform.SettingsForm) settingsform.SettingsForm {
	sanitized := form.Clone()
	for i := range specs {
		spec := specs[i]
		if spec.Widget != settingsform.WidgetKeyValue {
			if spec.Widget == settingsform.WidgetArray {
				rows := sanitized.ObjectArrayValues[spec.Key]
				columns := visibleArrayColumns(&spec)
				filteredRows := make([]map[string]string, 0, len(rows))
				for _, row := range rows {
					if row == nil {
						continue
					}
					hasValue := false
					filtered := make(map[string]string, len(row))
					for _, col := range columns {
						value := strings.TrimSpace(row[col.Key])
						filtered[col.Key] = value
						if value != "" {
							hasValue = true
						}
					}
					if hasValue {
						filteredRows = append(filteredRows, filtered)
					}
				}
				sanitized.ObjectArrayValues[spec.Key] = filteredRows
			}
			continue
		}
		rows := sanitized.KeyValueValues[spec.Key]
		filteredRows := make([]settingsform.HeaderKV, 0, len(rows))
		for j := range rows {
			key := strings.TrimSpace(rows[j].Key)
			value := strings.TrimSpace(rows[j].Value)
			if key == "" || value == "" {
				continue
			}
			filteredRows = append(filteredRows, settingsform.HeaderKV{
				Key:   key,
				Value: value,
			})
		}
		sanitized.KeyValueValues[spec.Key] = filteredRows
	}
	return sanitized
}

func clearEmptyKeyValueMaps(settings *config.Settings, specs []settingsform.FieldSpec, form settingsform.SettingsForm) {
	if settings == nil {
		return
	}
	settingsValue := reflect.ValueOf(settings).Elem()
	for i := range specs {
		spec := specs[i]
		if spec.Widget != settingsform.WidgetKeyValue {
			continue
		}
		if len(form.KeyValueValues[spec.Key]) > 0 {
			continue
		}
		field := settingsValue.FieldByName(spec.FieldName)
		if !field.IsValid() || !field.CanSet() || field.Kind() != reflect.Map {
			continue
		}
		field.Set(reflect.Zero(field.Type()))
	}
}

func visibleArrayColumns(spec *settingsform.FieldSpec) []settingsform.FieldSpec {
	if spec == nil {
		return nil
	}
	columns := make([]settingsform.FieldSpec, 0, len(spec.ElementSpec))
	for _, col := range spec.ElementSpec {
		if !col.Visible {
			continue
		}
		columns = append(columns, col)
	}
	return columns
}

func (m *ConfigModal) Overlay(base string, width, height int) string {
	if !m.open {
		return base
	}
	modalView := m.View()
	if width <= 0 || height <= 0 {
		return modalView
	}
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, modalView)
}
