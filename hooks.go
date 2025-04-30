package thing

import (
	"context"
	"fmt"
	"log"
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
	// Note: This relies on getBaseModelPtr which might be moved later.
	// If moved to a different package, this dependency needs resolution.
	if bm := getBaseModelPtr(model); bm != nil { // Uses helper defined later
		modelID = bm.GetID()
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
