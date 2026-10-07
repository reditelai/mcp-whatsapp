# Změny

Formát vychází z [Keep a Changelog](https://keepachangelog.com/cs/1.1.0/),
čísla verzí ze [Semantic Versioning](https://semver.org/lang/cs/).

## [Nevydáno]

## [1.3.0] - 2026-10-08

Server je doplněk Miládky a běží jen v její složce.

- Vydaná binárka se spustí jen ze složky doplňků Miládky (`.doplnky/mcp-whatsapp/`, anglicky `.addons/`) ve složce, která má `.miladka/VERSION`. Jinde nenaběhne a česky i anglicky napíše, že je to doplněk Miládky a ať si ji uživatel pořídí na miladka.cz (v režimu `--wait` jako `chyba:` s kódem 6). Kontrolu zapíná release build (`-X main.release=yes`), sestavení ze zdrojového kódu ji nemá (Karel 8. 10. 2026).
- README jen pro Miládku: pryč „funguje s jakýmkoli MCP klientem" a instalace příkazem `claude mcp add` mimo Miládku. Návod A1 to říká výslovně, aktualizace (B5, krok 0) nejdřív zkontroluje místo.

### Při aktualizaci

- **Ještě před výměnou binárky** ověř místo (návod B5, krok 0): registrace musí ukazovat do `.doplnky/mcp-whatsapp/` a vault musí mít `.miladka/VERSION`, jinak nová verze nenaběhne. Když binárka leží jinde, přesuň ji podle kroku 0 i s `data/`.
- Nastavení se nemění.

## [1.2.3] - 2026-10-05

Aktualizace knihovny whatsmeow, aby spojení s WhatsAppem dál fungovalo.

- whatsmeow v0.0.0-20261005123207-68df7a5ec7b5.

### Při aktualizaci

- Nastavení se nemění. Stačí vyměnit binárku (návod, Aktualizace).

## [1.2.2] - 2026-10-05

Návod upřesňuje, že doplněné řádky v popisu prostředí automatického režimu platí hned.

- Návod B7, „Hlídač a automatický režim oprávnění": změna popisu prostředí platí hned, v téže konverzaci (ověřeno 30. 9. 2026). Dřív návod říkal, že to dokumentace Claude Code neuvádí.

### Při aktualizaci

- Nastavení se nemění. Stačí vyměnit binárku (návod, Aktualizace).

## [1.2.1] - 2026-09-30

Automatický režim oprávnění už nemá zastavovat hlídač ani odmítat souhlas majitele z telefonu: návod doplní dva řádky do popisu prostředí.

- Návod B7: se souhlasem uživatele doplnit do `autoMode.environment` v uživatelském nastavení Claude Code řádek, že spuštění hlídače s `--wait` a posun kurzoru jsou běžný provoz, a řádek, že výslovný souhlas majitele (`from_owner: true`) s odesláním konkrétního mailu nebo zprávy platí jako potvrzení uživatele. Bez nich klasifikátor hlídač občas zablokuje a souhlas z telefonu nebere (Věrka 30. 9. 2026, ověřeno u mailu). Zápis jde jen přes „Accept edits". Příkaz nastavení nevypíše, nepřidá řádek dvakrát a bez vlastního seznamu začne `"$defaults"`.

### Při aktualizaci

- Když uživatel používá hlídač, se souhlasem doplň řádky do popisu prostředí automatického režimu podle B7, odstavec „Hlídač a automatický režim oprávnění" (řádek se souhlasem jen s vyplněným `owner`). Potřebuje „Accept edits".

## [1.2.0] - 2026-09-30

Nastavení se zálohuje s Miládkou: patří do `system/whatsapp.json`, ne mezi hesla.

- Návod: nastavení ve `system/whatsapp.json` místo `.miladka/secrets/whatsapp/config.json`. Hesla v něm nejsou, takže se zálohuje s vaultem a kdo přijde o počítač, nemusí ho skládat znovu (Karel 30. 9. 2026).
- Návod A1: doplněk si sám ověří v `.gitignore` vaultu `.doplnky/` a `vstupy/*` s výjimkou `!vstupy/.gitkeep`, ať nevyloučí soubor z instalátoru Miládky. Nepředpokládá, že to zařídil balíček nebo jiný doplněk.
- Nový oddíl návodu B5b: přesun nastavení při aktualizaci.
- Návod B5a: obnova ze zálohy na novém počítači. Nastavení přijde zálohou, spárování a kotva se udělají znovu.

### Při aktualizaci

- Ověř `.gitignore` vaultu podle A1. Starší návod radil celé `vstupy/`, které vylučuje `vstupy/.gitkeep` z instalátoru Miládky: nahraď ho `vstupy/*` a `!vstupy/.gitkeep`.
- Staré místo nastavení funguje dál. Když binárka leží ve `.doplnky/` a je zapsaná v `.mcp.json`, nabídni přesun podle návodu B5b: **výjimečně hned po výměně souboru (B5, krok 3), ještě před novou konverzací**, ať stačí jedna. Mění `.mcp.json` (cesta za `--config`), takže potřebuje „Accept edits".
- Po nové konverzaci spusť hlídače s cestou za `--config` z `.mcp.json` (B7).
- `config.json` ze starších sekcí je po přesunu `system/whatsapp.json`.

## [1.1.5] - 2026-09-30

Aktualizace ve správném pořadí: nová verze platí až v nové konverzaci, změny nastavení se dělají až potom.

- Návod B5: předem říct, že nová verze potřebuje novou konverzaci (v terminálu `/mcp` a Reconnect), a změny nastavení z „Při aktualizaci" dělat až v ní. Starý server nové klíče odmítne, což vypadá jako chyba (Věrka při aktualizaci multigmailu na 1.3.0).
- Neznámý klíč v `config.json` hláška vysvětlí: nejspíš patří novější verzi a běží ještě stará, jinak je to překlep.

### Při aktualizaci

- Nastavení se nemění. Po nové konverzaci spusť hlídače znovu (B7), ať běží z nové verze.

## [1.1.4] - 2026-09-30

Návod B7: druhý hook při startu konverzace nepřidávat, když ho uživatel už má vlastní.

- Kdo měl pokyn ke spuštění hlídače už ve vlastním hooku (Věrka od 0.3.0), dostal ho po aktualizaci na 1.1.2 dvakrát.

### Při aktualizaci

- Když máš pokyn ke spuštění hlídače WhatsAppu ve vlastním hooku i v hooku z návodu B7, jeden z nich odeber.

## [1.1.3] - 2026-09-30

Převzatý hlídač se už nevzbudí na tutéž zprávu podruhé.

- Hlídač při konci nemaže soubor `wait.owner`. Dřív ho novější hlídač, který skončil hned, smazal a starší to vzal jako „pořád můj", takže se na tutéž zprávu probudili oba. Chybu našla revize hlídače pošty v multigmailu, u WhatsAppu byla stejná.
- Návod B7: zastavený hlídač (kód 5 nebo na přání uživatele) se zapíše do kotvy, ať ho hook při startu konverzace nespouští, dokud se problém nevyřeší.

### Při aktualizaci

- Nastavení se nemění. Po výměně binárky spusť znovu hlídače (B7), ať běží z nové verze.

## [1.1.2] - 2026-09-30

Hlídač naběhne v každé nové konverzaci: návod přidává hook při startu konverzace, který asistentovi připomene hlídače spustit.

- Návod B7: hook `SessionStart` (nová, obnovená i vyčištěná konverzace) v `.claude/settings.json`. Hlídač na pozadí skončí s konverzací a dřív se znovu spustil až s denním přehledem, takže druhá konverzace téhož dne zůstala bez hlídání.

### Při aktualizaci

- Se souhlasem uživatele přidej hook podle návodu B7, „Hlídač v každé konverzaci", do `.claude/settings.json` (slučovat, nepřepisovat; může chtít „Accept edits").
- Po výměně binárky spusť znovu hlídače (B7), ať běží z nové verze.

## [1.1.1] - 2026-09-30

Změnu nastavení jde ověřit: `wa_status` a `wa_reload_config` ukazují, co teď platí.

- `wa_status` nově ukazuje i mazání médií, složku médií, historii, nastavení přepisu a jméno zařízení (`media_keep_days`, `media_dir`, `history_sync`, `history_days`, `transcription_config`, `device_name`).
- `wa_reload_config` vrací k seznamu změněných klíčů i nastavení, které teď platí (`settings`), aby asistent ověřil, že se načetlo, co zapsal (Věrka 30. 9.).
- Návod B5 (aktualizace): přesné příkazy pro stažení nové verze vedle běžící a ověření součtu pod jiným jménem (Věrka 30. 9.).

### Při aktualizaci

- Nastavení se nemění. Po výměně binárky spusť znovu hlídače (B7), ať běží z nové verze.

## [1.1.0] - 2026-09-30

Změna nastavení bez nové konverzace: nový nástroj `wa_reload_config` načte `config.json` za běhu.

- `wa_reload_config`: po změně nastavení (se souhlasem uživatele) ho server načte hned. Uplatní čtení, odesílání, majitele, buzení, mazání médií, historii a přepis; složky a jméno zařízení dál až po nové konverzaci. Soubor s chybou nezmění nic. Aplikace Claude na Windows server znovu připojit neumí, takže každá změna dřív chtěla novou konverzaci.
- Návod B6 (odpojení): nejdřív zastavit hlídače a server (nová konverzace, na Windows jinak soubory smazat nejde), smazat i stažená média a nastavení, vrátit krok v denním přehledu, kotvu a zápis ve stavu Miládky (test na Windows 30. 9.).

### Při aktualizaci

- Nastavení se nemění. Po výměně binárky spusť znovu hlídače (B7), ať běží z nové verze.

## [1.0.0] - 2026-09-30

Stabilní verze: Miládka provede WhatsAppem i netechnického uživatele a od teď se nástroje a klíče nastavení mění jen s novou hlavní verzí.

- Návod pro asistenta vede laika jako u Gmailu: jedna otázka na krok, výchozí nastavení navržené najednou, přehled, co dělá uživatel a co asistent.
- Dva způsoby použití: asistent má vlastní číslo (píšeš mu z telefonu), nebo je na tvém čísle (čte tvoje chaty a posílá jen to, co schválíš).
- Anglická Miládka: návod počítá s jejími složkami (`.addons/`, `inbox/`), README je i anglicky (`README.en.md`).
- Nová volba `history_days`: s `history_sync` uloží po spárování jen posledních N dní historie (`1` = 24 hodin).
- Opravy z revize před 1.0:
  - Úpravu zprávy server přijme jen od jejího autora, smazání od autora nebo správce skupiny. Člen skupiny mohl přepsat zprávu majitele a ta se pak četla jako jeho pokyn.
  - Úklid médií se nezasekne na souboru, který nejde smazat (otevřený v prohlížeči, drží ho antivir nebo OneDrive). Dřív se točil dokola a vytěžoval procesor.
  - Datová složka, složky doplňků a `.miladka/secrets/` nejdou odeslat, ať `send.files` povoluje cokoli. Na Windows a macOS bez ohledu na velikost písmen.
  - Hlídač na Windows nekončí omylem kódem 3, po uspání notebooku nehlásí falešně, že server neběží, a chybu v nastavení hlásí kódem 6.
  - Databáze funguje i ve složce s `#` nebo `?` ve jménu.
  - Číslo bez předvolby země server odmítne místo tichého nefungování. `owner` bere i jedno číslo místo seznamu. Nastavení uložené s BOM (Poznámkový blok, PowerShell) se načte.
  - `--check` varuje, když data leží ve složce OneDrive, iCloudu, Dropboxu nebo Disku Google, a u asistenta na tvém čísle nehlásí, že hlídač nic neohlásí.
- Změny výstupu: `transcript_status` je u chyby jen `failed`, důvod je v novém `transcript_error`; `lock_holder_pid` ve `wa_status` je číslo.

### Při aktualizaci

- Nastavení se nemění. Jestli v configu máš číslo bez předvolby země (třeba `777123456`), server se nespustí: se souhlasem uživatele ho oprav na `+420777123456` a ověř `--check`.
- Po výměně binárky spusť znovu hlídače (B7), ať běží z nové verze.

## [0.3.0] - 2026-09-30

Z telefonu rovnou k asistentovi: server sám hlídá nové zprávy a asistenta probudí, až mu napíšeš. Pokyny bere jen od tebe.

- Režim hlídání `--wait --cursor N`: asistent ho spustí na pozadí, server čte uložené zprávy a skončí, až přijde zpráva od majitele (hlasovka až s přepisem). Čekání nestojí žádné tokeny. Funguje v aplikaci Claude i v terminálu. Výpadek serveru nebo odhlášení hlásí, ticho tak neznamená výpadek. Před stropem procesů na pozadí (2 hodiny) skončí sám a řekne si o nové spuštění.
- Nová volba `owner` (čísla majitele): jen jeho zprávy jsou pro asistenta pokyny (`from_owner: true`), zprávy ostatních a přeposlané zprávy jsou jen informace. Volba `wake`: `owner` (výchozí), seznam dalších lidí a skupin, které budí spolu s majitelem, nebo `all`.
- Zprávy nesou `forwarded`.

### Při aktualizaci

- Se souhlasem uživatele doplň do configu `"owner": ["+420…"]` s jeho číslem (musí být v `read.chats`). Bez `owner` žádná zpráva není pokyn a hlídač se nespustí.
- Spusť hlídače podle návodu B7 a přidej jeho spuštění do denního přehledu, hned po kroku „nové zprávy na WhatsAppu".

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
