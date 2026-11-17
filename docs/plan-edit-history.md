# Plan: edit history and undo for concerts

Branch: `edit-history` (one slice, slice 8, merged with `--no-ff`). This plan is committed there and deleted before the merge.

## Problem

- Anyone holding the link can edit any concert (ADR 0002, kept). A delete can be undone (« Annuler la suppression »), an edit cannot: « Enregistrer » overwrites the previous values, and nobody can see who changed what.
- Goal: every edit is traceable (who, when, what changed) and reversible, without accounts or roles.

## Model: wiki-style versions, nothing destructive

- Each edit that changes something first copies the concert's current editable fields into a revision row, with the editor and the time.
- Going back to a revision is an ordinary edit with that revision's values: it goes through today's validation and records a revision too, so it can itself be undone. Nothing erases a version except the per-concert cap.

## Changes

1. **Migration `0006_concert_revisions.sql`**:
   ```sql
   CREATE TABLE concert_revisions (
       id INTEGER PRIMARY KEY AUTOINCREMENT,  -- ids in ?saved= banners never point at a newer revision
       concert_id INTEGER NOT NULL REFERENCES concerts (id) ON DELETE CASCADE,
       artist TEXT NOT NULL, date TEXT NOT NULL, time TEXT, venue TEXT NOT NULL,
       address TEXT, ticket_url TEXT, on_sale_date TEXT,   -- values before the edit
       edited_by INTEGER REFERENCES members (id) ON DELETE SET NULL,  -- no index: few rows, like concerts.created_by
       edited_at TEXT NOT NULL
   ) STRICT;
   CREATE INDEX concert_revisions_concert ON concert_revisions (concert_id, id);
   ```
   No backfill: existing concerts start with no history.
2. **Store** (`internal/store/revisions.go` for the new queries):
   - `UpdateConcert(ctx, groupID, memberID, id, c, today) (revID int64, err error)`, one immediate transaction:
     1. `INSERT INTO concert_revisions (…) SELECT … FROM concerts WHERE id = ? AND group_id = ? AND deleted_at IS NULL AND (artist IS NOT ? OR time IS NOT nullif(?, '') OR …)`: NULL-safe, no Go-side comparison; `RowsAffected` 0 means a no-op (`revID` 0), else `LastInsertId`.
     2. The existing UPDATE with `mustAffect` (`ErrNotFound` unchanged).
     3. Prune: `DELETE FROM concert_revisions WHERE concert_id = ? AND id <= (SELECT id … ORDER BY id DESC LIMIT 1 OFFSET 20)` (unexported `maxRevisions = 20`).
     4. `commitWithin`: an `ErrLimit` rolls the revision back too.
     - `edited_at` from the injected clock (`s.timestamp()`).
   - `Revisions(ctx, groupID, concertID) ([]Revision, error)` (newest first, editor first name or "") and `Revision(ctx, groupID, concertID, revID) (Revision, error)`: both in one statement joining `concerts` on `r.concert_id = ? AND c.group_id = ? AND c.deleted_at IS NULL` (and `r.id = ?`), never load-then-check.
   - `Concert` gains `EditedBy string` and `LastRevID int64` (0 = never edited), from one correlated subquery `ORDER BY id DESC LIMIT 1` on the index, in `Concert()` only; the list query stays unchanged.
   - No `RestoreRevision`: going back is `Revision` then `UpdateConcert` with its validated values. If the revision is pruned in between, applying the values already read is still what the user asked.
   - Callers to update: the web update handler and `TestConcertLimit`'s `update` helper.
3. **Routes** (POSTs `requireMe`, all through `inGroup`):
   - `POST …/concerts/{id}` (update): 303 `…/concerts/{id}?saved=<revID>` (`?saved` alone for a no-op).
   - `GET …/concerts/{id}/history`: readable without identity like the concert page; buttons only `{{if .Me}}`.
   - `POST …/concerts/{id}/revisions/{rev}/revert`: loads the revision, turns it into form values and runs `parseConcert` (422 with a generic message if a value is no longer valid, nothing written), then `UpdateConcert`; 303 `…/concerts/{id}?reverted=<newRevID>`; 404 for a revision of another concert or group or a deleted concert; 422 at the limit.
4. **Concert page**:
   - Under « ajouté par Léa »: « modifié par Tom · Voir l'historique » when `LastRevID != 0` (« modifié · Voir l'historique » if Tom was deleted).
   - Banners, shown only while true (`rev` numeric, equal to `LastRevID`): `?saved=<rev>` « Modifications enregistrées. » / `?reverted=<rev>` « Version rétablie. » (title Notice too), each with an « Annuler » POST form reverting `rev`. Otherwise the plain message without the form.
5. **History page** (`history.html`, `<h1>` « Historique », not in a `<summary>`), one entry per edit, newest first, only the fields it changed:
   - « Tom · mar. 3 mars à 18 h 05 » (Paris time, `frDate`/`frTime` from `dates.go`), then « Date : sam. 14 mars → dim. 15 mars », one line per changed field (before = this revision, after = the next newer revision or the current concert). Plain text, no `<mark>`: « avant → après » reads aloud.
   - Button « Revenir avant cette modification » (also undoes newer edits, hence not « Annuler »; « Rétablir » is taken by undelete).
   - Never edited: « Aucune modification. » (200, no special 404). « ← Retour au concert ».
6. **Demo** (`cmd/concerts/demo.go`): concert 1 edited twice by two members, rev ids asserted. `scripts/verify-ui`: add `concerts/1/history` and `concerts/1?saved=<rev>` (banner with its form).
7. **Tests**:
   - Store: revision holds old values, editor, clock time; no-op edit (incl. optional "" → "") records nothing, "x" → "" records one; prune keeps exactly the 20 newest ids; `ErrLimit` writes no revision; `Revision`/`Revisions` of another concert, another group (same concert path id) or a deleted concert → `ErrNotFound`/empty; history survives delete + undo; deleted member keeps revisions with no name; row-cap purge removes the purged concert's revisions (count rows); `LastRevID`/`EditedBy`.
   - Web (`newTestServer`, table tests): update → `?saved=<rev>`; banner undo form only while `rev` is newest (absent for `?saved=abc`, a foreign rev, or after another edit); revert → values back, `?reverted=`, its banner; revert of a now-invalid stored value (inserted directly) → 422, nothing written; revert at the limit → 422; foreign revision 404; deleted concert history 404; history without identity: readable, no buttons; never-edited history: « Aucune modification ».
8. **Docs** in the same commit, `docs/design.md`:
   - Slice 8; data model (`concert_revisions`); routes table (`?saved=<rev>`, history, revert); concert-page wireframe; banner rule (line ~106) lists `saved` and `reverted`.
   - Edge cases: no-op edit, deleted member, revert at the limit, revert of a value now invalid, concurrent edits (last write wins, each recorded).
   - Threat model: a leaked link can flush a concert's history with 20 edits (as deleting then purging already flushes the past; backups remain the remedy); worst case ≈ 1000 × 20 × 3.3 kB ≈ 66 MB per group, bounded since only the admin creates groups; « modifié par » adds a first name to what link previewers see (accepted risk, listed).

## Out of scope

- Concurrent-edit detection (version check on save).
- History of statuses, deletes and restores (the delete banner already covers deletes).
- History on the main list's cards.

## Verification

- `make test`, `make lint`, `make verify-ui`, `make measure` (logged for the slice).
- `make demo`: edit a concert, undo from the banner, go back to an older version from the history page, check « modifié par ».
