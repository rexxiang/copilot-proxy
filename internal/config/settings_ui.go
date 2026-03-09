package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"
)

type FieldWidget string

const (
	WidgetText     FieldWidget = "text"
	WidgetURL      FieldWidget = "url"
	WidgetInt      FieldWidget = "int"
	WidgetBool     FieldWidget = "bool"
	WidgetDuration FieldWidget = "duration"
	WidgetKeyValue FieldWidget = "kv"
	WidgetArray    FieldWidget = "array"
)

const (
	uiTagLabel       = "label"
	uiTagWidget      = "widget"
	uiTagVisible     = "visible"
	uiTagReadonly    = "readonly"
	uiTagOrder       = "order"
	uiTagPlaceholder = "placeholder"
	uiTagMin         = "min"
	uiTagMax         = "max"
	uiTagDescription = "description"
	uiTagEnum        = "enum"
	uiTagKey         = "key"
	uiTagEmpty       = "empty"
)

const (
	tagPairParts = 2
)

var (
	errUnknownUIKey       = errors.New("unknown ui key")
	errInvalidUIBool      = errors.New("invalid ui bool")
	errInvalidUIOrder     = errors.New("invalid ui order")
	errInvalidUIWidget    = errors.New("invalid ui widget")
	errInvalidUIEmpty     = errors.New("invalid ui empty behavior")
	errInvalidWidgetType  = errors.New("invalid widget type")
	errFieldNotFound      = errors.New("settings field not found")
	errInvalidURL         = errors.New("invalid url")
	errInvalidInt         = errors.New("invalid integer")
	errInvalidBool        = errors.New("invalid bool")
	errInvalidEnum        = errors.New("invalid enum value")
	errInvalidMinMax      = errors.New("invalid min/max")
	errDuplicateHeaderKey = errors.New("duplicate header key")
	errEmptyHeaderKey     = errors.New("empty header key")
	errReadonlyModified   = errors.New("readonly field modified")
	errVisibleMapField    = errors.New("visible map field unsupported")
)

type FieldSpec struct {
	FieldName   string
	Key         string
	Label       string
	Widget      FieldWidget
	Visible     bool
	ReadOnly    bool
	Order       int
	Placeholder string
	Min         string
	Max         string
	Description string
	EnumValues  []string
	EmptyZero   bool
	ElementType reflect.Type
	ElementSpec []FieldSpec
}

type HeaderKV struct {
	Key   string
	Value string
}

type SettingsForm struct {
	ScalarValues      map[string]string
	KeyValueValues    map[string][]HeaderKV
	ObjectArrayValues map[string][]map[string]string
}

func (f SettingsForm) Clone() SettingsForm {
	clone := SettingsForm{
		ScalarValues:      make(map[string]string, len(f.ScalarValues)),
		KeyValueValues:    make(map[string][]HeaderKV, len(f.KeyValueValues)),
		ObjectArrayValues: make(map[string][]map[string]string, len(f.ObjectArrayValues)),
	}
	for key, value := range f.ScalarValues {
		clone.ScalarValues[key] = value
	}
	for key, rows := range f.KeyValueValues {
		clonedRows := make([]HeaderKV, len(rows))
		copy(clonedRows, rows)
		clone.KeyValueValues[key] = clonedRows
	}
	for key, rows := range f.ObjectArrayValues {
		clonedRows := make([]map[string]string, 0, len(rows))
		for _, row := range rows {
			cloned := make(map[string]string, len(row))
			for rowKey, rowValue := range row {
				cloned[rowKey] = rowValue
			}
			clonedRows = append(clonedRows, cloned)
		}
		clone.ObjectArrayValues[key] = clonedRows
	}
	return clone
}

func SettingsFieldSpecs() ([]FieldSpec, error) {
	var settings Settings
	return buildFieldSpecsForType(reflect.TypeOf(settings))
}

func buildFieldSpecsForType(t reflect.Type) ([]FieldSpec, error) {
	specs := make([]FieldSpec, 0, t.NumField())
	for i := range t.NumField() {
		field := t.Field(i)
		if !field.IsExported() {
			continue
		}
		jsonKey := fieldJSONKey(&field)
		if jsonKey == "" && !strings.Contains(field.Tag.Get("ui"), uiTagKey+"=") {
			continue
		}

		spec, err := parseFieldSpec(&field, jsonKey, i)
		if err != nil {
			return nil, err
		}
		specs = append(specs, spec)
	}

	sort.Slice(specs, func(i, j int) bool {
		if specs[i].Order == specs[j].Order {
			return specs[i].Key < specs[j].Key
		}
		return specs[i].Order < specs[j].Order
	})

	return specs, nil
}

