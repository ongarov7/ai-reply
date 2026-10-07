package productevents

import (
	"bytes"
	"encoding/json"
	"errors"
	"strconv"
	"time"
)

// Event — catalog-checked event, ready to store.
type Event struct {
	Name string
	// Props — catalog keys only, each an int, a bool or a catalog code.
	Props map[string]any
	// ClientTime — when it happened by the device's clock; zero when not sent.
	ClientTime time.Time
}

// Why an event is rejected. Only counted, never logged with its content.
var (
	ErrMalformedEvent  = errors.New("malformed event")
	ErrUnknownEvent    = errors.New("unknown event")
	ErrUnknownProperty = errors.New("unknown event property")
	ErrInvalidValue    = errors.New("invalid event property value")
	ErrInvalidTime     = errors.New("invalid event time")
)

// wireEvent — one element of "events" in the request body.
type wireEvent struct {
	Name  string                     `json:"name"`
	TS    string                     `json:"ts"`
	Props map[string]json.RawMessage `json:"props"`
}

// Parse — бір оқиғаны каталог бойынша тексереді. Белгісіз кілт, атау, қасиет
// не мән — қате; оқиға жазылмайды.
func Parse(raw json.RawMessage) (Event, error) {
	var wire wireEvent
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&wire); err != nil {
		return Event{}, ErrMalformedEvent
	}
	properties, ok := catalog[wire.Name]
	if !ok {
		return Event{}, ErrUnknownEvent
	}
	event := Event{Name: wire.Name, Props: make(map[string]any, len(wire.Props))}
	for key, value := range wire.Props {
		p, ok := properties[key]
		if !ok {
			return Event{}, ErrUnknownProperty
		}
		checked, err := p.parse(value)
		if err != nil {
			return Event{}, err
		}
		event.Props[key] = checked
	}
	if wire.TS != "" {
		at, err := time.Parse(time.RFC3339, wire.TS)
		if err != nil {
			return Event{}, ErrInvalidTime
		}
		event.ClientTime = at.UTC()
	}
	return event, nil
}

// parse — the value exactly as JSON spelled it: an integer literal, true or
// false, or a string from the property's codes. A quoted number, a fraction,
// null or any other string is rejected.
func (p property) parse(raw json.RawMessage) (any, error) {
	literal := string(bytes.TrimSpace(raw))
	switch p.kind {
	case kindInteger:
		n, err := strconv.Atoi(literal)
		if err != nil || n < 0 || n > MaxInteger {
			return nil, ErrInvalidValue
		}
		return n, nil
	case kindFlag:
		switch literal {
		case "true":
			return true, nil
		case "false":
			return false, nil
		}
		return nil, ErrInvalidValue
	default:
		var s string
		if err := json.Unmarshal(raw, &s); err != nil || !p.allows(s) {
			return nil, ErrInvalidValue
		}
		return s, nil
	}
}
