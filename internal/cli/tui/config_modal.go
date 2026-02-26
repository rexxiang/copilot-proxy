package tui

import (
	"fmt"
	"reflect"
	"strings"

	bubblecursor "github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"copilot-proxy/internal/config"
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

	inputEmptyGlyph = " "
	inputWidth      = 36
)

type kvCursor struct {
	row int
	col int // 0=key, 1=value
}

type kvInputRow struct {
	keyInput   *textinput.Model
	valueInput *textinput.Model
}

type ConfigModal struct {
	open           bool
	specs          []config.FieldSpec
	form           config.SettingsForm
	baseForm       config.SettingsForm
	kvCursors      map[string]kvCursor
	scalarInputs   map[string]*textinput.Model
	keyValueInputs map[string][]kvInputRow
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
		form: config.SettingsForm{
			ScalarValues:   make(map[string]string),
			KeyValueValues: make(map[string][]config.HeaderKV),
		},
		baseForm: config.SettingsForm{
			ScalarValues:   make(map[string]string),
			KeyValueValues: make(map[string][]config.HeaderKV),
		},
		kvCursors:      make(map[string]kvCursor),
		scalarInputs:   make(map[string]*textinput.Model),
		keyValueInputs: make(map[string][]kvInputRow),
		focus:          0,
		confirmDiscard: false,
		errorMsg:       "",
	}
}

