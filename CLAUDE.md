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

Nová verze se k uživatelům dostane jen releasem. Z něj ji čte info kanál Miládky (`miladka.cz/moduly.json`) a podle `CHANGELOG.md` a `docs/pro-asistenta.md` ji Miládka u uživatele aktualizuje. **Všechny body níž jsou jedno vydání a dělají se spolu, jinak se aktualizace u uživatelů rozbije.**

1. **Dokumentace je hotová před tagem.** Info kanál posílá Miládku na `CHANGELOG.md` a `docs/pro-asistenta.md` **v tagu vydané verze**. Co se dopíše po tagu, Miládka u uživatele neuvidí.
2. **Changelog:** sekce `## [X.Y.Z] - RRRR-MM-DD` (z „Nevydáno"). **Piš stručně.** **První řádek je souhrn jednou větou** - info kanál ho vezme jako `zmeny`. Pak pár bodů. **Starší sekce se nikdy nemažou.**
3. **„Při aktualizaci"** - podsekce, **kdykoli má Miládka při aktualizaci něco udělat nebo ověřit** (nový klíč v `config.json`, nové ověření, přepárování). Přesné kroky, nastavení jen se souhlasem uživatele. Když není potřeba nic: „Nastavení se nemění."
4. **Návod pro asistenta:** když se změna týká instalace nebo nastavení, uprav příslušný krok v `docs/pro-asistenta.md` i tabulku stavů. Nová instalace a aktualizace musí vést ke stejnému výsledku. README taky.
5. **Verze na třech místech:** `VERSION`, sekce v changelogu, tag `vX.Y.Z`. `release.yml` nepustí nesoulad a přiloží binárky a `SHA256SUMS`.
6. **Web, info kanál** (repo `web-miladka`, push do `main` = produkce, jen na Karlův pokyn): první vydání = záznam v `MODULY` v `src/lib/moduly.ts` s `id` `whatsapp`, stejným, jaké návod zapisuje do `system/moduly-instalovane.json`. Vážná chyba ve starší verzi (třeba odstřižený klient) = `minVerze`. Novinka nebo problém pro uživatele = položka v `src/kanal/info.json`. Verzi a souhrn si web bere z releasu sám.
7. **Po vydání ověř** `https://miladka.cz/moduly.json` (drží se 10 minut).
8. **Týdenní `whatsmeow.yml`** vydá patch verzi sám, jen když je sekce „Nevydáno" prázdná (a už existuje vydaná verze). Rozdělanou práci proto na `main` nenechávej dlouho - blokuje automatické aktualizace. Automatické vydání má „Při aktualizaci" jen tehdy, když ho někdo dopíše ručně; knihovna samotná nastavení nemění.
9. **Čísla verzí:** do 1.0.0 se rozhraní může měnit (vždy s „Při aktualizaci"). Od 1.0.0 drží nástroje a klíče konfigurace zpětnou kompatibilitu. Stejný seznam je v `miladka-vyvoj/CLAUDE.md`; když se tady změní, změň ho tam.
10. **Článek na webu:** když se mění, co dělá uživatel (kroky instalace, co musí nastavit, co uvidí), uprav článek na miladka.cz v repu `web-miladka` (`src/clanky/`, CS i EN; evidence v `miladka-vyvoj/clanky.md`).
11. **Test celé cesty před tagem:** aktualizace z předchozí verze podle changelogu a návodu (na Karlově nebo Věrčině instalaci), a když se změnila instalace, i nová instalace. Návod, který v půlce nefunguje, se k uživatelům nesmí dostat.
12. **Závislost na verzi Miládky:** když modul potřebuje soubory nebo pravidla z novější verze balíčku, napiš to do návodu (oddíl pro danou verzi Miládky) i do „Při aktualizaci", a starší Miládku ať návod zastaví s vysvětlením.
13. **Zápis ve vývoji:** `miladka-vyvoj` - `CHANGELOG-vyvoj.md` (co vyšlo a proč), `_vyvoj/STAV.md`, katalog v `_vyvoj/moduly.md` (stav a verze).

**Hlídá automat:** release workflow neprojde bez souhrnné věty a bez podsekce „Při aktualizaci" v sekci verze a bez souhlasu verzí; týdenní aktualizace „Při aktualizaci" dopisuje sama; po vydání 20 minut čeká, až verzi ukáže `miladka.cz/moduly.json`, a když ne, založí issue. Body 1, 4, 6 a 10 až 13 hlídá jen tenhle seznam.

## Konvence

- **Česky:** `README.md`, `SPEC.md`, tenhle soubor, `docs/pro-asistenta.md`, chyby konfigurace při startu (čte je člověk v logu) a stavové texty ve `wa_status` (asistent je tlumočí uživateli).
- **Anglicky:** kód, komentáře, názvy a popisy nástrojů, instrukce serveru, chyby z nástrojů. Čte je jen model; ve dvou jazycích by se tiše rozešly.
- **Nástroje mají prefix `wa_`.**