func fieldJSONKey(field *reflect.StructField) string {
	if field == nil {
		return ""
	}
	tag := field.Tag.Get("json")
	if tag == "" || tag == "-" {
		return ""
	}
	parts := strings.Split(tag, ",")
	key := strings.TrimSpace(parts[0])
	if key == "" || key == "-" {
		return ""
	}
	return key
}

func parseFieldSpec(field *reflect.StructField, jsonKey string, order int) (FieldSpec, error) {
	if field == nil {
		return FieldSpec{}, errFieldNotFound
	}
	uiOpts, err := parseUIOptions(field.Tag.Get("ui"))
	if err != nil {
		return FieldSpec{}, fmt.Errorf("parse ui options for %s: %w", field.Name, err)
	}

	spec := FieldSpec{
		FieldName:   field.Name,
		Key:         jsonKey,
		Label:       field.Name,
		Widget:      WidgetText,
		Visible:     false,
		ReadOnly:    true,
		Order:       order,
		Placeholder: "",
		Min:         "",
		Max:         "",
		Description: "",
		EnumValues:  nil,
		EmptyZero:   false,
		ElementType: nil,
		ElementSpec: nil,
	}

	if label, ok := uiOpts[uiTagLabel]; ok && label != "" {
		spec.Label = label
	}
	if placeholder, ok := uiOpts[uiTagPlaceholder]; ok {
		spec.Placeholder = placeholder
	}
	if description, ok := uiOpts[uiTagDescription]; ok {
		spec.Description = description
	}
	if minRaw, ok := uiOpts[uiTagMin]; ok {
		spec.Min = minRaw
	}
	if maxRaw, ok := uiOpts[uiTagMax]; ok {
		spec.Max = maxRaw
	}
	if spec.Key == "" {
		if customKey, ok := uiOpts[uiTagKey]; ok && strings.TrimSpace(customKey) != "" {
			spec.Key = strings.TrimSpace(customKey)
		}
	}
	if spec.Key == "" {
		return FieldSpec{}, fmt.Errorf("%w: %s", errFieldNotFound, field.Name)
	}

	if rawVisible, ok := uiOpts[uiTagVisible]; ok {
		visible, parseErr := strconv.ParseBool(rawVisible)
		if parseErr != nil {
			return FieldSpec{}, fmt.Errorf("%w: %s=%q", errInvalidUIBool, uiTagVisible, rawVisible)
		}
		spec.Visible = visible
	}
	if rawReadOnly, ok := uiOpts[uiTagReadonly]; ok {
		readOnly, parseErr := strconv.ParseBool(rawReadOnly)
		if parseErr != nil {
			return FieldSpec{}, fmt.Errorf("%w: %s=%q", errInvalidUIBool, uiTagReadonly, rawReadOnly)
		}
		spec.ReadOnly = readOnly
	}
	if rawOrder, ok := uiOpts[uiTagOrder]; ok {
		parsedOrder, parseErr := strconv.Atoi(rawOrder)
		if parseErr != nil {
			return FieldSpec{}, fmt.Errorf("%w: %s=%q", errInvalidUIOrder, uiTagOrder, rawOrder)
		}
		spec.Order = parsedOrder
	}

	if rawWidget, ok := uiOpts[uiTagWidget]; ok && rawWidget != "" {
		spec.Widget = FieldWidget(rawWidget)
	} else {
		spec.Widget = inferWidget(field.Type)
	}
	if rawEnum, ok := uiOpts[uiTagEnum]; ok {
		enumValues, enumErr := parseEnumValues(rawEnum)
		if enumErr != nil {
			return FieldSpec{}, fmt.Errorf("field %s: %w", field.Name, enumErr)
		}
		spec.EnumValues = enumValues
	}
	if rawEmpty, ok := uiOpts[uiTagEmpty]; ok {
		switch strings.ToLower(strings.TrimSpace(rawEmpty)) {
		case "":
		case "zero":
			spec.EmptyZero = true
		default:
			return FieldSpec{}, fmt.Errorf("%w: %s=%q", errInvalidUIEmpty, uiTagEmpty, rawEmpty)
		}
	}
	if err := validateWidget(spec.Widget, field.Type); err != nil {
		return FieldSpec{}, fmt.Errorf("field %s: %w", field.Name, err)
	}
	if spec.Widget == WidgetKeyValue && spec.Visible {
		return FieldSpec{}, fmt.Errorf(
			"field %s: %w (map is storage-only; use a tagged []struct shadow field for TUI editing)",
			field.Name,
			errVisibleMapField,
		)
	}
	if spec.Widget == WidgetArray {
		elemType := field.Type.Elem()
		if elemType.Kind() == reflect.Pointer {
			elemType = elemType.Elem()
		}
		spec.ElementType = elemType
		elementSpecs, elemErr := buildFieldSpecsForType(elemType)
		if elemErr != nil {
			return FieldSpec{}, fmt.Errorf("field %s: parse element spec: %w", field.Name, elemErr)
		}
		spec.ElementSpec = elementSpecs
	}

	return spec, nil
}

