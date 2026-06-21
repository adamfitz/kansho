# OpenSpec — kansho Specification Index

This directory contains Gherkin-style specifications for the kansho manga download manager. Each spec describes the behavior of a major component.

## Specs

| Spec | File | Covers |
|------|------|--------|
| [Overview](specs/kansho-overview/spec.md) | `specs/kansho-overview/spec.md` | Application entrypoint, window, menus, keyboard shortcuts |
| [Site Plugin System](specs/site-plugin-system/spec.md) | `specs/site-plugin-system/spec.md` | Plugin interface, extraction methods (JS/selector/custom/api), site registry, config, filename normalization, CF bypass support, API client User-Agent |
| [Download Manager](specs/download-manager/spec.md) | `specs/download-manager/spec.md` | Download lifecycle, chapter/image fetch orchestration, CBZ creation, retry with backoff (skip CF challenges), context cancellation |
| [Download Queue](specs/download-queue/spec.md) | `specs/download-queue/spec.md` | FIFO queue singleton, task states, cancellation (single + all), CF challenge handling, retry, UI callbacks |
| [HTTP Client](specs/http-client/spec.md) | `specs/http-client/spec.md` | Unified HTTP client, CF bypass headers, timeout-based retry, decompression (gzip/brotli/deflate), CF challenge detection, debug HTML saving |
| [Request Executor](specs/request-executor/spec.md) | `specs/request-executor/spec.md` | HTTP-first with browser fallback strategy, CF challenge handling, domain resolution |
| [Browser Automation](specs/browser-automation/spec.md) | `specs/browser-automation/spec.md` | Chromedp session management, JS evaluation, CF cookie injection, CF challenge detection, batched HTML fetching |
| [User Interface](specs/user-interface/spec.md) | `specs/user-interface/spec.md` | Fyne layout (2-column, gradient), centralized state with observer callbacks, menus, keyboard shortcuts, views |
| [Manga Bookmarks](specs/manga-bookmarks/spec.md) | `specs/manga-bookmarks/spec.md` | Bookmark CRUD, JSON persistence, config directory, import/export, log file setup |
| [Image Processing](specs/image-processing/spec.md) | `specs/image-processing/spec.md` | Image download, WebP/PNG/GIF→JPEG conversion, CBZ archive creation, 1500ms rate limiting, context-aware sleep |
| [MangaDex](specs/mangadex/spec.md) | `specs/mangadex/spec.md` | MangaDex API integration, non-spoofed User-Agent policy, chapter/image endpoints, URL parsing, chapter filtering |
| [Validation](specs/validation/spec.md) | `specs/validation/spec.md` | Add-manga input validation against site required fields |

## Format

Each spec follows: **Purpose** → **Requirements** → **Scenarios** (Given/When/Then with `SHALL`).

## Keeping Specs in Sync

When changing behavior, update both the code and its spec. Cross-reference specs from code comments using the path `openspec/specs/<name>/spec.md`.
