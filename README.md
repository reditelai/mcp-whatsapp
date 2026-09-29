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
- **Čitelný stav:** zastaralý klient, odhlášení z telefonu, výpadek sítě
  nebo spojení v jiné konverzaci jsou vidět jako různé stavy.
- **Samo se udržuje:** knihovna whatsmeow se jednou týdně aktualizuje a
  vyjde nová verze, když testy projdou.

## Instalace

Binárky pro Windows, macOS a Linux (x64 i ARM) jsou v
[releasech](https://github.com/reditelai/mcp-whatsapp/releases) spolu se
`SHA256SUMS`. Nic dalšího se neinstaluje.

1. Stáhni binárku pro svůj systém, ověř součet a ulož ji do
   `~/mcp-whatsapp/` (na Windows `%USERPROFILE%\mcp-whatsapp\`).
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
| `data_dir` | kde leží data serveru, výchozí `~/.mcp-whatsapp` |

**Prázdný seznam i chybějící klíč znamená nikam**, ne kamkoli.

V datové složce je `session.db` s klíči spárovaného zařízení. **Kdo má ten
soubor, má přístup k tvému WhatsAppu.** Nepatří do gitu ani do zálohy.

## Licence

Apache-2.0, viz [`LICENSE`](LICENSE). Server vychází z částí
[wadb](https://github.com/sausheong/wadb) (MIT, upozornění v
[`NOTICE`](NOTICE)). Knihovna [whatsmeow](https://github.com/tulir/whatsmeow)
je závislost pod MPL-2.0, její zdrojový kód je na GitHubu autora.