func parseUIOptions(raw string) (map[string]string, error) {
	if strings.TrimSpace(raw) == "" {
		return map[string]string{}, nil
	}
	knownKeys := map[string]struct{}{
		uiTagLabel:       {},
		uiTagWidget:      {},
		uiTagVisible:     {},
		uiTagReadonly:    {},
		uiTagOrder:       {},
		uiTagPlaceholder: {},
		uiTagMin:         {},
		uiTagMax:         {},
		uiTagDescription: {},
		uiTagEnum:        {},
		uiTagKey:         {},
		uiTagEmpty:       {},
	}

	opts := make(map[string]string)
	parts := strings.Split(raw, ";")
	for i := range parts {
		part := strings.TrimSpace(parts[i])
		if part == "" {
			continue
		}
		pair := strings.SplitN(part, "=", tagPairParts)
		if len(pair) != tagPairParts {
			return nil, fmt.Errorf("%w: %s", errUnknownUIKey, part)
		}
		key := strings.TrimSpace(pair[0])
		value := strings.TrimSpace(pair[1])
		if _, ok := knownKeys[key]; !ok {
			return nil, fmt.Errorf("%w: %s", errUnknownUIKey, key)
		}
		opts[key] = value
	}
	return opts, nil
}

func inferWidget(t reflect.Type) FieldWidget {
	var durationType Duration
	switch {
	case t.Kind() == reflect.String:
		return WidgetText
	case t.Kind() == reflect.Int:
		return WidgetInt
	case t.Kind() == reflect.Bool:
		return WidgetBool
	case t == reflect.TypeOf(durationType):
		return WidgetDuration
	case t.Kind() == reflect.Map:
		return WidgetKeyValue
	case t.Kind() == reflect.Slice && t.Elem().Kind() == reflect.Struct:
		return WidgetArray
	default:
		return WidgetText
	}
}

func validateWidget(widget FieldWidget, t reflect.Type) error {
	var durationType Duration
	switch widget {
	case WidgetText, WidgetURL:
		if t.Kind() != reflect.String {
			return fmt.Errorf("%w: widget %s requires string", errInvalidWidgetType, widget)
		}
	case WidgetInt:
		if t.Kind() != reflect.Int {
			return fmt.Errorf("%w: widget %s requires int", errInvalidWidgetType, widget)
		}
	case WidgetBool:
		if t.Kind() != reflect.Bool {
			return fmt.Errorf("%w: widget %s requires bool", errInvalidWidgetType, widget)
		}
	case WidgetDuration:
		if t != reflect.TypeOf(durationType) {
			return fmt.Errorf("%w: widget %s requires Duration", errInvalidWidgetType, widget)
		}
	case WidgetKeyValue:
		if t.Kind() != reflect.Map || t.Key().Kind() != reflect.String {
			return fmt.Errorf("%w: widget %s requires map[string]T", errInvalidWidgetType, widget)
		}
	case WidgetArray:
		if t.Kind() != reflect.Slice {
			return fmt.Errorf("%w: widget %s requires []struct", errInvalidWidgetType, widget)
		}
		elemType := t.Elem()
		if elemType.Kind() != reflect.Struct && !(elemType.Kind() == reflect.Pointer && elemType.Elem().Kind() == reflect.Struct) {
			return fmt.Errorf("%w: widget %s requires []struct", errInvalidWidgetType, widget)
		}
	default:
		return fmt.Errorf("%w: %s", errInvalidUIWidget, widget)
	}
	return nil
}

