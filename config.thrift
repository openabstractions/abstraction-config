namespace * abstraction.config

// Read-only configuration service. Run overrides are caller-supplied claims,
// never inferred from the service process environment or used as identity.
encoding json {
  escape = "minimal"
  indent = "2"
  map_keys = "utf8-bytes"
  numbers = "integer-decimal"
  opaque = "verbatim"
  terminator = "newline"
  duplicate_keys = "last"
  depth_limit = "64"
}
refusal {
  1: malformed (stage = "grammar")
  2: bad_string (stage = "grammar")
  3: number_spelling (stage = "grammar")
  4: wrong_type (stage = "grammar")
  5: depth_exceeded (stage = "grammar")
  6: duplicate_field (stage = "structure")
  7: unknown_field (stage = "structure")
  8: missing_field (stage = "structure")
 9: trailing_bytes (stage = "document")
}

struct RunOverrides {
  1: required string nas_store
  2: required string store
  3: required string log_sink
  4: required string log_service
} (unknown_fields = "refuse", doc="Existing per-run overrides. Empty strings do not override file values.")
struct Origin {
  1: required string rung
  2: required string path
} (unknown_fields = "refuse", doc="Per-key provenance: machine/user file path, or environment/default with empty path.")
struct Origins {
  1: required Origin nas_store
  2: required Origin store
  3: required Origin log_sink
  4: required Origin log_service
  5: required Origin off
} (unknown_fields = "refuse", doc="Provenance for every configuration key, including default answers.")
struct Snapshot {
  1: required string nas_store
  2: required string store
  3: required string log_sink
  4: required string log_service
  5: required map<string,string> off
  6: required Origins origins
  7: required string stamp
} (document = "true", unknown_fields = "refuse", doc="Existing provider values and provenance. Empty values mean absence. Stamp follows values rather than provenance; paths are diagnostic configuration data, not permission to access a store.")
service ConfigReader {
  Snapshot Read(1: RunOverrides overrides) (doc="Read same-user machine/user configuration with explicit caller run overrides. No writes, watch or provider fallback.")
} (wire_name = "abstraction.config/reader@1", doc="Per-user configuration read service. Caller identity is checked independently of overrides.")
