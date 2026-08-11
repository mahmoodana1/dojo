-- Offline demo seed: one judgeable problem so the full loop works before
-- ingest runs.  Usage: sqlite3 ~/.local/share/dojo/dojo.db < scripts/seed-demo.sql
INSERT OR IGNORE INTO problems (id, frontend_id, slug, title, difficulty, description_md, hints_json)
VALUES (900001, 9001, 'dojo-warmup-sum', 'Dojo Warmup: Sum of Integers', 'Easy',
'Given a line of space-separated integers on stdin, print their sum.

**Example**

```
Input:  1 2 3 4
Output: 10
```

This is a dojo warm-up problem to verify your judge pipeline end to end:
`i` to open your editor, solve, `:wq`, watch the verdict.',
'["Read stdin, split on whitespace, sum, print."]');

INSERT OR IGNORE INTO tags (slug, name) VALUES ('warmup', 'Warmup');
INSERT OR IGNORE INTO problem_tags (problem_id, tag_id)
SELECT 900001, id FROM tags WHERE slug = 'warmup';

INSERT OR IGNORE INTO test_cases (problem_id, ordinal, input, expected, is_hidden, source) VALUES
(900001, 0, '1 2 3 4', '10', 0, 'example'),
(900001, 1, '-5 5', '0', 0, 'example'),
(900001, 2, '1000000 1000000 1000000', '3000000', 1, 'user');