func EncodeSettingsToForm(settings *Settings, specs []FieldSpec) (SettingsForm, error) {
	if settings == nil {
		defaults := DefaultSettings()
		settings = &defaults
	}
	form := SettingsForm{
		ScalarValues:      make(map[string]string),
		KeyValueValues:    make(map[string][]HeaderKV),
		ObjectArrayValues: make(map[string][]map[string]string),
	}

	value := reflect.ValueOf(*settings)
	for i := range specs {
		spec := specs[i]
		field := value.FieldByName(spec.FieldName)
		if !field.IsValid() {
			return SettingsForm{}, fmt.Errorf("%w: %s", errFieldNotFound, spec.FieldName)
		}
		switch spec.Widget {
		case WidgetText, WidgetURL:
			form.ScalarValues[spec.Key] = field.String()
		case WidgetInt:
			form.ScalarValues[spec.Key] = strconv.Itoa(int(field.Int()))
		case WidgetBool:
			form.ScalarValues[spec.Key] = strconv.FormatBool(field.Bool())
		case WidgetDuration:
			durationValue, ok := field.Interface().(Duration)
			if !ok {
				return SettingsForm{}, fmt.Errorf("%w: %s", errInvalidWidgetType, spec.Widget)
			}
			form.ScalarValues[spec.Key] = durationValue.String()
		case WidgetKeyValue:
			rows, rowsErr := encodeMapRows(field)
			if rowsErr != nil {
				return SettingsForm{}, fmt.Errorf("field %s: %w", spec.Key, rowsErr)
			}
			form.KeyValueValues[spec.Key] = rows
		case WidgetArray:
			rows, rowsErr := encodeObjectArrayRows(field, spec.ElementSpec)
			if rowsErr != nil {
				return SettingsForm{}, fmt.Errorf("field %s: %w", spec.Key, rowsErr)
			}
			form.ObjectArrayValues[spec.Key] = rows
		default:
			return SettingsForm{}, fmt.Errorf("%w: %s", errInvalidUIWidget, spec.Widget)
		}
	}

	return form, nil
}

func DecodeFormToSettings(base *Settings, specs []FieldSpec, form SettingsForm) (Settings, error) {
	if base == nil {
		defaults := DefaultSettings()
		base = &defaults
	}
	out := *base
	encodedBase, err := EncodeSettingsToForm(base, specs)
	if err != nil {
		return Settings{}, fmt.Errorf("encode base settings: %w", err)
	}

	value := reflect.ValueOf(&out).Elem()
	for i := range specs {
		spec := &specs[i]
		field := value.FieldByName(spec.FieldName)
		if !field.IsValid() {
			return Settings{}, fmt.Errorf("%w: %s", errFieldNotFound, spec.FieldName)
		}

		if spec.ReadOnly {
			if readonlyChanged(spec, encodedBase, form) {
				return Settings{}, fmt.Errorf("%w: %s", errReadonlyModified, spec.Key)
			}
			continue
		}
		if !spec.Visible {
			continue
		}

		switch spec.Widget {
		case WidgetText:
			value := form.ScalarValues[spec.Key]
			if err := validateEnum(value, spec.EnumValues); err != nil {
				return Settings{}, fmt.Errorf("field %s: %w", spec.Key, err)
			}
			field.SetString(value)
		case WidgetURL:
			rawURL := strings.TrimSpace(form.ScalarValues[spec.Key])
			if err := validateURL(rawURL); err != nil {
				return Settings{}, fmt.Errorf("field %s: %w", spec.Key, err)
			}
			if err := validateEnum(rawURL, spec.EnumValues); err != nil {
				return Settings{}, fmt.Errorf("field %s: %w", spec.Key, err)
			}
			field.SetString(rawURL)
		case WidgetInt:
			parsedInt, parseErr := parseIntValue(form.ScalarValues[spec.Key], spec.Min, spec.Max, spec.EmptyZero)
			if parseErr != nil {
				return Settings{}, fmt.Errorf("field %s: %w", spec.Key, parseErr)
			}
			field.SetInt(int64(parsedInt))
		case WidgetBool:
			parsedBool, parseErr := parseBoolValue(form.ScalarValues[spec.Key])
			if parseErr != nil {
				return Settings{}, fmt.Errorf("field %s: %w", spec.Key, parseErr)
			}
			field.SetBool(parsedBool)
		case WidgetDuration:
			durationValue, parseErr := parseDurationValue(form.ScalarValues[spec.Key])
			if parseErr != nil {
				return Settings{}, fmt.Errorf("field %s: %w", spec.Key, parseErr)
			}
			field.Set(reflect.ValueOf(durationValue))
		case WidgetKeyValue:
			decodedMap, parseErr := decodeMapRows(form.KeyValueValues[spec.Key], field.Type())
			if parseErr != nil {
				return Settings{}, fmt.Errorf("field %s: %w", spec.Key, parseErr)
			}
			field.Set(decodedMap)
		case WidgetArray:
			decodedArray, parseErr := decodeObjectArrayRows(form.ObjectArrayValues[spec.Key], field.Type(), spec.ElementSpec)
			if parseErr != nil {
				return Settings{}, fmt.Errorf("field %s: %w", spec.Key, parseErr)
			}
			field.Set(decodedArray)
		default:
			return Settings{}, fmt.Errorf("%w: %s", errInvalidUIWidget, spec.Widget)
		}
	}

	finalSettings := applyDefaults(&out)
	if err := finalSettings.syncReasoningPoliciesToMap(); err != nil {
		return Settings{}, fmt.Errorf("sync reasoning policies map: %w", err)
	}
	finalSettings.syncClaudeHaikuFallbackModelsToStorage()
	return finalSettings, nil
}

