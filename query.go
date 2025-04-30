package thing

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"thing/internal/cache"
	"thing/internal/sql"
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
	params         cache.QueryParams
	cachedIDs      []int64
	cachedCount    int64
	hasLoadedIDs   bool
	hasLoadedCount bool
	hasLoadedAll   bool
	all            []*T
}

// --- Thing Method for Querying ---

// Query prepares a query based on QueryParams and returns a *CachedResult[T] for lazy execution.
// The actual database query happens when Count() or Fetch() is called on the result.
// It returns the CachedResult instance and a nil error, assuming basic validation passed.
// Error handling for query execution is done within CachedResult methods.
func (t *Thing[T]) Query(params cache.QueryParams) (*CachedResult[T], error) {
	// TODO: Add validation for params if necessary?
	return &CachedResult[T]{
		thing:  t,
		params: params,
		// cachedIDs, cachedCount, hasLoadedIDs, hasLoadedCount, hasLoadedAll, all initialized to zero values
	}, nil
}

// --- CachedResult Methods ---

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
	} else if errors.Is(cacheErr, cache.ErrNotFound) {
		// Cache Miss
		log.Printf("CACHE MISS (Count): Key %s not found.", cacheKey)
		// Fall through to DB fetch
	}

	// 4. Cache miss or error, query database
	// Assuming DBAdapter has a GetCount method
	// Add the soft delete condition implicitly here, *unless* IncludeDeleted is true
	countParams := cr.params
	if !countParams.IncludeDeleted { // Check the flag
		if countParams.Where != "" {
			countParams.Where = fmt.Sprintf("(%s) AND \"deleted\" = false", countParams.Where)
		} else {
			countParams.Where = "\"deleted\" = false"
		}
	}
	dbCount, dbErr := cr.thing.db.GetCount(cr.thing.ctx, cr.thing.info, countParams)
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

	// Register the count key
	cache.GlobalCacheIndex.RegisterQuery(cr.thing.info.TableName, cacheKey, cr.params)

	return cr.cachedCount, nil
}

