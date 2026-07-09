# Upload Page — Clipboard Paste Support

**Date:** 2026-07-09
**Status:** Planning
**Scope:** Web client (`client/`)

## Problem

The upload page (`/upload`) currently supports two ways to provide a receipt photo: clicking the dropzone to open a file picker, and dragging a file onto the dropzone. There is no way to paste an image from the clipboard, which is a common workflow — users frequently take screenshots of digital receipts (email confirmations, banking apps, delivery services) or have already copied an image to their clipboard when they think to log it.

The `Cmd+V` / `Ctrl+V` shortcut does nothing on this page, so users either have to save the image to disk first (annoying) or realize the page is missing a feature and fall back to drag-and-drop from a different app.

## Goals

1. Users can paste an image from the clipboard on the upload page using `Cmd+V` / `Ctrl+V` from anywhere on the page (no focus required).
2. The pasted image flows through the exact same pipeline as the existing file-input and drag-and-drop paths: hash → preview → cropper → upload.
3. The dropzone text is updated to advertise the paste capability so users know it exists.
4. Non-image clipboard contents (text, etc.) pass through unchanged so we don't break other expected behavior.

## Non-goals

- A dedicated "Paste from clipboard" button — `Cmd+V` is the universal shortcut and a button would clutter the small dropzone.
- Paste support on any other page (proposal editing, login, etc.) — only the upload page needs it.
- Programmatic clipboard access via a "Read clipboard" button (would require the Clipboard API `read()` permission prompt, which is heavyweight for this use case).
- Showing a different UI for paste vs. drag — both end up in the same cropper, so no distinction is needed.

## Design

### Event handling

Add a `paste` event listener on `document`, registered when the upload page mounts and removed when it unmounts. This is the standard pattern for "paste anywhere" file uploads (GitHub, Cloudflare, etc.) because most non-input elements don't fire `paste` on their own.

The handler:
1. Returns early if `preview.val` is not null — a cropper is already open, ignore the paste.
2. Iterates `e.clipboardData?.items` looking for the first item with `kind === "file"` and `type.startsWith("image/")`.
3. If found, calls `e.preventDefault()` and converts the item to a `File` via `item.getAsFile()`, then passes it through the same `processFile` helper that file-input and drag-and-drop use.
4. If no image is found, returns without calling `preventDefault()` — default paste behavior proceeds (text would be pasted into whatever is focused, which is normally nothing on this page).

### Refactor: shared `processFile` helper

Currently `handleFileSelect` (file input) and `handleDrop` (drag-and-drop) duplicate the same logic: assign `URL.createObjectURL(file)` to `preview.val`, compute hash, clear error. Extract this into a single `processFile(file: File)` helper that all three entry points (file input, drop, paste) call.

### CSS / dropzone text

Update the dropzone hint text in `client/pages/upload.ts` to advertise the paste capability. Current text:

```
Drag & drop receipt photo here
or click to select
```

New text:

```
Drag & drop, paste (⌘V), or click to upload
or select a file
```

(Use `Ctrl+V` on non-Mac platforms. A small inline check `navigator.platform.includes("Mac")` keeps the hint accurate.)

The existing `.dropzone` styles in `client/styles/main.css` need no structural changes — only the text inside changes. No new CSS rules are required.

### Lifecycle / cleanup

The `paste` listener is added on mount and never explicitly removed — this matches the existing pattern in `client/components/cropper.ts` (which leaks `document` listeners and is accepted because the upload page is short-lived). The handler is safe to call after unmount: the early-return on `preview.val` (which is non-null in any normal post-unmount state) and the closure-captured `processFile` operating on a no-longer-rendered `van.state` are both no-ops. The accepted trade-off is a small closure leak, consistent with the rest of the app. If a global cleanup pass happens later, this listener should be added to it.

## Files changed

| File | Change |
|------|--------|
| `client/pages/upload.ts` | Add `paste` event listener on `document`; extract `processFile` helper; update dropzone text. |
| `client/styles/main.css` | No structural changes — text-only update lives in the page. |

## Edge cases

| Case | Behavior |
|------|----------|
| No image on clipboard (text, HTML) | Handler returns without `preventDefault()`. Default paste proceeds. |
| Multiple images on clipboard | First image wins. |
| Pasting while cropper is open | Ignored — `preview.val` is set, handler early-returns. |
| Clipboard API not available (very old browser) | `navigator.clipboard` is not used; only the `paste` event's `clipboardData` is read, which has been supported since IE 11. No special handling needed. |
| User pastes into a future text input on the page | Dropdown text input doesn't exist on this page, so this isn't a concern today. If a text input is added later, the handler should check `e.target` and skip if it's a text input. |

## Testing

- Manual: open the upload page, copy an image (right-click → Copy Image, or take a screenshot and paste), press `Cmd+V`. The image should appear in the cropper, with the same hash and upload flow as drag-and-drop.
- Manual: copy text to clipboard, press `Cmd+V` on the upload page. Nothing should change (no error, no UI shift).
- Manual: after image is cropped and submitted, navigate away and back. Paste should still work; no duplicate listeners.
- Manual: navigate to `/receipts` (or any other page) and press `Cmd+V`. Should behave normally (default paste or no-op), not attempt to upload.

## Out of scope

- Receiving pasted **files** (not images) — rejected by the `type.startsWith("image/")` check. Could be relaxed later if needed.
- Showing a toast / visual feedback that a paste was received — the cropper appearing is the feedback.
- Drag-and-drop visual feedback for paste — paste has no drag preview, so the same dropzone hover styles don't apply. Not needed.
