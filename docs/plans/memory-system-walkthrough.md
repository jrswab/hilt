# How to Build a Dependency-Free Persistent Memory Palace for AI Agents

A practical guide to giving your AI agent persistent memory using nothing but markdown files and grep.

---

## Table of Contents

1. [Introduction](#introduction)
2. [Prerequisites](#prerequisites)
3. [System Overview](#system-overview)
4. [Component 1: The Wake-Up Layer](#component-1-the-wake-up-layer-criticalmd)
5. [Component 2: Structured Daily Notes](#component-2-structured-daily-notes-winghallroom)
6. [Component 3: Temporal Knowledge Graph](#component-3-temporal-knowledge-graph-graphmd)
7. [Component 4: Cross-Links (Tunnels)](#component-4-cross-links-tunnels)
8. [Component 5: Closet Summaries](#component-5-closet-summaries)
9. [Component 6: Memory Search](#component-6-memory-search)
10. [Putting It All Together](#putting-it-all-together)
11. [Suggested Implementation Order](#suggested-implementation-order)
12. [Appendix: LLM Instructions Template](#appendix-llm-instructions-template)

---

## Introduction

AI agents wake up blank every session. They have no continuity — no memory of yesterday's decisions, last week's architecture change, or the blocker that's been stalling a project for a month. Most solutions reach for vector databases, embedding APIs, or external memory services. This guide takes a different approach.

This system is a **dependency-free persistent memory** built entirely from markdown files and standard unix tools. No databases. No APIs. No Python packages. Just structured text files and `grep`.

The design is adapted from [MemPalace](https://github.com/milla-jovovich/mempalace), which reports 96.6% retrieval accuracy with zero API calls and a 34% retrieval improvement from hierarchical structure over flat search. We adapted MemPalace's structural principles for a local-first markdown system to avoid introducing external dependencies. We have not independently replicated those benchmark numbers — but the design principles (layered loading, hierarchical structure, compressed summaries) are sound and have proven effective in daily use.

**What you'll build:** 6 components that layer on top of each other to give your AI agent persistent, searchable, structured memory.

---

## Prerequisites

- A workspace directory your LLM can read and write files in
- `grep` and/or `ripgrep` (`rg`) available in the terminal
- Standard unix tools: `find`, `head`, `wc`, `test`
- An LLM with file access (Claude Code, Cursor, Windsurf, Aider, or similar)
- A `memory/` directory in your workspace root

---

## System Overview

The system uses **layered loading** — not everything is read every session. Files are organized into tiers by how often they're needed:

| Layer | What | When Loaded | Purpose |
|-------|------|-------------|---------|
| L0 | Agent instructions (your AGENTS.md, system prompt, etc.) | Every session | Identity, behavior rules |
| L1 | `memory/critical.md` | Every session | Compressed current state (~200 tokens) |
| L2 | `memory/YYYY-MM-DD.md` (today + yesterday) | Every session | Recent context |
| L3 | Everything else (`graph.md`, closets, old notes) | On demand | Deep archive, searched when needed |

**File map:**

```
your-workspace/
├── memory/
│   ├── critical.md              # L1 — compressed wake-up layer
│   ├── graph.md                 # L3 — temporal knowledge graph
│   ├── 2025-01-15.md            # L2/L3 — daily note (main)
│   ├── 2025-01-15-meeting.md    # L2/L3 — daily note (topic file)
│   ├── 2025-01-08-closet.md     # L3 — summary of an old day
│   └── ...
├── projects/
│   ├── project-a/AGENTS.md      # L2 — includes Tunnels section
│   └── project-b/AGENTS.md
└── AGENTS.md                    # L0 — your agent's instructions
```

**Core principle:** Structure over summarization. Tiered loading over "dump everything into context." The hierarchy itself is what makes retrieval fast — not clever prompting or semantic search.

---

## Component 1: The Wake-Up Layer (`critical.md`)

### Why

Every session, your agent needs to know what's currently true: who's on the team, what projects are active, what decisions are in effect. Loading all daily notes to reconstruct this is wasteful. `critical.md` compresses the current state into ~200 tokens that load every single session.

### Format

AAAK-style compressed shorthand. No prose, no markdown headers, no tables. Just `KEY: value` lines.

```markdown
TEAM: ALICE(eng-lead) | BOB(pm) | CAROL(backend)
PROJ: ATLAS(integration,80%) | BEACON(deployed,monitoring) | COMPASS(planning)
HEALTH: RUNNING(5k-goal,3x-week)
DECISIONS: AUTH→oauth2(2025-01-10) | DB→postgres>mysql(2025-01-08)
STACK: review→projects/atlas/priority-stack.md
EXPERIMENTS: NEW-SEARCH(testing,phase1)→docs/search-experiment.md
BLOCKERS: DEPLOY(staging-env-down,waiting-on-infra)
UPDATED: 2025-01-15
```

### Rules

- Each line starts with the category label in UPPERCASE, followed by a colon and space
- Values are pipe-delimited with spaces: ` | `
- Parenthetical details attach to the preceding item: `ATLAS(integration,80%)`
- Arrow `→` indicates a transition or pointer. Greater-than `>` indicates a choice between alternatives
- Dates are always absolute (`2025-01-15`), never relative
- No sentences, no explanation. If it can't be compressed to shorthand, it doesn't belong here
- The `UPDATED:` line at the bottom tracks when this was last refreshed
- Omit categories that have no current content — don't include empty categories

### Categories

| Category | Purpose |
|----------|---------|
| `TEAM` | People you work with regularly — names with roles |
| `PROJ` | Active projects with current status/phase — only active, not completed |
| `HEALTH` | Active tracking metrics (optional — only if relevant to your use case) |
| `DECISIONS` | Recent significant decisions still in effect (~last 30 days) |
| `STACK` | Pointer to where current priorities live |
| `EXPERIMENTS` | Active experiments or evaluations in progress |
| `BLOCKERS` | Anything currently blocking progress — remove when resolved |

### Maintenance

- **Cadence:** Weekly full rewrite (e.g., every Monday)
- **Process:** Scan daily notes from the past 7 days, extract current state for each category, rewrite the entire file
- This is a **full rewrite**, not an append — the file always reflects current state
- Remove items when they're no longer active (project completed, blocker resolved, decision superseded)

---

## Component 2: Structured Daily Notes (Wing/Hall/Room)

### Why

Flat bullet lists are hard to grep through. When every note is just `- did this`, `- decided that`, `- met with Alice`, retrieval requires reading everything. A consistent hierarchy lets you target exactly what you need: `grep "Hall: decisions" memory/` instantly finds every decision across all daily notes.

### Format

Daily notes use a three-level hierarchy:

- **Wing** (`## Wing: {domain}`) — Top-level domain (e.g., Work, Health, Projects, Personal)
- **Hall** (`### Hall: {category}`) — Category within a wing (e.g., decisions, events, discoveries)
- **Room** (`#### Room: {topic}`) — Optional sub-topic within a hall, for when a hall has multiple threads

```markdown
# 2025-01-15

## Wing: Work

### Hall: decisions
- [decision] Switched auth provider to OAuth2 — latency was 3x better in benchmarks

### Hall: events
- [event] Sprint planning — Atlas integration at 80%, targeting Friday demo

## Wing: Projects

### Hall: tasks
- [task] Shipped Beacon monitoring dashboard
- [task] Started Compass project planning doc

#### Room: compass-architecture
- [discovery] Found that event sourcing simplifies the audit trail requirement

## Wing: Health

### Hall: metrics
- [metric] 5K run: 24:32 (PB by 15 seconds)
```

### Recommended Halls

These are defaults — invent new ones when content doesn't fit:

| Hall | Purpose |
|------|---------|
| `decisions` | Choices made, options locked in |
| `events` | Sessions, milestones, meetings, debugging |
| `discoveries` | Breakthroughs, new insights, learnings |
| `preferences` | Habits, likes, opinions expressed |
| `advice` | Recommendations given or received |
| `metrics` | Quantitative tracking data |
| `tasks` | Work items completed or in progress |
| `blockers` | Things preventing progress |

### Rules

- Each bullet entry uses a bracketed tag: `- [tag] Content here` (lowercase, matches the hall purpose)
- Tags are recommended for grep support, not strictly enforced
- One wing per domain per file — don't repeat the same wing header
- Use consistent wing names across all daily notes (always "Work", not sometimes "Work" and sometimes "Job")
- Main daily notes keep `# YYYY-MM-DD` as the first header

### The 5-Bullet Rule

If a section in a daily note exceeds ~5 bullets, move the detail to a dedicated topic file and replace with a one-line pointer:

```markdown
### Hall: events
- [event] Sprint planning — see `memory/2025-01-15-sprint-planning.md`
```

Topic files (`memory/YYYY-MM-DD-topic.md`) use the same Wing/Hall structure for their substantive content.

---

## Component 3: Temporal Knowledge Graph (`graph.md`)

### Why

Daily notes record what happened. But answering "who was assigned to Project Atlas on January 8th?" requires reading through every note until you find the answer. The temporal graph stores facts as structured triples that can be queried instantly with grep.

### Format

A markdown table in `memory/graph.md`:

```markdown
# Temporal Graph

Lightweight temporal knowledge graph for fact-checking and timeline queries.

## Temporal Triples

| Date | Subject | Relation | Object | Status |
|------|---------|----------|--------|--------|
| 2025-01-08 | ATLAS | decided→ | event-sourcing-architecture | active |
| 2025-01-10 | AUTH | switched→ | oauth2-provider | active |
| 2025-01-12 | ALICE | assigned→ | ATLAS | 80% |
| 2025-01-12 | BEACON | released→ | v2.1.0 | complete |
| 2025-01-05 | BOB | works_on→ | COMPASS | ended:2025-01-11 |
| 2025-01-11 | BOB | assigned→ | ATLAS | active |
```

### Entity Naming

Consistent names make grep reliable:

- **People:** ALL-CAPS first name (`ALICE`, `BOB`). If ambiguous, append last initial (`DAVE-F`).
- **Projects:** ALL-CAPS project name (`ATLAS`, `BEACON`, `COMPASS`)
- **Systems/tools:** ALL-CAPS descriptive name (`AUTH`, `PROXY`, `DEPLOY`)

### What Gets a Triple

| Event Type | Example |
|------------|---------|
| Assignment (person → project) | `ALICE assigned→ ATLAS` |
| Decision | `AUTH decided→ oauth2-over-saml` |
| Status change | `ATLAS status→ integration-phase` |
| Configuration change | `PROXY switched→ new-endpoint` |
| Blocker raised/resolved | `DEPLOY blocked_by→ staging-env-down` |
| Release/deployment | `BEACON released→ v2.1.0` |

**Do NOT create triples for:** routine metrics, ephemeral session events, opinions or preferences. Those belong in daily notes.

The test: if you'd want to answer "what was X doing/using/decided on date Y?", it deserves a triple.

### Invalidation

When a fact is no longer true, update the Status column to `ended:YYYY-MM-DD`:

```
| 2025-01-05 | BOB | works_on→ | COMPASS | ended:2025-01-11 |
```

Then add a new row for the replacement fact if one exists. This is an in-place edit, not a new row for the ending.

### Query Patterns

| Intent | Command |
|--------|---------|
| All facts about an entity | `grep "ENTITY" memory/graph.md` |
| Current facts (exclude ended) | `grep "ENTITY" memory/graph.md \| grep -v "ended:"` |
| All ended facts | `grep "ended:" memory/graph.md` |
| All facts of a relation type | `grep "relation→" memory/graph.md` |

---

## Component 4: Cross-Links (Tunnels)

### Why

Projects don't exist in isolation. Your CLI tool shares patterns with your backend service. Your blog posts reference your open-source project. Without explicit links, knowledge stays siloed — the agent working on Project A doesn't know that Project B solved the same problem last month.

### Format

Add a `## Tunnels` section to each project's configuration file (AGENTS.md or equivalent):

```markdown
## Tunnels
- [[projects/beacon/AGENTS.md]] — Shared monitoring patterns, same observability stack
- [[projects/compass/AGENTS.md]] — Compass planning references Atlas architecture decisions
- [[docs/architecture-decisions.md]] — Past ADRs that inform current work
```

### Rules

- Use wiki-link syntax: `[[relative/path/from/repo/root]]`
- Include a brief description of **why** the link exists
- **Bidirectional:** If A links to B, B must link back to A
- Point to AGENTS.md files or specific section anchors (`#section-name`)
- Data stores (like a zettelkasten index) can be linked one-directionally — they don't need a reverse tunnel

### Example

In `projects/atlas/AGENTS.md`:
```markdown
## Tunnels
- [[projects/beacon/AGENTS.md]] — Atlas feeds events that Beacon monitors
- [[projects/compass/AGENTS.md]] — Compass will replace Atlas's legacy module
```

In `projects/beacon/AGENTS.md`:
```markdown
## Tunnels
- [[projects/atlas/AGENTS.md]] — Beacon monitors Atlas event pipeline
```

---

## Component 5: Closet Summaries

### Why

After a few weeks, your `memory/` directory accumulates dozens of files. Reading through a 150-line daily note from three weeks ago to find one decision is slow. Closet summaries compress old days into ≤20-line overviews that surface key points without losing the original.

### When to Create

A closet is created when **both** conditions are met:

1. The date is **older than 7 days**
2. The **combined line count** of all files for that date exceeds **50 lines**

### Format

```markdown
# Summary: 2025-01-08

## Key Points
- [event] Built OAuth2 proxy with auto token refresh
- [decision] Trimmed model catalog to two providers only
- [discovery] Root cause of timeout: upstream 120s ceiling on origin response
- [blocker] Timeout blocking milestone 003

## Files
- `2025-01-08.md` — Main daily log: sprint planning, proxy setup
- `2025-01-08-auth-research.md` — Deep dive on OAuth2 vs SAML benchmarks
```

### Rules

- **Naming:** `memory/YYYY-MM-DD-closet.md`
- **One closet per date** — summarizes all files for that date (main note + topic files)
- **Max 10 bullet points** under Key Points — capture what happened and what was decided, not implementation details
- **Each bullet is one line**, max ~120 characters
- The **Files** section lists every `memory/YYYY-MM-DD*.md` file for that date with a brief description
- **Additive only** — original files are never deleted or modified
- **Don't overwrite** an existing closet. If stale, update in place
- Closets are **L3 / on-demand** — not loaded at session startup
- Created on demand during historical scanning or dedicated maintenance, not on a fixed schedule

---

## Component 6: Memory Search

### Why

Without a defined search procedure, agents either load everything into context (expensive) or grep randomly (unreliable). This component defines exactly how to search, in what order, and when to stop.

### When to Search

- User asks about a past event, decision, or fact
- Agent needs to verify a claim or check for contradictions
- User explicitly says "search memory" or "check if we discussed X"
- **NOT** during session startup — startup loads L0/L1/L2 only

### Search Order

Stop as soon as the query is answered:

| Priority | Source | When to Use |
|----------|--------|-------------|
| 1 | `memory/graph.md` | Query is about an entity, assignment, status, or timeline |
| 2 | `memory/YYYY-MM-DD-closet.md` | Query targets a date >7 days old and a closet exists |
| 3 | `memory/YYYY-MM-DD*.md` (excl. closets) | No closet exists, closet didn't answer, or date is within 7 days |
| 4 | `memory/critical.md` | Query is about current active state (already in context at startup) |
| 5 | Long-term memory file (your equivalent of MEMORY.md) | Curated facts not in daily notes |

### Closet Prioritization Logic

- Compute the 7-day cutoff dynamically at search time
- Before reading a closet, verify it exists: `test -f memory/YYYY-MM-DD-closet.md`
- If the closet exists but doesn't answer the query, fall through to full daily notes
- For today or yesterday: skip closet check — these are already loaded at startup

### Commands by Source

**Temporal graph:**
```bash
grep "ENTITY" memory/graph.md                     # All facts about an entity
grep "ENTITY" memory/graph.md | grep -v "ended:"  # Current facts only
grep "relation→" memory/graph.md                   # All facts of a relation type
```

**Closet summaries (dates >7 days old):**
```bash
test -f memory/YYYY-MM-DD-closet.md && echo "exists"  # Check existence
rg "KEYWORD" memory/YYYY-MM-DD-closet.md               # Search specific closet
rg "KEYWORD" memory/*-closet.md                         # Search all closets
```

**Daily notes and topic files:**
```bash
# Search a specific date (exclude closets)
find memory/ -name "YYYY-MM-DD*.md" -not -name "*-closet.md" | xargs rg "KEYWORD"

# Search by tag across all notes
rg "\[decision\]" memory/YYYY-MM-DD*.md

# Search by Wing or Hall
rg "Wing: Work" memory/
rg "Hall: decisions" memory/

# Search a date range
find memory/ -name "2025-01-*.md" -not -name "*-closet.md" | xargs rg "KEYWORD"

# Broad search (no date, no entity)
rg "KEYWORD" memory/graph.md; rg "KEYWORD" memory/*-closet.md; rg -l "KEYWORD" memory/
```

### Termination Criteria

- **Stop** when the query is answered (specific fact, decision, or event found)
- **Stop** after checking all applicable sources and report "not found in memory"
- **Do NOT** exhaustively search all files if an earlier source already answered

### Example Queries

**Entity/timeline query:** "What is Alice working on?"
```bash
# Priority 1: Check temporal graph
grep "ALICE" memory/graph.md | grep -v "ended:"
# → 2025-01-12 | ALICE | assigned→ | ATLAS | 80%
# Answer found. Stop.
```

**Historical query (>7 days old):** "What was the root cause of the timeout issue?"
```bash
# Priority 1: Check graph
grep "TIMEOUT" memory/graph.md
# → Nothing specific. Continue.

# Priority 2: Search closets
rg "timeout" memory/*-closet.md
# → memory/2025-01-08-closet.md: [discovery] Root cause of timeout: upstream 120s ceiling
# Answer found. Stop.
```

**Recent query (<7 days old):** "What did we decide about auth yesterday?"
```bash
# Yesterday is within 7 days — skip closet check
# Priority 1: Check graph
grep "AUTH" memory/graph.md | grep -v "ended:"
# → 2025-01-10 | AUTH | switched→ | oauth2-provider | active

# If more detail needed, Priority 3: daily notes
find memory/ -name "2025-01-14*.md" -not -name "*-closet.md" | xargs rg "auth"
# Answer found. Stop.
```

---

## Putting It All Together

### Session Startup Sequence

Every session, the agent reads these files in order:

1. **Agent instructions** (AGENTS.md, system prompt, etc.) — identity and behavior rules
2. **`memory/critical.md`** — compressed current state
3. **Today + yesterday's daily notes** (`memory/YYYY-MM-DD.md`) — recent context
4. **Long-term memory** (optional, main sessions only) — curated preferences and project indexes

Everything else (`graph.md`, closets, old daily notes) is loaded on demand when a query requires it.

### Maintenance Cadence

| Task | When | What to Do |
|------|------|------------|
| Write daily notes | Every session | Log decisions, events, discoveries in Wing/Hall/Room format |
| Add graph triples | Every session (when qualifying events occur) | One triple per decision, assignment, status change |
| Refresh `critical.md` | Weekly | Scan past 7 days of notes, rewrite the file to reflect current state |
| Create closet summaries | On demand | When scanning historical notes >7 days old that exceed 50 lines |
| Update tunnels | When project relationships change | Add/remove cross-links in project AGENTS.md files |

### How the Components Interact

```
Daily Notes (Wing/Hall/Room)
    │
    ├──→ feeds graph triples (decisions, assignments, status changes)
    ├──→ feeds critical.md snapshots (weekly refresh from recent notes)
    ├──→ feeds closet summaries (compressed versions of old notes)
    │
    └──→ Memory Search queries across all of the above
```

Daily notes are the primary record. Everything else derives from or indexes into them.

---

## Suggested Implementation Order

The components build on each other. Here's the recommended order:

### Phase 1: Foundation (Start Here)

**1. `critical.md`** — Create first. Immediately reduces per-session context load. Takes ~30 minutes to set up, ~5 minutes per week to maintain. You'll feel the difference on session 1.

**2. Structured Daily Notes** — Adopt next. Every note you write from this point forward uses Wing/Hall/Room. Optionally retrofit the last 7 days of existing notes. Takes ~1 hour to establish the pattern, then ~2 minutes per day of overhead.

These two components are independently useful. If you stop here, you already have a significantly better memory system than flat files.

### Phase 2: Queryability

**3. Temporal Graph** — Build after you have a week of structured daily notes to seed from. The graph makes timeline queries instant instead of requiring full-text search. Takes ~1 hour to set up, ~1 minute per day maintenance.

**4. Memory Search** — Define after the graph exists. The search workflow ties all the pieces together with a defined procedure. Takes ~1 hour to document and test.

### Phase 3: Scale and Connectivity

**5. Closet Summaries** — Create once your daily notes are old enough (>7 days) and long enough (>50 lines combined). These become more valuable as your archive grows. Created on demand as needed.

**6. Cross-Links (Tunnels)** — Add when you have multiple projects that reference each other. This is the least time-sensitive component — it's a one-time audit of relationships. Takes ~45 minutes to establish.

### Can I Skip Components?

- **`critical.md` + Daily Notes** work as a standalone pair
- **Temporal Graph** requires structured daily notes to seed from effectively
- **Memory Search** is most useful when the graph and closets exist
- **Closet Summaries** only matter once you have enough history (weeks/months)
- **Tunnels** are independent — add them whenever you have multiple projects

---

## Appendix: LLM Instructions Template

Copy and paste the following block into your agent's system prompt, AGENTS.md, CLAUDE.md, or equivalent configuration file. Replace placeholder values with your own.

---

````markdown
## Memory System

You have a persistent, file-based memory system in the `memory/` directory. You wake up fresh each session — these files are your continuity.

### Session Startup

Every session, read these files in order:
1. Your agent instructions (this file)
2. `memory/critical.md` — compressed current state
3. `memory/YYYY-MM-DD.md` for today and yesterday — recent context

Do not load other memory files at startup. They are searched on demand.

### critical.md — Wake-Up Layer

`memory/critical.md` is a compressed snapshot of current state (~200 tokens). Format:

```
TEAM: NAME(role) | NAME(role)
PROJ: PROJECT(status,%) | PROJECT(phase)
DECISIONS: CHOICE→result(YYYY-MM-DD) | OPTION>alternative(YYYY-MM-DD)
STACK: review→path/to/priorities.md
BLOCKERS: ISSUE(description)
UPDATED: YYYY-MM-DD
```

Rules:
- UPPERCASE category labels, pipe-delimited values, parenthetical details
- No prose, no sentences. Shorthand only.
- Absolute dates only (never "last week")
- Omit empty categories
- Full rewrite weekly (scan past 7 days of daily notes, update to current state)

### Daily Notes — Wing/Hall/Room Structure

Daily notes live in `memory/YYYY-MM-DD.md`. Use this hierarchy:

- `## Wing: {domain}` — top-level domain (Work, Health, Projects, Personal)
- `### Hall: {category}` — category (decisions, events, discoveries, metrics, tasks, blockers, preferences, advice)
- `#### Room: {topic}` — optional sub-topic when a hall has multiple threads

Each entry uses a bracketed tag: `- [tag] Content here`

Example:
```markdown
# 2025-01-15

## Wing: Work

### Hall: decisions
- [decision] Switched to OAuth2 — 3x latency improvement over SAML

### Hall: events
- [event] Sprint planning — Atlas at 80%, targeting Friday demo

## Wing: Health

### Hall: metrics
- [metric] 5K run: 24:32 (personal best)
```

Rules:
- One wing per domain per file
- Consistent wing names across all daily notes
- If a section exceeds ~5 bullets, extract to `memory/YYYY-MM-DD-topic.md` and leave a pointer
- Topic files use the same Wing/Hall structure

### Temporal Graph — memory/graph.md

A markdown table for fact-checking and timeline queries. NOT loaded at startup — consulted on demand.

Table columns: `| Date | Subject | Relation | Object | Status |`

Add one triple per: decision, assignment, status change, configuration change, blocker raised/resolved, or release. Skip routine metrics and ephemeral events.

Entity naming: ALL-CAPS. People: `ALICE`, `BOB`. Projects: `ATLAS`, `BEACON`. Systems: `AUTH`, `PROXY`.

When a fact is no longer true, update its Status to `ended:YYYY-MM-DD`.

Query patterns:
```bash
grep "ENTITY" memory/graph.md                     # All facts about an entity
grep "ENTITY" memory/graph.md | grep -v "ended:"  # Current facts only
grep "ended:" memory/graph.md                      # All ended facts
grep "relation→" memory/graph.md                   # All facts of a relation type
```

### Cross-Links (Tunnels)

Each project directory's config file (AGENTS.md or equivalent) should have a `## Tunnels` section linking to related projects:

```markdown
## Tunnels
- [[projects/other-project/AGENTS.md]] — Why this relationship exists
```

Tunnels are bidirectional: if A links to B, B links back to A.

### Closet Summaries

When a date is >7 days old AND its combined files exceed 50 lines, create `memory/YYYY-MM-DD-closet.md`:

```markdown
# Summary: YYYY-MM-DD

## Key Points
- [tag] One-line summary of significant item (max 10 bullets)

## Files
- `YYYY-MM-DD.md` — Brief description of contents
- `YYYY-MM-DD-topic.md` — Brief description
```

Rules: ≤20 lines total. Additive only — never delete originals. Created on demand, not on a schedule.

### Memory Search

When you need to find something in memory, follow this search order. Stop as soon as the query is answered.

| Priority | Source | When to Use |
|----------|--------|-------------|
| 1 | `memory/graph.md` | Entity, assignment, status, or timeline query |
| 2 | `memory/YYYY-MM-DD-closet.md` | Date >7 days old and closet exists |
| 3 | `memory/YYYY-MM-DD*.md` (excl. closets) | No closet, closet didn't answer, or date within 7 days |
| 4 | `memory/critical.md` | Current active state (already in context) |

Before reading a closet, verify it exists. If it doesn't answer the query, fall through to full daily notes. For today/yesterday, skip closet check — those are already loaded.

Termination: stop when answered. Do not exhaustively search all files if an earlier source answered.
````

---

*This guide was created as part of a memory system built on MemPalace principles. The system is in active daily use and continues to evolve. Contributions and adaptations are welcome.*