func (m *ConfigModal) Open(settings *config.Settings) error {
	specs, err := config.SettingsFieldSpecs()
	if err != nil {
		return fmt.Errorf("load settings field specs: %w", err)
	}
	form, err := config.EncodeSettingsToForm(settings, specs)
	if err != nil {
		return fmt.Errorf("encode settings form: %w", err)
	}

	visibleSpecs := make([]config.FieldSpec, 0, len(specs))
	kvCursors := make(map[string]kvCursor)
	scalarInputs := make(map[string]*textinput.Model)
	keyValueInputs := make(map[string][]kvInputRow)
	for i := range specs {
		spec := specs[i]
		if !spec.Visible {
			continue
		}
		visibleSpecs = append(visibleSpecs, spec)

		if spec.Widget == config.WidgetKeyValue {
			rows := form.KeyValueValues[spec.Key]
			if len(rows) == 0 {
				rows = []config.HeaderKV{{Key: "", Value: ""}}
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

		scalarInputs[spec.Key] = newModalTextInput(form.ScalarValues[spec.Key], spec.Placeholder)
	}

	m.open = true
	m.specs = visibleSpecs
	m.form = form.Clone()
	m.baseForm = form.Clone()
	m.kvCursors = kvCursors
	m.scalarInputs = scalarInputs
	m.keyValueInputs = keyValueInputs
	m.focus = 0
	m.confirmDiscard = false
	m.errorMsg = ""
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
	settings, err := config.DecodeFormToSettings(base, m.specs, sanitizedForm)
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

	switch msg.String() {
	case "esc":
		if m.IsDirty() {
			m.confirmDiscard = true
			return ModalActionNone
		}
		return ModalActionClose
	case "enter":
		return ModalActionSave
	case "tab":
		if m.handleKeyValueTab(1) {
			return ModalActionNone
		}
		m.moveFocus(1)
		m.syncInputFocus()
		return ModalActionNone
	case "shift+tab":
		if m.handleKeyValueTab(-1) {
			return ModalActionNone
		}
		m.moveFocus(-1)
		m.syncInputFocus()
		return ModalActionNone
	case "up":
		m.handleVerticalMove(-1)
		return ModalActionNone
	case "down":
		m.handleVerticalMove(1)
		return ModalActionNone
	case "home":
		m.moveFocusTo(0)
		m.syncInputFocus()
		return ModalActionNone
	case "end":
		m.moveFocusTo(len(m.specs) - 1)
		m.syncInputFocus()
		return ModalActionNone
	case "ctrl+n":
		m.handleKeyValueAdd()
		return ModalActionNone
	case "ctrl+d":
		m.handleKeyValueDelete()
		return ModalActionNone
	case "ctrl+left":
		m.handleKeyValueColMove(-1)
		return ModalActionNone
	case "ctrl+right":
		m.handleKeyValueColMove(1)
		return ModalActionNone
	}

	m.handleInputKey(msg)
	return ModalActionNone
}

func (m *ConfigModal) handleDiscardConfirmKey(msg tea.KeyMsg) ModalAction {
	switch msg.String() {
	case "enter", "y":
		return ModalActionClose
	case "esc", "n":
		m.confirmDiscard = false
		return ModalActionNone
	default:
		return ModalActionNone
	}
}

func (m *ConfigModal) moveFocus(step int) {
	if len(m.specs) == 0 {
		return
	}
	next := m.focus + step
	if next < 0 {
		next = len(m.specs) - 1
	}
	if next >= len(m.specs) {
		next = 0
	}
	m.focus = next
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
	m.focus = index
}

func (m *ConfigModal) handleVerticalMove(step int) {
	spec := m.currentSpec()
	if spec == nil {
		return
	}
	if spec.Widget != config.WidgetKeyValue {
		m.moveFocus(step)
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
	cursor.row += step
	if cursor.row < 0 {
		cursor.row = 0
	}
	if cursor.row >= len(rows) {
		cursor.row = len(rows) - 1
	}
	m.kvCursors[spec.Key] = cursor
	m.syncInputFocus()
}

func (m *ConfigModal) handleKeyValueTab(step int) bool {
	spec := m.currentSpec()
	if spec == nil || spec.Widget != config.WidgetKeyValue || spec.ReadOnly {
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

func (m *ConfigModal) handleKeyValueAdd() {
	spec := m.currentSpec()
	if spec == nil || spec.Widget != config.WidgetKeyValue || spec.ReadOnly {
		return
	}

	rows := m.form.KeyValueValues[spec.Key]
	rows = append(rows, config.HeaderKV{Key: "", Value: ""})
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

func (m *ConfigModal) handleKeyValueDelete() {
	spec := m.currentSpec()
	if spec == nil || spec.Widget != config.WidgetKeyValue || spec.ReadOnly {
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

func (m *ConfigModal) handleKeyValueColMove(step int) {
	spec := m.currentSpec()
	if spec == nil || spec.Widget != config.WidgetKeyValue {
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

func (m *ConfigModal) handleInputKey(msg tea.KeyMsg) {
	spec := m.currentSpec()
	if spec == nil || spec.ReadOnly {
		return
	}

	if spec.Widget == config.WidgetKeyValue {
		m.updateKeyValueInput(msg, spec)
		return
	}
	m.updateScalarInput(msg, spec)
}

func (m *ConfigModal) updateScalarInput(msg tea.KeyMsg, spec *config.FieldSpec) {
	if spec == nil {
		return
	}
	input, ok := m.scalarInputs[spec.Key]
	if !ok || input == nil {
		return
	}
	updatedInput, _ := input.Update(msg)
	*input = updatedInput
	m.form.ScalarValues[spec.Key] = input.Value()
}

func (m *ConfigModal) updateKeyValueInput(msg tea.KeyMsg, spec *config.FieldSpec) {
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
		rows[cursor.row].Key = rowInputs.keyInput.Value()
	case kvCursorColValue:
		updatedInput, _ := rowInputs.valueInput.Update(msg)
		*rowInputs.valueInput = updatedInput
		rows[cursor.row].Value = rowInputs.valueInput.Value()
	default:
		return
	}

	inputRows[cursor.row] = rowInputs
	m.keyValueInputs[spec.Key] = inputRows
	m.form.KeyValueValues[spec.Key] = rows
}

func (m *ConfigModal) currentSpec() *config.FieldSpec {
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

	sb.WriteString(DimStyle.Render("Enter=save  Esc=close  Tab/↑↓=navigate  Ctrl+N/Ctrl+D=row  Ctrl+←/→=column"))
	sb.WriteString("\n\n")
	for i := range m.specs {
		sb.WriteString(m.renderFieldBlock(i))
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

	if spec.Widget == config.WidgetKeyValue {
		return m.renderKeyValueFieldBlock(spec, label, focused)
	}
	value := m.renderScalarFieldValue(spec, focused)
	return fmt.Sprintf("%-*s : %s", configModalLabelW, label, value)
}

func (m *ConfigModal) renderScalarFieldValue(spec *config.FieldSpec, focused bool) string {
	if spec == nil {
		return ""
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

func (m *ConfigModal) renderKeyValueFieldBlock(spec *config.FieldSpec, label string, focused bool) string {
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

func renderTextInputBox(input *textinput.Model, value string, focused, readOnly bool) string {
	if readOnly {
		return configModalInputReadOnlyStyle.Render(wrapInputValue(value))
	}
	if focused && input != nil {
		return configModalInputFocusStyle.Render("[" + input.View() + "]")
	}
	return configModalInputStyle.Render(wrapInputValue(value))
}

func wrapInputValue(value string) string {
	content := value
	if content == "" {
		content = inputEmptyGlyph
	}
	return "[" + content + "]"
}

func (m *ConfigModal) syncInputFocus() {
	m.blurAllInputs()

	spec := m.currentSpec()
	if spec == nil || spec.ReadOnly {
		return
	}

	if spec.Widget != config.WidgetKeyValue {
		input, ok := m.scalarInputs[spec.Key]
		if !ok || input == nil {
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
}

func newModalTextInput(value, placeholder string) *textinput.Model {
	input := textinput.New()
	input.Prompt = ""
	input.Placeholder = placeholder
	input.SetValue(value)
	input.CharLimit = 0
	input.Width = inputWidth
	input.TextStyle = lipgloss.NewStyle()
	input.PlaceholderStyle = DimStyle
	input.Cursor.SetMode(bubblecursor.CursorStatic)
	input.Blur()
	return &input
}

func sanitizeFormForSave(specs []config.FieldSpec, form config.SettingsForm) config.SettingsForm {
	sanitized := form.Clone()
	for i := range specs {
		spec := specs[i]
		if spec.Widget != config.WidgetKeyValue {
			continue
		}
		rows := sanitized.KeyValueValues[spec.Key]
		filteredRows := make([]config.HeaderKV, 0, len(rows))
		for j := range rows {
			key := strings.TrimSpace(rows[j].Key)
			value := strings.TrimSpace(rows[j].Value)
			if key == "" || value == "" {
				continue
			}
			filteredRows = append(filteredRows, config.HeaderKV{
				Key:   key,
				Value: value,
			})
		}
		sanitized.KeyValueValues[spec.Key] = filteredRows
	}
	return sanitized
}

func clearEmptyKeyValueMaps(settings *config.Settings, specs []config.FieldSpec, form config.SettingsForm) {
	if settings == nil {
		return
	}
	settingsValue := reflect.ValueOf(settings).Elem()
	for i := range specs {
		spec := specs[i]
		if spec.Widget != config.WidgetKeyValue {
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
