# Plan: switch between one's groups

Branch: `group-switcher` (one slice, merged with `--no-ff`). This plan is committed there and deleted before the merge.

## Problem

- Belonging to several groups already works: each link gives an independent member (identity cookie `Path=/g/<secret>`, group cookie `g<group id>`), and no group sees anything of another.
- Going from one group to another means finding the other link in WhatsApp again.

## Changes

1. **Group cookie `g<id>` on `Path=/`** instead of `/c`, so `/g/<secret>` pages receive the cookies of every group this browser opened. Value, other attributes and refresh (every GET under `/g/<secret>`) unchanged; update `setGroupCookie`'s comment.
   - Shim: the same refresh also expires the old `Path=/c` cookie (same name, path and `Secure`). Otherwise, as the longer path, browsers send it first and `r.Cookie` reads it: a stale one (rotated secret) would hide the valid new one on `/c`. Before that first GET, the old cookie alone keeps `/c` working. Removal condition: after 2028-01-15 (deploy + 400-day `Max-Age`).
   - Rollout: an old `/c` cookie never reaches `/g/…`, so a group joins the switcher once its link is opened again after the deploy.
2. **`otherGroups`** (`internal/web/identity.go`), called by `home` after the picker return (the picker pays nothing):
   - distinct cookie names `g<digits>`, minus `groupCookie(gr.Group.ID)`, first 10 in request order **before any query** (cookies are client-controlled; invalid ones use up slots);
   - value matched by `groupCookieValue`, then `GroupBySecret` alone (no member lookup: only name and link are shown), kept only if its id equals the cookie's;
   - sorted by group id; returned as name + `Base` in a new `homePage.Others`.
   - A group whose cookie names no member (picker seen, or « Ce n'est pas moi ») is listed too: holding the link is membership.
   - Other groups' credentials are only displayed, never used to authorize or set `Me` in this group's request.
3. **Shown on the main page only** (`/g/<secret>`, every filter, with identity), when there is at least one other group: not on the picker, forms, concert or history pages. Rendered in `group.html` (its `brand` or `content` block), not in `layout.html` or `partials.html`: htmx fragments of the `group` page are executed with other data and never re-render the header. Placement: see « Open choices ».
4. **`Vary: Cookie`** in `inGroup`, as `/c` already sends: group pages now depend on cookies beyond identity.
5. **Demo and `verify-ui`**:
   - `demo` creates « Les collègues » (with Léa) after « Les copains du concert », so `start_demo`'s first `/g/` link stays the main group (`measure-scroll` relies on it); update `demo`'s comment.
   - `ui-lib.sh` also sets `$other` (second `/g/` link of the log); `verify-ui.sh` passes it to `verify-ui.mjs`, which opens it before `groupURL` in the `picker` screen and in the `login` context (so the page-weight check counts the switcher), and checks the list screen shows « Les collègues » and its link.
6. **Tests** (`newTestServer`, table test):
   - no other group cookie → no switcher; a valid other group → its name and link; rotated secret, cookie value of group B under name `gA`, malformed value → absent; the current group never listed; the picker shows none;
   - 11 valid other groups → 10 listed; a group B cookie never sets `Me` in group A;
   - a GET under `/g/<secret>` sets `g<id>` with `Path=/` and expires it with `Path=/c`; `Vary: Cookie` on group pages.
7. **Docs**, in the slice's commit: `docs/design.md` Solution (a browser that opened several links lists them), threat model (below), Main screen mockup and bullet, slice 13; ADR 0009 gets an « Amended by slice 13: `Path=/` » line.

## Threat model notes (for `docs/design.md`)

- Secrets now ride on every request to the host (static files, `/healthz`): nothing logs cookies (app logs the route pattern, proxy has no access log).
- Group B's name and link appear in group A's page, only in a browser holding both cookies; nothing leaves it thanks to the CSP (`default-src 'self'`, `frame-ancestors 'none'`), `Referrer-Policy: no-referrer` and `SameSite=Lax`: relaxing any of them must weigh the switcher. A group page from a hostile operator gets the visitor's other groups rendered in the visitor's browser only.
- A screenshot of the list shows B's name; a long-press « copy link » copies B's secret.
- Shared phone: « Ce n'est pas moi » forgets the identity, not the groups; the next person reaches every group this browser opened in one tap (before, only through one of its concert links). Accepted: members are friends and the phone is theirs.
- Not done: a `__Host-` cookie prefix (blocks a sibling subdomain from planting cookies). It needs `Secure`, so development would use another name; the operator picks a host without untrusted siblings.

## Open choices

- Placement of the switcher, picked on a mockup: a line under the header, a menu next to the group name, or a section at the bottom of the list.

## Verification

- `make test`, `make lint`, `make verify-ui`.
- `make demo`: open both links, check the switcher on each; a concert link of each group still shows its member page.
