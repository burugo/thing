package thing

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"thing/internal/utils"
)

const (
	// Max number of IDs to cache per query list
	cacheListCountLimit = 300
)

// CachedResult represents a cached query result with lazy loading capabilities.
// It allows for efficient querying with pagination and caching.
type CachedResult[T any] struct {
	thing          *Thing[T]
	params         QueryParams
	cachedIDs      []int64
	cachedCount    int64
	hasLoadedIDs   bool
	hasLoadedCount bool
	hasLoadedAll   bool
	all            []*T
}

// Helper function to generate cache key for count queries.
// Similar to generateQueryCacheKey but with a different prefix.
func (cr *CachedResult[T]) generateCountCacheKey() (string, error) {
	if cr.thing == nil || cr.thing.info == nil {
		return "", errors.New("generateCountCacheKey: Thing or model info not initialized")
	}
	tableName := cr.thing.info.TableName
	// Use the same normalization logic as the query key generation
	normalizedParams := cr.params // Assuming QueryParams is simple enough or normalization happens elsewhere if needed
	normalizedArgs := make([]interface{}, len(cr.params.Args))
	for i, arg := range cr.params.Args {
		normalizedArgs[i] = utils.NormalizeValue(arg) // Already uses utils.
	}
	normalizedParams.Args = normalizedArgs

	paramsBytes, err := json.Marshal(normalizedParams)
	if err != nil {
		log.Printf("ERROR: json.Marshal(params) failed in generateCountCacheKey: %v\nParams: %+v", err, normalizedParams)
		return "", fmt.Errorf("failed to marshal query params for count cache key: %w", err)
	}

	hasher := sha256.New()
	hasher.Write([]byte(tableName))
	hasher.Write(paramsBytes)
	hash := hex.EncodeToString(hasher.Sum(nil))
	// Using format: count:{tableName}:{hash}
	return fmt.Sprintf("count:%s:%s", tableName, hash), nil
}

// Helper function to generate cache key for list queries.
func (cr *CachedResult[T]) generateListCacheKey() (string, error) {
	if cr.thing == nil || cr.thing.info == nil {
		return "", errors.New("generateListCacheKey: Thing or model info not initialized")
	}
	tableName := cr.thing.info.TableName
	// Use the same normalization logic as the query key generation
	normalizedParams := cr.params // Assuming QueryParams is simple enough or normalization happens elsewhere if needed
	normalizedArgs := make([]interface{}, len(cr.params.Args))
	for i, arg := range cr.params.Args {
		normalizedArgs[i] = utils.NormalizeValue(arg) // Already uses utils.
	}
	normalizedParams.Args = normalizedArgs

	paramsBytes, err := json.Marshal(normalizedParams)
	if err != nil {
		log.Printf("ERROR: json.Marshal(params) failed in generateListCacheKey: %v\nParams: %+v", err, normalizedParams)
		return "", fmt.Errorf("failed to marshal query params for list cache key: %w", err)
	}

	hasher := sha256.New()
	hasher.Write([]byte(tableName))
	hasher.Write(paramsBytes)
	hash := hex.EncodeToString(hasher.Sum(nil))
	// Using format: list:{tableName}:{hash}
	return fmt.Sprintf("list:%s:%s", tableName, hash), nil
}

