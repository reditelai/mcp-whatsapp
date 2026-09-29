# mcp-whatsapp - specifikace

MCP server, přes který asistent čte a posílá zprávy na WhatsAppu. Stavěný pro
[Miládku](https://miladka.cz) v Claude Code na Windows, Macu a Linuxu, funguje
s jakýmkoli MCP klientem.

Schválená architektura (Karel, 29. 9. 2026). Vznikla z provozu u Karlovy
asistentky, kde dva měsíce běžel server `wadb` a 28. 9. 2026 přestal fungovat:
WhatsApp zvedl minimální verzi klienta a `wadb` nikdo neaktualizuje.

## Proč vlastní server

- **WhatsApp klienta průběžně odstřihává.** Server postavený na neoficiální
  knihovně musí být aktualizovaný, jinak jednou za čas přestane fungovat a
  uživatel to sám nespraví. Údržba je jádro celé věci, ne doplněk.
- **Nastavení musí jít celé z chatu.** Uživatel Miládky terminál nevidí:
  spárování, stav, chyby, znovupřipojení i odhlášení dělá asistent nástroji.
- **Chyby musí být vidět.** Zastaralý klient, odhlášení z telefonu a výpadek
  sítě nesmí vypadat stejně.

## Riziko, které musí znát uživatel

Podmínky WhatsAppu neoficiální klienty zakazují a WhatsApp za to může číslo
zablokovat. Knihovna `whatsmeow` není oficiální API. **Návod pro uživatele
to říká na začátku a doporučuje samostatné číslo pro asistenta, ne hlavní.**

## Stavba

- **Go, jedna binárka bez C knihoven** (`CGO_ENABLED=0`): Windows, macOS,
  Linux, amd64 i arm64. SQLite přes `modernc.org/sqlite`.
- **WhatsApp přes `go.mau.fi/whatsmeow`** (MPL-2.0, závislost, ne součást
  repa; zdroj na github.com/tulir/whatsmeow). Knihovnu neupravujeme, jen
  aktualizujeme.
- **MCP přes `github.com/mark3labs/mcp-go`**, stdio.
- **Z `wadb`** (github.com/sausheong/wadb, MIT) jsou převzaté jednotlivé části,
  hlavně převod zpráv z whatsmeow a stavba odesílaných zpráv. Upozornění a
  licence MIT jsou v `NOTICE`.

### Dvě databáze v datové složce

- `session.db` - klíče spárovaného zařízení (sqlstore whatsmeow). **Kdo má
  tenhle soubor, má přístup k WhatsAppu.** Nikdy do gitu ani do zálohy vaultu.
- `app.db` - zprávy, chaty a kontakty, jen z chatů, které konfigurace dovoluje
  číst.

**Všechno je ve složce Miládky** (Karel, 29. 9. 2026: uživatel zná jen
složku `miladka`, přesouvá a zálohuje ji celou). Binárka a data jsou ve
skryté podsložce doplňků `<vault>/.doplnky/mcp-whatsapp/` (anglicky
`.addons`), data výchozí v `data/` vedle binárky, přepis ve sdíleném
`.doplnky/prepis/`. `.doplnky/` je v `.gitignore`: klíče ani programy do
zálohy nejdou. Obsidian složky s tečkou nezobrazuje. Konfigurace leží
v `.miladka/secrets/whatsapp/config.json` (vzor mcp-multi-gmail). Registrace
v `.mcp.json` má cesty relativní ke kořeni vaultu, Claude Code server
spouští z kořene projektu.

### Jedna instance drží spojení

Claude Code spouští server pro každou konverzaci zvlášť. Dvě spojení se stejným
zařízením se navzájem shazují. Proto:

- **Spojení drží jen instance se zámkem** (`lock` v datové složce, zámek
  operačního systému, uvolní se i při pádu procesu). V zámku je PID.
- **Další instance nespadne.** Čte z `app.db` (SQLite ve WAL to snese),
  odesílání a párování odmítne s `locked_by_other_instance` a zámek zkouší
  převzít každých 30 s. Když první konverzace skončí, druhá spojení převezme
  sama.
- **Předání mezi verzemi.** Claude Code po přeregistrování nebo reconnectu
  starý proces serveru vždy neukončí (29. 9. 2026 u Věrky) a na Windows
  běžící binárku nejde přepsat. Držitel zámku proto zapisuje do `lock.pid`
  svou verzi. Instance **jiné verze** ho požádá o předání (`handover.json`),
  držitel se odpojí, uvolní zámek a skončí, nová verze převezme spojení do
  pár sekund. Dvě instance téže verze jsou dvě konverzace a nepředávají si
  nic. Verze 0.1.0 předání nezná, tu je při první aktualizaci potřeba
  ukončit ručně.

## Konfigurace

```json
{
  "read":  { "chats": "all", "groups": false },
  "send":  { "chats": ["+420777123456"], "groups": false, "files": [] },
  "history_sync": false,
  "device_name": "Miládka"
}
```

- **`read` a `send` se nastavují zvlášť.** `chats` jsou osobní chaty, `groups`
  skupiny. Hodnota je `"all"`, `false`, nebo seznam: telefonní čísla
  v mezinárodním tvaru (`+420…`) a JID skupin (`…@g.us`, zjistí je
  `wa_list_chats`).
- **Prázdný seznam i chybějící klíč znamená nikam**, ne kamkoli.
- **`send.files`**: celé cesty ke složkám, ze kterých smí posílat soubory.
  Prázdné = soubory ne. Z datové složky serveru nikdy.
- **`send` smí jen to, co smí `read`.** Odeslat do chatu, který server nečte,
  by asistent nemohl zkontrolovat.
- **Výchozí nastavení, které asistent nabízí:** číst všechny osobní chaty,
  skupiny ne; odesílat jen do vyjmenovaných chatů.
- **Jeden člověk má na WhatsAppu dvě identity:** telefonní číslo a interní LID
  (`…@lid`). Filtr porovnává obě, převod přes úložiště whatsmeow. Bez toho by
  povoleného člověka vyřadil.
- **`history_sync`**: po spárování WhatsApp nabídne historii. Výchozí `false`
  ji zahodí; `true` uloží jen povolené chaty.
- **`device_name`**: jak se zařízení jmenuje v telefonu v Propojených
  zařízeních.
- Konfigurace se čte při startu. **Asistent ji nemění sám**, jen se souhlasem
  uživatele, a změna platí od nové konverzace (server se spustí znovu).
- Nad konfigurací platí pravidla asistenta: Miládka odešle jen zprávu, kterou
  jí uživatel v rozhovoru výslovně řekl odeslat. Konfigurace říká komu, pravidla
  kdy.

## Stav

`wa_status` vrací `state` a u chyby i `error` s vysvětlením:

| `state` | Co to znamená | Co s tím |
|---|---|---|
| `connected` | spojení běží | - |
| `connecting` | připojuje se, nebo se po výpadku připojuje znovu | počkat |
| `not_paired` | zařízení není spárované | `wa_pair` |
| `pairing` | běží párování | naskenovat kód |
| `logged_out` | zařízení bylo odebrané v telefonu | znovu `wa_pair` |
| `client_outdated` | WhatsApp odmítl verzi klienta | aktualizovat server |
| `replaced` | spojení převzal jiný program se stejným zařízením | najít a zastavit ho |
| `temporary_ban` | WhatsApp číslo dočasně zablokoval (`error` nese do kdy) | počkat, nic neposílat |
| `locked_by_other_instance` | spojení drží jiná konverzace | pracovat tam, nebo ji zavřít |
| `error` | jiná trvalá chyba | text v `error` |

- **Zastaralý klient:** po chybě 405 si server zjistí aktuální
  verzi WhatsApp Webu (`whatsmeow.GetLatestVersion`) a zkusí to znovu. Teprve
  když to nepomůže, hlásí `client_outdated`.
- **Výpadek sítě:** opakované připojování s rostoucí prodlevou do 5 minut.
- **`replaced` se neopakuje** - dvě instance by se přetahovaly donekonečna.

## Párování z chatu

- **`wa_pair`** vrátí QR kód jako obrázek a zároveň ho uloží do
  `pair-qr.png` v datové složce. Obrázek z nástroje aplikace ukazuje jen
  v rozbaleném volání (ověřeno v Remote Control 29. 9.), proto ho asistent
  uživateli pošle jako soubor, nebo ho otevře.
- WhatsApp dává šest kódů za sebou, první platí 60 s, další 20 s. **Kódy
  převezme proces na pozadí**, takže každé volání vrátí hned ten platný.
  Když dojdou, další volání spustí nové párování.
- **Asistent kód vyžádá až ve chvíli, kdy má uživatel telefon připravený**
  (WhatsApp → Propojená zařízení → Propojit zařízení).
- **Záloha:** `wa_pair` s `method: "code"` a číslem telefonu vrátí osmimístný
  kód, který uživatel opíše do telefonu.
- Po naskenování server sám přejde do `connected`.

## Zprávy

- **Uloží se jen zprávy z chatů, které smí číst**, i vlastní zprávy odeslané
  z telefonu (asistent pak vidí celý rozhovor).
- **Média se stahují hned při příchodu** do `media/` v datové složce. Odkazy
  WhatsAppu po čase vyprší.
- **`wa_new_messages`** vrátí zprávy nové nebo změněné od kurzoru a nový
  kurzor. Změna je úprava nebo smazání pro všechny: zpráva přijde znovu se
  stejným `id`. Kurzor je čítač změn (`rev`), drží ho klient (vzor kotvy
  z mcp-multi-gmail): víc konverzací tak nemá společný stav, který by se
  rozešel.
- **Úpravy chodí zašifrované** klíčem původní zprávy (`SecretEncryptedMessage`).
  Server je rozšifruje přes whatsmeow; úprava, kterou nejde přiřadit, se
  zapíše do logu, nikdy nezmizí potichu. Totéž zpráva nepodporovaného typu.
- **Časy** ve všech výstupech jsou UTC v RFC 3339.
- Když je aplikace zavřená, zprávy čekají v telefonu a po připojení dorazí.

## Nástroje (0.1)

| Nástroj | Co dělá |
|---|---|
| `wa_status` | stav spojení, spárované číslo, souhrn konfigurace |
| `wa_pair` | spárování QR kódem, nebo kódem k opsání |
| `wa_logout` | odhlásí zařízení a smaže klíče; chce `confirm: true` |
| `wa_reconnect` | spojení znovu, třeba po `replaced` |
| `wa_list_chats` | chaty, které smí číst, s posledním časem a počtem zpráv |
| `wa_get_messages` | zprávy jednoho chatu, od nejnovějších, stránkovaně |
| `wa_new_messages` | nové zprávy od kurzoru napříč povolenými chaty |
| `wa_search_messages` | fulltext v uložených zprávách |
| `wa_send_text` | text do chatu, který smí odesílat, volitelně jako odpověď |
| `wa_send_file` | soubor s popiskem, stejná pravidla |
| `wa_download_media` | cesta ke staženému souboru zprávy |
| `wa_mark_read` | označí chat jako přečtený v telefonu |
| `wa_transcription_setup` | stáhne a ověří engine a model pro přepis hlasovek (0.2) |
| `wa_transcribe` | přepíše jednu hlasovku hned (0.2) |

Chyby vrací nástroje jako `{"error": {"code": "…", "message": "…"}}`.

## Další verze

- **0.2 - přepis hlasovek** (hotové, viz „Přepis hlasovek" níž).
- **0.3 - příchozí zprávy do konverzace** přes `claude/channel`, až po ověření.
  Podle hlášených chyb Claude Code se v nečinné konverzaci ztrácejí; do té
  doby jsou hlavní cestou `wa_new_messages` a denní přehled.
- **1.0 - návod pro asistenta** (`docs/pro-asistenta.md`, instalace a provoz),
  článek na miladka.cz, info kanál, test na Windows i Macu.

## Přepis hlasovek

- **Lokálně, nic neodchází.** Model **Parakeet TDT 0.6B v3** (NVIDIA, čeština
  na FLEURS 11,0 % WER, Whisper large-v3 11,3 %) přes **sherpa-onnx** (Apache-2.0),
  hotové binárky pro všech šest cílů. Server engine spouští jako podproces,
  sám zůstává v čistém Go.
- **Instalace na souhlas uživatele:** `wa_transcription_setup` stáhne engine
  (asi 20 MB) a model (487 MB) z releasů sherpa-onnx, **ověří SHA-256**
  (připnuté v kódu) a rozbalí jen potřebné soubory do `stt/` v datové složce
  (asi 700 MB). Stav a průběh ve `wa_status`, `transcription`.
- **Opus se dekóduje v Go** (`pion/opus`), bez ffmpeg: 16 kHz mono WAV.
- **Dlouhé hlasovky se dělí** v nejtišších místech na úseky do 25 s. Dvě
  minuty vcelku dopadly znatelně hůř a vzaly 2 GB paměti; po úsecích bez
  chyby. Úseky jdou do enginu po dvou (`transcription.batch`), model se načte
  jednou za dávku.
- **Změřeno na derfl-srv1** (2 vCPU, 29. 9. 2026): 13,6 s řeči za 3,2 s plus
  3,7 s načtení modelu; paměť 1,1 GB na jeden úsek, asi 0,23 GB na každý další
  v dávce.
- **Tok:** hlasovka se stáhne, dostane `transcript_status: pending`, jediný
  pracovník ve instanci se zámkem ji přepíše a uloží `transcript` s `done`
  (nebo `failed: …`). Hotový přepis je změna: zpráva přijde znovu ve
  `wa_new_messages`. Po instalaci a po restartu se dopíší hlasovky z posledních
  7 dní, starší na požádání (`wa_transcribe`).
- **Přepis je strojový**: asistent podle něj jedná, ale jména, čísla a termíny
  potvrdí, když na nich záleží.
- Konfigurace `transcription`: `enabled` (výchozí `true`, platí až po
  instalaci), `threads` (výchozí 2), `batch` (výchozí 2).
- Whisper jsme neporovnávali: Parakeet přepsal skutečnou hlasovku bez chyby
  a podle zveřejněných měření je na češtině stejně přesný a několikrát
  rychlejší. Cloudový přepis zatím není.

## Údržba

- **GitHub Actions jednou týdně** aktualizují `whatsmeow` na poslední verzi,
  sestaví server a spustí testy. Když projdou, vydají novou verzi s krátkým
  záznamem v changelogu. Když ne, založí issue.
- Vydání jako u mcp-multi-gmail: verze v `VERSION`, sekce v `CHANGELOG.md`,
  tag `vX.Y.Z`, release s binárkami a `SHA256SUMS`. Z releasu čte info kanál
  Miládky.

## Mimo rozsah

- Oficiální WhatsApp Business API.
- Hovory, statusy, komunity, správa skupin (zakládání, přidávání lidí).
- Úprava knihovny `whatsmeow` (MPL: úpravy by se musely zveřejnit a rozešly by
  se s aktualizacemi).
- Druhé zařízení nebo víc čísel v jednom serveru.

## Známá omezení

- **Historie jednoho člověka se může rozdělit.** Když přijde zpráva z LID,
  který zatím nejde převést na číslo, uloží se pod LID; pozdější zprávy
  (když už převod známe) pod číslem. `wa_list_chats` pak ukáže dva chaty.
- **Úpravy zpráv ve stažené historii se ztratí** (`history_sync`): WhatsApp
  je v historii posílá pod ID původní zprávy, která už uložená je.
- **Párování přes passkey** server nepodporuje, jen QR a kód.

## Jak se pozná, že to funguje

Na testovacím čísle, ne hlavním:

1. Spárování QR z chatu do 60 s, `wa_status` pak `connected`.
2. Zpráva z povoleného chatu je v `wa_new_messages`, z nepovoleného ne. Totéž
   u kontaktu, který přijde jako `@lid`.
3. Odeslání do povoleného chatu projde, do nepovoleného vrátí
   `send_forbidden`.
4. Odebrání zařízení v telefonu: `logged_out` do minuty.
5. Druhá konverzace: `locked_by_other_instance`, čtení funguje, první
   konverzace zůstane připojená. Po zavření první druhá převezme spojení.
6. Vypnutá síť na 10 minut: `connecting`, po zapnutí sama `connected` a
   zprávy z výpadku dorazí.
