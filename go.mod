module thing

go 1.24.2

require (
	// github.com/google/wire v0.6.0 // Dependency removed, examples/wire.go will no longer compile
	github.com/jmoiron/sqlx v1.4.0
	github.com/mattn/go-sqlite3 v1.14.28
	github.com/redis/go-redis/v9 v9.7.3
)

require github.com/google/wire v0.6.0

require (
	github.com/cespare/xxhash/v2 v2.2.0 // indirect
	github.com/dgryski/go-rendezvous v0.0.0-20200823014737-9f7001d12a5f // indirect
)