// Count returns the total number of records matching the query.
// It utilizes caching to avoid redundant database calls.
func (cr *CachedResult[T]) Count() (int64, error) {
	if cr.thing == nil || cr.thing.cache == nil || cr.thing.db == nil {
		return 0, errors.New("Count: CachedResult not properly initialized")
	}

	// 1. Check if count is already loaded in memory
	if cr.hasLoadedCount {
		return cr.cachedCount, nil
	}

	// 2. Generate cache key
	cacheKey, err := cr.generateCountCacheKey()
	if err != nil {
		log.Printf("Error generating count cache key: %v", err)
		// Fallback to DB query without caching?
		// For now, return error
		return 0, fmt.Errorf("failed to generate cache key for count: %w", err)
	}

	// 3. Check cache (using a generic Get method, assuming it returns string)
	cacheValStr, cacheErr := cr.thing.cache.Get(cr.thing.ctx, cacheKey)
	if cacheErr == nil {
		// Cache hit
		count, convErr := strconv.ParseInt(cacheValStr, 10, 64)
		if convErr == nil {
			log.Printf("CACHE HIT: Count Key: %s", cacheKey)
			cr.cachedCount = count
			cr.hasLoadedCount = true
			return count, nil
		} else {
			// Invalid data in cache, proceed to DB query
			log.Printf("WARN: Invalid count value found in cache for key %s: %s. Error: %v", cacheKey, cacheValStr, convErr)
			// Optionally delete the invalid cache entry here
			_ = cr.thing.cache.Delete(cr.thing.ctx, cacheKey)
		}
	} else if !errors.Is(cacheErr, ErrNotFound) {
		// Unexpected cache error, log but proceed to DB
		log.Printf("WARN: Cache Get error for count key %s: %v", cacheKey, cacheErr)
	}
	log.Printf("CACHE MISS: Count Key: %s", cacheKey)

	// 4. Cache miss or error, query database
	// Assuming DBAdapter has a GetCount method
	dbCount, dbErr := cr.thing.db.GetCount(cr.thing.ctx, cr.thing.info, cr.params)
	if dbErr != nil {
		log.Printf("DB ERROR: Count query failed: %v", dbErr)
		return 0, fmt.Errorf("database count query failed: %w", dbErr)
	}

	// 5. Store result in memory and cache
	log.Printf("DB HIT: Count Key: %s, Count: %d", cacheKey, dbCount)
	cr.cachedCount = dbCount
	cr.hasLoadedCount = true

	cacheSetErr := cr.thing.cache.Set(cr.thing.ctx, cacheKey, strconv.FormatInt(dbCount, 10), globalCacheTTL)
	if cacheSetErr != nil {
		log.Printf("WARN: Failed to cache count for key %s: %v", cacheKey, cacheSetErr)
	}

	return cr.cachedCount, nil
}

// _fetch ensures that the list of IDs matching the query is loaded, either from cache or DB.
func (cr *CachedResult[T]) _fetch() error {
	if cr.hasLoadedIDs {
		return nil // Already loaded
	}

	ids, err := cr._fetch_data()
	if err != nil {
		return err // Propagate error from data fetching
	}

	cr.cachedIDs = ids
	cr.hasLoadedIDs = true
	log.Printf("Internal fetch completed. Loaded %d IDs.", len(ids))
	return nil
}

// _fetch_ids_from_db fetches IDs from the database with pagination support.
// It accepts offset and limit parameters to enable proper pagination.
func (cr *CachedResult[T]) _fetch_ids_from_db(offset, limit int) ([]int64, error) {
	if cr.thing == nil || cr.thing.db == nil {
		return nil, errors.New("_fetch_ids_from_db: CachedResult not properly initialized")
	}

	// Build the SQL query with pagination
	query, args := buildSelectIDsSQL(cr.thing.info, cr.params)
	queryWithPagination := fmt.Sprintf("%s LIMIT %d OFFSET %d", query, limit, offset)

	// Execute the query
	var fetchedIDs []int64
	dbErr := cr.thing.db.Select(cr.thing.ctx, &fetchedIDs, queryWithPagination, args...)
	if dbErr != nil {
		log.Printf("DB ERROR: Fetching IDs with offset %d, limit %d failed: %v", offset, limit, dbErr)
		return nil, fmt.Errorf("database query for IDs with offset %d, limit %d failed: %w", offset, limit, dbErr)
	}

	log.Printf("DB QUERY: Fetched %d IDs with offset %d, limit %d", len(fetchedIDs), offset, limit)
	return fetchedIDs, nil
}

