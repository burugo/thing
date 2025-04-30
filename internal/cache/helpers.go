package cache

// "thing" // REMOVED import of root package

// Assuming redis is used, adjust if needed

// AddIDToListIfNotExists adds an ID to a list if it's not already present.
// Exported: Needed by root cache.go
func AddIDToListIfNotExists(ids []int64, idToAdd int64) []int64 {
	for _, id := range ids {
		if id == idToAdd {
			return ids // Already exists
		}
	}
	return append(ids, idToAdd)
}

// RemoveIDFromList removes an ID from a list.
// Exported: Needed by root cache.go
func RemoveIDFromList(ids []int64, idToRemove int64) []int64 {
	newIDs := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id != idToRemove {
			newIDs = append(newIDs, id)
		}
	}
	return newIDs
}