// WithDeleted returns a new CachedResult instance that will include
// soft-deleted records in its results.
func (cr *CachedResult[T]) WithDeleted() *CachedResult[T] {
	// Create a shallow copy of the original CachedResult
	newCr := *cr
	// Copy the params to avoid modifying the original
	newParams := cr.params
	newParams.IncludeDeleted = true
	// Set the modified params on the new CachedResult
	newCr.params = newParams
	// Reset loaded state flags, as the query parameters have changed
	newCr.hasLoadedCount = false
	newCr.hasLoadedIDs = false
	newCr.cachedIDs = nil
	newCr.cachedCount = 0
	newCr.hasLoadedAll = false
	newCr.all = nil

	return &newCr
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
	if cr.thing == nil || cr.thing.db == nil || cr.thing.info == nil {
		return nil, errors.New("_fetch_ids_from_db: CachedResult not properly initialized")
	}

	// Build the SQL query with pagination
	// Pass includeDeleted flag via params to the builder
	query, args := sql.BuildSelectIDsSQL(cr.thing.info.TableName, cr.thing.info.PkName, cr.params)
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

// _fetch_data attempts to load up to `cacheListCountLimit` valid IDs from cache or database.
// It filters out soft-deleted items before caching.
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

	// 2. Check Cache directly using GetQueryIDs
	cachedIDs, idsCacheErr := cr.thing.cache.GetQueryIDs(cr.thing.ctx, listCacheKey)
	if idsCacheErr == nil {
		log.Printf("CACHE HIT: List Key: %s (%d IDs)", listCacheKey, len(cachedIDs))
		return cachedIDs, nil // Cache hit with actual IDs or empty slice
	}

	// 3. Handle Cache Miss or Error
	if !errors.Is(idsCacheErr, ErrNotFound) {
		// Log unexpected errors but treat as cache miss
		log.Printf("WARN: Cache GetQueryIDs error for list key %s: %v. Proceeding to DB query.", listCacheKey, idsCacheErr)
	}
	log.Printf("CACHE MISS: List Key: %s (Error: %v)", listCacheKey, idsCacheErr)

	// 4. Cache Miss: Query Database and filter results
	// 4a. Prepare to collect valid, non-deleted IDs
	validIDs := make([]int64, 0, cacheListCountLimit)
	currentOffset := 0
	batchSize := int(float64(cacheListCountLimit) * 1.5) // Larger batch for efficiency

	// 4b. Loop until we have enough IDs or no more results, with max iteration protection
	const maxIterations = 20 // Prevent excessive looping when many records are soft-deleted
	iterationCount := 0

	for len(validIDs) < cacheListCountLimit {
		iterationCount++
		if iterationCount > maxIterations {
			log.Printf("WARN: Reached maximum number of iterations (%d) in _fetch_data. Returning %d valid IDs found so far.",
				maxIterations, len(validIDs))
			break
		}

		// Fetch a batch of IDs from DB
		batchIDs, dbErr := cr._fetch_ids_from_db(currentOffset, batchSize)
		if dbErr != nil {
			return nil, fmt.Errorf("failed to fetch IDs from database: %w", dbErr)
		}

		// If no more IDs, break
		if len(batchIDs) == 0 {
			break
		}

		// Get models for these IDs to check KeepItem()
		models, modelsErr := cr.thing.ByIDs(batchIDs)
		if modelsErr != nil {
			log.Printf("WARN: Failed to fetch models for IDs: %v", modelsErr)
			// Continue with next batch
			currentOffset += len(batchIDs)
			continue
		}

		// Filter models based on KeepItem()
		for _, id := range batchIDs {
			// If we have enough valid IDs, stop
			if len(validIDs) >= cacheListCountLimit {
				break
			}

			model, found := models[id]
			if !found {
				continue // Model not found, skip
			}

			basePtr := getBaseModelPtr(model)
			if basePtr == nil {
				continue // Invalid model, skip
			}

			// Keep the ID if either IncludeDeleted is true OR the item is not soft-deleted
			if cr.params.IncludeDeleted || basePtr.KeepItem() {
				validIDs = append(validIDs, id)
			}
		}

		// Advance offset for next batch
		currentOffset += len(batchIDs)

		// If this batch returned fewer than expected, no more results
		if len(batchIDs) < batchSize {
			break
		}
	}

	// 5. Handle filtered DB results and cache appropriately
	log.Printf("DB HIT: List Key: %s, Found %d valid IDs after filtering. Caching IDs.", listCacheKey, len(validIDs))
	// Use the helper function to store the list (works for empty lists too)
	// cacheSetErr := cache.SetCachedListIDs(cr.thing.ctx, cr.thing.cache, listCacheKey, validIDs, globalCacheTTL) // Use helper
	cacheSetErr := cr.thing.cache.SetQueryIDs(cr.thing.ctx, listCacheKey, validIDs, globalCacheTTL)
	if cacheSetErr != nil {
		log.Printf("WARN: Failed to cache list IDs for key %s: %v", listCacheKey, cacheSetErr) // Log remains the same
	}
	// Register the list key
	cache.GlobalCacheIndex.RegisterQuery(cr.thing.info.TableName, listCacheKey, cr.params)

	// If fetched count <= limit, update Count cache as well (handles count=0 correctly)
	if len(validIDs) <= cacheListCountLimit { // Changed to <= for clarity, but < also works for count 0
		countCacheKey, keyErr := cr.generateCountCacheKey()
		if keyErr == nil {
			countStr := strconv.FormatInt(int64(len(validIDs)), 10) // Correctly gets "0" if len is 0
			countSetErr := cr.thing.cache.Set(cr.thing.ctx, countCacheKey, countStr, globalCacheTTL)
			if countSetErr != nil {
				log.Printf("WARN: Failed to update count cache (key: %s) after list fetch: %v", countCacheKey, countSetErr)
			} else {
				log.Printf("Updated count cache (key: %s) with count %d after list fetch", countCacheKey, len(validIDs))
			}
			// Register the count key
			cache.GlobalCacheIndex.RegisterQuery(cr.thing.info.TableName, countCacheKey, cr.params)
		} else {
			log.Printf("WARN: Failed to generate count cache key for update after list fetch: %v", keyErr)
		}
	}
	return validIDs, nil // Return filtered valid IDs (or empty slice)
}

// invalidateCache invalidates both the list and count cache for the current query./ This is used when we detect inconsistencies in the cached data.
func (cr *CachedResult[T]) invalidateCache() error {
	if cr.thing == nil || cr.thing.cache == nil {
		return errors.New("invalidateCache: CachedResult not properly initialized")
	}

	// 1. Invalidate list cache
	listCacheKey, err := cr.generateListCacheKey()
	if err != nil {
		return fmt.Errorf("failed to generate list cache key for invalidation: %w", err)
	}

	deleteErr := cr.thing.cache.Delete(cr.thing.ctx, listCacheKey)
	if deleteErr != nil && !errors.Is(deleteErr, ErrNotFound) {
		log.Printf("WARN: Failed to invalidate list cache for key %s: %v", listCacheKey, deleteErr)
		// Continue despite error, try to invalidate count cache as well
	} else {
		log.Printf("Cache invalidated for list key: %s", listCacheKey)
	}

	// 2. Invalidate count cache
	countCacheKey, err := cr.generateCountCacheKey()
	if err != nil {
		return fmt.Errorf("failed to generate count cache key for invalidation: %w", err)
	}

	deleteErr = cr.thing.cache.Delete(cr.thing.ctx, countCacheKey)
	if deleteErr != nil && !errors.Is(deleteErr, ErrNotFound) {
		log.Printf("WARN: Failed to invalidate count cache for key %s: %v", countCacheKey, deleteErr)
	} else {
		log.Printf("Cache invalidated for count key: %s", countCacheKey)
	}

	// 3. Reset in-memory cache state to trigger reload on next access
	cr.hasLoadedIDs = false
	cr.hasLoadedCount = false
	cr.cachedIDs = nil
	cr.cachedCount = 0

	return nil
}

