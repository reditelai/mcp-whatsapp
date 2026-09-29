# Změny

Formát vychází z [Keep a Changelog](https://keepachangelog.com/cs/1.1.0/),
čísla verzí ze [Semantic Versioning](https://semver.org/lang/cs/).

## [Nevydáno]

Z telefonu rovnou do konverzace: zprávy a hlasovky majitele jdou asistentovi hned, pokyny bere jen od něj.

- Nová volba `owner` (čísla majitele): jen jeho zprávy jsou pro asistenta pokyny, zprávy ostatních a přeposlané zprávy jsou jen informace.
- Příchozí zprávy jdou rovnou do konverzace přes Claude Code channel (zatím Claude Code v terminálu s `--dangerously-load-development-channels server:whatsapp`). Volba `channel.notify`: `owner` (výchozí), `all`, `off`. Hlasovka až s přepisem.
- V aplikaci Claude hlídá nové zprávy opakovaná kontrola (návod B7).
- Zprávy nesou `forwarded` a `pushed`.

### Při aktualizaci

- Se souhlasem uživatele doplň do configu `"owner": ["+420…"]` s jeho číslem (musí být v `read.chats`). Bez `owner` server do konverzace nic neposílá a žádná zpráva není pokyn.
- Nastav hlídání nových zpráv podle návodu B7 (terminál: přepínač při spuštění, aplikace: hlídač).

## [0.2.0] - 2026-09-29

Hlasovky jako text: přepis přímo v počítači, a aktualizace, při které nová verze převezme spojení sama.

- Přepis hlasovek lokálně (Parakeet v3 přes sherpa-onnx), nic neodchází ven. Instaluje se na souhlas uživatele nástrojem `wa_transcription_setup` (asi 510 MB, ověřené součty). Hlasovka přijde znovu s textem v `transcript`; starší na požádání `wa_transcribe`.
- Nová volba `transcription` v `config.json` (`enabled`, `threads`, `batch`, `dir`), výchozí hodnoty stačí.
- Fotky, hlasovky a dokumenty do `media_dir` (s Miládkou `vstupy/whatsapp/`, po chatech a s datem v názvu), po `media_keep_days` (výchozí 30) se samy smažou; text a přepis zůstanou.
- Všechno ve složce Miládky: server a jeho data patří do `<vault>/.doplnky/mcp-whatsapp/`, data jsou výchozí vedle binárky (`data/`), přepis ve sdíleném `.doplnky/prepis/`. Registrace v `.mcp.json` s relativními cestami, přesun vaultu ji nerozbije.
- Běžící server předá spojení instanci jiné verze a skončí; dvě konverzace se stejnou verzí se dál nepřetahují.
- Návod: aktualizace výměnou souboru přejmenováním (na Windows běžící binárku nejde přepsat), návrat při selhání, založení `moduly-instalovane.json`, když chybí.

### Při aktualizaci

- Nastavení se nemění, `transcription` se doplňovat nemusí. Postup je v `docs/pro-asistenta.md`, B5.
- Po aktualizaci nabídni uživateli přepis hlasovek (návod A8).
- **Přestěhování do složky Miládky** (z 0.1.x, kde byla binárka jinde a data v `~/.mcp-whatsapp`), se souhlasem uživatele:
  1. Ověř, že `.doplnky/` je v `.gitignore` vaultu (návod A1). Bez toho nepokračuj.
  2. Novou binárku stáhni a ověř rovnou do `VAULT/.doplnky/mcp-whatsapp/` (A2).
  3. Zavři konverzace se serverem (nebo ukonči starý proces), přesuň `~/.mcp-whatsapp` do `VAULT/.doplnky/mcp-whatsapp/data` (klíče, zprávy, média; spárování zůstane) a přepiš registraci v `.mcp.json` na relativní cesty (A5). Starou registraci (`claude mcp remove whatsapp -s user`) a starou binárku odeber.
  4. Do configu doplň `"media_dir": "vstupy/whatsapp"` a řekni uživateli o mazání médií po 30 dnech (návod A3, bod 5). Ověř, že i `vstupy/` je v `.gitignore` (A1).
  5. Nová konverzace, `wa_status`: `connected` bez párování. Stará média zůstanou v `data/media`, nová půjdou do `vstupy/whatsapp`.
- **Z 0.1.0:** stará verze předání nezná. Když nová po minutě hlásí `locked_by_other_instance`, ukonči starý proces podle `lock_holder_pid` (B5, krok 4).

## [0.1.0] - 2026-09-29

První verze: asistent čte a posílá zprávy na WhatsAppu a nastaví ho celý z chatu.

- Spárování QR kódem jako obrázkem, nebo kódem k opsání do telefonu.
- Čtení a odesílání jen v chatech, které povoluje `config.json`, zvlášť osobní chaty a skupiny.
- Nové zprávy od kurzoru včetně úprav a smazání, hledání, fotky, soubory a hlasovky ke stažení.
- Čitelný stav spojení a jedna instance se spojením i při víc konverzacích.
- Týdenní automatická aktualizace knihovny whatsmeow.

### Při aktualizaci

- První vydaná verze, není z čeho aktualizovat. U Věrky nahrazuje server `wadb`: postup je v `docs/pro-asistenta.md`, část A.
