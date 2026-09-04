# WinCV Remake

[繁體中文](README.md) ｜ **English** ｜ [日本語](README_jp.md)

WinCV (CView for Windows) is a piece of Taiwanese shareware. Its author, Lcc Wizard (Lin Chien-tsung), maintained it alone from 1999 to 2011 on the site cview.com.tw; the last release, 0.52, was updated on November 24, 2011.
Its executable contains no application logic — the actual program is a bundled Forth image. This repo reverse-engineers that image and rewrites it in Go as a program that is not tied to Windows. The standard is not "close enough." Which character sits in which cell on the original screen, and what color that cell is, were both measured against the original before being written down — the 8x16 grid does not shift by a single cell.

## What's done

In August 2004, someone wrote on their blog that CView was "my favorite piece of software from the DOS era" and that its image viewing was "super fast." The post's last line was: "I really wish he'd make a Linux version~~"

The original stopped at November 24, 2011. It's a 32-bit Windows executable, and extraction and image viewing depend on a set of Windows DLLs — `unrar.dll`, `unlha32.dll`, `CAB32.DLL`, `FreeImage.dll`. Those dependencies are locked to one platform, and nothing guarantees they'll still load on the next version of Windows.

Keeping the executable around isn't the same as keeping the software alive. What needs preserving is the behavior: sorting rules, how columns align, how encoding is detected, what happens when you press a given key. That only survives if it's reimplemented.

So the target of reverse engineering isn't the 52 KB `wincv.exe` (that's just the Forth kernel, with no application logic) but the 1.52 MB `WINCV.IMG`: 3663 words, 9497 symbol entries, and the RGB value of 29 named colors, pulled one by one out of the word bodies. The color of each column in the file list was measured by cutting both screenshots into the same character cell grid and comparing cell by cell — the indicator column is solid dark blue and doesn't change even when the cursor passes over it, dates are always green, and the long-filename column is underlined. Guessing from terminal conventions would have gotten none of these right.

All 12 archive formats are supported. Six of them — LZH, `.Z`, CAB, ARJ, ARC, and ACE — are decoders written from scratch, each checked file-by-file against a reference implementation's sha256: p7zip for 675 members, ncompress, cabextract, arj 3.10, arc 5.21, and acefile for 269 members. No decoder gets written for a format with no oracle to check against: an unverified decoder will silently produce files that look like they have content but are actually wrong.

Formats the original could never read are wired in too: gopher sites, web pages, EPUB ebooks, Word, PowerPoint, Excel, and PDF, all through the same screen and the same set of keys. The approach is to flatten everything down to "blocks of text, individual links, a handful of images" — that's exactly what this character cell grid can draw, and it's exactly what the 1999 interface was already doing. PDF gets a second view: `V` renders the whole page for real — paths, fills, gradients, transparency, and glyph outlines from embedded fonts are all decoded from scratch, measured cell-by-cell against poppler's rendering.

It now runs on Linux, Windows, macOS, and Android. That 2004 line — "I really wish he'd make a Linux version" — got an answer, twenty-two years later.

Calling this a renaissance of Taiwanese software would be overstating it — it's just one piece of software. But it was something a lot of people used every day, and it was headed toward disappearing in an age where nobody can run 32-bit Windows anymore. Now it won't.

> Remake by Wang Chun-yu — a small contribution to preserving Taiwanese Chinese-language software

---

This project reverse-engineers **WinCV 0.52 (CView for Windows)**, a piece of Taiwanese shareware from 1999-2011, and rewrites it in Go + Ebiten as a version that runs on Linux / Windows / macOS / Android, with the interface pixel-aligned to the original's bitmap. The original author is Lcc Wizard (Lin Chien-tsung); the original site was `cview.com.tw`.

WinCV is a Chinese-language utility that does a lot within a single screen: browsing directories, viewing text files, viewing images and thumbnails, walking into archives as if they were directories, editing text, viewing hex, converting between Big5 / GB / SJIS / KOR / Unicode, looking up an English-Chinese dictionary and KK phonetic transcriptions, and computing MD5 and SFV checksums. Its last update stopped on November 24, 2011 (the final entry in `whatsnew.txt`), and there was never a Linux version or a macOS version after that.

