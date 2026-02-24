# Platform Evolution: Fastest, Most Intelligent Code Awareness Platform

**Date:** 2026-02-24
**Revised:** 2026-02-24 (third revision — signature flow shipped, protocol hardening landed)
**Status:** Approved
**Scope:** Got (structural VCS engine) + GotHub (hosting platform)

---

## Vision

Transform Got + GotHub from a working prototype into the fastest, most intelligent code awareness platform — capable of serving solo developers, teams, and enterprises. Every phase delivers a complete, shippable product.

### Ecosystem

Four repos work together (all under active development):

| Repo | Role | Recent work |
|------|------|-------------|
| **got** | Structural VCS engine | SSH commit signing, remote sync, structural merge, multi-round batch negotiation, gothub e2e coverage |
| **gothub** | Hosting platform | Signature verification, merge gates, proactive commit indexing, entity lineage |
| **gotreesitter** | Pure-Go tree-sitter runtime | 205 grammars, external scanner support, fuzz testing, benchmarks |
| **gts-suite** | Code intelligence library | Tree-sitter AST classification, xref/structdiff/model test suites (2000+ new test LOC), lint integration |

---

## Current State (Audited)

### Got — Structural VCS Engine

**~11,500 LOC across 7 packages. 160+ tests. All passing.**

**What works:**
- Content-addressed object store (SHA-256, 2-char fan-out, atomic writes)
- Entity extraction via pure-Go tree-sitter (205 grammars, tested: Go, Python, Rust, TypeScript, C, C++, Java, JavaScript)
- Byte-for-byte reconstruction invariant (entity concatenation = original source)
- Three-way structural merge: identity matching, anchor-based positioning, import set-union, diff3 fallback
- Full CLI: init, add, status, commit, log, diff, branch, checkout, merge
- Remote operations: clone, push, pull, fetch via Got protocol over HTTP
- Batch object negotiation (wants/haves), graph closure guarantees
- .gotignore support, config management, detached HEAD
- Benchmarks for store, entity extraction, merge, diff3
- SSH commit signing: `CommitSigner` function type, `CommitWithSigner()`, `--sign` / `--sign-key` CLI flags, sshsig-v1 format with ed25519/ecdsa/rsa key auto-detection
- `CommitSigningPayload()` produces canonical bytes excluding signature field for deterministic verification

**What doesn't work / doesn't exist:**
- No pack files — every object is a loose file (100x storage penalty at scale)
- No compression — objects stored raw
- No delta encoding — similar objects stored in full
- No garbage collection — dead objects persist forever
- No SSH transport for push/pull — HTTP only (SSH signing exists but not SSH protocol)
- No rename detection, rebase, cherry-pick, stash, submodules, shallow clones
- JSON wire protocol for Got-to-Got (50% bandwidth overhead vs binary)
- No delta transfer — pushes send full objects, not diffs against known bases
- Merge base uses naive two-queue BFS — O(n) in DAG depth

### GotHub — Code Hosting Platform

**~15,000 LOC backend + Preact/TypeScript frontend. Comprehensive API test suite.**

**What works:**
- Complete REST API: 70+ endpoints covering repos, PRs, issues, reviews, webhooks, notifications, orgs, code browsing, code intelligence, branch protection
- Git smart HTTP protocol: git-receive-pack (push), git-upload-pack (fetch) with full Git↔Got object conversion
- Got protocol: refs, objects, batch, push, with CAS ref updates
- Entity extraction on push: tree rewriting to embed entity lists into Got trees
- Entity lineage tracking: stable IDs assigned across commits, first/last seen tracking
- Code intelligence via gts-suite: symbol search (query.ParseSelector), find references, call graph (xref.Build + Walk)
- Persistent index storage: code intel indexes serialized as JSON blobs to object store, hash persisted in DB
- In-memory LRU cache backed by persisted indexes (128 items, 15min TTL)
- Proactive indexing: EnsureCommitIndexed called on push to pre-build indexes
- Merge gates: required approvals, entity owner approval (.gotowners), lint pass, dead code detection, status checks
- Semantic versioning recommendations from structural diff analysis
- Webhook delivery with entity change tracking in payloads
- Dual database: SQLite (WAL mode) + PostgreSQL, full schema with migrations
- JWT auth with bcrypt passwords, SSH key management
- SSH commit signature verification: parses sshsig-v1 format, verifies signature against SSH keys in DB, resolves signer to username. `CommitInfo` includes `Verified` and `Signer` fields. Both `GetCommit` and `ListCommits` verify signatures.
- Organizations with member roles, collaborator permissions
- Rate limiting (10 req/sec per IP), CORS, request body limits (8MB)
- Preact SPA with code browsing, PRs, issues, merge preview, entity diff views

