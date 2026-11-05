// Package indexadvice analyses normalised SQL statements and index definitions
// without a database. Parse extracts the tables and the predicates that an
// index could serve; constructs it cannot place are skipped and counted by
// reason, never guessed. ParseIndexDef reads a pg_get_indexdef text. Both are
// pure functions: no I/O, no panics, bounded work.
package indexadvice
