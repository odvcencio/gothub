# Platform Evolution: Fastest, Most Intelligent Code Awareness Platform

**Date:** 2026-02-24
**Status:** Approved
**Scope:** Got (structural VCS engine) + GotHub (hosting platform)

---

## Vision

Transform Got + GotHub from a working prototype into the fastest, most intelligent code awareness platform — capable of serving solo developers, teams, and enterprises. Every phase delivers a complete, shippable product.

## Current State

**Strengths:**
- Entity lineage with stable IDs across commits (the crown jewel)
- Structural three-way merge that eliminates trivial conflicts
- Semantic versioning recommendations from structural change analysis
- Entity-level ownership and merge gate policies
- 205-language support via pure-Go tree-sitter
- Git smart HTTP interop

**Weaknesses:**
- One file per object, no compression, no delta encoding (100x storage penalty)
- Synchronous entity extraction blocks pushes
- In-memory-only cache, lost on restart
- Naive O(n) merge base algorithm
- Full tree re-parse on every code intelligence query
- JSON wire protocol (50% bandwidth overhead)
- No remotes, no pack files, no garbage collection in Got
- Minimal frontend (no syntax highlighting, no real-time, no search)

## Approach: Layered Cake

Four phases. Each delivers a complete vertical slice. No phase depends on "we'll fix it later."

---

## Phase 1 — Fast & Smart Core

**Goal:** Got becomes objectively faster than Git for common operations. GotHub stops choking on pushes. The foundation is production-grade.

### 1.1 Git-Compatible Pack File Engine (Got)

**Problem:** One file per object. Filesystem thrashes at scale. No compression.

**Design:**

Pack format follows Git v2 exactly — same header, same OFS_DELTA/REF_DELTA encoding, same `.idx` v2 fan-out index.

- `git index-pack` can read Got's packs
- `git verify-pack` works for validation
- Got can read Git's packs natively
- Migration: `git clone` → `got init --from-git` is essentially free

Compression modes:
- zlib for Git-compatible packs (interop)
- zstd for Got-native packs (3-5x faster decompression, flagged in header)
- When serving Git clients: always zlib. Got-to-Got: zstd.

Delta selection: sort objects by type+size, delta against nearest neighbor. Window size configurable (default 10, same as Git).

Got pack extension: optional entity index trailer appended after standard Git pack data. Maps `object_hash → [entity_stable_id, ...]`. Git clients ignore it. Got clients use it for O(1) entity lookups without unpacking.

Object lifecycle:
1. Loose objects written on commit (fast writes)
2. Background `got gc` packs loose objects
3. Auto-pack threshold: 256 loose objects (configurable)
4. Pack files are immutable — no concurrent mutation

Delta chain depth limit: 50 (matches Git).

New commands:
- `got gc` — pack loose objects, prune unreachable
- `got verify` — integrity check objects and packs

**Expected impact:**
- 10-50x storage reduction
- 10-100x faster sequential reads
- 1M+ objects feasible

### 1.2 Async Indexing Pipeline (GotHub)

**Problem:** Entity extraction and lineage indexing block the push response.

**Design:**

Push path (fast):
1. Receive objects → write to store
2. Update refs (CAS)
3. Enqueue indexing job
4. Return success to client immediately

Background indexing worker:
- Goroutine pool (configurable, default 4 workers)
- Job queue backed by database: `indexing_jobs(repo_id, commit_hash, status, created_at, started_at, completed_at)`
- Each job: extract entities for changed files only (diff against parent), update lineage, update code intelligence index
- Incremental: only changed files, not full tree
- Idempotent: safe to retry on failure

Index status API:
- `GET /api/repos/{owner}/{repo}/index/status` — indexing state per commit
- Frontend shows "indexing..." badge until complete
- PRs show graceful degradation while indexing

**Expected impact:**
- Push latency: seconds → milliseconds
- Indexing throughput: 10-50x (incremental)
- Concurrent pushes unblocked

### 1.3 Full Code Intelligence Engine (GotHub)

**Problem:** Every code intel request rebuilds the full index from scratch.

