# Změny

Formát vychází z [Keep a Changelog](https://keepachangelog.com/cs/1.1.0/),
čísla verzí ze [Semantic Versioning](https://semver.org/lang/cs/).

## [Nevydáno]

## [0.2.0] - 2026-09-29

Hlasovky jako text: přepis přímo v počítači, a aktualizace, při které nová verze převezme spojení sama.

- Přepis hlasovek lokálně (Parakeet v3 přes sherpa-onnx), nic neodchází ven. Instaluje se na souhlas uživatele nástrojem `wa_transcription_setup` (asi 510 MB, ověřené součty). Hlasovka přijde znovu s textem v `transcript`; starší na požádání `wa_transcribe`.
- Nová volba `transcription` v `config.json` (`enabled`, `threads`, `batch`), výchozí hodnoty stačí.
- Běžící server předá spojení instanci jiné verze a skončí; dvě konverzace se stejnou verzí se dál nepřetahují.
- Návod: aktualizace výměnou souboru přejmenováním (na Windows běžící binárku nejde přepsat), návrat při selhání, založení `moduly-instalovane.json`, když chybí.

### Při aktualizaci

- Nastavení se nemění, `transcription` se doplňovat nemusí. Postup je v `docs/pro-asistenta.md`, B5.
- Po aktualizaci nabídni uživateli přepis hlasovek (návod A8).
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
