package opencode

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// orderedObject is a JSON object that preserves key insertion order. Values are held
// as raw JSON so untouched entries round-trip byte-for-byte (modulo re-indentation).
type orderedObject struct {
	keys []string
	vals map[string]json.RawMessage
}

func newOrderedObject() *orderedObject {
	return &orderedObject{vals: map[string]json.RawMessage{}}
}

// parseOrderedObject decodes a JSON object, preserving the order keys appear in.
// Empty input yields an empty object.
func parseOrderedObject(data []byte) (*orderedObject, error) {
	o := newOrderedObject()
	if len(bytes.TrimSpace(data)) == 0 {
		return o, nil
	}

	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil, fmt.Errorf("expected JSON object, got %v", tok)
	}

	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, ok := keyTok.(string)
		if !ok {
			return nil, fmt.Errorf("expected string key, got %v", keyTok)
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, err
		}
		o.set(key, raw)
	}

	if _, err := dec.Token(); err != nil { // consume closing '}'
		return nil, err
	}
	return o, nil
}

// set stores raw under key, appending the key on first insertion and keeping its
// existing position on update.
func (o *orderedObject) set(key string, raw json.RawMessage) {
	if _, exists := o.vals[key]; !exists {
		o.keys = append(o.keys, key)
	}
	o.vals[key] = raw
}

func (o *orderedObject) get(key string) (json.RawMessage, bool) {
	v, ok := o.vals[key]
	return v, ok
}

// compact renders the object as compact JSON in key order. Run the result through
// json.Indent for a pretty-printed form identical to json.MarshalIndent's.
func (o *orderedObject) compact() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, k := range o.keys {
		if i > 0 {
			b.WriteByte(',')
		}
		keyJSON, err := json.Marshal(k)
		if err != nil {
			return nil, err
		}
		b.Write(keyJSON)
		b.WriteByte(':')
		var cv bytes.Buffer
		if err := json.Compact(&cv, o.vals[k]); err != nil {
			return nil, err
		}
		b.Write(cv.Bytes())
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}