**What's broken / bottlenecked:**
- **Push blocks on TWO synchronous operations:**
  1. Entity extraction: `rewriteTreeWithEntities()` walks entire tree, calls `entity.Extract()` per file, writes entity objects, rewrites tree — all during git-receive-pack response
  2. Code intel indexing: `EnsureCommitIndexed()` builds full gts-suite index (parses every file via `index.NewBuilder()`) during push callback
- **Code intel index is monolithic:** entire index serialized/deserialized as single JSON blob (~MBs for large repos). No incremental updates — full rebuild per commit
- **Symbol search is linear scan:** `SearchSymbols` iterates every symbol in every file in the index. No database-backed FTS or indexing
- **FindReferences is linear scan:** same pattern — iterate all references in all files
- **Call graph rebuilt on every query:** `xref.Build(idx)` constructs full call graph from scratch per request
- **No background workers:** zero goroutines, channels, or job queues anywhere. Everything synchronous on request path
- **Webhook delivery blocks request:** 5s timeout per webhook, delivered inline
- **LRU eviction is O(n):** iterates all entries to find oldest on overflow
- **No observability:** no metrics, no tracing, no profiling endpoints
- **Frontend is basic:** no syntax highlighting, no real-time updates, no search
- **No connection pool tuning:** PostgreSQL max 25 connections

---

## Approach: Layered Cake

Four phases. Each delivers a complete vertical slice. No phase depends on "we'll fix it later."

---

## Phase 1 — Fast & Smart Core

**Goal:** Got becomes objectively faster than Git for common operations. GotHub stops blocking on pushes. Foundation is production-grade with observability.

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
1. Loose objects written on commit (fast writes, existing behavior preserved)
2. Background `got gc` packs loose objects into pack files
3. Auto-pack threshold: 256 loose objects (configurable)
4. Pack files are immutable — no concurrent mutation

Delta chain depth limit: 50 (matches Git).

Store integration: `Store.Read()` and `Store.Has()` check loose objects first, then pack files. Write path remains loose-only.

New commands:
- `got gc` — pack loose objects, prune unreachable
- `got verify` — integrity check all objects (loose + packed)

**Expected impact:**
- 10-50x storage reduction
- 10-100x faster sequential reads (single file seek vs N filesystem lookups)
- 1M+ objects feasible

### 1.2 Async Indexing Pipeline (GotHub)

**Problem:** Push blocks on two synchronous operations: entity extraction (tree rewriting) and code intel indexing (full gts-suite parse of every file). Both happen during the HTTP response to git-receive-pack / Got ref update.

**Current push path:**
```
git push → receive packfile → convert objects → persist hash mappings
→ extractEntitiesForCommits() [BLOCKS: walks tree, entity.Extract per file]
→ lineageSvc.IndexCommit() [BLOCKS: walks commit ancestry]
→ codeIntelSvc.EnsureCommitIndexed() [BLOCKS: parses every file via gts-suite]
→ update refs → respond to client
```

**New push path:**
```
git push → receive packfile → convert objects → persist hash mappings
→ update refs → enqueue indexing job → respond to client [FAST]

[Background worker]:
→ extract entities for changed files only (diff against parent)
→ rewrite tree with entity lists
→ update entity lineage
→ build incremental code intel index
→ mark job complete
```

Background indexing worker:
- Goroutine pool (configurable, default 4 workers)
- Job queue backed by database: `indexing_jobs(repo_id, commit_hash, job_type, status, error, created_at, started_at, completed_at)`
- Job types: entity_extraction, lineage_indexing, codeintel_indexing (sequenced per commit)
- Incremental: diff against parent commit, only process changed files
- Idempotent: safe to retry on failure
- Graceful shutdown: finish in-progress jobs on SIGINT

Index status API:
- `GET /api/v1/repos/{owner}/{repo}/index/status` — indexing state per commit
- Frontend shows "indexing..." badge until complete
- PRs and code intel endpoints return partial results with `"indexed": false` flag while indexing

**Expected impact:**
- Push latency: seconds → milliseconds (object write + ref update only)
- Indexing throughput: 10-50x (incremental, not full tree)
- Server handles concurrent pushes without serialization

