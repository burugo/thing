package thing

import (
	"context"
	"fmt"
	"log"
	"reflect"
	"sync"
)

// --- Event System ---

// EventType defines the type for lifecycle events.
type EventType string

// Standard lifecycle event types
const (
	EventTypeBeforeSave       EventType = "BeforeSave"
	EventTypeAfterSave        EventType = "AfterSave"
	EventTypeBeforeCreate     EventType = "BeforeCreate"
	EventTypeAfterCreate      EventType = "AfterCreate"
	EventTypeBeforeDelete     EventType = "BeforeDelete"
	EventTypeAfterDelete      EventType = "AfterDelete"
	EventTypeBeforeSoftDelete EventType = "BeforeSoftDelete"
	EventTypeAfterSoftDelete  EventType = "AfterSoftDelete"
)

// EventListener defines the signature for functions that can listen to events.
type EventListener func(ctx context.Context, eventType EventType, model interface{}, eventData interface{}) error

// listenerRegistry holds the registered listeners for each event type.
var (
	listenerRegistry = make(map[EventType][]EventListener)
	listenerMutex    sync.RWMutex // To protect concurrent access to the registry
)

// RegisterListener adds a listener function for a specific event type.
func RegisterListener(eventType EventType, listener EventListener) {
	listenerMutex.Lock()
	defer listenerMutex.Unlock()
	listenerRegistry[eventType] = append(listenerRegistry[eventType], listener)
}

// UnregisterListener removes a specific listener function for a specific event type.
// It compares function pointers to find the listener to remove.
func UnregisterListener(eventType EventType, listenerToRemove EventListener) {
	listenerMutex.Lock()
	defer listenerMutex.Unlock()

	listeners, ok := listenerRegistry[eventType]
	if !ok || len(listeners) == 0 {
		return // No listeners for this event type
	}

	// Find the listener by comparing function pointers
	foundIndex := -1
	listenerToRemovePtr := reflect.ValueOf(listenerToRemove).Pointer()
	for i, listener := range listeners {
		if reflect.ValueOf(listener).Pointer() == listenerToRemovePtr {
			foundIndex = i
			break
		}
	}

	// If found, remove it from the slice
	if foundIndex != -1 {
		listenerRegistry[eventType] = append(listeners[:foundIndex], listeners[foundIndex+1:]...)
		log.Printf("DEBUG: Unregistered listener for event %s", eventType)
	} else {
		log.Printf("WARN: Listener not found for event %s during unregister attempt.", eventType)
	}
}

// ResetListeners clears all registered event listeners. Primarily intended for use in tests.
func ResetListeners() {
	listenerMutex.Lock()
	defer listenerMutex.Unlock()
	// Create a new empty map instead of clearing the old one
	listenerRegistry = make(map[EventType][]EventListener)
	log.Println("DEBUG: All event listeners have been reset.")
}

// triggerEvent executes all registered listeners for a given event type.
// Model is passed as interface{}.
func triggerEvent(ctx context.Context, eventType EventType, model interface{}, eventData interface{}) error {
	listenerMutex.RLock()
	listeners, ok := listenerRegistry[eventType]
	listenerMutex.RUnlock()

	if !ok || len(listeners) == 0 {
		return nil
	}

	// Get model ID for logging
	var modelID int64 = -1
	if m, ok := model.(Model); ok {
		modelID = m.GetID()
	}

	// Execute listeners sequentially.
	for _, listener := range listeners {
		err := listener(ctx, eventType, model, eventData)
		if err != nil {
			log.Printf("Error executing listener for event %s on model %T(%d): %v", eventType, model, modelID, err)
			return fmt.Errorf("event listener for %s failed: %w", eventType, err)
		}
	}
	return nil
}