// Fetch returns a subset of records starting from the given offset with the specified limit.
// It filters out soft-deleted items and triggers cache updates if inconsistencies are found.
// This implementation closely follows the PHP CachedResult.fetch() logic:
// - It iteratively fetches batches from cache or DB
// - It filters items using KeepItem()
// - It dynamically calculates how many more items to fetch based on filtering results
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

	// Get total count for this query to determine if there are more records to fetch
	totalCount, err := cr.Count()
	if err != nil {
		log.Printf("WARN: Failed to get total count for query: %v. Will proceed with available IDs.", err)
		// Even if count fails, we can still use cachedIDs
		totalCount = int64(len(cr.cachedIDs))
	}

	// --- Setup for iterative fetching (similar to PHP CachedResult.fetch implementation) ---
	finalResults := make([]*T, 0, limit)
	nextFetchOffset := offset // Starting offset (PHP's $start)
	nextFetchLimit := limit   // Initial fetch limit (PHP's $limit)
	remainingNeeded := limit  // How many more items we need (PHP's $need_count)
	cacheInvalidated := false // Flag to track if cache was invalidated

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

		} else if int64(nextFetchOffset) < totalCount {
			// Still have more data in the database according to total count
			fetchSource = "Database"

			// Get IDs directly from DB with proper offset and limit
			var dbErr error
			idsToCheck, dbErr = cr._fetch_ids_from_db(nextFetchOffset, nextFetchLimit)
			if dbErr != nil {
				return nil, fmt.Errorf("failed to fetch additional IDs from database: %w", dbErr)
			}

			if len(idsToCheck) == 0 {
				// No more results in DB despite what count says
				log.Printf("Fetch Iteration: No more IDs from database (source: %s)", fetchSource)
				break
			}

			log.Printf("Fetch Iteration: Fetched %d IDs from database, need %d more results (source: %s)",
				len(idsToCheck), remainingNeeded, fetchSource)

		} else {
			// Reached the end of total records
			log.Printf("Fetch Iteration: Reached end of all results (%d total)", totalCount)
			break
		}

		if len(idsToCheck) == 0 {
			break // Should not happen with above checks, but safety first
		}

		// --- Fetch models for IDs (PHP does this via parent::fetch return) ---
		// Pass preloads from the query params to ByIDs to support relationship loading
		models, err := cr.thing.ByIDs(idsToCheck, cr.params.Preloads...)
		if err != nil {
			log.Printf("WARN: Fetch Iteration: ByIDs failed: %v", err)

			// If fetching from cache failed, invalidate cache
			if fetchSource == "Cache" && !cacheInvalidated {
				log.Printf("Invalidating cache due to ByIDs failure for cached IDs")
				invErr := cr.invalidateCache()
				if invErr != nil {
					log.Printf("WARN: Failed to invalidate cache: %v", invErr)
				}
				cacheInvalidated = true

				// Skip this batch and continue
				nextFetchOffset += len(idsToCheck)
				continue
			}

			nextFetchOffset += len(idsToCheck) // Still advance $start
			nextFetchLimit = remainingNeeded   // Set next limit to remaining need
			continue
		}

		// Flag to track if any issue was found with cached IDs
		anyIssueFound := false

		// --- Process and filter fetched models (similar to PHP fetch foreach loop) ---
		processedFromBatch := 0
		for _, id := range idsToCheck {
			processedFromBatch++

			model, found := models[id]
			if !found {
				// ID exists but model not returned (e.g., deleted between queries)
				if fetchSource == "Cache" {
					anyIssueFound = true
				}
				continue
			}

			basePtr := getBaseModelPtr(model)
			if basePtr == nil {
				continue // Skip invalid models
			}

			// Check if item should be kept based on IncludeDeleted flag
			if cr.params.IncludeDeleted || basePtr.KeepItem() {
				finalResults = append(finalResults, model)
				remainingNeeded--
				if remainingNeeded == 0 {
					break // Got all we need
				}
			} else if fetchSource == "Cache" {
				// Item is soft-deleted (KeepItem is false),
				// we are NOT including deleted items,
				// AND it came from cache -> inconsistency
				anyIssueFound = true
			}
		}

		// If any issues found with cached IDs and cache hasn't been invalidated yet
		if fetchSource == "Cache" && anyIssueFound && !cacheInvalidated {
			log.Printf("Invalidating cache due to inconsistencies found in cached IDs")
			invErr := cr.invalidateCache()
			if invErr != nil {
				log.Printf("WARN: Failed to invalidate cache: %v", invErr)
			}
			cacheInvalidated = true

			// We continue with the results we have so far, and possibly fetch more
			// from the database in the next iteration
		}

		// --- Prepare for next iteration (similar to PHP calculation after foreach) ---
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
