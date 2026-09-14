# SAO TRPG Engine

A lightweight TRPG engine with scene flow, layered locations, interaction, and AI-driven narration. Supports structured PostgreSQL storage with pgvector embeddings and local JSON fallback.

## Quick Start

### 1) Configure environment

Create/edit `.env` (already loaded on startup). Example:

```
WORLD_PG_DSN=postgres://<user>:<pass>@<host>:5432/SAO?sslmode=disable
WORLD_ID=default
WORLD_DB_MODE=structured

SEMANTIC_PG_DSN=postgres://<user>:<pass>@<host>:5432/SAO?sslmode=disable
SEMANTIC_EMBEDDING_DIM=768

AI_REFEREE_CONFIG=config/ai_referee.json
AI_REFEREE_TOKEN=<token>
```

If you need to override embedding endpoint/model:

```
LAYER_EMBEDDING_URL=http://127.0.0.1:11434/v1/embeddings
LAYER_EMBEDDING_MODEL=nomic-embed-text:latest
```

### 2) Initialize database schema

```
go run -tags pgvector ./cmd/migrate_db
```

### 3) Import existing world.json into structured tables

```
go run -tags pgvector ./cmd/migrate_world_to_db
```

### 4) Run CLI demo

```
go run -tags pgvector ./cmd/demo
```

### 5) Run the text TRPG server

```
go run -tags pgvector ./cmd/server
```

Server listens on `:8080`.

This server exposes only the text TRPG APIs documented below. It does not load
the pixel-world save, rendering pipeline, or pixel asset generation code.

## Pixel Prototype Isolation

The pixel prototype is retained as an independent optional application and is
not part of the text server dependency graph:

- server entry: `cmd/pixel_server`
- HTTP adapter: `internal/pixelapi`
- world implementation: `internal/pixelworld`
- clients: `game-client` and `unity-client`

Only start it explicitly with `run-pixel-server.cmd` or:

```bash
go run -buildvcs=false ./cmd/pixel_server
```

## CLI Commands

Layer commands:
- `/push <layer>`
- `/pop`
- `/replace <layer>`
- `/addlayer <layer> [relation] [parent]`
- `/where`
- `/visible`
- `/inventory`

Interaction commands:
- `/interact <target>`
- `/observe <target>`
- `/pick <target>`
- `/drop <target>`

Natural language works too, e.g.
- `进入洋房`
- `返回上层`
- `打开箱子`
- `观察四周`

## Storage Modes

- Structured DB mode (recommended):
  - `WORLD_PG_DSN` set
  - `WORLD_DB_MODE=structured`
  - Requires `-tags pgvector`

- JSON file fallback:
  - If `WORLD_PG_DSN` is empty or build tag is missing, the engine uses `data/world.json`.

## Embeddings

- Every **scene/layer/object** gets an embedding when persisted.
- Matching for **layer** and **visible objects** is done via database embeddings first, then falls back to old matcher if needed.

## API Docs

See `docs/api.md`.

## Frontend SSE

The backend exposes `GET /events?player_id=<id>` as SSE stream.

Example:

```ts
const es = new EventSource(`${baseUrl}/events?player_id=p1`);
es.onmessage = (e) => {
  const data = JSON.parse(e.data);
  console.log(data);
};
```

## Streamlit Demo

A lightweight Streamlit frontend demo is available at `streamlit_app.py`.

Run backend first:

```bash
go run -tags pgvector ./cmd/server
```

Run Streamlit UI in another terminal:

```bash
streamlit run streamlit_app.py
```

The demo covers:
- auth login / logout / account listing
- `/nl-action`
- `/action`
- `/interact`
- `/observe`
- `/pick-drop`
- `/scene/layer`
- `/scene/nearby`
- `/player/panel/get`
- `/player/panel/set`
- `/player/inventory/get`
- SSE event stream viewer
