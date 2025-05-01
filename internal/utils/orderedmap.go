package utils

import (
	"encoding/json"
	"fmt"
)

// OrderedMap is a map that preserves key insertion order and supports JSON serialization.
type OrderedMap struct {
	keys   []string
	values map[string]interface{}
}

// NewOrderedMap creates a new empty OrderedMap.
func NewOrderedMap() *OrderedMap {
	return &OrderedMap{
		keys:   make([]string, 0),
		values: make(map[string]interface{}),
	}
}

// Set sets the value for a key, preserving insertion order.
func (om *OrderedMap) Set(key string, value interface{}) {
	if _, exists := om.values[key]; !exists {
		om.keys = append(om.keys, key)
	}
	om.values[key] = value
}

// Get retrieves the value for a key.
func (om *OrderedMap) Get(key string) (interface{}, bool) {
	v, ok := om.values[key]
	return v, ok
}

// Keys returns the keys in insertion order.
func (om *OrderedMap) Keys() []string {
	return append([]string(nil), om.keys...)
}

// MarshalJSON implements json.Marshaler, outputting keys in order.
func (om *OrderedMap) MarshalJSON() ([]byte, error) {
	buf := []byte{'{'}
	for i, k := range om.keys {
		v := om.values[k]
		keyBytes, err := json.Marshal(k)
		if err != nil {
			return nil, fmt.Errorf("marshal key: %w", err)
		}
		valBytes, err := json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("marshal value for key %q: %w", k, err)
		}
		buf = append(buf, keyBytes...)
		buf = append(buf, ':')
		buf = append(buf, valBytes...)
		if i < len(om.keys)-1 {
			buf = append(buf, ',')
		}
	}
	buf = append(buf, '}')
	return buf, nil
}
