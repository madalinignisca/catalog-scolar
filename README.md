# CatalogRO

Catalog Școlar Digital pentru școli primare, gimnaziale și liceale din România.

## Quick Start

```bash
# 1. Start infrastructure (PostgreSQL, Redis, MinIO, Mailpit)
docker compose up -d

# 2. Run database migrations
make migrate

# 3. Load seed data (2 test schools)
make seed

# 4. Start API + web dev servers
make dev
```

**Dev URLs:**
- Web app: http://localhost:3000
- API: http://localhost:8080
- Mailpit (email testing): http://localhost:8025
- MinIO console: http://localhost:9001

**Test accounts** (password: `catalog2026`):
- `director@scoala-rebreanu.ro` — Admin, Școala Gimnazială "Liviu Rebreanu" (primar + gimnaziu)
- `director@vianu.ro` — Admin, Liceul Teoretic "Tudor Vianu" (liceu)
- `ana.dumitrescu@scoala-rebreanu.ro` — Profesor, diriginte 2A
- `ion.moldovan@gmail.com` — Părinte

## Stack

| Layer | Technology |
|-------|-----------|
| API | Go 1.24, chi, sqlc, goose, River |
| Database | PostgreSQL 17 with Row-Level Security |
| Frontend | Nuxt 3, TypeScript, Tailwind CSS |
| Offline | IndexedDB (Dexie.js) + sync queue |
| Infrastructure | K3S, Helm, Traefik, cert-manager |

## Project Structure

```
catalogro/
├── api/          Go backend
├── web/          Nuxt 3 frontend (PWA)
├── helm/         Kubernetes charts
└── docs/         Documentation
```

See [CLAUDE.md](CLAUDE.md) for development conventions.

## License

Proprietary. All rights reserved.
