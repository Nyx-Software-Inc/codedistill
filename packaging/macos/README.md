# Running CodeDistill on macOS

The macOS build is **unsigned**. Gatekeeper (the OS feature that
gates "unidentified developer" software) blocks unsigned binaries
by default with a dialog that doesn't offer an "Open anyway"
button on the first try.

**This is expected. Two ways to get past it.**

## Option 1: Approve in System Settings (one-time)

1. Unzip `CodeDistill-<version>-darwin-arm64.zip`.
2. Try to launch `codedistill` once (double-click, or run it
   from a terminal). macOS blocks it and shows a warning.
3. Open **System Settings → Privacy & Security**, scroll to
   the **Security** section, and click **Open Anyway** next to
   the message about `codedistill` being blocked.
4. Confirm once more when prompted. macOS remembers this
   decision; subsequent launches don't prompt.

> On macOS 12–14 the older **right-click (Control-click) → Open**
> shortcut also works. macOS 15 (Sequoia) **removed** that
> bypass, so on current systems use the System Settings path
> above.

## Option 2: Strip the quarantine attribute (CLI)

```sh
xattr -d com.apple.quarantine codedistill
./codedistill init
./codedistill serve
```

This removes the "this file was downloaded from the internet"
flag that Gatekeeper checks. Faster than the right-click dance
when you're already in a terminal.

## Why unsigned?

Code signing + notarization requires an Apple Developer Program
membership ($99/year) and a per-build round-trip to Apple's
notarization service. The current builds are unsigned and the
steps above are the workaround. Signed + notarized builds are
in progress; once they land, the download will open with no
Gatekeeper prompt and this page's workarounds won't be needed.

## Picking the right zip

- `darwin-arm64` — Apple Silicon Macs (M1, M2, M3, M4)
- `darwin-amd64` — Intel Macs (mostly pre-2020)

If you're not sure, check Apple menu → About This Mac →
"Chip". An Intel processor name (e.g. "2.6 GHz 6-Core
Intel Core i7") needs `darwin-amd64`; anything starting with
"Apple" needs `darwin-arm64`.

## After launch

The server listens on `http://127.0.0.1:8080` by default.
Open that in your browser. Data lives next to the binary
(`codedistill.db` + `codedistill.db.blobs/`); move both files
together if you relocate.

Classification + Q&A need Ollama running on
`http://localhost:11434`. Install Ollama from
<https://ollama.com> and pull the models:

```sh
ollama pull qwen2.5:7b
ollama pull nomic-embed-text
```