// _fetch_data attempts to load the first `cacheListCountLimit` IDs from cache or database.
func (cr *CachedResult[T]) _fetch_data() ([]int64, error) {
	if cr.thing == nil || cr.thing.cache == nil || cr.thing.db == nil {
		return nil, errors.New("_fetch_data: CachedResult not properly initialized")
	}

	// 1. Generate List Cache Key
	listCacheKey, err := cr.generateListCacheKey()
	if err != nil {
		log.Printf("Error generating list cache key: %v", err)
		return nil, fmt.Errorf("failed to generate cache key for list: %w", err)
	}

	// 2. Check Cache (Handle NoneResult marker)
	// 2a. Try generic Get first for NoneResult marker
	cacheValStr, genericCacheErr := cr.thing.cache.Get(cr.thing.ctx, listCacheKey)
	if genericCacheErr == nil {
		if cacheValStr == NoneResult {
			log.Printf("CACHE HIT (NoneResult): List Key: %s", listCacheKey)
			return []int64{}, nil // Explicitly cached empty result
		} else {
			// Found something, but not NoneResult. Maybe old data or invalid state?
			// Attempt to treat it as IDs via GetQueryIDs, or log warning and proceed.
			log.Printf("WARN: Cache Get returned unexpected value for list key %s: %s. Attempting GetQueryIDs.", listCacheKey, cacheValStr)
		}
	} else if !errors.Is(genericCacheErr, ErrNotFound) {
		// Unexpected error during generic Get
		log.Printf("WARN: Cache Get error for list key %s: %v. Proceeding to GetQueryIDs.", listCacheKey, genericCacheErr)
	}
	// If Get returned ErrNotFound or an unexpected value, try GetQueryIDs

	// 2b. Check for actual ID list
	cachedIDs, idsCacheErr := cr.thing.cache.GetQueryIDs(cr.thing.ctx, listCacheKey)
	if idsCacheErr == nil {
		log.Printf("CACHE HIT: List Key: %s (%d IDs)", listCacheKey, len(cachedIDs))
		// If we somehow got here after finding a non-NoneResult string with Get, overwrite it.
		if genericCacheErr == nil && cacheValStr != NoneResult {
			log.Printf("INFO: Overwriting potentially stale string value in cache with fetched IDs for key %s", listCacheKey)
			// No need to explicitly delete, SetQueryIDs below will overwrite if necessary,
			// and this path returns immediately anyway.
		}
		return cachedIDs, nil // Cache hit with actual IDs
	}
	if !errors.Is(idsCacheErr, ErrNotFound) {
		log.Printf("WARN: Cache GetQueryIDs error for list key %s: %v", listCacheKey, idsCacheErr)
	}
	log.Printf("CACHE MISS: List Key: %s (Tried Get: %v, Tried GetQueryIDs: %v)", listCacheKey, genericCacheErr, idsCacheErr)

	// 3. Cache Miss: Query Database for first `cacheListCountLimit` IDs
	// Use the common method to fetch IDs from database
	fetchedIDs, err := cr._fetch_ids_from_db(0, cacheListCountLimit)
	if err != nil {
		return nil, err
	}

	// 4. Handle DB results and cache appropriately
	if len(fetchedIDs) == 0 {
		// Store NoneResult marker in cache
		log.Printf("DB HIT (Zero Results): List Key: %s. Caching NoneResult.", listCacheKey)
		cacheSetErr := cr.thing.cache.Set(cr.thing.ctx, listCacheKey, NoneResult, globalCacheTTL)
		if cacheSetErr != nil {
			log.Printf("WARN: Failed to cache NoneResult for key %s: %v", listCacheKey, cacheSetErr)
		}
		// Also ensure count cache is updated or set to 0 if possible
		countCacheKey, keyErr := cr.generateCountCacheKey()
		if keyErr == nil {
			countSetErr := cr.thing.cache.Set(cr.thing.ctx, countCacheKey, "0", globalCacheTTL)
			if countSetErr != nil {
				log.Printf("WARN: Failed to update count cache to 0 for key %s: %v", countCacheKey, countSetErr)
			}
		} else {
			log.Printf("WARN: Failed to generate count cache key for zero result update: %v", keyErr)
		}
		return fetchedIDs, nil // Return empty slice
	} else {
		// Store actual fetched IDs in Cache
		log.Printf("DB HIT: List Key: %s, Fetched %d IDs (Limit %d). Caching IDs.", listCacheKey, len(fetchedIDs), cacheListCountLimit)
		cacheSetErr := cr.thing.cache.SetQueryIDs(cr.thing.ctx, listCacheKey, fetchedIDs, globalCacheTTL)
		if cacheSetErr != nil {
			log.Printf("WARN: Failed to cache list IDs for key %s: %v", listCacheKey, cacheSetErr)
		}

		// If fetched count < limit, update Count cache as well
		if len(fetchedIDs) < cacheListCountLimit {
			countCacheKey, keyErr := cr.generateCountCacheKey()
			if keyErr == nil {
				countStr := strconv.FormatInt(int64(len(fetchedIDs)), 10)
				countSetErr := cr.thing.cache.Set(cr.thing.ctx, countCacheKey, countStr, globalCacheTTL)
				if countSetErr != nil {
					log.Printf("WARN: Failed to update count cache (key: %s) after partial list fetch: %v", countCacheKey, countSetErr)
				} else {
					log.Printf("Updated count cache (key: %s) with count %d after partial list fetch", countCacheKey, len(fetchedIDs))
				}
			} else {
				log.Printf("WARN: Failed to generate count cache key for update after partial list fetch: %v", keyErr)
			}
		}
		return fetchedIDs, nil // Return fetched IDs
	}
}

