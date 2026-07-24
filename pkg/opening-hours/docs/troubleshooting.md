# Troubleshooting

| Symptom | Cause | Action |
| --- | --- | --- |
| `invalid_timezone` | empty/unknown IANA identity | install tzdata or correct identity |
| `overlap` | ranges overlap in owner-day coordinates | reject input or select merge policy |
| `adjacent` | strict adjacency policy | preserve or explicitly merge adjacency |
| `ambiguous_exception` | equal date/priority | assign priorities or choose canonical policy |
| `ambiguous_local_time` | autumn fold | choose earlier or later explicitly |
| `nonexistent_local_time` | spring gap | reject or select forward shift |
| `search_exhausted` | no boundary in horizon | increase horizon within 366 days |
| `invalid_encoding` | hostile/noncanonical structure | validate source adapter and limits |
| PostgreSQL test skips | `POSTGRES_URL` absent | set URL for disposable test database |

When a civil-time fixture fails, inspect the actual IANA transition rather than
broadening assertions. When coverage is below 100%, use the per-function report
to add behavioral boundary proof, not lines executed without assertions.
