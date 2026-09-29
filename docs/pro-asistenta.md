# mcp-whatsapp - návod pro asistenta

Tenhle návod čteš ty, asistent. Provede tě nastavením WhatsAppu s uživatelem
(část A) a prací se zprávami (část B). Uživatel terminál nevidí: co jde
udělat bez něj, uděláš sama, a řekneš mu jen to, co musí on.

## Zásady, než začneš

- **Riziko řekni hned na začátku.** Server je neoficiální klient WhatsAppu.
  Podmínky WhatsAppu to zakazují a číslo může být zablokované. Doporuč
  samostatné číslo pro asistenta, ne hlavní. Rozhodnutí je na uživateli.
- **Konfiguraci měníš jen se souhlasem uživatele.** Co smíš číst a kam psát,
  určuje `config.json`. Chyba `send_forbidden` je jeho nastavení, ne překážka
  k obejití.
- **Odesíláš jen to, co ti uživatel v rozhovoru výslovně řekl odeslat.** Platí
  i s `"all"` v konfiguraci.
- **`session.db` v datové složce jsou klíče k jeho WhatsAppu.** Nikdy do vaultu,
  gitu ani chatu.
- **Když ti zápis zablokuje automatický režim oprávnění**, nic neobcházej:
  požádej uživatele o dočasné přepnutí na „Accept edits" a po dokončení mu
  připomeň návrat.

## Část A - nastavení

### A1. Prostředí a kam server patří

**Všechno patří do složky Miládky.** Server, jeho data i přepis hlasovek jdou
do skryté podsložky `.doplnky/` v kořeni vaultu (dál `VAULT`):

```
VAULT/.doplnky/mcp-whatsapp/     binárka, SHA256SUMS a data (klíče, zprávy, média)
VAULT/.doplnky/prepis/           přepis hlasovek, sdílený s dalšími doplňky
VAULT/.miladka/secrets/whatsapp/ config.json
```

Uživatel pak Miládku přestěhuje nebo zazálohuje jednou složkou. Obsidian
složky s tečkou nezobrazuje.

**Než cokoli stáhneš, ověř, že `.doplnky/` je v `.gitignore` vaultu.** Budou
v ní klíče k WhatsAppu a ty do zálohy nesmí nikdy:

```sh
cd VAULT && mkdir -p .doplnky && git check-ignore -v .doplnky/x
```

Když příkaz nic nevypíše (nebo vault není v gitu, ale záloha se zapnout
může), přidej do `VAULT/.gitignore` řádek `.doplnky/` a ověř znovu. Od verze
balíčku, která `.doplnky` zavádí, tam řádek je.

Zjisti systém:

```sh
uname -s
uname -m
```

| `uname -s` | Soubor z releasu |
|---|---|
| `Darwin` + `arm64` | `mcp-whatsapp-darwin-arm64` |
| `Darwin` + `x86_64` | `mcp-whatsapp-darwin-amd64` |
| `MINGW…`/`MSYS…` nebo chyba (PowerShell) | `mcp-whatsapp-windows-amd64.exe` (ARM: `-arm64.exe`) |
| `Linux` + `x86_64` | `mcp-whatsapp-linux-amd64` |

### A2. Stažení poslední verze

```sh
curl -s https://api.github.com/repos/reditelai/mcp-whatsapp/releases/latest
```

Verze je v `tag_name` (dál `VERZE`). Stáhni binárku a `SHA256SUMS` do
`VAULT/.doplnky/mcp-whatsapp/`:

```sh
mkdir -p "VAULT/.doplnky/mcp-whatsapp" && cd "VAULT/.doplnky/mcp-whatsapp"
curl -sLO https://github.com/reditelai/mcp-whatsapp/releases/download/VERZE/SOUBOR
curl -sLO https://github.com/reditelai/mcp-whatsapp/releases/download/VERZE/SHA256SUMS
grep " SOUBOR$" SHA256SUMS | sha256sum -c -
chmod +x SOUBOR
./SOUBOR --version
```

