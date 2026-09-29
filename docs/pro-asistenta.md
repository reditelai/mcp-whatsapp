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

### A1. Prostředí

```sh
uname -s
uname -m
echo "$HOME"
```

| `uname -s` | Soubor z releasu |
|---|---|
| `Darwin` + `arm64` | `mcp-whatsapp-darwin-arm64` |
| `Darwin` + `x86_64` | `mcp-whatsapp-darwin-amd64` |
| `MINGW…`/`MSYS…` nebo chyba (PowerShell) | `mcp-whatsapp-windows-amd64.exe` (ARM: `-arm64.exe`) |
| `Linux` + `x86_64` | `mcp-whatsapp-linux-amd64` |

Na Windows v PowerShellu je domovská složka `$env:USERPROFILE`.

### A2. Stažení poslední verze

```sh
curl -s https://api.github.com/repos/reditelai/mcp-whatsapp/releases/latest
```

Verze je v `tag_name` (dál `VERZE`). Stáhni binárku a `SHA256SUMS` do
`$HOME/mcp-whatsapp/`:

```sh
mkdir -p "$HOME/mcp-whatsapp" && cd "$HOME/mcp-whatsapp"
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

S Miládkou patří do `.miladka/secrets/whatsapp/config.json` ve vaultu
(`.miladka/secrets/` je v `.gitignore`, ověř `git check-ignore -v`). Jinde
do `$HOME/mcp-whatsapp/config.json`.

```json
{
  "read": { "chats": "all", "groups": false },
  "send": { "chats": ["+420777123456"], "groups": false, "files": [] },
  "history_sync": false,
  "device_name": "Miládka"
}
```

Zkontroluj ho:

```sh
"$HOME/mcp-whatsapp/SOUBOR" --config CESTA_KE_CONFIGU --check
```

Na Windows piš cesty s obyčejnými lomítky (`C:/Users/…`).

### A5. Připojení do Claude Code

Buď do `.mcp.json` v kořeni vaultu (Claude Code se na něj v nové konverzaci
zeptá, uživatel povolí):

```json
{
  "mcpServers": {
    "whatsapp": {
      "command": "CELÁ_CESTA_K_BINÁRCE",
      "args": ["--config", "CELÁ_CESTA_KE_CONFIGU"]
    }
  }
}
```

Nebo pro uživatele napříč složkami:

```sh
claude mcp add whatsapp --scope user -- CELÁ_CESTA_K_BINÁRCE --config CELÁ_CESTA_KE_CONFIGU
```

Pak požádej uživatele o novou konverzaci. V ní zavolej `wa_status`: čekáš
`not_paired`.

### A6. Spárování

1. Řekni uživateli, ať si připraví telefon: **WhatsApp → Nastavení →
   Propojená zařízení → Propojit zařízení.** Počkej, až řekne, že má kameru
   připravenou. Kód platí jen 20 až 60 sekund.
2. Zavolej `wa_pair`. Hned otevři obrázek z `qr_path`:
   - Windows: `start "" "CESTA"`
   - macOS: `open "CESTA"`
3. Uživatel naskenuje. Když kód mezitím vyprší, zavolej `wa_pair` znovu, vrátí
   čerstvý.
4. Když obrázek nejde ukázat (server bez obrazovky, vzdálený přístup), použij
   `wa_pair` s `method: "code"` a jeho číslem. Uživatel v telefonu zvolí
   **Propojit s telefonním číslem** a kód opíše.
5. Po naskenování `wa_status` přejde na `connected`.

### A7. Po spárování

- Zapiš do `system/moduly-instalovane.json` záznam
  `{"id": "whatsapp", "verze": "X.Y.Z", "nainstalovano": "RRRR-MM-DD"}`
  (verze bez „v").
- Založ `system/whatsapp-kotva.md` s kurzorem z prvního `wa_new_messages`
  (bez `cursor`). Viz část B.
- Když má denní přehled, přidej do jeho postupu krok „nové zprávy na
  WhatsAppu" (část B) a udělej generálku jako u jiných změn přehledu.

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
   pravidel. Hlasovky (`kind: "voice"`) mají soubor v `media_path`; přepis
   zatím neumíš, řekni, že přišla hlasovka od koho a jak dlouhá.
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

Novou verzi hlásí info kanál Miládky. Postup:

1. Přečti `CHANGELOG.md` nové verze a všech mezi jeho a novou
   (`https://raw.githubusercontent.com/reditelai/mcp-whatsapp/VERZE/CHANGELOG.md`).
   Podsekce „Při aktualizaci" proveď se souhlasem uživatele.
2. Stáhni novou binárku a součty (A2) vedle staré, ověř, a starou nahraď.
3. Nová konverzace, `wa_status`. Spárování zůstává, klíče jsou v datové
   složce.
4. Zapiš novou verzi do `system/moduly-instalovane.json`.

### B6. Odpojení

1. `wa_logout` s `confirm: true` (odhlásí zařízení, smaže klíče).
2. Odeber server z `.mcp.json`, nebo `claude mcp remove whatsapp --scope user`.
3. Se souhlasem uživatele smaž `$HOME/mcp-whatsapp/` a datovou složku
   (`~/.mcp-whatsapp/`) i s uloženými zprávami.