// Fetch returns a subset of records starting from the given offset with the specified limit.
// It filters out soft-deleted items and triggers cache updates if inconsistencies are found.
// This implementation closely follows the PHP CachedResult.fetch() logic:
// - It iteratively fetches batches from cache or DB
// - It filters items using KeepItem()
// - It dynamically calculates how many more items to fetch based on filtering results
// - It tracks deleted/missing IDs for cache maintenance
func (cr *CachedResult[T]) Fetch(offset, limit int) ([]*T, error) {
	if cr.thing == nil || cr.thing.cache == nil || cr.thing.db == nil {
		return nil, errors.New("Fetch: CachedResult not properly initialized")
	}

	// 1. Ensure initial IDs are loaded (from cache or DB first attempt)
	if err := cr._fetch(); err != nil {
		return nil, fmt.Errorf("failed to fetch underlying IDs: %w", err)
	}

	// Handle case where query yielded no results initially
	if len(cr.cachedIDs) == 0 {
		log.Printf("Fetch: No cached IDs found for query.")
		return []*T{}, nil
	}

	// --- Setup for iterative fetching (similar to PHP CachedResult.fetch implementation) ---
	finalResults := make([]*T, 0, limit)
	deletedOrMissingIDs := make([]int64, 0)
	nextFetchOffset := offset // Starting offset (PHP's $start)
	nextFetchLimit := limit   // Initial fetch limit (PHP's $limit)
	remainingNeeded := limit  // How many more items we need (PHP's $need_count)

	// Main loop - keep fetching until we have enough results or run out of data
	for remainingNeeded > 0 {
		// Determine what IDs to check in this iteration
		var idsToCheck []int64
		var fetchSource string

		// --- Similar to PHP parent::fetch($start, $limit) - get items from cache or DB ---
		if nextFetchOffset < len(cr.cachedIDs) {
			// Get slice from cached IDs (similar to ListResult.ids() in PHP)
			fetchSource = "Cache"

			// Adjust limit if it would exceed cached IDs
			availableCachedCount := len(cr.cachedIDs) - nextFetchOffset
			actualFetchLimit := nextFetchLimit
			if actualFetchLimit > availableCachedCount {
				actualFetchLimit = availableCachedCount
			}

			endOffset := nextFetchOffset + actualFetchLimit
			idsToCheck = cr.cachedIDs[nextFetchOffset:endOffset]
			log.Printf("Fetch Iteration: Using %d cached IDs [%d:%d], need %d more results (source: %s)",
				len(idsToCheck), nextFetchOffset, endOffset, remainingNeeded, fetchSource)

		} else if len(cr.cachedIDs) == cacheListCountLimit {
			// Cache list might be truncated; fetch next batch directly from DB
			fetchSource = "Database"

			// Get IDs directly from DB with proper offset and limit
			var dbErr error
			idsToCheck, dbErr = cr._fetch_ids_from_db(nextFetchOffset, nextFetchLimit)
			if dbErr != nil {
				return nil, fmt.Errorf("failed to fetch additional IDs from database: %w", dbErr)
			}

			if len(idsToCheck) == 0 {
				// No more results in DB
				log.Printf("Fetch Iteration: No more IDs from database (source: %s)", fetchSource)
				break
			}

			log.Printf("Fetch Iteration: Fetched %d IDs from database, need %d more results (source: %s)",
				len(idsToCheck), remainingNeeded, fetchSource)

		} else {
			// Cache wasn't truncated and we're past its end - no more results
			log.Printf("Fetch Iteration: Reached end of cached results (%d IDs)", len(cr.cachedIDs))
			break
		}

		if len(idsToCheck) == 0 {
			break // Should not happen with above checks, but safety first
		}

		// --- Fetch models for IDs (PHP does this via parent::fetch return) ---
		models, err := cr.thing.ByIDs(idsToCheck /* preloads */)
		if err != nil {
			log.Printf("WARN: Fetch Iteration: ByIDs failed: %v", err)
			deletedOrMissingIDs = append(deletedOrMissingIDs, idsToCheck...)
			nextFetchOffset += len(idsToCheck) // PHP would still advance $start
			nextFetchLimit = remainingNeeded   // Set next limit to remaining need
			continue
		}

		// --- Process and filter fetched models (similar to PHP fetch foreach loop) ---
		processedFromBatch := 0
		for _, id := range idsToCheck {
			processedFromBatch++

			model, found := models[id]
			if !found {
				// ID exists but model not returned (e.g., deleted between queries)
				// PHP: $key = array_search($_id, $this->data); if ($key !== False) { unset($this->data[$key]); }
				deletedOrMissingIDs = append(deletedOrMissingIDs, id)
				continue
			}

			basePtr := getBaseModelPtr(model)
			if basePtr == nil {
				continue // Skip invalid models
			}

			// PHP: if ($item->keep_item()) { ... } else { ... }
			if basePtr.KeepItem() {
				finalResults = append(finalResults, model)
				remainingNeeded--
				if remainingNeeded == 0 {
					break // Got all we need
				}
			} else {
				// PHP: $deleted[] = $_id
				deletedOrMissingIDs = append(deletedOrMissingIDs, id)
			}
		}

		// --- Prepare for next iteration (similar to PHP calculation after foreach) ---
		// PHP: $cached_count = count($this->data)
		// PHP: if ($cached_count) $limit = $cached_count > ($start + $limit) ? $limit : $cached_count - $start
		// PHP: if ($need_count == 0 || $limit <= 0) break

		// Advance offset by how many we processed this iteration
		nextFetchOffset += processedFromBatch

		// Set next limit to how many more we need
		nextFetchLimit = remainingNeeded

		// PHP checks if we need more and if there's anything left to fetch
		if remainingNeeded == 0 {
			log.Printf("Fetch Iteration: Collected all %d needed items", limit)
			break
		}

		log.Printf("Fetch Iteration: Got %d/%d items so far, need %d more. Next fetch: offset=%d, limit=%d",
			len(finalResults), limit, remainingNeeded, nextFetchOffset, nextFetchLimit)
	}

	// --- Clean up deleted/missing IDs from cache if needed ---
	if len(deletedOrMissingIDs) > 0 {
		uniqueDeletedIDsMap := make(map[int64]struct{})
		uniqueDeletedIDs := make([]int64, 0, len(deletedOrMissingIDs))
		for _, id := range deletedOrMissingIDs {
			if _, exists := uniqueDeletedIDsMap[id]; !exists {
				uniqueDeletedIDsMap[id] = struct{}{}
				uniqueDeletedIDs = append(uniqueDeletedIDs, id)
			}
		}

		log.Printf("Fetch: Found %d deleted/missing IDs, will update cache", len(uniqueDeletedIDs))

		// TODO: PHP: $this->delete($deleted)
		// TODO: Implement and call cr.deleteIDsFromCacheList(uniqueDeletedIDs)
	}

	log.Printf("Fetch: Returning %d/%d requested results", len(finalResults), limit)
	return finalResults, nil
}

// All retrieves all records matching the query.
// It first gets the total count and then fetches all records using Fetch.
func (cr *CachedResult[T]) All() ([]*T, error) {
	// 0. Check if already loaded
	if cr.hasLoadedAll {
		return cr.all, nil
	}

	// 1. Get the total count
	count, err := cr.Count()
	if err != nil {
		return nil, fmt.Errorf("failed to get count for All(): %w", err)
	}

	// 2. If count is zero, return empty slice
	if count == 0 {
		log.Printf("All: Count is zero, returning empty slice.")
		return []*T{}, nil
	}

	// 3. Fetch all records using Fetch(0, count)
	log.Printf("All: Fetching %d records...", count)
	results, err := cr.Fetch(0, int(count))
	if err != nil {
		return nil, fmt.Errorf("failed to fetch %d records for All(): %w", count, err)
	}

	log.Printf("All: Successfully fetched %d records.", len(results))
	// Store the results and mark as loaded
	cr.all = results
	cr.hasLoadedAll = true
	return results, nil
}
