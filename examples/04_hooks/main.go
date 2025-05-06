package main

import (
	"context"
	"errors"
	"io"
	"log"
	"os"

	"github.com/burugo/thing"
	// "thing/examples/models" // Old import
	"github.com/burugo/thing/drivers/cache/redis"
	"github.com/burugo/thing/drivers/db/sqlite"
	goRedis "github.com/redis/go-redis/v9"
)

// User definition is now in models.go within the same package

func main() {
	ctx := context.Background()
	log.Println("--- Starting Hooks Example ---")

	// --- Database Setup (SQLite) ---
	dbFile := "hooks_example.db"
	os.Remove(dbFile) // Clean start
	dbAdapter, err := sqlite.NewSQLiteAdapter(dbFile)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer dbAdapter.Close()

	// Auto-create schema (assuming models.User exists)
	// _, err = dbAdapter.Exec(ctx, models.User{}.CreateTableSQL(), nil) // Old way
	_, err = dbAdapter.Exec(ctx, User{}.CreateTableSQL(), nil) // Use User directly from package main
	if err != nil {
		log.Fatalf("Failed to create user table: %v", err)
	}
	log.Println("Database initialized and schema created.")

	// --- Cache Setup (Redis) ---
	// Note: Requires a running Redis instance
	redisAddr := "127.0.0.1:6379" // Use IP address instead of hostname
	opts := redis.Options{Addr: redisAddr}
	cacheClient, err := redis.NewClient(nil, &opts)
	if err != nil {
		log.Fatalf("Failed to create redis cache client (opts): %v", err)
	}
	defer cacheClient.(io.Closer).Close()
	log.Println("Cache client initialized (Redis, via opts).")

	// 方式二：传入已存在的 *redis.Client
	rdb := goRedis.NewClient(&goRedis.Options{
		Addr:     redisAddr,
		Password: "",
		DB:       0,
	})
	cacheClient2, err := redis.NewClient(rdb, nil)
	if err != nil {
		log.Fatalf("Failed to create redis cache client (existing *redis.Client): %v", err)
	}
	defer cacheClient2.(io.Closer).Close()
	log.Println("Cache client initialized (Redis, via existing *redis.Client).")

	// --- Thing ORM Initialization ---
	// thingOrm, err := thing.New[models.User](dbAdapter, cacheClient, nil) // Old call
	thingOrm, err := thing.New[*User](dbAdapter, cacheClient) // Corrected call
	if err != nil {
		log.Fatalf("Failed to initialize Thing ORM: %v", err)
	}
	log.Println("Thing ORM initialized for User model.")

	// --- Register Hooks ---
	log.Println("Registering event listeners...")

	// BeforeCreate: Log and potentially modify
	thing.RegisterListener(thing.EventTypeBeforeCreate, func(ctx context.Context, eventType thing.EventType, model interface{}, eventData interface{}) error {
		user, ok := model.(*User) // Use local User type
		if !ok {
			return nil
		} // Should not happen
		log.Printf("[HOOK - %s] User ID: %d, Name: %s, Email: %s", eventType, user.ID, user.Name, user.Email)
		if user.Email == "invalid@example.com" {
			log.Printf("[HOOK - %s] Modifying email from 'invalid@example.com' to 'valid_hook@example.com'", eventType)
			user.Email = "valid_hook@example.com" // Modify data before creation
		}
		return nil
	})

	// AfterCreate: Log the assigned ID
	thing.RegisterListener(thing.EventTypeAfterCreate, func(ctx context.Context, eventType thing.EventType, model interface{}, eventData interface{}) error {
		user, ok := model.(*User) // Use local User type
		if !ok {
			return nil
		}
		log.Printf("[HOOK - %s] User ID: %d assigned! Name: %s", eventType, user.ID, user.Name)
		return nil
	})

	// BeforeSave: Log before any save (create or update)
	thing.RegisterListener(thing.EventTypeBeforeSave, func(ctx context.Context, eventType thing.EventType, model interface{}, eventData interface{}) error {
		user, ok := model.(*User) // Use local User type
		if !ok {
			return nil
		}
		log.Printf("[HOOK - %s] User ID: %d, Name: %s, Email: %s. IsNew: %t", eventType, user.ID, user.Name, user.Email, user.IsNewRecord())
		if user.Name == "AbortMe" {
			log.Printf("[HOOK - %s] Aborting save because name is 'AbortMe'", eventType)
			return errors.New("save aborted by BeforeSave hook")
		}
		return nil
	})

	// AfterSave: Log changed fields on update
	thing.RegisterListener(thing.EventTypeAfterSave, func(ctx context.Context, eventType thing.EventType, model interface{}, eventData interface{}) error {
		user, ok := model.(*User) // Use local User type
		if !ok {
			return nil
		}
		changedFields, _ := eventData.(map[string]interface{}) // Can be nil on create
		log.Printf("[HOOK - %s] User ID: %d saved. Changed fields: %v", eventType, user.ID, changedFields)
		return nil
	})

	// BeforeDelete: Log before deletion
	thing.RegisterListener(thing.EventTypeBeforeDelete, func(ctx context.Context, eventType thing.EventType, model interface{}, eventData interface{}) error {
		user, ok := model.(*User) // Use local User type
		if !ok {
			return nil
		}
		log.Printf("[HOOK - %s] Preparing to delete User ID: %d, Name: %s", eventType, user.ID, user.Name)
		return nil
	})

	// AfterDelete: Log after deletion
	thing.RegisterListener(thing.EventTypeAfterDelete, func(ctx context.Context, eventType thing.EventType, model interface{}, eventData interface{}) error {
		user, ok := model.(*User) // Use local User type
		if !ok {
			return nil
		}
		log.Printf("[HOOK - %s] Successfully deleted User ID: %d, Name: %s", eventType, user.ID, user.Name)
		return nil
	})

	log.Println("Listeners registered.")

	// --- Demonstrate Hooks ---

	// 1. Create a user (triggers BeforeSave, BeforeCreate, AfterCreate, AfterSave)
	log.Println("--- Creating User 'Alice' ---")
	alice := &User{Name: "Alice", Email: "alice@example.com"} // Use local User type
	err = thingOrm.Save(alice)                                // Pass pointer
	if err != nil {
		log.Printf("Error saving Alice: %v", err)
	} else {
		log.Printf("Alice saved successfully. ID: %d", alice.ID)
	}

	// 2. Create a user with email modification hook
	log.Println("--- Creating User 'Bob' with invalid email ---")
	bob := &User{Name: "Bob", Email: "invalid@example.com"} // Use local User type
	err = thingOrm.Save(bob)                                // Pass pointer
	if err != nil {
		log.Printf("Error saving Bob: %v", err)
	} else {
		log.Printf("Bob saved successfully. ID: %d, Email: %s", bob.ID, bob.Email) // Email should be modified
	}

	// 3. Attempt to create user that triggers abort hook
	log.Println("--- Attempting to create User 'AbortMe' ---")
	abortUser := &User{Name: "AbortMe", Email: "abort@example.com"} // Use local User type
	err = thingOrm.Save(abortUser)                                  // Pass pointer
	if err != nil {
		log.Printf("Expected error saving AbortMe: %v", err) // Error expected here
	} else {
		log.Printf("AbortMe saved unexpectedly! ID: %d", abortUser.ID)
	}

	// 4. Update Alice (triggers BeforeSave, AfterSave)
	if alice.ID > 0 {
		log.Println("--- Updating User 'Alice' ---")
		alice.Email = "alice_updated@example.com"
		err = thingOrm.Save(alice) // Pass pointer
		if err != nil {
			log.Printf("Error updating Alice: %v", err)
		} else {
			log.Printf("Alice updated successfully.")
		}
	}

	// 5. Delete Bob (triggers BeforeDelete, AfterDelete)
	if bob.ID > 0 {
		log.Println("--- Deleting User 'Bob' ---")
		err = thingOrm.Delete(bob) // Pass pointer
		if err != nil {
			log.Printf("Error deleting Bob: %v", err)
		} else {
			log.Printf("Bob deleted successfully.")
		}
	}

	log.Println("--- Hooks Example Finished ---")
}
