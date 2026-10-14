# Development policy

This project has no actual users yet. Until the maintainer explicitly announces adoption:

- Prefer the current design over backward compatibility. Do not add database migrations, old-format readers, legacy API adapters or fallback execution paths solely to preserve previous project behavior.
- Remove obsolete code and tests with the contract they covered. Keep tests for current behavior, resource permissions, concurrency, persistence, recovery and atomic publication.
- Accept only the current database and share-package formats. Reject unsupported formats clearly; do not automatically delete existing data files.
- Profile revisions and restoration of current configuration are product features, not legacy compatibility.
