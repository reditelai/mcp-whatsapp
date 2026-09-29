# MCP server pro WhatsApp

MCP server, přes který asistent čte a posílá zprávy na WhatsAppu. Stojí na knihovně `whatsmeow`, neoficiálním klientovi WhatsApp Web.

**Plná specifikace je v `SPEC.md`.** Přečti si ji před první změnou v kódu. Tenhle soubor drží jen to, co se při psaní kódu snadno poruší.

## Zásady, které se snadno poruší

**1. Spojení drží jedna instance.** Claude Code spouští server pro každou konverzaci zvlášť a dvě spojení se stejným zařízením se navzájem shazují. Zámek (`lock` v datové složce) se nesmí obejít ani „na chvíli". Instance bez zámku jen čte z `app.db`.

**2. Prázdný seznam znamená nikam, ne kamkoli.** Platí pro `read`, `send` i `send.files`. Opačný výchozí stav vypadá jako uzamčený a není.

**3. Neznámá identita neprojde filtrem.** Člověk přijde jako telefonní číslo, nebo jako LID (`…@lid`). Když LID nejde převést na číslo, chat s ním se nečte ani mu nejde psát (kromě `"all"`). Nikdy nepovolovat „protože nevíme".

**4. Ukládá se jen to, co smí číst.** Zprávy z nepovolených chatů se nezapisují ani dočasně. Při čtení se filtruje znovu podle aktuální konfigurace, protože se mohla zúžit.

**5. Chyby jsou vidět.** Každý trvalý stav (odhlášeno, zastaralý klient, převzato, zákaz) má vlastní `state` ve `wa_status`. Nic nesmí skončit jen v logu.

**6. `stdout` je protokol MCP.** Logy jen na `stderr`. Jediný `fmt.Print` na stdout rozbije spojení s klientem.

**7. Knihovna `whatsmeow` se neupravuje, jen aktualizuje.** Je pod MPL: úpravy by se musely zveřejnit a rozešly by se s týdenními aktualizacemi.

## Co nikdy nesmí do gitu

- **`session.db` a cokoli z datové složky.** Jsou v něm klíče zařízení: kdo je má, má přístup k WhatsAppu.
- **Skutečná telefonní čísla, jména a obsah zpráv**, ani v testech. Příklady mají vymyšlené hodnoty (`+420777123456`).
- `.gitignore` má tyhle vzory od prvního commitu.

## Kde se staví a testuje

- Build a testy běží v GitHub Actions (`ci.yml`). Lokálně: Go z `go.mod`, `CGO_ENABLED=0`.
- **Na serveru derfl-srv1 se nepáruje ani nepřipojuje k WhatsAppu mimo session Věrky.** Druhé spojení by shodilo její kanál. Nespárovaný server se k WhatsAppu nepřipojuje, takže spuštění naprázdno a volání `wa_status` je bezpečné; `wa_pair` ne.
- Funkční test se dělá na testovacím čísle podle `SPEC.md`, „Jak se pozná, že to funguje".

## Vydání verze

Nová verze se k uživatelům dostane jen releasem. Z něj ji čte info kanál Miládky (`miladka.cz/moduly.json`) a podle `CHANGELOG.md` ji Miládka u uživatele aktualizuje.

1. **Verze na třech místech:** `VERSION`, sekce `## [X.Y.Z]` v `CHANGELOG.md` (z „Nevydáno") a tag `vX.Y.Z`.
2. **V changelogu u každé verze, co se změnilo. Piš stručně.** **První řádek sekce je souhrn jednou větou** - ten si info kanál vezme jako `zmeny`. Když aktualizace vyžaduje zásah do nastavení uživatele (nový klíč v `config.json`), přidej podsekci **„Při aktualizaci"**. **Starší sekce se nikdy nemažou.**
3. **Pushnutý tag spustí `release.yml`**: binárky pro šest cílů, `SHA256SUMS`, release se souhrnnou větou.
4. **Týdenní `whatsmeow.yml`** vydá patch verzi sám, jen když je sekce „Nevydáno" prázdná. Rozdělanou práci proto na `main` nenechávej dlouho - blokuje automatické aktualizace.

## Konvence

- **Česky:** `README.md`, `SPEC.md`, tenhle soubor, `docs/pro-asistenta.md`, chyby konfigurace při startu (čte je člověk v logu) a stavové texty ve `wa_status` (asistent je tlumočí uživateli).
- **Anglicky:** kód, komentáře, názvy a popisy nástrojů, instrukce serveru, chyby z nástrojů. Čte je jen model; ve dvou jazycích by se tiše rozešly.
- **Nástroje mají prefix `wa_`.**
