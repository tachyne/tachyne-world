package plugin

// KV is a plugin's persistent key-value store, JSON-encoded to the
// plugin's data directory and flushed with world state. Values must be
// JSON-marshalable. Tick-goroutine-only.
type KV interface {
	// Get unmarshals the stored value into v, reporting whether the key
	// existed.
	Get(key string, v any) bool
	Set(key string, v any)
	Delete(key string)
}
