package config

import (
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
	WidgetDuration FieldWidget = "duration"
	WidgetKeyValue FieldWidget = "kv"
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
)

const (
	tagPairParts = 2
)

var (
	errUnknownUIKey       = errors.New("unknown ui key")
	errInvalidUIBool      = errors.New("invalid ui bool")
	errInvalidUIOrder     = errors.New("invalid ui order")
	errInvalidUIWidget    = errors.New("invalid ui widget")
	errInvalidWidgetType  = errors.New("invalid widget type")
	errFieldNotFound      = errors.New("settings field not found")
	errInvalidURL         = errors.New("invalid url")
	errInvalidInt         = errors.New("invalid integer")
	errInvalidMinMax      = errors.New("invalid min/max")
	errDuplicateHeaderKey = errors.New("duplicate header key")
	errEmptyHeaderKey     = errors.New("empty header key")
	errReadonlyModified   = errors.New("readonly field modified")
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
}

type HeaderKV struct {
	Key   string
	Value string
}

type SettingsForm struct {
	ScalarValues   map[string]string
	KeyValueValues map[string][]HeaderKV
}

func (f SettingsForm) Clone() SettingsForm {
	clone := SettingsForm{
		ScalarValues:   make(map[string]string, len(f.ScalarValues)),
		KeyValueValues: make(map[string][]HeaderKV, len(f.KeyValueValues)),
	}
	for key, value := range f.ScalarValues {
		clone.ScalarValues[key] = value
	}
	for key, rows := range f.KeyValueValues {
		clonedRows := make([]HeaderKV, len(rows))
		copy(clonedRows, rows)
		clone.KeyValueValues[key] = clonedRows
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
		if jsonKey == "" {
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
	}

	if label, ok := uiOpts[uiTagLabel]; ok && label != "" {
		spec.Label = label
	}
	if placeholder, ok := uiOpts[uiTagPlaceholder]; ok {
		spec.Placeholder = placeholder
	}
	if minRaw, ok := uiOpts[uiTagMin]; ok {
		spec.Min = minRaw
	}
	if maxRaw, ok := uiOpts[uiTagMax]; ok {
		spec.Max = maxRaw
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
	if err := validateWidget(spec.Widget, field.Type); err != nil {
		return FieldSpec{}, fmt.Errorf("field %s: %w", field.Name, err)
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
	case t == reflect.TypeOf(durationType):
		return WidgetDuration
	case t.Kind() == reflect.Map:
		return WidgetKeyValue
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
	case WidgetDuration:
		if t != reflect.TypeOf(durationType) {
			return fmt.Errorf("%w: widget %s requires Duration", errInvalidWidgetType, widget)
		}
	case WidgetKeyValue:
		if t.Kind() != reflect.Map || t.Key().Kind() != reflect.String || t.Elem().Kind() != reflect.String {
			return fmt.Errorf("%w: widget %s requires map[string]string", errInvalidWidgetType, widget)
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
		ScalarValues:   make(map[string]string),
		KeyValueValues: make(map[string][]HeaderKV),
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
		case WidgetDuration:
			durationValue, ok := field.Interface().(Duration)
			if !ok {
				return SettingsForm{}, fmt.Errorf("%w: %s", errInvalidWidgetType, spec.Widget)
			}
			form.ScalarValues[spec.Key] = durationValue.Duration().String()
		case WidgetKeyValue:
			headersMap, ok := field.Interface().(map[string]string)
			if !ok {
				return SettingsForm{}, fmt.Errorf("%w: %s", errInvalidWidgetType, spec.Widget)
			}
			form.KeyValueValues[spec.Key] = encodeHeaders(headersMap)
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
			field.SetString(form.ScalarValues[spec.Key])
		case WidgetURL:
			rawURL := strings.TrimSpace(form.ScalarValues[spec.Key])
			if err := validateURL(rawURL); err != nil {
				return Settings{}, fmt.Errorf("field %s: %w", spec.Key, err)
			}
			field.SetString(rawURL)
		case WidgetInt:
			rawInt := strings.TrimSpace(form.ScalarValues[spec.Key])
			parsedInt, parseErr := strconv.Atoi(rawInt)
			if parseErr != nil {
				return Settings{}, fmt.Errorf("field %s: %w", spec.Key, errInvalidInt)
			}
			if err := validateMinMax(parsedInt, spec.Min, spec.Max); err != nil {
				return Settings{}, fmt.Errorf("field %s: %w", spec.Key, err)
			}
			field.SetInt(int64(parsedInt))
		case WidgetDuration:
			durationValue, parseErr := parseDurationValue(form.ScalarValues[spec.Key])
			if parseErr != nil {
				return Settings{}, fmt.Errorf("field %s: %w", spec.Key, parseErr)
			}
			field.Set(reflect.ValueOf(durationValue))
		case WidgetKeyValue:
			headers, parseErr := decodeHeaders(form.KeyValueValues[spec.Key])
			if parseErr != nil {
				return Settings{}, fmt.Errorf("field %s: %w", spec.Key, parseErr)
			}
			field.Set(reflect.ValueOf(headers))
		default:
			return Settings{}, fmt.Errorf("%w: %s", errInvalidUIWidget, spec.Widget)
		}
	}

	return applyDefaults(&out), nil
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
	case WidgetDuration:
		return base.ScalarValues[spec.Key] != current.ScalarValues[spec.Key]
	case WidgetKeyValue:
		return !reflect.DeepEqual(base.KeyValueValues[spec.Key], current.KeyValueValues[spec.Key])
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

func encodeHeaders(headers map[string]string) []HeaderKV {
	if len(headers) == 0 {
		return nil
	}
	keys := make([]string, 0, len(headers))
	for key := range headers {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	result := make([]HeaderKV, 0, len(keys))
	for i := range keys {
		key := keys[i]
		result = append(result, HeaderKV{Key: key, Value: headers[key]})
	}
	return result
}

func decodeHeaders(rows []HeaderKV) (map[string]string, error) {
	headers := make(map[string]string, len(rows))
	if len(rows) == 0 {
		return headers, nil
	}
	seen := make(map[string]struct{}, len(rows))
	for i := range rows {
		row := rows[i]
		key := strings.TrimSpace(row.Key)
		if key == "" {
			return nil, errEmptyHeaderKey
		}
		normalizedKey := strings.ToLower(key)
		if _, exists := seen[normalizedKey]; exists {
			return nil, fmt.Errorf("%w: %s", errDuplicateHeaderKey, key)
		}
		seen[normalizedKey] = struct{}{}
		headers[key] = row.Value
	}
	return headers, nil
}