Download: [v0.52-remake.2](https://github.com/wicanr2/wincv-remake/releases/tag/v0.52-remake.2)
(Linux / Windows / macOS / Android — extract and run, fonts already included)

The remake's own source code, tools, and documentation are licensed under [BSD 2-Clause](LICENSE). **Copyright of the original WinCV belongs to its original author, Lcc Wizard (Lin Chien-tsung)**, and the license does not cover it — the rewrite exists to preserve this cultural artifact, not to acquire rights to the original. The repo keeps the original installer and data extracted from it (the symbol table, UI strings, reference screenshots) so the reverse-engineering conclusions can be rerun and verified. The released executables **embed** the original's bundled half-width bitmap font and ETEN's full-width font library, so that extracting the archive gives a pixel-aligned screen matching the original; the rights to those fonts remain with their original holders and are not covered by BSD 2-Clause. Dictionary data is not bundled. Item-by-item copyright and provenance are in [NOTICE](NOTICE); rights holders who want any of this handled differently are welcome to open an issue.

![File browser](docs/ui/shot-main.png)

*The remake browsing the original's own install directory. Columns, alignment, and colors align to the same character cell grid as the original.*

---

## What CView was

CView was a DOS-era file browser written in F-PC Forth. After the move to Windows, the author rewrote it using subroutine-threaded code in Win32Forth v4, and it became WinCV. Both versions share the same feel: a full-screen character cell grid, a cursor running through the file list, one keypress to open a text file, another to walk into an archive — no need to extract it to some temp directory first.

What people who used it back then mostly remember is speed. In 2004, Tsung wrote on his blog that CView was "my favorite piece of software from the DOS era," that its image viewing was "super fast," and closed the post with: "I really wish he'd make a Linux version~~"
([Tsung's Blog, 2004-08](https://blog.longwin.com.tw/2004/08/cview/)).

Someone else spent years looking for a replacement. Dabinn searched from 2006 to 2009, trying WinCV 0.5, FreeCommander, Universal Viewer 5.1.0, and Directory Opus 9.5 in turn — most of them failed on Chinese line-wrapping or encoding. What he wanted was "software that can view text files fast, like CVIEW from the DOS days"; along the way he complained that "it's a shame ACDSee can't view text." His conclusion was that only Directory Opus was barely usable
([Dabinn's notes](https://blog.dabinn.net/cview替代軟體/)).

The author's side of the record is still there too. In January 2012, someone left a comment on his blog saying "your wincv is really great... I use it every day"; the author replied mentioning that his time was limited and updates were slow
([lcc-wizard.blogspot.com](http://lcc-wizard.blogspot.com/2012/01/blog-post.html)).
Softking's [WinCV 0.52 download page](https://www.softking.com.tw/7555/) is still online today, listed under Traditional Chinese file management tools, with a file size of 5.56 MB.

![Text viewer](docs/ui/shot-viewer.png)

*The remake displaying the original's own `whatsnew.txt`: Big5 encoding detection, syntax highlighting, Big5 full-width characters. The horizontal bar on the first line is the cursor (highlight bar); `↑↓` moves it.*

## Why rewrite it

The original is a 32-bit Windows executable, and both extraction and image viewing depend on a set of Windows DLLs: `unrar.dll`, `unlha32.dll`, `unarj32j.dll`, `unacev2.dll`, `CAB32.DLL`, `7-zip32.dll`, plus `FreeImage.dll` and Intel's `ijl15.dll`. Those dependencies are locked to one platform, and nothing guarantees they'll still load on the next version of Windows.

What needs preserving is the behavior — sorting rules, how columns align, how encoding is detected, what happens on each keypress — and that only survives if it's reimplemented. So the target of reverse engineering isn't `wincv.exe` (it's only 52 KB, just the Forth kernel, with no application logic) but the 1.52 MB `WINCV.IMG`, where the actual program lives.

That 2004 line — "I really wish he'd make a Linux version" — never got a reply from the original author. This repo is the do-it-yourself version.

## Differences from the original

| Item | Original WinCV 0.52 | This remake | Notes |
|---|---|---|---|
| Platform | 32-bit Windows | Linux / Windows / macOS / Android | The same `internal/` package builds for all four platforms |
| Extraction | Plugin Windows DLLs | Pure Go: standard library, third-party packages, plus six hand-written decoders | All 12 formats supported |
| Image decoding | `FreeImage.dll` + Intel `ijl15.dll` | Pure Go decoding | 11 of 12 formats supported; Photo CD is missing |
| Half-width font | Bundled bitmap font `cvga.fon`, 8x15 | Parses the original `.FON` directly for glyphs | Glyph ground truth comes from the font file itself |
| Full-width Chinese glyphs | Left to Windows GDI to draw with a system font (the image names PMingLiU by name) | ETEN (倚天), the Taiwanese DOS Chinese system, bitmap library `STDFONT.15`, 16x15 | The original's Chinese rendering varied by whoever's Windows it ran on; the remake needs a fixed source |
| Cell size | 8 x 16 | 8 x 16 | The 8x15 glyph is top-aligned, leaving one row of space below. The original's row height comes from the 16px full-width glyphs it also required |
| Menu and toolbar | Win32 menu bar + icon toolbar | Self-drawn menu bar (`F9`), split into File / View / Tools / Settings / Help | The whole screen is a self-drawn character cell grid with no native controls. A native menu sits outside the client area and doesn't take up a cell; a self-drawn one necessarily costs one row |
| Help | Help file | `F1` embedded help | Embedded rather than read from a file — help has to be available even with no install directory, when run out of an archive, or on Android where there's no readable program directory |
| Preview pane | Present | `Alt-P`, 8 rows at the bottom | Shows the first few lines for text, hex layout for binary, format and dimensions for images |
| Status bar | Two rows: file fields + full filename | Same two rows, checked cell by cell | The second row is **pixel-identical** to the original |
| Left drive column | A pane next to the Win32 toolbar, listing drive letters | A cell-grid pane on the left of the list, toggled with `Alt-D` | Linux / macOS have no drive letters, so mount points are listed instead; removable ones get a separate color |
| Scrollbar | Win32 control | Rightmost column, arrows + thumb | The original's is drawn in system colors, so pixel equivalence isn't achievable there |
| Half-width code page | CP437 | Also CP437 | The `░▒▓█│─` characters used in ANSI art all sit in 0xB0-0xDF; treating them as Latin-1 would draw an entirely different set of characters |
| Extension-based coloring | Present (`.bat` rows are entirely magenta) | Not yet implemented | The original lets you change this in settings |
| KK phonetics | Custom encoding, one byte per phoneme | Converted to IPA for display (`ˈlɪtl̩`, `ˈbʌtn̩`) | The lookup table is worked out in `internal/dict/kk.go` |
| Font size | Pick one of three bitmap fonts in settings | `Ctrl-+` / `Ctrl--` switches live between the same three | The 24-point full-width glyphs on the ETEN disc are compressed, so at that size Chinese is scaled up from 16x15 |
| Window | Fixed layout | Enlarging the window adds more cells | Cell size doesn't change; the extra space becomes more columns and rows |
| Characters outside Big5 | Left to Windows system fonts | A fallback chain built by scanning the system font directories | Covers Simplified Chinese, Japanese, Korean, Cyrillic, Greek, Arabic, Thai, math and box-drawing symbols, and emoji. A genuinely missing glyph is drawn as an empty box rather than left blank |
| Mouse | A Win32 program, clickable by default | Click the list to move the cursor, double-click to open, click the menu bar to expand it, wheel to scroll | See "Controls" below |
| Close and reopen | Starts fresh | Returns to the last directory, cursor position, and open document — **each file also individually remembers where you left off** | Written out the moment you change directory or open/close a file — writing only on close would mean a kill or crash loses everything |
| Distribution | Shareware, released by the author | BSD 2-Clause (for the parts written from scratch) | The build embeds the original's half-width `.FON` and ETEN's full-width font library, so extracting it gives a screen aligned with the original; rights to those fonts remain with their original holders, see `NOTICE`. Dictionary data is not bundled |

The ETEN row determines how text lays out: `STDFONT.15`'s full-width cell is exactly 16x15 — exactly twice the width and the same height as `cvga`'s half-width 8x15 — so Chinese and Latin characters land on the same character cell grid without scaling or padding.

The screen colors follow the original: the names and RGB values of all 29 named colors were decoded straight out of `WINCV.IMG`, not repicked from scratch. Which color each column of the file list uses was measured by cutting both screenshots into the same character cell grid and comparing cell by cell — the indicator column is solid dark blue and the cursor doesn't cover it, dates are always green, the time column follows each file's own color, and the long-filename column is underlined; guessing any of these from convention would get them wrong.
The measurements and the commands to rerun them are recorded in `docs/ui/main-screen.md`.

![F1 menu](docs/ui/shot-menu.png)

*The `F1` menu doubles as the manual: each row shows its corresponding key on the right, and pressing that key while the menu is open selects it directly.*

## Archive formats

All six hand-written decoders are checked file-by-file against a reference implementation's sha256, not validated against their own output:

| Format | Implementation | Verified against |
|---|---|---|
| ZIP / TAR / GZ / BZ2 | Go standard library | — |
| RAR / 7z | `nwaples/rardecode`, `bodgit/sevenzip` | those packages |
| LHA / LZH | hand-written | p7zip, 675 members |
| `.Z` | hand-written | ncompress |
| CAB (MSZIP) | hand-written | gcab / cabextract |
| ARJ (methods 0-4) | hand-written | arj 3.10 |
| ARC / PAK | hand-written | arc 5.21 |
| ACE | hand-written | acefile, 269 members |

No decoder gets written for a format with no oracle to check against. An unverified decoder will silently produce files that look like they have content but are actually wrong — worse than plainly saying the format isn't supported.

ACE was never built into the original — the original loaded WinACE's own `unace.dll` (1999, v1 API) or `unacev2.dll` (2002, v2 API), and `WINCV.IMG` only contains the binding layer. This one was written from Marcel Lemke's 1998 "Technical information of the archiver ACE v1.2", checked against the BSD-licensed [acefile](https://github.com/droe/acefile), using [acefile-testdata](https://github.com/droe/acefile-testdata) as test data.
It supports stored, ACE 1.0's LZ77, and ACE 2.0's blocked mode (with its three sub-modes LZ77 / DELTA / EXE); the SOUND and PIC sub-modes, encryption, and multi-volume archives aren't done yet.

## Where fonts live

Fonts come in two layers, from different sources, and are looked up in different places.

**The original's bitmap fonts and the ETEN font library** — the set that gives pixel alignment — are third-party copyrighted material. Desktop releases **embed them in the executable**, so they work right out of the archive; the Android APK doesn't embed them, and neither does a self-built binary. To supply your own, or to override the embedded set with your own copy, put `cvga.fon`, `CVGA1018.FON`, `cvga1224.FON`, `STDFONT.15`, `SPCFONT.15` into any of the locations below (earlier ones win). The syntax highlighting config `keyword_*.cfg` and the dictionary data look in the same set of locations:

| Location | Notes |
|---|---|
| `$WINCV_HOME` | Environment variable. Double-clicking to launch or a desktop shortcut give you nowhere to pass a flag, and that's exactly when the working directory is least predictable |
| The directory the executable is in | What most people will do. It also checks `wincv/`, `original/app/`, and `original/eten/` underneath |
| Per-user config directory | Linux `~/.config/wincv`, macOS `~/Library/Application Support/wincv`, Windows `%AppData%\wincv`; the same convention as `session.json` |
| `~/.wincv` | Fallback when the previous location isn't available |
| Working directory | For development, run from the repo root — the assets are under `original/` |

On Android it's `wincv/` on external storage — the same name as the desktop's "wincv/ next to the executable." Passing a path directly with `-half` / `-eten-std` / `-eten-spc` always wins over all of the above. When none of them turn up anything, the program prints every directory it checked, one per line — a "file not found" message that doesn't say where it looked leaves the user guessing, and this is exactly the part that differs on every platform.

**Characters outside Big5** are filled in from system fonts (Simplified Chinese, Japanese, Korean, Cyrillic, Greek, Arabic, Thai, and various symbols). The full build additionally embeds a few Noto subsets, so it doesn't have to depend on the system.
System font locations are looked up per platform:

| Platform | Directories scanned |
|---|---|
| Linux | `/usr/share/fonts`, `/usr/local/share/fonts`, `$XDG_DATA_HOME/fonts` (`~/.local/share/fonts` if unset), `~/.fonts`, `fonts/` under `$XDG_DATA_DIRS`, `/run/host/fonts` (Flatpak), `/run/current-system/sw/share/X11/fonts` (NixOS) |
| Windows | `%SystemRoot%\Fonts` (not hardcoded to `C:\` — Windows isn't always installed there), `%LOCALAPPDATA%\Microsoft\Windows\Fonts` (starting with Windows 10 1803, "install for me only" fonts land here and don't show up in the system directory) |
| macOS | `/System/Library/Fonts`, `/System/Library/Fonts/Supplemental` (bundled fonts moved here starting with Catalina), `/Library/Fonts`, `~/Library/Fonts` |
| Android | `/system/fonts`, `/product/fonts`, `/system_ext/fonts` (the system got split into several partitions starting with Android 10) |

Fonts that turn up are then filtered by filename: a desktop's font directory can hold several hundred files, loading all of them would take seconds and tens of MB, and the vast majority of them (decorative fonts, single-script cursive faces) wouldn't fill in a single missing glyph. The recognized names cover each of the four platforms' own built-in fonts, not just Noto — recognizing only Noto would mean the scan finds nothing at all on Windows or macOS, which is exactly the reason the scan exists.

## Building and verification

```bash
tools/build-all.sh      # Desktop three-platform builds into dist/ (Android is tools/build-android.sh)
tools/verify-dist.sh    # Static verification: file format, macOS ad-hoc signing, dynamic library dependencies
tools/go.sh test ./...  # Tests (all run inside docker)
```

The original as oracle:

```bash
tools/setup-wine-oracle.sh                    # Extract installer + build the Wine prefix
tools/oracle-measure.sh docs/ui/oracle.png    # Measure window geometry, screenshot, print the selected font
```

Checking the remake's screen without opening a window:

```bash
tools/go.sh run ./cmd/celldump -app <directory> -keys "F1" -o shot.png -cols 93 -rows 30
```

The syntax for `-keys` matches the key column in `docs/ui/keymap.md` (`Ctrl-O`, `Alt-Z`, `F6`, `Down`...).

## Porting results

![Markdown view mode](docs/ui/showcase-markdown.png)

The image above was **drawn by the remake itself** — the document is `docs/demo/showcase.md`, and the glyph atlas (PNG) and the 29-color swatch (SVG) inside it aren't pasted in; the markdown view mode parses `![...](...)`, rasterizes it, and embeds it into the character cell grid.
To rerun:

```bash
tools/go.sh run ./cmd/celldump -app docs/demo -cols 104 -rows 56 \
    -keys "Down,Down,Down,Enter" -o docs/ui/showcase-markdown.png
```

This is the same `render.Rasterizer` the Ebiten window uses, so this PNG and what you'd see in the window are the same pixels.

### File browser (compared cell-by-cell against the original)

| Remake | Original (Wine) |
|---|---|
| ![Remake main screen](docs/ui/shot-main.png) | ![Original main screen](original/ref-shots/main-cjk.png) |

The 16 rows of the file list have **zero attribute differences** from the original, and the second row of the status bar matches down to the glyphs.
The measurement method and numbers are recorded in [`docs/ui/main-screen.md`](docs/ui/main-screen.md).

## What the remake adds

Things the original doesn't have, that this version does:

| Feature | Notes |
|---|---|
| Markdown view | Opening a `.md` file lays it out directly: headings, lists, tables, blockquotes, code blocks, **with images embedded inline** (PNG / GIF / SVG all work), `Enter` enlarges an image |
| SVG | Paths are rasterized with oksvg; text is drawn by hand (oksvg has no `<text>` support). The original is 2011-era software, from before SVG became common |
| UTF-8 fallback font | Characters ETEN doesn't have are filled in with system TrueType fonts, found by scanning font directories rather than checking only hardcoded paths |
| Font size and scale | `Ctrl-+`/`Ctrl--` switches bitmap font size, `Alt-+`/`Alt--` changes the scale factor (0.1 per step). The scale isn't just stretching the glyphs: the ratios between the original's three bundled bitmap fonts (8x15 / 10x18 / 12x24) are exactly 1.19 and 1.56, so at the 1.2 / 1.5 / 1.6 steps it switches to the **native glyphs** instead — every pixel drawn as it was back then |
| Window size | `F9` → Settings → Window Size, with presets like the original's 93x21 layout, and custom column/row counts too |
| Network browsing | `F2` to enter an address. `gopher://` and `http(s)://` go through **the same screen, the same set of keys**: `↑↓` moves between links, `Enter` opens one, `Backspace` goes back. Web pages are stripped down to text, links, and images; stylesheets and scripts are discarded entirely |
| Ebooks | Pressing `Enter` on a `.epub` shows the table of contents first; click a chapter to read it, with next/previous chapter links at the end. EPUB is just ZIP + XHTML, and both of those pieces already existed |
| PDF | Two views: `Enter` opens the **text** of the first page (reassembling characters back into lines and detecting columns), `V` **renders the whole page** (zoomable, see below). Details in the next section |
| Text view highlight bar | Reading a `.txt` file highlights the entire line the cursor is on; `↑↓` moves the cursor, scrolling only once it goes off-screen. The original's toolbar was already reporting which line the cursor was on — it just never drew that row. On by default, toggled off with `L` (the background color of an ANSI color signature file is meaningful on its own) |
| Word | Reads all three of `.docx`, `.doc` (Word 97-2003), and `.rtf`: headings, bold/italic, ordered/unordered lists, tables, embedded images |
| PowerPoint | Each `.pptx` slide is one block, speaker notes included; it opens on a slide list |
| Excel | Each `.xlsx` sheet is one block, cells laid out as a table |
| Localization | The interface is available in 繁體中文, 简体中文, English, and 日本語. It follows the system locale at startup; `F9` → Settings → Language switches at any time and the choice is remembered. The `F1` help text has a version per language |
| Separate menu font | The menu layer has its own character cell grid and can use a **different font and size** from the content (`F9` → Settings → Menu Font Size, or `-menu-font` at launch). Content needs to be pixel-aligned to the original; the menu is just interface |
| Per-file resume position | Leaving and returning to a text view, hex view, markdown view, or the editor goes back to the last position (the editor even remembers the cursor's row and column). One entry per file, up to 500 entries. The original had this setting back in 0.5x; the remake keeps it on by default |
| Resizable filename column | The original's list is always laid out for 8.3 filenames, dumping long filenames into the rightmost column. Here the main filename column can be widened (`Ctrl-→` / `Ctrl-←`, or hold the left button and drag horizontally over the list), showing long filenames directly in the list. The width is remembered |
| Zooming in image view and PDF full-page view | `+` / `-` steps through zoom levels, `1` returns to original size. The PDF full page is **redrawn at higher resolution**, not a stretched bitmap — zooming in reveals fine table lines and small footnote text |

![Text view highlight bar](docs/ui/shot-viewer-bar.png)

*Reading a text file highlights the entire line the cursor is on, with the status bar reporting `15/626` on the right.
The original's toolbar was already reporting which line the cursor was on ("1 char, 1 line/626"); it just never drew that row — halfway through scrolling a long file, "where was I" is tracked by that one line.
The background color of an ANSI color signature file is meaningful on its own, and covering it would distort it, so `L` toggles it off.*

![Gopher menu](docs/ui/gopher-menu.png)

*A gopher menu. The type tag sits at the front of each line; info lines have neither a tag nor a link.*

Gopher was picked over HTTP because of the shape of the protocol: one item per line, the type written in the first byte, and the menu structure is nearly a directory listing already — which is exactly what this screen already does. HTTP's difficulty is all in HTML, and this screen can't give HTML the layout, fonts, or scripting it wants.
Text content is **not rewrapped** (gopher content is mostly ASCII already formatted to 70 columns), and encoding detection is left to the existing detector (gopher has no charset field, and Chinese-language gopher sites from that era were mostly Big5).

Images in markdown only resolve relative paths **under the document's own directory**, and remote images are never downloaded — reading a document shouldn't turn into a network operation, and a `.md` file of unknown origin shouldn't be able to read files from elsewhere.

## Controls

Keybindings follow the original (the key-by-key tested reference is in [`docs/ui/keymap.md`](docs/ui/keymap.md)); the remake additionally adds mouse support and a few actions the original didn't have.

| Main screen | |
|---|---|
| `↑` `↓` `PgUp` `PgDn` `Home` `End` | Move cursor (**holding down repeats automatically**) |
| `Enter` | Enter a directory / open a file; also how you walk into an archive |
| `Space` | Mark; `C` copy, `M` move, `R` rename |
| `Ctrl-→` / `Ctrl-←` | Widen / narrow the main filename column by one cell |
| `F1` `F2` `F9` | Help / network browsing / menu |
| `Ctrl-+` `Ctrl--` / `Alt-+` `Alt--` | Font size / zoom level |
| `Alt-D` `Alt-P` | Drive pane / preview pane |

| Mouse | |
|---|---|
| Click list | Cursor moves there |
| Double-click list | Same as `Enter`: enter directory, open file |
| Click menu bar | Expands that category; clicking again collapses it; clicking the content area only closes the menu, without also acting on that click |
| Drag horizontally over list | Widen / narrow the main filename column |
| Wheel | One notch = 3x `↑`/`↓`; the meaning in each mode follows that mode's own keys |
| Links on web pages | Single click follows it (browser-style, no double-click required) |

Auto-repeat when holding down an arrow key had to be built by hand: Ebiten only reports "just pressed," and the OS's keyboard repeat doesn't pass through it — without this, holding `↓` would only move one cell, and a directory can hold hundreds of files.

## Office documents and PDF

The original never had these formats, so there's no original to check against — verification instead uses external tools: for Office formats, LibreOffice running in a container converts the same file to plain text; for PDF full-page rendering, the comparison is against poppler.
The demo documents used in the screenshots below are in [`docs/demo/office/`](docs/demo/office/), and their content was written for this project; the commands to rebuild and re-capture them are in that directory's README.

All five Office formats are collected behind the same narrow interface (`internal/officedoc`). That interface layer only understands "some blocks of content plus some images," so adding a sixth format wouldn't require changing a single line of it.

### Word

![Word document](docs/ui/shot-docx.png)

*What a `.docx` looks like opened: headings, bold, ordered lists, and tables all laid out on the same character cell grid.
`.doc` (Word 97-2003's FIB + piece table) and `.rtf` go through the same screen.*

### PowerPoint

![PowerPoint presentation](docs/ui/shot-pptx.png)

*Each `.pptx` slide is one block, with its speaker notes filed right after it. The "slide list / next slide" navigation underneath is the same mechanism as EPUB's chapter navigation.*

### Excel

![Excel spreadsheet](docs/ui/shot-xlsx.png)

*`.xlsx` cells laid out as a table. The shared string table, number formats, and date serials are all resolved at the parsing layer — the display layer only ever sees text.*

### PDF: two views

What PDF describes is "put this character at this coordinate" — there's no paragraph, no column, no reading order; those are the outcome of layout, not part of the data. So the text-extraction path has to reassemble characters into lines itself and then detect columns (the criterion is "a vertical band of whitespace running from the top of the page to the bottom that no character crosses," with tables then excluded by checking whether most rows in each column fill the column's width).

| Text extraction (`Enter`) | Full-page rendering (`V`) |
|---|---|
| ![PDF text extraction](docs/ui/shot-pdf-text.png) | ![PDF full page](docs/ui/shot-pdf-page.png) |

The left side is fast for finding text; the right side is where you can see tables, charts, formulas, signatures — things whose meaning lives in their position.

The full-page path is a hand-written renderer that adds no new dependency — the rasterizer is `rasterx`, already in use on the SVG path, and glyph outlines use `x/image/font/sfnt` plus hand-written CFF and Type1 interpreters:

| | |
|---|---|
| Paths | Fill and stroke, line caps, joins, dashes; nonzero winding and even-odd fill rules |
| Color | Gray / RGB / CMYK / ICCBased / Indexed / Separation |
| Images | Decoded from scratch where the object layer can't provide it: DCTDecode goes straight to JPEG, everything else goes through raw sample values, with the image's own `/SMask` applied |
| Gradients | Axial and radial; exponential, stitching, and sampled function types; gradients used as fill patterns |
| Transparency | Constant alpha, both luminosity and alpha soft masks, transparency groups |
| Fonts | Outlines decoded for all three embedded formats — TrueType/OpenType, CFF, Type1 — including `seac` accent composition |

Verification is against poppler, an independent PDF renderer: hand-written minimal samples (gradients, transparency, accent composition, images) all score above 0.9997 ink-density correlation, and measuring across real papers, catalogs, and slide decks, all six pages tested came out at 0.95 or above.

Which features to implement was decided by measurement, not by checking off items from the spec: `tools/pdfprobe scan` scanned 105 real-world PDFs, and tiling patterns, mesh gradients, and PostScript calculator functions **never showed up once** — so those three are plainly marked as not implemented. A gradient it can't interpret is left blank rather than filled with a guessed color.
The measurement method and numbers are in [`docs/plan/office-docs.md`](docs/plan/office-docs.md).

## Attribution

The original work is Lcc Wizard's (Lin Chien-tsung) WinCV 0.52, last updated 2011-11-24.
This repo is a remake, and the attribution here is for the remake itself:

> Remake by Wang Chun-yu — a small contribution to preserving Taiwanese Chinese-language software

The same text is shown in-program under `F1` → About.

### Android

![Android device screen](docs/ui/android-emulator.png)

This is an actual screenshot from an Android 14 emulator (Pixel 5 profile, 1080x2340). Visible on screen: the directory list, the `Free: 5,114MB / 5,939MB` capacity readout, two rows of touch controls at the bottom, and a status message reading "currently using system font `DroidSansMono.ttf`" — the APK doesn't bundle the original's `cvga.fon` (third-party copyrighted material); it switches to the pixel-aligned bitmap version only once the user drops it into `wincv/` themselves.

`tools/build-android.sh` builds the APK (four ABIs, minSdk 21), and `tools/run-android-emulator.sh` installs it into the emulator, runs it, takes screenshots, and collects logcat.
**It hasn't been run on real hardware yet, and touch input hasn't been tested either** — a screenshot proves it can draw and read files, not that tapping it actually does anything.

The bottom has three rows: the top row's actions change with the mode (mark/copy/move/rename while browsing; search/encoding/language while reading a document; previous/next/zoom-out/zoom-in/original-size while viewing an image or a PDF full page), and the two rows below it are a fixed key HUD —

```
Esc  | PgUp | ▲ | PgDn | Enter
Home | ◀    | ▼ | ▶    | End
```

The arrow keys form a cross, with `Esc` and `Enter` in different colors at either end. The HUD only shows **real keys**, so there's never a button that does nothing in the current mode; mode-specific actions live in the row above.

With a physical keyboard attached (dock, Bluetooth), the keys match the desktop version: there's only one keyboard translation (`internal/ebikeys`), and Ebiten on Android delivers `KeyEvent` as the same set of `ebiten.Key` values. This path has only been verified to compile — **there's been no real-hardware test with a keyboard attached**.

What actually took time in the Android port wasn't the packages under `internal/` (they compiled without a single line changed) — it was three behaviors buried in Ebiten's Android layer: gomobile's native layer requires manually feeding it a `Context`, an Activity getting recreated once is effectively the app ending, and `Layout` can't just pass the received size straight back. The cause and symptoms of all three are written up in
[`docs/plan/android.md`](docs/plan/android.md).

## Planned

- [Android version assessment and plan](docs/plan/android.md) — the first release **is read-only browsing only**. A private sideload uses the "all files access" permission, with `vfs.OS` working directly; SAF is the alternative path needed only for listing on Play.

## Documentation

- `CLAUDE.md` — Verified facts about the target software, reverse-engineering methods, architecture, hard rules
- `CONTEXT.md` — Ubiquitous language, overturned claims, decision log
- `docs/ui/main-screen.md` — Character cell grid measurements of the original main screen
- `docs/ui/keymap.md` — Key table, evidence graded in three tiers
- `docs/plan/office-docs.md` — Office and PDF parsing, rendering, and verification numbers
- `docs/formats/` — Data file format specifications
- `docs/re/` — Symbol table, word list