func readonlyChanged(spec *FieldSpec, base, current SettingsForm) bool {
	if spec == nil {
		return false
	}
	switch spec.Widget {
	case WidgetText:
		return base.ScalarValues[spec.Key] != current.ScalarValues[spec.Key]
	case WidgetURL:
		return base.ScalarValues[spec.Key] != current.ScalarValues[spec.Key]
	case WidgetInt:
		return base.ScalarValues[spec.Key] != current.ScalarValues[spec.Key]
	case WidgetBool:
		return base.ScalarValues[spec.Key] != current.ScalarValues[spec.Key]
	case WidgetDuration:
		return base.ScalarValues[spec.Key] != current.ScalarValues[spec.Key]
	case WidgetKeyValue:
		return !reflect.DeepEqual(base.KeyValueValues[spec.Key], current.KeyValueValues[spec.Key])
	case WidgetArray:
		return !reflect.DeepEqual(base.ObjectArrayValues[spec.Key], current.ObjectArrayValues[spec.Key])
	default:
		return false
	}
}

func validateURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%w: %s", errInvalidURL, raw)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("%w: %s", errInvalidURL, raw)
	}
	return nil
}

func validateMinMax(value int, minRaw, maxRaw string) error {
	if minRaw != "" {
		minValue, err := strconv.Atoi(minRaw)
		if err != nil {
			return fmt.Errorf("%w: min=%s", errInvalidMinMax, minRaw)
		}
		if value < minValue {
			return fmt.Errorf("%w: below min %d", errInvalidInt, minValue)
		}
	}
	if maxRaw != "" {
		maxValue, err := strconv.Atoi(maxRaw)
		if err != nil {
			return fmt.Errorf("%w: max=%s", errInvalidMinMax, maxRaw)
		}
		if value > maxValue {
			return fmt.Errorf("%w: above max %d", errInvalidInt, maxValue)
		}
	}
	return nil
}

func parseIntValue(raw, minRaw, maxRaw string, emptyZero bool) (int, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		if emptyZero {
			return 0, nil
		}
		return 0, errInvalidInt
	}
	parsedInt, parseErr := strconv.Atoi(trimmed)
	if parseErr != nil {
		return 0, errInvalidInt
	}
	if err := validateMinMax(parsedInt, minRaw, maxRaw); err != nil {
		return 0, err
	}
	return parsedInt, nil
}

func parseDurationValue(raw string) (Duration, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return Duration{}, ErrDurationEmpty
	}
	parsed, err := time.ParseDuration(trimmed)
	if err != nil {
		return Duration{}, fmt.Errorf("%w: %s", ErrInvalidDuration, trimmed)
	}
	if parsed < 0 {
		return Duration{}, ErrDurationNegative
	}
	if parsed%time.Second != 0 {
		return Duration{}, fmt.Errorf("%w: %s", ErrDurationWholeSeconds, trimmed)
	}
	return NewDuration(parsed), nil
}

