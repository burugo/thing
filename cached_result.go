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
		return 0, errors.New("CachedResult not properly initialized with Thing instance")
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
	ctx := cr.thing.ctx // Use context from Thing instance
	cacheValStr, cacheErr := cr.thing.cache.Get(ctx, cacheKey)
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
			_ = cr.thing.cache.Delete(ctx, cacheKey)
		}
	} else if !errors.Is(cacheErr, ErrNotFound) {
		// Unexpected cache error, log but proceed to DB
		log.Printf("WARN: Cache Get error for count key %s: %v", cacheKey, cacheErr)
	}
	log.Printf("CACHE MISS: Count Key: %s", cacheKey)

	// 4. Cache miss or error, query database
	// Assuming DBAdapter has a GetCount method
	dbCount, dbErr := cr.thing.db.GetCount(ctx, cr.thing.info, cr.params)
	if dbErr != nil {
		log.Printf("DB ERROR: Count query failed: %v", dbErr)
		return 0, fmt.Errorf("database count query failed: %w", dbErr)
	}

	// 5. Store result in memory and cache
	log.Printf("DB HIT: Count Key: %s, Count: %d", cacheKey, dbCount)
	cr.cachedCount = dbCount
	cr.hasLoadedCount = true

	cacheSetErr := cr.thing.cache.Set(ctx, cacheKey, strconv.FormatInt(dbCount, 10), DefaultCacheDuration) // Use appropriate TTL
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