Na Macu místo `sha256sum -c -` použij `shasum -a 256 -c -`. Na Windows
`certutil -hashfile SOUBOR SHA256` a porovnej s řádkem v `SHA256SUMS`.
Když součet nesedí, soubor smaž a stáhni znovu. Nikdy ho nespouštěj.

### A3. Domluva o nastavení

Zeptej se po jednom:

1. **Které osobní chaty smíš číst?** Doporuč: všechny (`"all"`).
2. **Skupiny?** Doporuč: ne. Když chce, konkrétní skupiny doplníte po
   spárování, JID ukáže `wa_list_chats`.
3. **Komu smíš psát?** Doporuč: jen vyjmenovaným číslům, třeba jemu samotnému.
   `"all"` jen na jeho výslovné přání.
4. **Posílání souborů?** Doporuč: jen z vaultu (celá cesta ke složce vaultu
   do `send.files`), nebo vůbec.

### A4. `config.json`

Patří do `VAULT/.miladka/secrets/whatsapp/config.json` (`.miladka/secrets/`
je v `.gitignore`, ověř `git check-ignore -v`).

```json
{
  "read": { "chats": "all", "groups": false },
  "send": { "chats": ["+420777123456"], "groups": false, "files": [] },
  "history_sync": false,
  "device_name": "Miládka"
}
```

`data_dir` ani `transcription.dir` nenastavuj: server si data dá vedle
binárky (`.doplnky/mcp-whatsapp/data`) a přepis do `.doplnky/prepis`, takže
se stěhují s vaultem. Zkontroluj ho (z kořene vaultu):

```sh
cd VAULT && .doplnky/mcp-whatsapp/SOUBOR --config .miladka/secrets/whatsapp/config.json --check
```

Výpis říká i datovou složku; musí ležet v `.doplnky/mcp-whatsapp/data`.

### A5. Připojení do Claude Code

Do `.mcp.json` v kořeni vaultu, **s cestami relativními ke kořeni vaultu**
(Claude Code server spouští z kořene projektu, takže přesun vaultu
registraci nerozbije):

```json
{
  "mcpServers": {
    "whatsapp": {
      "command": ".doplnky/mcp-whatsapp/SOUBOR",
      "args": ["--config", ".miladka/secrets/whatsapp/config.json"]
    }
  }
}
```

Když `.mcp.json` už existuje (třeba s `multi-gmail`), jen přidej záznam
`whatsapp` do `mcpServers`. Pak požádej uživatele o novou konverzaci; Claude
Code se zeptá, jestli server povolit, a uživatel povolí. V ní zavolej
`wa_status`: čekáš `not_paired`.

Když server v nové konverzaci nenaběhne a jde o relativní cestu (některá
verze aplikace ji nenajde), dej do `command` celou cestu k binárce. Po
přesunu vaultu ji pak přepiš.

### A6. Spárování

Párování je závod s časem: kód platí jen 20 až 60 sekund. Každá vteřina mezi
vytvořením kódu a jeho zobrazením ubírá uživateli čas na naskenování.

1. **Nejdřív všechno vysvětli** a řekni uživateli, ať si připraví telefon:
   **WhatsApp → Nastavení → Propojená zařízení → Propojit zařízení.** Počkej,
   až řekne, že má kameru připravenou.
2. **Pak jako jeden rychlý krok, nic mezi tím** (žádné další nástroje,
   kontroly ani dlouhý text): zavolej `wa_pair` a hned ukaž soubor
   z `qr_path`. Obrázek ve výsledku nástroje uživatel nevidí, je ve sbaleném
   volání:
   - když máš nástroj na poslání souboru uživateli, pošli ho (v Remote Control je to jediná spolehlivá cesta),
   - jinak ho otevři: Windows `start "" "CESTA"`, macOS `open "CESTA"`.
3. Uživatel naskenuje. Když kód mezitím vyprší, zavolej `wa_pair` znovu, vrátí
   čerstvý. Vysvětlování a kontrolu stavu (`wa_status`) nech až na dobu po
   naskenování.