func parseBoolValue(raw string) (bool, error) {
	trimmed := strings.TrimSpace(raw)
	if strings.EqualFold(trimmed, "true") {
		return true, nil
	}
	if strings.EqualFold(trimmed, "false") {
		return false, nil
	}
	return false, fmt.Errorf("%w: %s", errInvalidBool, trimmed)
}

func encodeMapRows(field reflect.Value) ([]HeaderKV, error) {
	if field.Kind() != reflect.Map || field.Type().Key().Kind() != reflect.String {
		return nil, fmt.Errorf("%w: expected map with string key", errInvalidWidgetType)
	}
	if field.Len() == 0 {
		return nil, nil
	}

	keys := field.MapKeys()
	sortedKeys := make([]string, 0, len(keys))
	for _, key := range keys {
		sortedKeys = append(sortedKeys, key.String())
	}
	sort.Strings(sortedKeys)

	rows := make([]HeaderKV, 0, len(sortedKeys))
	for _, key := range sortedKeys {
		value := field.MapIndex(reflect.ValueOf(key))
		encodedValue, err := encodeMapValue(value)
		if err != nil {
			return nil, err
		}
		rows = append(rows, HeaderKV{
			Key:   key,
			Value: encodedValue,
		})
	}
	return rows, nil
}

func encodeMapValue(value reflect.Value) (string, error) {
	if !value.IsValid() {
		return "", nil
	}
	if value.Kind() == reflect.String {
		return value.String(), nil
	}
	raw, err := json.Marshal(value.Interface())
	if err != nil {
		return "", fmt.Errorf("encode map value: %w", err)
	}
	return string(raw), nil
}

func decodeMapRows(rows []HeaderKV, mapType reflect.Type) (reflect.Value, error) {
	if mapType.Kind() != reflect.Map || mapType.Key().Kind() != reflect.String {
		return reflect.Value{}, fmt.Errorf("%w: expected map[string]T", errInvalidWidgetType)
	}
	decoded := reflect.MakeMapWithSize(mapType, len(rows))
	if len(rows) == 0 {
		return decoded, nil
	}
	seen := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		key := strings.TrimSpace(row.Key)
		if key == "" {
			return reflect.Value{}, errEmptyHeaderKey
		}
		normalizedKey := strings.ToLower(key)
		if _, exists := seen[normalizedKey]; exists {
			return reflect.Value{}, fmt.Errorf("%w: %s", errDuplicateHeaderKey, key)
		}
		seen[normalizedKey] = struct{}{}

		value, err := decodeMapValue(strings.TrimSpace(row.Value), mapType.Elem())
		if err != nil {
			return reflect.Value{}, fmt.Errorf("key %s: %w", key, err)
		}
		decoded.SetMapIndex(reflect.ValueOf(key), value)
	}
	return decoded, nil
}

func decodeMapValue(raw string, targetType reflect.Type) (reflect.Value, error) {
	if targetType.Kind() == reflect.String {
		return reflect.ValueOf(raw).Convert(targetType), nil
	}
	ptr := reflect.New(targetType)
	if err := json.Unmarshal([]byte(raw), ptr.Interface()); err != nil {
		return reflect.Value{}, fmt.Errorf("decode map value json: %w", err)
	}
	return ptr.Elem(), nil
}

