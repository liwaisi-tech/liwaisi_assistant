# brae — home layout

Conventions for every file you read or write inside `$HOME` (`/home/brae`).
Match content to the right folder; do **not** dump everything into `$HOME`
root. The directories below are created at image build time and always
exist — never `mkdir -p ~/<Name>` to re-create one listed here.

| Path               | Purpose                                                | Examples                                |
|--------------------|--------------------------------------------------------|-----------------------------------------|
| `~/workspace`      | All coding work. One subfolder per project.            | `~/workspace/api-server/`, `~/workspace/scratch/main.go` |
| `~/bin`            | Compiled brae-owned executables (tool-forge output).   | `~/bin/my-linter`                       |
| `~/Documents`      | Long-form text, markdown notes, specs, PDFs.           | `~/Documents/design.md`                 |
| `~/Notes`          | Short scratch notes the agent keeps across turns.      | `~/Notes/2026-04-22-debug.md`           |
| `~/Music`          | Audio files (wav/mp3/flac/ogg).                        | `~/Music/clip.wav`                      |
| `~/Images`         | Image files (png/jpg/svg/webp).                        | `~/Images/diagram.png`                  |
| `~/Videos`         | Video files (mp4/webm/mkv).                            | `~/Videos/recording.mp4`                |
| `~/Downloads`      | Files fetched from the network (curl, git clone, etc). | `~/Downloads/report.csv`                |
| `~/.cache`         | Tool caches (Go build cache, pip, npm). Disposable.    | `~/.cache/go-build`                     |
| `~/.brae`          | Agent-internal state. Don't expose to the user unless asked. | `~/.brae/layout.json`             |
| `~/.brae/tmp`      | Scratch space for a single turn. Clean up after use.   | `~/.brae/tmp/patch.diff`                |

## Rules

1. **Coding → `~/workspace` only.** Source files, repos, build artefacts.
2. **Binaries → `~/bin`.** Anything executable that should be on `$PATH`.
3. **Match extension to folder.** `.mp3` → `~/Music`, `.png` → `~/Images`,
   `.mp4` → `~/Videos`. Mixed-media bundles go under `~/workspace/<project>/assets/`.
4. **Scratch work → `~/.brae/tmp`.** Never `/tmp` and never `$HOME` root.
5. **Network downloads land in `~/Downloads` first**, then move to the
   correct folder once classified.
6. **Paths are always absolute** (`~`, `$HOME`, `${HOME}` are expanded by
   both bash and `file_read`/`file_write`).

Environment variables for each path: `$BRAE_WORKSPACE`, `$BRAE_BIN`,
`$BRAE_DOCUMENTS`, `$BRAE_NOTES`, `$BRAE_MUSIC`, `$BRAE_IMAGES`,
`$BRAE_VIDEOS`, `$BRAE_DOWNLOADS`, `$BRAE_TMP`.
