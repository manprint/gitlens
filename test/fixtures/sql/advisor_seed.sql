-- Advisor acceptance fixture. It is intentionally idempotent so a test can
-- apply it to a clean target more than once.
CREATE TABLE IF NOT EXISTS advisor_unused (id bigint PRIMARY KEY, payload text);
INSERT INTO advisor_unused
SELECT g, repeat('x', 256) FROM generate_series(1, 50000) AS g
ON CONFLICT DO NOTHING;
CREATE INDEX IF NOT EXISTS advisor_unused_large_idx ON advisor_unused(payload);

CREATE TABLE IF NOT EXISTS advisor_duplicate (id bigint PRIMARY KEY, payload text);
CREATE INDEX IF NOT EXISTS advisor_duplicate_a ON advisor_duplicate(id);
CREATE INDEX IF NOT EXISTS advisor_duplicate_b ON advisor_duplicate(id);
CREATE TABLE IF NOT EXISTS advisor_prefix (a bigint PRIMARY KEY, b bigint, payload text);
CREATE INDEX IF NOT EXISTS advisor_prefix_a ON advisor_prefix(a);
CREATE INDEX IF NOT EXISTS advisor_prefix_ab ON advisor_prefix(a, b);

CREATE TABLE IF NOT EXISTS advisor_dead (id bigint PRIMARY KEY, payload text);
INSERT INTO advisor_dead SELECT g, repeat('dead', 32) FROM generate_series(1, 20000) AS g
ON CONFLICT DO NOTHING;
DELETE FROM advisor_dead WHERE id % 2 = 0;

CREATE TABLE IF NOT EXISTS advisor_never_vacuumed (id bigint PRIMARY KEY, payload text);
INSERT INTO advisor_never_vacuumed
SELECT g, repeat('live', 32) FROM generate_series(1, 100001) AS g
ON CONFLICT DO NOTHING;

CREATE TABLE IF NOT EXISTS advisor_bloated (id bigint PRIMARY KEY, payload text);
INSERT INTO advisor_bloated
SELECT g, repeat('bloat', 1024) FROM generate_series(1, 100001) AS g
ON CONFLICT DO NOTHING;

-- A deliberately slow, repeated statement for pg_stat_statements-backed tests.
SELECT pg_sleep(0.01) FROM generate_series(1, 10);