func encodeObjectArrayRows(field reflect.Value, elementSpecs []FieldSpec) ([]map[string]string, error) {
	if field.Kind() != reflect.Slice {
		return nil, fmt.Errorf("%w: expected slice", errInvalidWidgetType)
	}
	rows := make([]map[string]string, 0, field.Len())
	for i := 0; i < field.Len(); i++ {
		item := field.Index(i)
		if item.Kind() == reflect.Pointer {
			item = item.Elem()
		}
		if item.Kind() != reflect.Struct {
			return nil, fmt.Errorf("%w: expected struct element", errInvalidWidgetType)
		}
		row := make(map[string]string, len(elementSpecs))
		for _, elementSpec := range elementSpecs {
			fieldValue := item.FieldByName(elementSpec.FieldName)
			encoded, err := encodeScalarFieldValue(fieldValue, &elementSpec)
			if err != nil {
				return nil, err
			}
			row[elementSpec.Key] = encoded
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func decodeObjectArrayRows(rows []map[string]string, targetType reflect.Type, elementSpecs []FieldSpec) (reflect.Value, error) {
	if targetType.Kind() != reflect.Slice {
		return reflect.Value{}, fmt.Errorf("%w: expected slice", errInvalidWidgetType)
	}
	result := reflect.MakeSlice(targetType, 0, len(rows))
	elemType := targetType.Elem()
	isPointerElem := elemType.Kind() == reflect.Pointer
	structType := elemType
	if isPointerElem {
		structType = elemType.Elem()
	}
	for _, row := range rows {
		item := reflect.New(structType).Elem()
		for _, elementSpec := range elementSpecs {
			field := item.FieldByName(elementSpec.FieldName)
			if !field.IsValid() || !field.CanSet() {
				return reflect.Value{}, fmt.Errorf("%w: %s", errFieldNotFound, elementSpec.FieldName)
			}
			if elementSpec.ReadOnly {
				continue
			}
			raw := row[elementSpec.Key]
			if err := decodeScalarFieldValue(field, &elementSpec, raw); err != nil {
				return reflect.Value{}, err
			}
		}
		if isPointerElem {
			ptr := reflect.New(structType)
			ptr.Elem().Set(item)
			result = reflect.Append(result, ptr)
		} else {
			result = reflect.Append(result, item)
		}
	}
	return result, nil
}

func encodeScalarFieldValue(field reflect.Value, spec *FieldSpec) (string, error) {
	if !field.IsValid() || spec == nil {
		return "", nil
	}
	switch spec.Widget {
	case WidgetText, WidgetURL:
		return field.String(), nil
	case WidgetInt:
		return strconv.Itoa(int(field.Int())), nil
	case WidgetBool:
		return strconv.FormatBool(field.Bool()), nil
	case WidgetDuration:
		durationValue, ok := field.Interface().(Duration)
		if !ok {
			return "", fmt.Errorf("%w: %s", errInvalidWidgetType, spec.Widget)
		}
		return durationValue.String(), nil
	default:
		return "", fmt.Errorf("%w: %s", errInvalidUIWidget, spec.Widget)
	}
}

func decodeScalarFieldValue(field reflect.Value, spec *FieldSpec, raw string) error {
	if !field.IsValid() || spec == nil {
		return nil
	}
	switch spec.Widget {
	case WidgetText:
		if err := validateEnum(raw, spec.EnumValues); err != nil {
			return err
		}
		field.SetString(raw)
	case WidgetURL:
		trimmed := strings.TrimSpace(raw)
		if err := validateURL(trimmed); err != nil {
			return err
		}
		if err := validateEnum(trimmed, spec.EnumValues); err != nil {
			return err
		}
		field.SetString(trimmed)
	case WidgetInt:
		parsedInt, parseErr := parseIntValue(raw, spec.Min, spec.Max, spec.EmptyZero)
		if parseErr != nil {
			return parseErr
		}
		field.SetInt(int64(parsedInt))
	case WidgetBool:
		parsedBool, parseErr := parseBoolValue(raw)
		if parseErr != nil {
			return parseErr
		}
		field.SetBool(parsedBool)
	case WidgetDuration:
		durationValue, parseErr := parseDurationValue(raw)
		if parseErr != nil {
			return parseErr
		}
		field.Set(reflect.ValueOf(durationValue))
	default:
		return fmt.Errorf("%w: %s", errInvalidUIWidget, spec.Widget)
	}
	return nil
}

func validateEnum(raw string, enumValues []string) error {
	if len(enumValues) == 0 {
		return nil
	}
	for _, item := range enumValues {
		if raw == item {
			return nil
		}
	}
	return fmt.Errorf("%w: expected one of %s", errInvalidEnum, strings.Join(enumValues, ","))
}

func parseEnumValues(raw string) ([]string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, nil
	}
	parts := strings.Split(trimmed, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		token := strings.TrimSpace(part)
		if token == "" {
			continue
		}
		if strings.HasPrefix(token, "'") && strings.HasSuffix(token, "'") && len(token) >= 2 {
			token = token[1 : len(token)-1]
		}
		values = append(values, token)
	}
	return values, nil
}
