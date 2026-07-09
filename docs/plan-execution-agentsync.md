# Plan d'exécution final — AgentSync

## Objectif (une ligne)
Verrouiller les 8 décisions ouvertes du PRD, puis construire le binaire Go `agents-sync` par tranches verticales jusqu'aux critères d'acceptation v1.

## Décisions verrouillées (3 arbitrages confirmés)
1. **Périmètre** : PRD régénéré (Phase 0) + build complet (Phases 1–5).
2. **Q7 — Merge `opencode.json`** : adaptateur pur + méthode d'interface optionnelle `MergeFile`. L'adaptateur renvoie des fragments logiques (`map[clé]fragment` pour les blocs `agent`/`command`) ; le cœur fait le merge JSON (parse → remplace les clés `agent`/`command` → réécrit en préservant le reste et l'indentation). Pas d'I/O dans l'adaptateur → testable.
3. **Q2 — Permission `read`** : le pivot garde un `read` global (sémantique outil-agnostique). La granularité fine OpenCode (`glob`/`grep`/`list`/`lsp`) passe par `extensions.opencode.permission`. L'adaptateur OpenCode lit `read` + `extensions.opencode.permission` (ce dernier override si présent).

---

## Phase 0 — Régénérer le PRD (verrouiller Q1–Q8) ✅ DONE
Éditer `docs/prd/agentsync.md` :

- **Q1** → FR2 : `promptFile` relatif au fichier pivot. Ajouter règle + exemple.
- **Q2** → FR3 : ajouter table de mapping par CLI + convention `extensions.opencode.permission` pour la granularité fine (décision 3).
- **Q3** → FR6 : conflit = refuser par défaut, `--force` pour écraser. Pas de merge 3-way en v1.
- **Q4** → FR5/FR6 + Architecture : `.agentsync-state.json` à côté du pivot.
- **Q5** → Non-goals + FR1 : skills référencés en lecture seule (`skills: [{name}]`), non gérés en v1.
- **Q6** → FR1 : walk-up CWD→root + fallback `$HOME/.agentsync/agentsync.yaml`.
- **Q7** → FR10 : interface `Adapter` étendue d'une `MergeFile` optionnelle (décision 2).
- **Q8** → Distribution : `agents-sync`, release GitHub `curl|sh`.
- Convertir « Open questions » en « Decisions log » (Q1–Q8 résolues).

~~ **Vérif Phase 0** : relire le PRD ; chaque FR référence sa décision ; plus aucune question ouverte bloquante. ~~

---

## Phase 1 — Socle Go + pivot (tracer bullet)
1. `go mod init`, Go 1.24, `.gitignore`, `Makefile` (build/test/lint).
2. `cmd/agents-sync/main.go` : Cobra root + sous-commandes stub (`diff`, `push`, `validate`, `init`).
3. `internal/pivot/schema.go` : structs `AgentDefinition`, `CommandDefinition`, `Permissions`, `Extensions`, `PivotFile`.
4. `internal/pivot/parser.go` : parse yaml.v3 + validation (obligatoires, regex `id`, `systemPrompt` xor `promptFile`, `promptFile` existe, `commands[].agent` résolu, `extensions.opencode.permission` bien formée).
5. `internal/pivot/discover.go` : walk-up CWD→root + fallback `$HOME/.agentsync/` + flag `-c`.
6. `internal/fsutil/{paths,write}.go` : résolution `~/.claude`, `~/.config/opencode` ; écriture atomique (temp+rename).
7. Tests unitaires parser (valide + chaque cas d'erreur).

**Vérif Phase 1** : `go test ./internal/pivot/...` vert ; `agents-sync validate` retourne 0/1 selon validité.

---

## Phase 2 — Adapter OpenCode end-to-end (premier chemin complet)
Critères d'acceptation 1–5.

1. `internal/adapter/adapter.go` : interface `Adapter` + `MergeFile(path, existing, fragments) (newBytes, error)` optionnelle.
2. `internal/adapter/opencode/agent.go` : bloc `agent.<id>` (`description`, `mode`, `model`, `temperature`, `prompt: {file:./prompts/<id>.md}`, `steps` via `extensions.opencode.steps`) + `prompts/<id>.md`.
3. `internal/adapter/opencode/command.go` : bloc `command.<id>` + `command/<id>.md`.
4. `internal/adapter/opencode/adapter.go` : `Name`, `ValidateAgent`, `TargetPaths`, `MergeFile` (merge JSON préservant l'indentation et les champs hors-scope — valider la stratégie de préservation de format en début de phase).
5. `internal/cli/{push,diff}.go` : orchestration accumulate→merge→write (diff sans détection modif manuelle, Phase 4).
6. Tests golden : `expected_opencode.json`, `expected_prompts_build.md` ; test qu'un champ hors-scope ajouté manuellement est préservé.

**Vérif Phase 2** : `push --target opencode` écrit les fichiers ; 2e `diff` → "No changes" ; champ hors-scope préservé.

---

## Phase 3 — Adapter Claude Code end-to-end
Critères 9–10.

1. `internal/adapter/claude/agent.go` : Markdown + frontmatter YAML (`name`, `description`, `tools`, `model`, `permissionMode` mappé via table Q2).
2. `internal/adapter/claude/command.go` : `~/.claude/commands/<id>.md`.
3. `internal/adapter/claude/adapter.go` : mapping permissions (Q2), `TargetPaths`.
4. Tests golden (`expected_agents_build.md`, `expected_commands_ship.md`).

**Vérif Phase 3** : `push --target claude-code` produit frontmatter valide ; `push` sans `--target` écrit pour les deux adaptateurs.

---

## Phase 4 — Diff, état, modif manuelle, `--force`, `init`
Critères 6–8.

1. `internal/diff/differ.go` : diff unifié coloré (créé/modifié/supprimé/orphelin).
2. `internal/diff/state.go` : `.agentsync-state.json` (hash par chemin) à côté du pivot (Q4).
3. Détection modif manuelle : natif ≠ état connu ET ≠ sortie pivot → signalé ; `push` refuse sans `--force` (Q3).
4. `--dry-run` = `diff` ; `--force` écrase.
5. `init` : bootstrap depuis le 1er adaptateur installé (ordre OpenCode→Claude, Q8).

**Vérif Phase 4** : scénario complet 1–10 du PRD passe.

---

## Phase 5 — Intégration, golden files, build/release
1. Fixtures **sanitisés** dérivés des configs réelles (pas de symlink vers `~`).
2. Intégration end-to-end via `t.TempDir()`.
3. `golangci-lint`, `go build`, CI GitHub Actions (mac/linux arm64/amd64), release `curl|sh`.

---

## Risks & edge cases
- **Préservation format JSON (Q7)** : `encoding/json` peut réordonner/perdre l'indentation. À valider dès le début Phase 2 ; fallback lib préservant le format si besoin.
- **`steps` OpenCode** : champ natif sans équivalent pivot → via `extensions.opencode.steps`. Documenter dans FR2.
- **Walk-up + cible locale (Q6)** : `push --target opencode` écrit dans `.opencode/` local si pivot trouvé dans un repo. Définir la priorité local/global pour éviter la collision.
- **State file dans repo** : `.agentsync-state.json` committé par erreur → template `.gitignore`.

## Vérification post-implémentation
```
go test ./...
golangci-lint run
go build -o agents-sync ./cmd/agents-sync
./agents-sync validate -c testdata/agentsync.yaml
./agents-sync diff --target opencode
./agents-sync push --target opencode --dry-run
```
+ scénario 1–10 du PRD en test d'intégration.
