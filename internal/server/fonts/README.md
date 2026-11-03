Fonts the panel themes use, embedded so every theme renders in its own face without a network fetch.
Each file is the Latin subset from the Fontsource npm packages (Departure Mono from its upstream release),
served by the panel at `/fonts/<name>.woff2`. Every font is under the SIL Open Font License 1.1; the
matching `<name>.OFL.txt` carries its copyright line and the licence text.

| File | Family | Themes |
| --- | --- | --- |
| inter.woff2 | Inter (variable) | Pine, Kurisu, Ava, Ledger |
| outfit.woff2 | Outfit (variable) | Pine, Kurisu, Ava, Ledger titles; Uika, Sumimi |
| jetbrains-mono.woff2 | JetBrains Mono (variable) | Pine, Kurisu, Ava, Ledger, Uika, Sumimi labels |
| ibm-plex-sans.woff2 | IBM Plex Sans (variable) | Astra, Astra light |
| ibm-plex-mono-400/500.woff2 | IBM Plex Mono | Astra, Astra light labels |
| source-serif-4.woff2 | Source Serif 4 (variable) | Astra, Astra light titles |
| cinzel.woff2 | Cinzel (variable) | Uika, Sumimi titles |
| departure-mono.woff2 | Departure Mono | Mariyaholic-98 |

Night sky and Confluence use faces Windows ships (Segoe UI, Georgia, Consolas), so they need no files.