### 1.3 Code Intelligence Upgrades (GotHub)

**Problem:** Code intel indexes are monolithic JSON blobs. Symbol search and find-references do linear scans. Call graph rebuilt from scratch per query. No FTS. No incremental updates.

**What exists today (preserve and build on):**
- `CodeIntelService` with LRU cache (128 items, 15min TTL)
- `BuildIndex()` → checks LRU → checks persisted index → builds from store
- `persistIndex()` / `loadPersistedIndex()` — serialize full `model.Index` as JSON to object store
- `SearchSymbols()` — query.ParseSelector + linear scan over idx.Files[].Symbols[]
- `FindReferences()` — linear scan over idx.Files[].References[]
- `GetCallGraph()` — xref.Build(idx) + graph.Walk()

**Layer 1 — Persistent Entity Index (database-backed):**
- New table: `entity_index(repo_id, commit_hash, file_path, entity_hash, stable_id, kind, name, signature, doc_comment, start_line, end_line)`
- Populated by async indexing pipeline from Task 1.2
- Composite indexes on `(repo_id, commit_hash)`, `(repo_id, stable_id)`, `(repo_id, commit_hash, kind)`
- Incremental: new commit copies unchanged entries from parent, only re-indexes changed files
- `CodeIntelService.SearchSymbols()` queries this table instead of linear scan

**Layer 2 — Full-Text Symbol Search:**
- SQLite: FTS5 virtual table over entity names, signatures, doc comments
- PostgreSQL: tsvector/tsquery with GIN index
- Supports prefix search, fuzzy matching, ranked results
- Filter by kind (function/type/method), language, file path
- New API: `GET /api/v1/repos/{owner}/{repo}/search?q=ProcessOrder&kind=function&lang=go`

**Layer 3 — Bloom Filters for Fast Negative Lookups:**
- Per-commit bloom filter (~1KB per 1000 entities, <1% false positive rate)
- Stored in `commit_bloom(repo_id, commit_hash, bloom_data)` table
- O(1) "does commit X contain entity Y?" without DB round-trip
- Short-circuits lineage walks, merge base entity resolution, impact analysis
- Built during async indexing

**Layer 4 — Persistent Cross-Reference Graph:**
- New table: `xref(repo_id, commit_hash, source_entity_id, target_entity_id, kind, source_file, source_line)`
- Kinds: call, type_ref, import
- Populated during async indexing (gts-suite already extracts references)
- `FindReferences()` becomes a single indexed query instead of linear scan
- `GetCallGraph()` reads from xref table instead of rebuilding from scratch
- Impact analysis: "if I change X, what breaks?" = BFS over xref graph
- New API: `GET /api/v1/repos/{owner}/{repo}/impact/{entity_id}?ref=main&depth=3`

**Layer 5 — Semantic Diff Intelligence:**
- Classify entity changes: signature change, body change, doc change, visibility change
- Detect breaking changes: public signature modified, parameter removed, return type changed
- Feed into existing `RecommendSemver()` endpoint (currently works, but backed by real indexed data instead of on-the-fly parsing)
- New API: `GET /api/v1/repos/{owner}/{repo}/diff/{base}...{head}/semantic` — structured change classification

### 1.4 Merge Performance (Got + GotHub)

**Problem:** Merge base uses naive two-queue BFS (O(n) in DAG depth). No caching. Merge preview recomputes from scratch every time.

**LCA with generation number pruning:**
- Compute generation numbers (topological depth) during async indexing
- Store in `commit_meta(repo_id, commit_hash, generation, timestamp)` table
- Prune BFS: skip commits with generation > min(gen_a, gen_b)
- O(n) → O(k) where k = branch distance, not total history depth

**Merge base cache:**
- `merge_base_cache(repo_id, commit_a, commit_b, base_hash, computed_at)` table
- Normalize key order (lexicographic sort of commit hashes) for consistent cache hits
- Populated on first computation, invalidated when refs update

**Merge preview cache:**
- `merge_preview_cache(pr_id, src_hash, tgt_hash, result_json, computed_at)` table
- If neither branch moved since last computation, return cached result
- If one branch moved, recompute only files that changed since cached preview

**Parallel file merging:**
- Three-way merge of individual files is embarrassingly parallel
- Bounded goroutine pool (default: runtime.NumCPU())
- Entity data read from persistent entity_index (Layer 1) when available

