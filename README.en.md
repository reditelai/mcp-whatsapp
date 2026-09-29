# mcp-whatsapp

An MCP server through which an assistant reads and sends WhatsApp messages.

*[Česká verze: `README.md`](README.md) - the project is Czech-first, so the
Czech README is the one kept in step with the code.*

**The server is built for [Miladka](https://miladka.cz)**, an AI assistant
that runs in Claude Code on top of your vault. It works with any MCP client,
though. The guide telling the assistant how to set the server up with you and
how to work with it is in [`docs/pro-asistenta.md`](docs/pro-asistenta.md)
(in Czech; the assistant reads it and talks to you in your language). Design
and decisions are in [`SPEC.md`](SPEC.md).

## Before you start: the risk to your number

The server connects to WhatsApp as another linked device, just like WhatsApp
Web, but through the unofficial library
[whatsmeow](https://github.com/tulir/whatsmeow). **WhatsApp's terms forbid
unofficial clients and WhatsApp can ban the number for it.** We therefore
recommend a separate number for the assistant, not your main one.

## Two ways to use it

The server links to the WhatsApp account that scans the QR code.

- **The assistant has its own number** (recommended): a second SIM, an eSIM
  or an old phone. You write and dictate to it from your phone like to anyone
  else, and it replies. The risk of a ban lies with its number.
- **The assistant on your number**, much like with email: it reads your
  chats, drafts replies and sends on your behalf only what you approve. Every
  message goes out under your name and the risk lies with your main number.
  You cannot give it instructions over WhatsApp: to the server, your messages
  are sent by you.

## What it does

- **Pairing from the chat:** a QR code as an image, or a code to type into
  the phone. No terminal needed.
- **Reading and sending within fixed limits.** In `config.json` you set which
  chats it may read and where it may write, separately for direct chats and
  groups. The assistant does not change the settings on its own.
- **New messages from a cursor**, search in stored messages, photos, files
  and voice notes to download.
- **You write and dictate to the assistant from your phone.** It takes
  instructions only from your number; everyone else's messages are just
  information. The server watches for new messages itself (`--wait`) and wakes
  the assistant when you write. Waiting costs no tokens.
- **Voice notes transcribed right on your computer.** Nothing leaves it. The
  model is downloaded once (about 510 MB), when you agree to it.
- **Readable state:** an outdated client, a logout from the phone, a network
  outage or the connection held by another conversation show up as different
  states.
- **Keeps itself up to date:** the whatsmeow library is updated once a week
  and a new version comes out when the tests pass.

## Installation

Binaries for Windows, macOS and Linux (x64 and ARM) are in the
[releases](https://github.com/reditelai/mcp-whatsapp/releases) together with
`SHA256SUMS`. Nothing else gets installed.

1. Download the binary for your system, check the checksum and save it in the
   folder where the server should live, for example `~/mcp-whatsapp/`. **It
   keeps its data next to itself** in `data/`, so it moves as one folder. With
   Miladka it is `<Miladka's folder>/.addons/mcp-whatsapp/`.
2. Create `config.json` following [`config.example.json`](config.example.json).
3. Connect the server to Claude Code:

   ```sh
   claude mcp add whatsapp --scope user -- ~/mcp-whatsapp/mcp-whatsapp-linux-amd64 --config /path/to/config.json
   ```

4. In a new conversation, ask the assistant to pair WhatsApp.

With Miladka, the assistant does all of this following `docs/pro-asistenta.md`.

## Settings

```json
{
  "read": { "chats": "all", "groups": false },
  "send": { "chats": ["+420777123456"], "groups": false, "files": [] },
  "history_sync": false,
  "device_name": "Miladka"
}
```

| Key | Meaning |
|---|---|
| `read.chats` | direct chats it may read: `"all"`, `false`, or a list of numbers (`+420…`) |
| `read.groups` | groups: `"all"`, `false`, or a list of group JIDs (`…@g.us`, `wa_list_chats` shows them) |
| `send.chats`, `send.groups` | where it may write, same shape; only within what it may read |
| `send.files` | full paths of folders it may send files from; empty = no files |
| `history_sync` | after pairing, store the history of allowed chats (default no) |
| `history_days` | with `history_sync`: only the last N days of history (`1` = the last 24 hours); without it everything the phone offers |
| `device_name` | the device name in the phone under Linked devices |
| `transcription.enabled` | voice note transcription; default `true`, takes effect once installed (`wa_transcription_setup`) |
| `transcription.threads`, `transcription.batch` | threads and segments at once; default 2 and 2 (about 1.3 GB of memory). On a weak machine `batch: 1` |
| `owner` | your number or a list of numbers (`+420…`): only their messages are instructions for the assistant; must be in `read.chats` |
| `wake` | which new messages wake the assistant (`--wait` mode): `"owner"` (default), a list of further numbers and groups (they wake along with you), or `"all"` |
| `media_dir` | where photos, voice notes and documents are saved; default `data/media`, with Miladka `inbox/whatsapp` |
| `media_keep_days` | after how many days downloaded media are deleted (text and transcript stay); default 30, `0` = never |
| `data_dir` | where the server's data lives; default `data/` next to the binary |
| `transcription.dir` | the transcription engine and model; default `.addons/prepis` (shared with Miladka's other add-ons), elsewhere `data/stt` |

Numbers always with the country code (`+420777123456`); the server rejects
a number without it. **An empty list and a missing key both mean nowhere**,
not anywhere. Relative
paths are taken from the root of Miladka's folder when the server lives in
her add-on folder, elsewhere from the folder of `config.json`.

The data folder holds `session.db` with the keys of the paired device.
**Whoever has that file has access to your WhatsApp.** It belongs neither in
git nor in a backup.

## License

Apache-2.0, see [`LICENSE`](LICENSE). The server builds on parts of
[wadb](https://github.com/sausheong/wadb) (MIT, notice in
[`NOTICE`](NOTICE)). The [whatsmeow](https://github.com/tulir/whatsmeow)
library is a dependency under MPL-2.0; its source code is on its author's
GitHub. Voice note transcription downloads, on request,
[sherpa-onnx](https://github.com/k2-fsa/sherpa-onnx) (Apache-2.0) and the
NVIDIA Parakeet TDT 0.6B v3 model (CC BY 4.0); details in `NOTICE`.
