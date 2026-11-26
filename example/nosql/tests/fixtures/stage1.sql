.bail on
.mode list
.headers off

SELECT 'oracle', sqlite_version();

CREATE TABLE stage1_values(
    id INTEGER PRIMARY KEY,
    n INTEGER,
    r REAL,
    s TEXT,
    b BLOB,
    nullable TEXT
);

INSERT INTO stage1_values VALUES
    (1, -42, 3.5, 'hello', x'0001FEFF', NULL),
    (2, 0, 1.0, '', x'', 'not null'),
    (3, 9223372036854775807, -0.0, 'SQLite', x'53514C697465', 'text');

SELECT 'rows', count(*) FROM stage1_values;
SELECT 'storage', typeof(n), typeof(r), typeof(s), typeof(b), typeof(nullable)
FROM stage1_values WHERE id = 1;
SELECT 'null', nullable IS NULL FROM stage1_values WHERE id = 1;
SELECT 'blob', hex(b), length(b) FROM stage1_values WHERE id = 1;
SELECT 'integer', n FROM stage1_values WHERE id = 3;
SELECT 'ordering', group_concat(id, ',') FROM (
    SELECT id FROM stage1_values ORDER BY n, id
);

CREATE TABLE stage1_affinity(
    i INTEGER,
    t TEXT,
    n NUMERIC,
    b BLOB
);

INSERT INTO stage1_affinity VALUES ('123', '123', '123', '123');
SELECT 'affinity', typeof(i), typeof(t), typeof(n), typeof(b)
FROM stage1_affinity;
