# mcp-whatsapp

MCP server, přes který asistent čte a posílá zprávy na WhatsAppu.

**Server je stavěný pro [Miládku](https://miladka.cz)**, AI asistentku, která
běží v Claude Code nad tvým vaultem. Funguje ale s jakýmkoli MCP klientem.
Návod pro asistenta, jak s tebou server nastavit a jak s ním pracovat, je
v [`docs/pro-asistenta.md`](docs/pro-asistenta.md). Stavba a rozhodnutí jsou
v [`SPEC.md`](SPEC.md).

## Než začneš: riziko pro číslo

Server se k WhatsAppu připojuje jako další propojené zařízení, stejně jako
WhatsApp Web, ale přes neoficiální knihovnu
[whatsmeow](https://github.com/tulir/whatsmeow). **Podmínky WhatsAppu
neoficiální klienty zakazují a WhatsApp za to může číslo zablokovat.**
Doporučujeme proto pro asistenta samostatné číslo, ne tvoje hlavní.

## Co umí

- **Spárování z chatu:** QR kód jako obrázek, nebo kód k opsání do telefonu.
  Terminál nepotřebuješ.
- **Čtení a odesílání s pevnými hranicemi.** V `config.json` určíš, které
  chaty smí číst a kam smí psát, zvlášť pro osobní chaty a skupiny. Asistent
  si nastavení sám nepřepíše.
- **Nové zprávy od kurzoru**, hledání v uložených zprávách, fotky, soubory
  a hlasovky ke stažení.
- **Píšeš a diktuješ asistentovi z telefonu.** Pokyny bere jen od tvého
  čísla, zprávy ostatních jsou pro něj jen informace. V terminálu mu zprávy
  chodí rovnou do konverzace, v aplikaci Claude je hlídá pravidelně.
- **Přepis hlasovek na text přímo v počítači.** Nic neodchází ven. Model se
  stáhne jednou (asi 510 MB), až k tomu dáš souhlas.
- **Čitelný stav:** zastaralý klient, odhlášení z telefonu, výpadek sítě
  nebo spojení v jiné konverzaci jsou vidět jako různé stavy.
- **Samo se udržuje:** knihovna whatsmeow se jednou týdně aktualizuje a
  vyjde nová verze, když testy projdou.

## Instalace

Binárky pro Windows, macOS a Linux (x64 i ARM) jsou v
[releasech](https://github.com/reditelai/mcp-whatsapp/releases) spolu se
`SHA256SUMS`. Nic dalšího se neinstaluje.

1. Stáhni binárku pro svůj systém, ověř součet a ulož ji do složky, kde má
   server bydlet, třeba `~/mcp-whatsapp/`. **Data si dá vedle sebe** do
   `data/`, takže se stěhuje jednou složkou. S Miládkou je to
   `<složka Miládky>/.doplnky/mcp-whatsapp/`.
2. Vytvoř `config.json` podle [`config.example.json`](config.example.json).
3. Připoj server do Claude Code:

   ```sh
   claude mcp add whatsapp --scope user -- ~/mcp-whatsapp/mcp-whatsapp-linux-amd64 --config /cesta/ke/config.json
   ```

4. V nové konverzaci řekni asistentovi, ať WhatsApp spáruje.

S Miládkou to všechno udělá asistent podle `docs/pro-asistenta.md`.

## Nastavení

```json
{
  "read": { "chats": "all", "groups": false },
  "send": { "chats": ["+420777123456"], "groups": false, "files": [] },
  "history_sync": false,
  "device_name": "Miládka"
}
```

| Klíč | Význam |
|---|---|
| `read.chats` | osobní chaty, které smí číst: `"all"`, `false`, nebo seznam čísel (`+420…`) |
| `read.groups` | skupiny: `"all"`, `false`, nebo seznam JID skupin (`…@g.us`, zjistí `wa_list_chats`) |
| `send.chats`, `send.groups` | kam smí psát, stejný tvar; jen v rámci toho, co smí číst |
| `send.files` | celé cesty ke složkám, ze kterých smí posílat soubory; prázdné = soubory ne |
| `history_sync` | po spárování uložit historii povolených chatů (výchozí ne) |
| `device_name` | jméno zařízení v telefonu v Propojených zařízeních |
| `transcription.enabled` | přepis hlasovek; výchozí `true`, platí až po instalaci (`wa_transcription_setup`) |
| `transcription.threads`, `transcription.batch` | vlákna a počet úseků najednou; výchozí 2 a 2 (asi 1,3 GB paměti). Na slabém stroji `batch: 1` |
| `owner` | tvoje čísla (`+420…`): jen jejich zprávy jsou pro asistenta pokyny; musí být v `read.chats` |
| `channel.notify` | které zprávy jdou rovnou do konverzace (Claude Code v terminálu s kanály): `"owner"` (výchozí), `"all"`, `"off"` |
| `media_dir` | kam se ukládají fotky, hlasovky a dokumenty; výchozí `data/media`, s Miládkou `vstupy/whatsapp` |
| `media_keep_days` | po kolika dnech se stažená média smažou (text a přepis zůstanou); výchozí 30, `0` = nikdy |
| `data_dir` | kde leží data serveru; výchozí `data/` vedle binárky |
| `transcription.dir` | engine a model přepisu; výchozí `.doplnky/prepis` (sdílený s dalšími doplňky Miládky), jinde `data/stt` |

**Prázdný seznam i chybějící klíč znamená nikam**, ne kamkoli. Relativní
cesty se v Miládce berou od kořene její složky, jinde od složky s `config.json`.

V datové složce je `session.db` s klíči spárovaného zařízení. **Kdo má ten
soubor, má přístup k tvému WhatsAppu.** Nepatří do gitu ani do zálohy.

## Licence

Apache-2.0, viz [`LICENSE`](LICENSE). Server vychází z částí
[wadb](https://github.com/sausheong/wadb) (MIT, upozornění v
[`NOTICE`](NOTICE)). Knihovna [whatsmeow](https://github.com/tulir/whatsmeow)
je závislost pod MPL-2.0, její zdrojový kód je na GitHubu autora. Přepis
hlasovek stahuje na požádání [sherpa-onnx](https://github.com/k2-fsa/sherpa-onnx)
(Apache-2.0) a model NVIDIA Parakeet TDT 0.6B v3 (CC BY 4.0); podrobnosti
v `NOTICE`.