### 1.5 Observability & Benchmarking

**OpenTelemetry Tracing:**
- Instrument hot paths: push receive → object write → ref update → job enqueue
- Background worker: job claim → entity extract → lineage index → codeintel build
- Code intelligence: query → cache check → DB query → response
- Merge: base find → tree diff → entity merge → reconstruct
- Export: structured JSON (stdout default), configurable OTLP endpoint (Jaeger/Grafana Tempo)
- Every span carries: repo_id, commit_hash, operation, duration_ms, object_count

**Profiling (pprof):**
- `/debug/pprof/` endpoints enabled in dev/admin mode
- CPU, heap, goroutine, mutex profiles
- Continuous profiling integration point (Pyroscope-compatible)

**Prometheus Metrics:**
- `gothub_push_duration_seconds` (histogram, labels: protocol=git|got)
- `gothub_index_duration_seconds` (histogram, labels: phase=entity|lineage|codeintel)
- `gothub_index_queue_depth` (gauge)
- `gothub_index_queue_oldest_seconds` (gauge)
- `gothub_pack_objects_total` (counter, labels: type)
- `gothub_cache_hit_ratio` (gauge, labels: cache=codeintel|merge_base|merge_preview)
- `gothub_merge_base_duration_seconds` (histogram)
- `gothub_entity_parse_duration_seconds` (histogram, labels: language)
- `gothub_xref_query_duration_seconds` (histogram, labels: query_type)
- `gothub_symbol_search_duration_seconds` (histogram)
- `/metrics` endpoint, standard Prometheus scrape format

**Benchmark Suites (Got — new benchmarks to ADD, existing preserved):**
- Existing: store write/read, entity extraction per language, merge, diff3
- New: `BenchmarkPackWrite`, `BenchmarkPackRead`, `BenchmarkDeltaCompress`, `BenchmarkDeltaApply`, `BenchmarkGC`, `BenchmarkMergeBase` at various DAG depths

**Benchmark Suites (GotHub — new):**
- `BenchmarkPushReceive` — end-to-end push with N objects
- `BenchmarkCodeIntelQuery` — symbol search at various repo sizes
- `BenchmarkMergePreview` — PR preview cold + warm cache
- `BenchmarkEntityLineageWalk` — lineage walk at various depths
- `BenchmarkBloomFilterLookup` — throughput at various FPR
- `BenchmarkXRefQuery` — find references at various graph sizes

**Health endpoint:**
- `GET /admin/health` — JSON: queue depth, worker status, cache stats, pack stats, DB pool stats

---

## Phase 2 — Protocol & Ecosystem

**Goal:** Got's network protocol becomes production-grade. IDE integration makes adoption seamless.

**What already exists (preserve):**
- `pkg/remote/`: Client with ListRefs, BatchObjects, GetObject, PushObjects, UpdateRefs
- FetchIntoStore with multi-round batch negotiation and graph closure
- CollectObjectsForPush with DFS reachability
- Clone, pull, push, fetch CLI commands (all working)
- Auth via GOT_TOKEN (Bearer) or GOT_USERNAME/GOT_PASSWORD (Basic)
- Endpoint parsing for various URL formats

**What's missing / needs upgrade:**

| Component | Description |
|-----------|-------------|
| Delta transfer | Currently sends full objects. Add delta compression over the wire — only send diffs against objects the other side has. Massive bandwidth reduction for incremental pushes. |
| Binary wire protocol | Replace JSON transport in Got protocol with protobuf or CBOR. 2-5x bandwidth reduction for metadata. |
| Pack file transfer | Send pack files instead of individual objects for clone/fetch. Single HTTP response with streamed pack data. |
| SSH transport | Add SSH-based push/pull alongside HTTP. Key-based auth using existing SSH key management in GotHub. |
| LSP server | Language Server Protocol for VS Code/Neovim: go-to-definition, find-references, rename, hover — powered by entity index and xref graph from Phase 1. |
| `got import` | Import existing Git repos: `got import https://github.com/org/repo` — converts history, extracts entities, builds index. |
| CI/CD integration | First-class webhook events for CI systems. Status check reporting API (already exists as PRCheckRun, but needs CI-facing ergonomics). |

---

## Phase 3 — Intelligent Platform

**Goal:** The intelligence showcase. Features that don't exist anywhere else.