4. Když obrázek nejde ukázat (server bez obrazovky, vzdálený přístup), použij
   `wa_pair` s `method: "code"` a jeho číslem. Uživatel v telefonu zvolí
   **Propojit s telefonním číslem** a kód opíše.
5. Po naskenování `wa_status` přejde na `connected`.

### A7. Po spárování

- Zapiš do `system/moduly-instalovane.json` záznam
  `{"id": "whatsapp", "verze": "X.Y.Z", "nainstalovano": "RRRR-MM-DD"}`
  (verze bez „v"). Od Miládky 1.9 je soubor v balíčku; když chybí, založ ho
  ve tvaru `{"moduly": []}`.
- Založ `system/whatsapp-kotva.md` s kurzorem z prvního `wa_new_messages`
  (bez `cursor`). Viz část B.
- Když má denní přehled, přidej do jeho postupu krok „nové zprávy na
  WhatsAppu" (část B) a udělej generálku jako u jiných změn přehledu.

### A8. Přepis hlasovek

Nabídni ho: hlasovky pak přijdou i jako text a uživatel ti může diktovat.
Přepis běží v počítači, nic neodchází ven.

1. Řekni, co to stojí: **jednorázové stažení asi 510 MB**, na disku asi
   700 MB, při přepisu chvíli 1 až 1,5 GB paměti.
2. Po souhlasu zavolej `wa_transcription_setup`. Stahování běží na pozadí,
   průběh je ve `wa_status`, `transcription.progress`. Za pár minut
   `transcription.state` přejde na `ready`.
3. Když nechce, nic nedělej. Hlasovky dál přijdou jako soubory.

Na slabém stroji (4 GB paměti a méně) navrhni v `config.json`
`"transcription": {"batch": 1}`.

## Část B - provoz

### Stavy

| `state` | Co řekneš uživateli |
|---|---|
| `connected` | nic |
| `connecting` | že se WhatsApp připojuje; zkus to za chvíli |
| `not_paired`, `logged_out` | že je potřeba spárovat (A6); u `logged_out`, že zařízení v telefonu zmizelo |
| `client_outdated` | že je potřeba aktualizovat server (B5) |
| `replaced` | že spojení převzal jiný program se stejným zařízením; až ho vypne, `wa_reconnect` |
| `temporary_ban` | že WhatsApp číslo dočasně zablokoval; nic neposílej |
| `locked_by_other_instance` | že WhatsApp drží jiná konverzace; číst jde, psát ne |
| `error` | text z `error` vlastními slovy |

### B1. Nové zprávy

1. `wa_new_messages` s kurzorem ze `system/whatsapp-kotva.md`.
2. Projdi zprávy, co se týká úkolů a lidí, zapiš do vaultu podle svých
   pravidel. Hlasovky (`kind: "voice"`) mají soubor v `media_path` a po
   instalaci přepisu i text.
   - **Upravená zpráva** přijde znovu se stejným `id` a `edited: true`,
     **smazaná pro všechny** s `deleted: true` a bez textu. Oprav podle toho,
     co sis z ní zapsala.
   - **`from_me: true`** je zpráva, kterou poslal uživatel (nebo ty). Je to
     kontext rozhovoru, ne nové zadání.
   - **Hlasovka** přijde nejdřív s `transcript_status: "pending"` a za chvíli
     znovu se stejným `id` a textem v `transcript`. Ber ji podle přepisu, ale
     **jména, čísla a termíny** si u uživatele potvrď, když na nich záleží -
     přepis je strojový. `failed: …` nebo hlasovka starší než 7 dní:
     `wa_transcribe`. Bez nainstalovaného přepisu (`transcription.state`
     není `ready`) řekni, že přišla hlasovka od koho a jak dlouhá, a nabídni
     přepis (A8).
3. Nový `cursor` zapiš do kotvy. Když je `has_more`, opakuj.

Kurzor se posouvá, i když zprávy jen projdeš. Co z nich vzešlo, zapiš dřív
než kurzor.

### B2. Odesílání

- Návrh odpovědi ukaž uživateli a pošli ho, až řekne. `wa_send_text`, na
  konkrétní zprávu s `reply_to`.
- Soubor jen ze složek v `send.files` (`wa_send_file`).
- `wa_mark_read` jen když uživatel chce: odesílatel uvidí, že si to přečetl.

### B3. Hledání

`wa_search_messages` hledá v uložených zprávách (začátky slov, bez ohledu na
diakritiku). Starší zprávy, než je spárování, tam nejsou, pokud není
`history_sync`.

### B4. Změna nastavení

Změnu `config.json` udělej jen se souhlasem uživatele, zkontroluj `--check`
a řekni mu, že platí od nové konverzace (server se načte znovu).

### B5. Aktualizace

Registrace v Claude Code se nemění, jen se vymění soubor, na který ukazuje.
Běžící server předá spojení nové verzi sám. Postup (`DIR` =
`VAULT/.doplnky/mcp-whatsapp`):

1. Přečti `CHANGELOG.md` nové verze a všech mezi jeho a novou
   (`https://raw.githubusercontent.com/reditelai/mcp-whatsapp/VERZE/CHANGELOG.md`).
   Podsekce „Při aktualizaci" proveď se souhlasem uživatele.
2. **Stáhni vedle a ověř:** novou binárku a `SHA256SUMS` (A2) do `DIR` pod
   jménem `SOUBOR.new` (na Windows `SOUBOR.new.exe`), ověř součet a
   `--version`.
3. **Vyměň přejmenováním**, ne přepsáním - běžící binárku na Windows přepsat
   nejde, přejmenovat ano:
   - starou přejmenuj na `SOUBOR.old` (Windows `SOUBOR.old.exe`),
   - novou přejmenuj na původní jméno `SOUBOR`.
4. **Požádej uživatele o novou konverzaci.** Nová verze se spustí, starou
   požádá o předání a ta se sama odpojí a skončí. V nové konverzaci
   `wa_status`: do pár sekund `connected`, spárování zůstává (klíče jsou
   v `DIR/data`). Když i po minutě hlásí `locked_by_other_instance`, drží
   spojení verze, která předání nezná (0.1.0): ukonči ji podle
   `lock_holder_pid` (`kill PID`, na Windows `taskkill /PID PID /F`).
5. Smaž `SOUBOR.old` a zapiš novou verzi do `system/moduly-instalovane.json`.

Když cokoli selže, vrať `SOUBOR.old` na původní jméno a řekni to uživateli.

### B5a. Přesun vaultu a nový počítač

- **Přesun na stejném počítači:** stačí přesunout celou složku vaultu, když
  server neběží (zavřená aplikace). Registrace je relativní, data jdou s ním.
- **Jiný počítač nebo jiný systém:** binárka je pro konkrétní systém. Podle
  `system/moduly-instalovane.json` stáhni binárku pro nový systém (A1, A2)
  do stejné složky, starou smaž. Přepis hlasovek (`.doplnky/prepis`) stáhni
  znovu přes `wa_transcription_setup`, engine je taky pro konkrétní systém.
- **Dva počítače zároveň** (vault synchronizovaný zálohou): `.doplnky/` se
  nezálohuje, každý počítač má vlastní instalaci. **Klíče (`data/`) mezi
  počítači nikdy nekopíruj** - dvě stejná zařízení by se přetahovala. Na
  druhém počítači spáruj WhatsApp znovu (je to další propojené zařízení),
  nebo ho nech jen na jednom.

### B6. Odpojení

1. `wa_logout` s `confirm: true` (odhlásí zařízení, smaže klíče).
2. Odeber záznam `whatsapp` z `.mcp.json`.
3. Se souhlasem uživatele smaž `VAULT/.doplnky/mcp-whatsapp/` i s uloženými
   zprávami. `VAULT/.doplnky/prepis/` smaž jen tehdy, když ho nepoužívá jiný
   doplněk (`system/moduly-instalovane.json`).
4. Odeber záznam `whatsapp` ze `system/moduly-instalovane.json`.
