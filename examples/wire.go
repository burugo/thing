//go:build ignore || wireinject
// +build ignore wireinject

package main

import (
	"log"

	"github.com/google/wire"
	// "github.com/redis/go-redis/v9" // No longer needed here

	// Import necessary packages
	"thing"
	"thing/cache/redis" // Import the new redis cache package
	"thing/drivers/sqlite"
)

// --- Redis Cache Implementation Removed (Moved to thing/cache/redis) ---

// --- Providers ---

// provideDBAdapter creates the DB adapter. Includes cleanup.
func provideDBAdapter(dsn string) (thing.DBAdapter, func(), error) {
	// Keep using SQLite for the DB part
	db, err := sqlite.NewSQLiteAdapter(dsn)
	if err != nil {
		return nil, nil, err
	}
	cleanup := func() {
		if err := db.Close(); err != nil {
			log.Printf("Error closing DB adapter: %v", err)
		}
	}
	return db, cleanup, nil
}

// provideRedisCacheClient creates the Redis cache client using the thing/cache/redis package.
// Includes cleanup logic.
func provideRedisCacheClient() (thing.CacheClient, func(), error) {
	// Default Redis address
	redisOpts := redis.Options{
		Addr: "localhost:6379",
		// Password: "", // Set if needed
		// DB:       0, // Set if needed
	}
	// In a real app, get this from config/env vars
	// Use the constructor from the imported package
	return redis.NewClient(redisOpts)
}

// provideUserRepo creates the User repository.
func provideUserRepo(db thing.DBAdapter, cache thing.CacheClient) (*thing.Thing[User], error) {
	// We directly use New, injecting the dependencies managed by Wire
	return thing.New[User](db, cache)
}

// provideBookRepo creates the Book repository.
func provideBookRepo(db thing.DBAdapter, cache thing.CacheClient) (*thing.Thing[Book], error) {
	// We directly use New, injecting the dependencies managed by Wire
	return thing.New[Book](db, cache)
}

// --- Injector ---

// initializeApplication creates the main Application struct with dependencies.
func initializeApplication(dsn string) (*Application, func(), error) {
	wire.Build(
		provideDBAdapter,
		provideRedisCacheClient, // Use Redis cache provider
		provideUserRepo,
		provideBookRepo,
		// Wire will automatically use the providers to fill the fields
		wire.Struct(new(Application), "*"),
	)
	// The return values below are placeholders;
	// Wire generates the actual implementation.
	// The generated code will include cleanup for both DB and Cache.
	return nil, nil, nil
}
