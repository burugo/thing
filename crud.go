package thing

import (
	"errors"
	"fmt"
	"log"
	"reflect"
	"time"
)

// ByID fetches a single model by its ID.
func (t *Thing[T]) ByID(id int64) (T, error) {
	var dest T
	err := t.byIDInternal(t.ctx, id, &dest)
	return dest, err
}

// Save creates or updates a record in the database.
func (t *Thing[T]) Save(value T) error {
	return t.saveInternal(t.ctx, value) // value is already *User (T)
}

// SoftDelete performs a soft delete on the record by setting the 'deleted' flag to true
// and updating the 'updated_at' timestamp. It uses saveInternal to persist only these changes.
func (t *Thing[T]) SoftDelete(value T) error {
	id := value.GetID()
	if id == 0 {
		return errors.New("SoftDelete: cannot soft delete record with zero ID")
	}

	// --- Trigger BeforeSoftDelete hook ---
	if err := triggerEvent(t.ctx, EventTypeBeforeSoftDelete, value, nil); err != nil {
		return fmt.Errorf("BeforeSoftDelete hook failed: %w", err)
	}

	// Mark for soft deletion
	now := time.Now()
	// Set Deleted = true and UpdatedAt = now using reflection
	val := reflect.ValueOf(value).Elem()
	deletedField := val.FieldByName("Deleted")
	if deletedField.IsValid() && deletedField.CanSet() {
		deletedField.SetBool(true)
	} else {
		return errors.New("SoftDelete: could not set Deleted field via reflection")
	}
	updatedAtField := val.FieldByName("UpdatedAt")
	if updatedAtField.IsValid() && updatedAtField.CanSet() {
		updatedAtField.Set(reflect.ValueOf(now))
	} else {
		log.Printf("WARN: SoftDelete could not set UpdatedAt via reflection for %s ID %d", t.info.TableName, id)
	}

	// Call saveInternal - it will detect only Deleted and UpdatedAt changed
	err := t.saveInternal(t.ctx, value)
	if err != nil {
		return fmt.Errorf("SoftDelete failed during save operation: %w", err)
	}

	// --- Trigger AfterSoftDelete hook (only if save succeeded) ---
	if errHook := triggerEvent(t.ctx, EventTypeAfterSoftDelete, value, nil); errHook != nil {
		log.Printf("WARN: AfterSoftDelete hook failed: %v", errHook)
	}

	return nil // Success
}

// Delete performs a hard delete on the record from the database.
func (t *Thing[T]) Delete(value T) error {
	// Call the internal hard delete logic
	return t.deleteInternal(t.ctx, value)
}

// ByIDs retrieves multiple records by their primary keys and optionally preloads relations.
func (t *Thing[T]) ByIDs(ids []int64, preloads ...string) (map[int64]T, error) {
	modelType := reflect.TypeOf((*T)(nil)).Elem()
	resultsMapReflect, err := fetchModelsByIDsInternal(t.ctx, t.cache, t.db, t.info, modelType, ids)
	if err != nil {
		return nil, fmt.Errorf("ByIDs failed during internal fetch: %w", err)
	}

	// Convert map[int64]reflect.Value (containing T) to map[int64]T
	resultsMapTyped := make(map[int64]T, len(resultsMapReflect))
	// Also collect results in a slice for preloading
	resultsSliceForPreload := make([]T, 0, len(resultsMapReflect))
	for id, modelVal := range resultsMapReflect {
		if typedModel, ok := modelVal.Interface().(T); ok {
			resultsMapTyped[id] = typedModel
			resultsSliceForPreload = append(resultsSliceForPreload, typedModel)
		} else {
			log.Printf("WARN: ByIDs: Could not assert type for ID %d", id)
			log.Printf("DEBUG: modelVal.Interface() type: %T, reflect.TypeOf((*T)(nil)).Elem(): %v", modelVal.Interface(), reflect.TypeOf((*T)(nil)).Elem())
		}
	}

	// Apply preloads if requested
	if len(preloads) > 0 && len(resultsSliceForPreload) > 0 {
		for _, preloadName := range preloads {
			if preloadErr := t.preloadRelations(t.ctx, resultsSliceForPreload, preloadName); preloadErr != nil {
				log.Printf("WARN: ByIDs: failed to apply preload '%s': %v", preloadName, preloadErr)
			}
		}
	}

	return resultsMapTyped, nil
}
