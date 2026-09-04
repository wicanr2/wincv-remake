# WinCV Help

Press `Esc` to return to the file list. This help text can also be scrolled: `↑` `↓` `PgUp` `PgDn`.

The top line is the menu bar. Press `F9` to open it, `←` `→` to switch categories, `↑` `↓` to select an item, and `Enter` to run it. Each entry shows its shortcut key on the right — once you memorize it, you no longer need to open the menu.

## File List

| Key | Action |
|---|---|
| ↑ ↓ PgUp PgDn Home End | Move the cursor |
| Enter | Enter a directory; on a file, view it |
| BackSpace | Go up one directory |
| Space | Mark, cursor moves down one row |
| T / Alt-T / U | Mark all files / mark directories too / clear all marks |
| S | Change sort order |
| P | Change path |
| Alt-D | Toggle the drive pane on the left (Tab switches focus) |
| Alt-P | Toggle the preview pane at the bottom |

With the cursor on an archive, pressing `Enter` walks into it like a directory — files
inside can still be viewed and extracted. Supported: ZIP, RAR, 7Z, TAR, GZ, BZ2, LZH,
ARJ, CAB, ARC, ACE, `.Z`.

## File Operations

| Key | Action |
|---|---|
| C M R D | Copy / Move / Rename / Delete |
| Del | Permanently delete (overwrites with 0 before deleting) |
| Alt-C | Compare two marked files |
| Z / Alt-Z | Extract / Create an archive |
| W | Search filenames / strings / comments |
| Alt-E | Comment (written to that directory's `dir.doc`) |
| O / G | Open with the system's default program / Run |

MD5 and SFV checksums are under the "Tools" menu.

## Viewing Files

Press `Enter` to view a file, `Esc` to return to the list.

| Mode | How to enter | Notes |
|---|---|---|
| Text | Enter | Auto-detects Big5 / GB / UTF-8 / SJIS, recognizes ANSI color codes |
| Hex | Auto-entered for executables | |
| Image | Enter on an image file | `+` `-` zoom, arrow keys pan |
| Thumbnail list | 5 | View all images in the directory at once |
| markdown | `.md` files | Headings, lists, tables, inline images |
| Editor | E | PE2-style block editing, `F6` find/replace |

While viewing a text file, `/` searches and `n` finds the next match.

What gets extracted from a PDF is **text and images**, not layout. A PDF describes
"place this character at this coordinate" — it has no concept of paragraphs or
columns, so a paper with multiple columns comes out with the columns interleaved
left to right, and images are placed at the end of each page (a PDF never records
which paragraph an image belongs next to).

## Web Browsing

Press `F2` to enter an address. `gopher://` as well as `http://` and `https://` all
work; if no protocol is given, it is treated as gopher.

| Key | Action |
|---|---|
| ↑ ↓ Tab | Move between links; on a page with no links, this scrolls instead |
| Enter | Open the link under the cursor |
| BackSpace | Go back a page |
| F2 | Enter a new address |
| Esc | Return to the file list |

For web pages, only text, links, and images are kept — stylesheets and scripts are
always discarded, so the result looks the same as a gopher menu. **A network
connection is made only when you type an address yourself, or press `Enter` to
follow a link** — the program never connects anywhere on its own.

## Display

| Key | Action |
|---|---|
| Ctrl-+ Ctrl-- Ctrl-0 | Cycle font size (8×15 → 10×18 → 12×24) |
| Alt-+ Alt-- Alt-0 | Zoom, in steps of 0.1; Alt-0 resets to 1.0 |
| F8 | Toggle Chinese/English display |
| F11 | Fullscreen |
| F1 | This help |
| F9 | Menu |

The menu layer can use **a different font from the content**: `F9` → Settings →
Menu Font Size, or pass `-menu-font <font file> -menu-size <pixels>` at startup to
use a TTF. Content needs a bitmap font pixel-aligned with the original; the menu is
just interface — on a high-resolution screen, one size larger is actually easier to
read. Tying the two together would force a choice between one or the other.

Font size swaps **the bitmap font itself** — the glyphs are redesigned with more
detail. Zoom, by contrast, is continuous, in steps of 0.1, so it can be fine-tuned
to exactly fill the window.

Zoom does not simply stretch the glyphs — it first picks the native bitmap font
size closest to the target. The ratios between the three sizes bundled with the
original (8×15 / 10×18 / 12×24) happen to be 1.19 and 1.56, so the 1.2, 1.5, and
1.6 zoom steps land exactly on native glyphs, with every pixel as originally
designed. Other zoom levels still show some unevenness in stroke weight, but far
less than stretching 8×15 directly (1.4x zoom is really 10×18 stretched by only
1.18x). The full table is in `docs/ui/keymap.md`.

Fullwidth Chinese characters at font sizes other than 8×15 are drawn with the
system's vector font instead (anti-aliased) — ETEN only has the 15-point glyph
set, and scaling it up directly produces heavy jagged edges. To force ETEN
scaling everywhere, pass `-bitmap-cjk` at startup.

Resizing the window larger makes the content **show more**, not bigger — the cell
size is fixed, so the extra space just adds more cells. To fix the cell count, use
"Settings → Window Size" in the menu.

## Image and Full-Page PDF View

`+` / `-` zoom in and out, `1` returns to original size, `F` toggles between "fit
to window" and a fixed zoom level, and the arrow keys pan. When viewing a full PDF
page with `V`, zooming in **re-renders** at a higher resolution rather than
stretching the image — so zooming in reveals more detail.

In PDF full-page view, `PgUp` / `PgDn` (or `Enter` / `BackSpace`) turn **pages**,
not to the next image in the directory, and the zoom level carries over.
`Esc` takes you back to the text of the same page.

## Filename Column Width

The list defaults to the original's 8.3 layout. `Ctrl-→` / `Ctrl-←` widens or
narrows the filename column by one cell, or hold the left mouse button and drag
left/right on the list; long filenames then show in full in the list. This
setting is remembered.

## Mouse

Click in the list to move the cursor, double-click to open; clicking a link on a
web page follows it immediately. Click the top line to open the menu, click an
item to run it, click elsewhere to close it. The scroll wheel scrolls; holding an
arrow key auto-repeats.

## Android

Bottom three rows: the top row shows the actions for the current mode, and the two
rows below are keys — arrow keys arranged in a cross, `Esc` and `Enter` at the two
ends, `PgUp` / `PgDn` / `Home` / `End` alongside. With a physical keyboard attached
(dock or Bluetooth), the keys match the desktop version.

## Remembering the Position in Each File

Leaving and returning to a text view, editor, hex view, or markdown view restores
the last position (the editor even remembers the cursor). One entry is kept per
file, up to 500; once full, the least-recently-used entry is dropped.

## Remembering the Last Session

On exit, the program remembers which directory you were in, which file the cursor
was on, which document was open, and which line it was scrolled to — the next
launch returns to the same place. Font size, zoom, and window cell count are
carried over too.

Marks, search results, and browsing history are not remembered — those belong to
the current session only. If a directory is given on the command line at startup,
it takes precedence.

## About This Program

This is a remake of **WinCV 0.52** (CView for Windows, original author Lcc
Wizard), Taiwanese shareware from 1999–2011, rewritten in Go with open source
code. Copyright in the original belongs to its author; this rewrite exists to
preserve the software, not to claim rights to the original.

`F9` → Help → About has version and license information.
