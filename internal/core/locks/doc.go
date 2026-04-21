// Package locks is the pure lock state machine: acquire, queue, release,
// TTL expiry, reaper decisions. No blob I/O — adapters in internal/blob
// satisfy the storage interface. Injectable clock for tests.
package locks