**Layer 1 — Persistent Entity Index:**
- Database table: `entity_index(repo_id, commit_hash, file_path, entity_hash, stable_id, kind, name, start_line, end_line)`
- Populated by async indexing pipeline
- Code intelligence reads from table, not re-parses
- Composite indexes on `(repo_id, commit_hash)` and `(repo_id, stable_id)`

**Layer 2 — Full-Text Symbol Search:**
- SQLite: FTS5 virtual table over entity names, signatures, doc comments
- PostgreSQL: tsvector/tsquery with GIN index
- Fuzzy matching, prefix search, ranked results
- Indexes: function names, type names, method signatures, parameter types, doc comments
- API: `GET /api/repos/{owner}/{repo}/search?q=ProcessOrder&type=function&lang=go`

**Layer 3 — Bloom Filters:**
- Per-commit bloom filter (~1KB per 1000 entities, <1% false positive rate)
- Stored as binary blob: `commit_bloom(repo_id, commit_hash, bloom_data)`
- O(1) "does commit X contain entity Y?" without DB round-trip
- Short-circuits: lineage walks, merge base entity resolution, PR impact analysis
- Hierarchical: repo-level bloom aggregates all commit blooms

**Layer 4 — Cross-Reference Graph:**
- Persistent `xref(repo_id, commit_hash, source_entity_id, target_entity_id, kind)` where kind = call, type_ref, import
- Precomputed call graph per commit (adjacency list as JSONB/JSON)
- "Find all callers of X" = single indexed query
- "Impact analysis: if I change X, what breaks?" = graph traversal
- Transitive closure cache for hot paths

**Layer 5 — Semantic Diff Intelligence:**
- Classify entity changes: signature, body, doc, visibility
- Detect breaking changes (public signature modified, type changed, parameter removed)
- Feed into semantic versioning recommendation (backed by real indexed data)
- API: `GET /api/repos/{owner}/{repo}/diff/{base}...{head}/semantic`

**Incremental updates:**
- New commit → diff against parent → re-extract only changed files
- Copy unchanged entity records from parent (pointer, not duplication)
- Cost: O(changed files) not O(total files)

**In-memory cache upgrade:**
- Keep LRU, back with persistent store
- Cache miss → database read (fast) not full re-parse (slow)
- Warm on startup from most recent commits of active repos

### 1.4 Merge Performance (Got + GotHub)

**Problem:** Naive O(n) BFS for merge base. No caching. Full tree re-flatten on every preview.

**LCA with preprocessing:**
- Compute generation numbers (topological depth) on push
- Store: `commit_meta(repo_id, commit_hash, generation, timestamp)`
- Prune BFS: never explore commits with generation > min(gen_a, gen_b)
- O(n) → O(k) where k = branch distance, not history depth

**Merge base cache:**
- `merge_base_cache(repo_id, commit_a, commit_b, base_hash)`
- Populated on first computation, invalidated when refs update

**Incremental merge preview:**
- First request: compute full structural merge, cache result
- Subsequent: if neither branch moved, return cached
- If one moved: recompute only changed files
- Store: `merge_preview_cache(pr_id, src_hash, tgt_hash, result_json, computed_at)`

**Parallel file merging:**
- Three-way merge per file is embarrassingly parallel
- Goroutine pool, bounded by worker count
- Entity data read from persistent index (Section 1.3)

### 1.5 Observability & Benchmarking

**OpenTelemetry Tracing:**
- Instrument hot paths: push → object write → ref update → index enqueue
- Code intelligence: query → bloom check → cache → DB → response
- Merge: base find → tree diff → entity merge → reconstruct
- Export: structured JSON (stdout), configurable OTLP endpoint (Jaeger/Grafana Tempo)
- Every span: repo_id, commit_hash, operation, duration, object_count

**Profiling (pprof):**
- `/debug/pprof/` in dev/admin mode
- CPU, heap, goroutine, mutex profiles
- Continuous profiling integration point (Pyroscope-compatible)

