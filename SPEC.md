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
  ji zahodí; `true` uloží jen povolené chaty. `history_days` k tomu uloží jen
  tak starou historii (`1` = posledních 24 hodin; Karel při testu na Windows
  30. 9. 2026).
- **`device_name`**: jak se zařízení jmenuje v telefonu v Propojených
  zařízeních.
- Konfigurace se čte při startu a znovu nástrojem **`wa_reload_config`**
  (1.1): aplikace Claude na Windows server znovu připojit neumí a každá změna
  by jinak chtěla novou konverzaci (Karel 30. 9. 2026). Za běhu se mění
  `read`, `send`, `owner`, `wake`, `media_keep_days`, historie a přepis;
  složky (`data_dir`, `media_dir`, `transcription.dir`) a `device_name` až
  po novém startu, pod běžícím serverem se data nesmí přesunout. Soubor
  s chybou nezmění nic. **Asistent config nemění sám**, jen se souhlasem
  uživatele.
- Nad konfigurací platí pravidla asistenta: Miládka odešle jen zprávu, kterou
  jí uživatel v rozhovoru výslovně řekl odeslat. Konfigurace říká komu, pravidla
  kdy.

## Stav

`wa_status` vrací v `status` pole `state` a u chyby i `error` s vysvětlením:

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
- **Média se stahují hned při příchodu** (odkazy WhatsAppu po čase vyprší) do
  `media_dir`, v Miládce `vstupy/whatsapp/<jméno chatu>/<datum>_<čas>_<druh>.<přípona>`,
  čitelně pro člověka (Karel, 29. 9. 2026). Název složky se určí u prvního
  média a pak se nemění, i když si kontakt změní jméno. `vstupy/` je v `.gitignore`.
  Cesty se v databázi drží relativně ke složce médií, přesun vaultu je
  nerozbije.