**What already exists (preserve and extend):**
- Symbol search via gts-suite query selectors
- Find references
- Call graph building and traversal (xref.Build + Walk)
- Entity-level diffs with structural change detection
- Semantic versioning recommendations
- Dead code detection in merge gates
- Entity change tracking in webhook payloads
- Entity-anchored PR comments (FilePath + EntityKey + EntityStableID)

**What's new:**

| Component | Description |
|-----------|-------------|
| Semantic code search | Cross-repo symbol search, natural language queries ("find all implementations of interface X"), powered by FTS from Phase 1 + cross-repo index federation |
| Call graph visualization | Interactive graph UI — click a function, see callers/callees/impact radius. Uses xref graph from Phase 1. |
| AI-assisted review | LLM-powered PR summaries, "this change breaks the contract of X because...", suggested reviewers based on entity ownership |
| Real-time collaboration | WebSocket for live PR reviews, typing indicators, live diff updates, notification badges |
| Entity timeline | Visual history of a single function/type across commits — who changed it, when, why. Uses entity lineage (already tracked). |
| Dependency graph | Package/module level visualization, circular dependency detection, impact radius |
| Rich diff UI | Side-by-side entity-aware diffs with syntax highlighting, inline comments anchored to entities (model exists, needs UI) |
| Code health dashboard | Per-repo metrics: dead code ratio, API stability score, test coverage by entity, churn hotspots |

---

## Phase 4 — Enterprise

**Goal:** Enterprise buyer's checklist. The monetization path.

**What already exists (preserve):**
- Organizations with member roles (owner/member)
- Collaborator permissions (admin/write/read)
- Branch protection rules with multiple policy types
- Webhook system with delivery tracking and redelivery
- SSH commit signing + server-side verification — foundation for commit provenance, signed merge enforcement, and supply chain security

**What's new:**

| Component | Description |
|-----------|-------------|
| SSO/SAML | OAuth2, SAML 2.0, OIDC for enterprise identity providers |
| Audit logs | Immutable log of all actions: pushes, merges, permission changes, policy overrides |
| RBAC | Custom roles beyond admin/write/read, org-level and repo-level inheritance |
| Shared-instance multi-tenancy with RLS | Add `tenant_id` to all tenant-scoped tables, enforce Postgres RLS using `current_setting('app.tenant_id')`, set tenant context per request/connection, and isolate storage paths as `{root}/{tenant_id}/{repo_id}/` |
| Compliance policies | "All public API changes require security review", "No merge without 2 CODEOWNERS approvals" (extend existing merge gates) |
| Horizontal scaling | Stateless API servers + shared object storage (S3/GCS for packs) + distributed index |
| On-prem deployment | Helm chart, Terraform module, air-gapped install |
| Backup/restore | Point-in-time recovery, cross-region replication |
| Admin console | Org management, usage analytics, health monitoring, license management |

### Phase 4.1 (Queued): Shared-Instance Multi-Tenancy with RLS

This is the cloud hardening milestone required before running multiple companies on a shared Postgres instance.

**Required changes:**
- Add `tenant_id BIGINT NOT NULL` to tenant-scoped tables (`users`, `orgs`, `repositories`, and dependent tables via foreign-key propagation).
- Enable Postgres RLS on every tenant-scoped table with policies enforcing `tenant_id = current_setting('app.tenant_id')::bigint`.
- Add request middleware + DB session plumbing to set `app.tenant_id` for each request/transaction.
- Move repository storage layout to `{root}/{tenant_id}/{repo_id}/`.

**Why this matters:** database-enforced tenancy boundaries provide defense-in-depth when application logic fails.

---

## Success Metrics

| Metric | Current (estimated) | Phase 1 Target |
|--------|-------------------|----------------|
| Push latency (10K file repo) | 5-30s (sync indexing) | < 200ms (async) |
| Symbol search (100K entities) | 500ms+ (linear scan) | < 50ms (FTS + DB index) |
| Find references | 500ms+ (linear scan) | < 50ms (xref table query) |
| Call graph query | 2s+ (rebuild from scratch) | < 200ms (persistent xref) |
| Merge base (10K commit depth) | O(n) seconds | < 10ms (generation pruning + cache) |
| Merge preview (cached) | Full recompute | < 100ms |
| Storage efficiency | 1 file per object (1x) | 10-50x reduction (pack + delta) |
| Clone (1GB repo, Got-to-Got) | Minutes (individual objects) | < 30s (pack transfer + zstd) |