**Prometheus Metrics:**
- `got_push_duration_seconds` (histogram)
- `got_index_duration_seconds` (histogram, by phase)
- `got_index_queue_depth` (gauge)
- `got_pack_objects_total` (counter, by type)
- `got_cache_hit_ratio` (gauge, by cache_name)
- `got_merge_base_duration_seconds` (histogram)
- `got_entity_parse_duration_seconds` (histogram, by language)
- `got_xref_query_duration_seconds` (histogram, by query_type)
- `/metrics` endpoint, Prometheus scrape format

**Benchmark Suites (Got):**
- `BenchmarkPackWrite` — pack N objects, throughput (MB/s)
- `BenchmarkPackRead` — random access from pack, latency
- `BenchmarkDeltaCompress` — delta encode similar objects, ratio
- `BenchmarkEntityExtract` — parse by language, tokens/sec
- `BenchmarkMergeBase` — find base at depths 10, 100, 1K, 10K
- `BenchmarkThreeWayMerge` — merge files of 100, 1K, 10K lines
- `BenchmarkBloomFilter` — lookup throughput at various FPR

**Benchmark Suites (GotHub):**
- `BenchmarkPushReceive` — end-to-end push, N objects
- `BenchmarkCodeIntelQuery` — symbol search at various repo sizes
- `BenchmarkMergePreview` — PR preview cold + warm
- `BenchmarkEntityLineage` — lineage walk at various depths

**Health endpoint:**
- `/admin/health` — queue depth, cache stats, active workers, pack stats (JSON)

---

## Phase 2 — Connected

**Goal:** Got becomes usable in real workflows. Adoption path is clear.

| Component | Description |
|-----------|-------------|
| Remote protocol | `got clone`, `got push`, `got pull`, `got fetch` via Git-compatible smart HTTP + Got-native binary protocol |
| Binary wire protocol | Replace JSON with protobuf/CBOR for Got-to-Got. 2-5x bandwidth reduction |
| Delta transfer | Only send objects the other side lacks. Have/want negotiation |
| LSP server | VS Code/Neovim — go-to-definition, find-references, rename, powered by entity index |
| CI/CD hooks | First-class webhook events. Status check API for merge gates |
| `got import` | Import existing Git repos with full history conversion, entity extraction, index build |

---

## Phase 3 — Intelligent Platform

**Goal:** The intelligence showcase. The features that don't exist anywhere else.

| Component | Description |
|-----------|-------------|
| Semantic code search | Cross-repo symbol search, natural language queries, "find all implementations of interface X" |
| Call graph visualization | Interactive graph UI — click function, see callers/callees, impact radius |
| AI-assisted review | LLM-powered PR summaries, breaking change explanations, suggested reviewers |
| Real-time collaboration | WebSocket for live PR reviews, typing indicators, live diff updates |
| Entity timeline | Visual history of a function/type across commits — who, when, why |
| Dead code detection | Automated via xref graph (foundation in merge gates) |
| Dependency graph | Package/module level visualization, circular dependency detection |
| Rich diff UI | Side-by-side entity-aware diffs, syntax highlighting, entity-anchored comments |

---

## Phase 4 — Enterprise

**Goal:** Enterprise buyer's checklist. The monetization path.

| Component | Description |
|-----------|-------------|
| SSO/SAML | OAuth2, SAML 2.0, OIDC |
| Audit logs | Immutable log of all actions |
| RBAC | Custom roles, org-level and repo-level |
| Compliance policies | Configurable merge requirements, security review gates |
| Horizontal scaling | Stateless API + shared storage (S3/GCS) + distributed index |
| On-prem deployment | Helm chart, Terraform module, air-gapped install |
| Backup/restore | Point-in-time recovery, cross-region replication |
| Admin console | Org management, usage analytics, health monitoring |

---

## Success Metrics

| Metric | Target |
|--------|--------|
| Push latency (10K file repo) | < 200ms (async indexing) |
| Clone (1GB repo, Got-to-Got) | < 30s (pack + zstd + delta) |
| Symbol search (100K entities) | < 50ms |
| Merge base (10K commit depth) | < 10ms (generation numbers) |
| Merge preview (cached) | < 100ms |
| Entity extraction (1K files) | < 2s (incremental: < 100ms) |
| Storage vs Git | Within 1.2x (pack compat) |
| Network transfer vs Git | Within 1.5x (Got-native: 0.7x with zstd) |