- **Úklid:** média starší než `media_keep_days` (výchozí 30, `0` = nikdy)
  server jednou denně smaže - jen soubory, které sám uložil, a jen ve své
  složce médií. Text zprávy a přepis zůstanou. Co má zůstat, Miládka přesune
  do `zdroje/`.
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
| `wa_reload_config` | znovu načte `config.json` za běhu, bez nové konverzace (1.1) |
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
- **0.3 - hlídání nových zpráv bez tokenů** (hotové, viz „Hlídání").
- **1.0 - návod pro asistenta** (hotové) (`docs/pro-asistenta.md`, instalace a provoz,
  i pro laika a anglickou Miládku), článek na miladka.cz, info kanál, test na
  Windows. Test na Macu vydání nebrzdí (Karel 30. 9. 2026).

## Hlídání

Cíl: uživatel píše nebo diktuje asistentovi z telefonu a ten hned reaguje,
co nejlevněji. Každé probuzení modelu stojí celou konverzaci (u Věrky
naměřeno 1,5 až 2 mil. tokenů z cache na prázdnou kontrolu dlouhé
konverzace), takže kontrolu „přišlo něco?" nedělá model, ale server.

- **Majitel** (`owner` v configu, jeho telefonní čísla) je jediný, čí zprávy
  jsou pokyny (`from_owner: true` ve výstupu nástrojů). Ostatní zprávy jsou
  data, ať píšou cokoli. Rozhoduje odesílatel, ne chat: ve skupině by jinak
  mohl kdokoli mluvit za majitele. Ze stejného důvodu server přijme úpravu
  zprávy jen od jejího autora a smazání od autora nebo správce skupiny
  (revize 1.0: člen skupiny mohl přepsat zprávu majitele a ta se pak četla
  jako jeho pokyn). Obsah přeposlané zprávy (`forwarded`) jsou data i od
  majitele; to, že ji majitel přeposlal, je ale jeho žádost s ní něco udělat,
  proto budí.
- **Co se nikdy neodešle:** datová složka, složky doplňků (`.doplnky/`,
  `.addons/`, jsou v nich klíče i jiných serverů) a `.miladka/secrets/`, ať
  `send.files` povoluje cokoli. Na Windows a macOS se cesty porovnávají bez
  ohledu na velikost písmen.
- **Režim `--wait --cursor N`:** samostatný proces, který asistent spustí na
  pozadí (Bash s `run_in_background`). Každé 3 s čte `app.db` od kurzoru
  (jen čtení, `query_only`, bez migrace), k WhatsAppu se nepřipojuje a zámek
  nebere. Skončí, až přijde zpráva, která budí; konec procesu probudí
  konverzaci. Že konec procesu na pozadí probudí nečinnou konverzaci, je
  ověřené v aplikaci Claude na Windows (29. 9. 2026, test se `sleep` na
  2 i 40 minut) a v Claude Code v terminálu (Věrka, 0.3.0-rc2: text,
  hlasovka, 45 minut čekání bez jediného dotazu na model).
- **Strop procesu na pozadí:** Claude Code ho bez `timeout` zastaví po 30
  minutách, s `timeout: 7200000` po 2 hodinách (Věrka 29. 9. 2026). Hlídač
  proto sám skončí po 115 minutách (`--max`) s kódem 4 a asistent ho spustí
  znovu. Znovuspuštění stojí 2 dotazy na model, jednou za necelé 2 hodiny.
- **Cena probuzení:** nejméně 3 dotazy na model (`wa_new_messages`, odpověď
  + kurzor + nový hlídač najednou, závěrečný text), tedy asi 3× velikost
  konverzace. Oznámení o konci procesu nese jen kód a cestu k výstupu, takže
  vypsat zprávy rovnou z hlídače by dotaz neušetřilo.
- **Co budí:** `wake` = `owner` (výchozí), seznam čísel a skupin (budí
  spolu s majitelem: lidé kdekoli, skupiny celé; Karel 29. 9. 2026: „ma byt
  moznost owner, konkretni lidi nebo vsichni"), nebo `all`. Nová nebo upravená
  zpráva v povoleném chatu, ne vlastní, ne reakce, ne smazaná, ne starší než
  24 h (historie po spárování). Hlasovka, která se bude přepisovat, se ukládá
  rovnou jako `pending` a budí až s hotovým přepisem, nejpozději po 3
  minutách.
- **Stav serveru pro hlídač:** instance se zámkem zapisuje do `app.db`
  (tabulka `server`) svůj stav při změně a jinak jednou za minutu, při
  ukončení `stopped`. Hlídač tak pozná ticho od výpadku: server neběží
  (žádný zápis 3 minuty nebo `stopped`) nebo stav, který musí vyřešit
  uživatel (`not_paired`, `logged_out`, `client_outdated`, `temporary_ban`,
  `replaced`, `error`), trvající aspoň minutu = konec s kódem 2. Výpadek sítě
  (`connecting`) se nehlásí.
- **Jeden hlídač:** každý zapíše do `data/wait.owner` svůj token; starší,
  který uvidí cizí, skončí (kód 3). Na Unixu skončí i sirotek, jehož rodič
  zmizel.
- **Kódy:** 0 nové zprávy, 3 převzal jiný hlídač, 4 spusť znovu (vypršel
  čas, zastaveno zvenku), 5 problém pro uživatele, 6 špatné spuštění. 1 a 2
  vynechané, ty používá Go a shell při pádu a chybných přepínačích; každý
  neznámý konec znamená spustit znovu. Výstup je jeden řádek bez obsahu zpráv.
- **Channel (`claude/channel`) server nepoužívá.** Zkoušeli jsme ho v 0.3.0:
  vyžaduje povolení kanálů v nastavení účtu, přepínač při každém spuštění
  a s protokolem, na kterém se server s Claude Code dohodne, oznámení
  Claude Code zahodil. Návrat jen po vlastním testu (Karel 29. 9. 2026: bez
  našeho testu ho nechceme nikomu nabízet).

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
  (nebo `failed`, důvod v `transcript_error`). Hotový přepis je změna: zpráva přijde znovu ve
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