// _fetch_data attempts to load the first `cacheListCountLimit` IDs from cache or database.
func (cr *CachedResult[T]) _fetch_data() ([]int64, error) {
	if cr.thing == nil || cr.thing.cache == nil || cr.thing.db == nil {
		return nil, errors.New("_fetch_data: CachedResult not properly initialized")
	}
	ctx := cr.thing.ctx

	// 1. Generate List Cache Key
	listCacheKey, err := cr.generateListCacheKey()
	if err != nil {
		log.Printf("Error generating list cache key: %v", err)
		return nil, fmt.Errorf("failed to generate cache key for list: %w", err)
	}

	// 2. Check Cache (Handle NoneResult marker)
	// 2a. Try generic Get first for NoneResult marker
	cacheValStr, genericCacheErr := cr.thing.cache.Get(ctx, listCacheKey)
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
	cachedIDs, idsCacheErr := cr.thing.cache.GetQueryIDs(ctx, listCacheKey)
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
	query, args := buildSelectIDsSQL(cr.thing.info, cr.params) // Uses existing helper
	queryWithLimit := fmt.Sprintf("%s LIMIT %d", query, cacheListCountLimit)

	var fetchedIDs []int64
	dbErr := cr.thing.db.Select(ctx, &fetchedIDs, queryWithLimit, args...)
	if dbErr != nil {
		log.Printf("DB ERROR: Fetching initial IDs failed: %v", dbErr)
		return nil, fmt.Errorf("database query for initial IDs failed: %w", dbErr)
	}

	// 4. Handle DB results and cache appropriately
	if len(fetchedIDs) == 0 {
		// Store NoneResult marker in cache
		log.Printf("DB HIT (Zero Results): List Key: %s. Caching NoneResult.", listCacheKey)
		cacheSetErr := cr.thing.cache.Set(ctx, listCacheKey, NoneResult, NoneResultCacheDuration)
		if cacheSetErr != nil {
			log.Printf("WARN: Failed to cache NoneResult for key %s: %v", listCacheKey, cacheSetErr)
		}
		// Also ensure count cache is updated or set to 0 if possible
		countCacheKey, keyErr := cr.generateCountCacheKey()
		if keyErr == nil {
			countSetErr := cr.thing.cache.Set(ctx, countCacheKey, "0", DefaultCacheDuration) // Use longer duration for count? Or NoneResult duration?
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
		cacheSetErr := cr.thing.cache.SetQueryIDs(ctx, listCacheKey, fetchedIDs, DefaultCacheDuration)
		if cacheSetErr != nil {
			log.Printf("WARN: Failed to cache list IDs for key %s: %v", listCacheKey, cacheSetErr)
		}

		// If fetched count < limit, update Count cache as well
		if len(fetchedIDs) < cacheListCountLimit {
			countCacheKey, keyErr := cr.generateCountCacheKey()
			if keyErr == nil {
				countStr := strconv.FormatInt(int64(len(fetchedIDs)), 10)
				countSetErr := cr.thing.cache.Set(ctx, countCacheKey, countStr, DefaultCacheDuration)
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
func (cr *CachedResult[T]) Fetch(offset, limit int) ([]*T, error) {
	if cr.thing == nil || cr.thing.cache == nil || cr.thing.db == nil {
		return nil, errors.New("Fetch: CachedResult not properly initialized")
	}
	ctx := cr.thing.ctx

	// 1. Ensure IDs are loaded (from cache or DB)
	if err := cr._fetch(); err != nil {
		return nil, fmt.Errorf("failed to fetch underlying IDs: %w", err)
	}

	// Handle case where query yielded no results
	if len(cr.cachedIDs) == 0 {
		return []*T{}, nil
	}

	// 2. Determine required ID range
	start := offset
	if start < 0 {
		start = 0
	}
	// Check if offset is beyond the total number of *cached* IDs
	// Note: This doesn't necessarily mean offset is beyond the *total* results if count > cacheListCountLimit
	if start >= len(cr.cachedIDs) && len(cr.cachedIDs) < cacheListCountLimit {
		// We know the total count is exactly len(cr.cachedIDs) because we fetched less than the limit
		return []*T{}, nil
	}
	// If offset >= cache limit, we might need to fetch directly (see step 3b)

	end := start + limit

	// 3a. If requested range is within cached IDs (simple case)
	if end <= len(cr.cachedIDs) {
		idsToFetch := cr.cachedIDs[start:end]
		if len(idsToFetch) == 0 {
			return []*T{}, nil
		}
		log.Printf("Fetch: Using cached IDs range [%d:%d] (%d IDs)", start, end, len(idsToFetch))
		// Use ByIDs for efficient fetching
		recordMap, err := cr.thing.ByIDs(idsToFetch) // Assumes ByIDs exists and works
		if err != nil {
			return nil, fmt.Errorf("failed to fetch records by cached IDs: %w", err)
		}
		// Order results according to idsToFetch
		orderedRecords := make([]*T, 0, len(idsToFetch))
		for _, id := range idsToFetch {
			if record, ok := recordMap[id]; ok {
				orderedRecords = append(orderedRecords, record)
			}
		}
		return orderedRecords, nil
	}

	// 3b. Requested range exceeds cached IDs OR offset is large
	// Requires fetching directly from DB with pagination.
	// Initial simpler implementation: Query directly for the requested page.
	log.Printf("Fetch: Range [%d:%d] exceeds cached IDs (%d). Fetching page directly from DB.", start, end, len(cr.cachedIDs))

	// We need a way to select a specific page of *full objects* directly.
	// Assume a DBAdapter method like SelectPaginated exists.
	// SelectPaginated(ctx context.Context, dest interface{}, info *modelInfo, params QueryParams, offset int, limit int) error

	var results []*T
	// Assumes SelectPaginated exists on DBAdapter interface
	err := cr.thing.db.SelectPaginated(ctx, &results, cr.thing.info, cr.params, offset, limit)
	if err != nil {
		// Don't return ErrNotFound here, Select should return empty slice on no rows
		log.Printf("DB ERROR: Fetching page offset=%d, limit=%d failed: %v", offset, limit, err)
		return nil, fmt.Errorf("database query for page failed (offset=%d, limit=%d): %w", offset, limit, err)
	}

	// **** ADDED: Apply preloads if necessary ****
	if len(cr.params.Preloads) > 0 && len(results) > 0 {
		log.Printf("Fetch: Applying preloads (%v) to directly fetched page", cr.params.Preloads)
		for _, preloadName := range cr.params.Preloads {
			if preloadErr := cr.thing.preloadRelations(ctx, results, preloadName); preloadErr != nil {
				// Log the error but return the results fetched so far
				log.Printf("Warning: failed to apply preload '%s' to directly fetched page: %v", preloadName, preloadErr)
				// Decide whether to return error or partial results. Returning partial for now.
				// return nil, fmt.Errorf("failed to apply preload '%s': %w", preloadName, preloadErr)
			}
		}
	}
	// **** END ADDED SECTION ****

	log.Printf("Fetch: Successfully fetched page offset=%d, limit=%d directly from DB (%d results)", offset, limit, len(results))
	return results, nil
}

// All retrieves all records matching the query.
// It first gets the total count and then fetches all records using Fetch.
func (cr *CachedResult[T]) All() ([]*T, error) {
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
	return results, nil
}
