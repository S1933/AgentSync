# PRD — Shenron

## Problem statement

Je maintiens manuellement la même configuration d'agents, de skills et de slash-commands dans plusieurs CLI d'assistants de code (Claude Code, OpenCode, Codex). Chaque outil a son propre format de configuration (YAML frontmatter + Markdown pour Claude, JSON pour OpenCode, TOML pour Codex), sa propre structure de dossiers, et ses propres conventions de nommage. À chaque changement — nouvel agent, modification de prompt, ajustement de permissions — je dois répercuter la modification dans 2 ou 3 endroits différents, avec un risque élevé de divergence.

**Aujourd'hui, sur ma machine :**
- 7 agents dupliqués entre `~/.claude/agents/` et `~/.config/opencode/opencode.json`
- 1 slash-command dupliqué (`review-diff`) + plusieurs commandes présentes d'un seul côté
- 41 skills partagés via symlinks `~/.claude/skills/ → ~/.agents/skills/` — déjà une couche de pont ad-hoc

L'objectif est de remplacer cette duplication manuelle par un outil qui maintient **une source de vérité unique** et propage automatiquement les changements vers chaque CLI cible.

## Goals

1. **Source unique.** Une configuration déclarative (fichier YAML) qui décrit agents, skills, et slash-commands de façon outil-agnostique, dans un seul endroit.

2. **Propagation automatisée.** Une commande `shenron push` qui lit le fichier pivot et écrit/actualise les fichiers de configuration natifs dans les emplacements attendus par chaque CLI cible.

3. **Extensible.** Ajouter le support d'un nouveau CLI ne nécessite que l'implémentation d'un adaptateur — aucun changement dans le cœur du parsing ni dans le CLI.

4. **Non-destructif.** Un mode `diff` pour visualiser les changements avant écriture. Les modifications manuelles dans les fichiers natifs sont détectées et signalées (pas d'écrasement silencieux).

5. **Portable.** Un binaire statique unique compilé avec Go, sans dépendance runtime.

## Non-goals (v1)

- **Pas de wizard interactif.** Le fichier pivot est écrit à la main en YAML. Une commande `init` peut générer un squelette, mais pas de interface TUI/questionnaire.
- **Pas de `pull`.** L'import d'une configuration native existante vers le format pivot est hors scope v1. Priorité au sens `pivot → natif`.
- **Pas de gestion des settings.json / config.toml globaux.** Les permissions, modèles, providers, hooks, MCP servers, et autres settings "environnement" restent gérés manuellement dans chaque outil. Le pivot ne couvre que les **agents et les slash-commands** en v1. Les skills sont déjà standardisés via `SKILL.md` — le pivot les référence mais ne les réécrit pas.
- **Pas de gestion des contenus de skills Codex.** Shenron transmet seulement les noms de skills comme instruction aux agents Codex; il ne résout ni ne copie les chemins locaux.
- **Pas de support Windows en v1.** macOS et Linux uniquement (les chemins de config sont POSIX).

## Functional requirements

### FR1 — Fichier pivot

Le fichier pivot (`shenron.yaml`) est un fichier YAML contenant :

- `version` : chaîne de version du schéma (ex. `"1"`)
- `agents` : liste de définitions d'agents
- `commands` : liste de définitions de slash-commands
- `skills` (optionnel) : liste de références de skills (`[{name}]`), lecture seule. Les skills eux-mêmes ne sont pas gérés par le pivot (déjà standardisés via `agentskills.io`)

Le fichier est découvert par résolution ascendante depuis le répertoire courant jusqu'à la racine du filesystem (comportement similaire à `.gitignore`). Le premier `shenron.yaml` trouvé est utilisé. Un flag `-c <path>` permet de spécifier un chemin explicite.

**Emplacements de recherche (dans l'ordre) :**
1. `$CWD/shenron.yaml`
2. `$CWD/../shenron.yaml`
3. ... (remontée)
4. `$HOME/.shenron/shenron.yaml`

### FR2 — Définition d'agent (schéma pivot)

Chaque agent dans le pivot a les champs suivants :

| Champ | Type | Obligatoire | Description |
|---|---|---|---|
| `id` | string (regex `^[a-z][a-z0-9-]*$`) | Oui | Identifiant unique de l'agent |
| `description` | string (1-1024 chars) | Oui | Description affichée dans le sélecteur d'agent du CLI cible |
| `mode` | enum `primary` / `subagent` | Oui | Rôle : agent principal (mode par défaut) ou sous-agent (délégué) |
| `model` | string (provider/id) ou null | Non | Modèle LLM à utiliser. `null` = hérite du modèle par défaut du CLI cible. Exemples : `anthropic/claude-sonnet-4-5`, `opencode/deepseek-v4-pro` |
| `temperature` | float (0.0-2.0) | Non | Température du modèle (optionnelle) |
| `systemPrompt` | string multiligne | Non | Instructions système de l'agent (corps du prompt). Si absent, l'agent n'a que la description comme contexte — utile pour les agents "routeurs" qui délèguent. |
| `promptFile` | string (chemin relatif au répertoire du fichier pivot) | Non | Alternative à `systemPrompt` : référence un fichier Markdown externe contenant le prompt système. Exemple : `prompts/build.md` (relatif au répertoire où se trouve `shenron.yaml`). |
| `permissions` | objet | Non | Permissions de l'agent (voir section dédiée) |
| `extensions` | map string→any | Non | Champs spécifiques à un CLI cible (ex. `claudeCode.maxTurns`, `opencode.steps`). Le cœur ignore ces champs ; les adaptateurs les consomment. |
| `skills` | list[string] (kebab-case) | Non | Skills que cet agent déclare utiliser. Référence des noms de skills dans `~/.agents/skills/`. Émis par l'adaptateur Claude Code (frontmatter) et par l'adaptateur Codex (hint d'instruction). **Omis par l'adaptateur OpenCode** (voir décision D9). |

**Règle :** `systemPrompt` et `promptFile` ne peuvent pas être spécifiés simultanément.

### FR3 — Définition de permission (schéma pivot)

Les permissions sont décrites de façon normalisée, indépendante du CLI :

| Champ | Type | Obligatoire | Description |
|---|---|---|---|
| `read` | enum `allow` / `deny` / `ask` | Non | Droit de lecture sur le filesystem |
| `edit` | enum `allow` / `deny` / `ask` | Non | Droit d'édition de fichiers |
| `bash` | map pattern→mode (ou `allow`/`deny`/`ask` global) | Non | Commandes shell autorisées. Les patterns suivent la syntaxe glob. |
| `webfetch` | enum `allow` / `deny` / `ask` | Non | Droit de fetch HTTP |
| `websearch` | enum `allow` / `deny` / `ask` | Non | Droit de recherche web |
| `tasks` | map agentName→mode | Non | Sous-agents que cet agent peut déléguer |

La sémantique exacte (`allow` = autorisé sans confirmation, `ask` = confirmation requise, `deny` = interdit) est mappée par chaque adaptateur vers le système de permissions natif du CLI cible.

Les CLI qui n'ont pas de notion d'une permission donnée l'ignorent silencieusement (ex. Codex n'a pas de `webfetch` distinct — l'adaptateur Codex ignore ce champ).

**Mapping des permissions par CLI :**

| Permission pivot | Claude Code | OpenCode | Codex (v2) |
|---|---|---|---|
| `read` | `tools: Read` | `glob`, `grep`, `list`, `lsp` | ignoré |
| `edit` | `permissionMode` : `acceptEdits` (allow), `default` (ask), `plan` (deny) | `permission.edit` | `sandbox_mode` |
| `bash` | `tools: Bash` | `permission.bash` (patterns natifs) | `tools` |
| `webfetch` | `tools: WebFetch` | `permission.webfetch` | ignoré |
| `websearch` | `tools: WebSearch` | `permission.websearch` | ignoré |
| `tasks` | `tools: Skill` | `permission.task` (map par nom) | `subagents` |

**Granularité fine OpenCode :** Le champ `read` du pivot est un booléen global. Pour un contrôle fin au niveau des sous-permissions OpenCode (`glob`, `grep`, `list`, `lsp`), l'adaptateur lit la clé `extensions.opencode.permission`. Si présente, elle override le mapping de `read`. Format attendu :

```yaml
extensions:
  opencode:
    permission:
      glob: allow
      grep: allow
      list: allow
      lsp: deny
```

### FR4 — Définition de slash-command

Chaque commande dans le pivot :

| Champ | Type | Obligatoire | Description |
|---|---|---|---|
| `id` | string (regex `^[a-z][a-z0-9-]*$`) | Oui | Nom de la commande (ex. `review-diff`, `ship`) |
| `description` | string | Oui | Description affichée par le CLI |
| `template` | string multiligne | Oui | Contenu de la commande (prompt ou instructions) |
| `agent` | string (référence un `agents[].id`) | Non | Agent à utiliser pour exécuter cette commande. Si absent, l'agent par défaut du CLI cible est utilisé. |
| `model` | string (provider/id) | Non | Modèle à utiliser pour cette commande (prioritaire sur le modèle de l'agent) |

### FR5 — Commande `shenron diff`

> **Remplacé par D10 — voir `docs/prd/scope-flatten-commands.md` (FR4).**
> La commande `shenron diff` opère désormais sur un package installé via
> `shenron diff <name>`. Le format de sortie et la sémantique
> created/modified/manually-modified/orphaned sont conservés.

Affiche un résumé des différences entre le fichier pivot et la configuration native existante dans le(s) CLI cible(s) :

- Fichiers qui seraient créés
- Fichiers qui seraient modifiés (avec diff unifié)
- Fichiers qui seraient supprimés (agents ou commandes présents dans le natif mais absents du pivot)
- Fichiers modifiés manuellement (l'outil détecte si le contenu natif ne correspond ni au pivot ni à la dernière sortie connue de l'outil)

Sortie en texte coloré dans le terminal. Pas de sortie JSON en v1.

La détection des modifications manuelles s'appuie sur un fichier d'état `.shenron-state.json` stocké dans le même répertoire que le fichier pivot. Ce fichier enregistre le hash du contenu écrit lors du dernier `push` réussi.

### FR6 — Commande `shenron push`

> **Remplacé par D10 — voir `docs/prd/scope-flatten-commands.md` (FR5).**
> La commande `shenron push` opère désormais sur un package installé via
> `shenron push <name>`. Les options `--target`, `--force` et
> `--allow-permissions` sont conservées ; `--dry-run` n'existe pas (utiliser
> `shenron diff <name>` à la place).

Lit le fichier pivot, génère la configuration native pour le(s) CLI cible(s), et écrit les fichiers. Options :

- `--target <name>` : limite à un seul CLI cible (ex. `opencode`, `claude-code`). Si absent, tous les adaptateurs disponibles sont exécutés.
- `--dry-run` : équivalent à `diff` (pas d'écriture).
- `--force` : écrase les fichiers natifs même s'ils ont été modifiés manuellement. Comportement par défaut : refuser avec un message d'erreur et suggérer `--force`. Pas de merge 3-way en v1.

### FR7 — Commande `shenron validate`

> **Retiré par D10 — voir `docs/prd/scope-flatten-commands.md` (non-goal
> « Re-introducing `validate` »).** La validation reste exécutée
> implicitement par `shenron install`, `shenron update` et `shenron push`.
> Aucune commande `shenron validate` n'est exposée.

Valide le fichier pivot contre le schéma. Vérifie :

- Syntaxe YAML correcte
- Champs obligatoires présents
- Types corrects
- Références croisées valides (ex. `commands[].agent` référence un `agents[].id` existant)
- Les `promptFile` pointent vers des fichiers existants
- Les `skills` référencent des noms kebab-case valides (regex `^[a-z][a-z0-9-]*$`). Leur absence sur disque produit au plus un warning, car un pivot partagé peut référencer des skills non locales.

### FR8 — Commande `shenron init`

> **Retiré par D10 — voir `docs/prd/scope-flatten-commands.md` (non-goal
> « Re-introducing `init` »).** L'amorçage se fait en écrivant un
> `shenron-package.yaml` et un `shenron.yaml` à la main, puis en exécutant
> `shenron install ./mon-package`. Aucune commande d'import automatique
> n'est exposée.

Génère un fichier `shenron.yaml` squelette pré-rempli à partir des agents déjà présents dans le premier adaptateur installé trouvé (ordre : OpenCode, puis Claude Code, puis Codex). Objectif : bootstrapper rapidement à partir d'une config existante sans importer TOUT le format natif.

### FR9 — Découverte des adaptateurs

Le binaire embarque tous les adaptateurs connus à la compilation (pas de plugins dynamiques en v1). La commande `push --target` utilise une registry interne :

```go
adapters := map[string]Adapter{
    "claude-code": &claude.Adapter{...},
    "opencode":    &opencode.Adapter{...},
    "codex":       &codex.Adapter{...},
}
```

Ajouter un nouvel adaptateur = implémenter l'interface `Adapter` + l'enregistrer dans cette map. Aucune modification du cœur.

### FR10 — Interface Adapter

Chaque adaptateur implémente l'interface Go suivante :

```go
type AgentDefinition struct {
    ID           string
    Description  string
    Mode         string // "primary" | "subagent"
    Model        string
    Temperature  float64
    SystemPrompt string
    Permissions  Permissions
    Extensions   map[string]any
    Skills       []string
}

type CommandDefinition struct {
    ID          string
    Description string
    Template    string
    Agent       string
    Model       string
}

type Adapter interface {
    // Name returns the adapter identifier (e.g. "claude-code").
    Name() string

    // ValidateAgent checks that an agent definition is valid for this target.
    // Returns nil or a human-readable error.
    ValidateAgent(AgentDefinition) error

    // GenerateAgent produces the native config representation for an agent.
    // Returns a map of file path -> file content.
    GenerateAgent(AgentDefinition) (map[string]string, error)

    // GenerateCommand produces the native config representation for a command.
    GenerateCommand(CommandDefinition) (map[string]string, error)

    // TargetPaths returns the paths the adapter writes to, so the CLI can
    // detect manual edits and clean up orphaned files.
    TargetPaths() []string

    // MergeFile merges fragments into an existing file, preserving
    // unrelated content. Used when multiple agents/commands share one
    // file (e.g. opencode.json). If the adapter does not need shared-file
    // merging, it can return nil (the CLI falls back to plain file writes).
    // Method is optional — the CLI checks for nil before calling.
    MergeFile(path string, existing []byte, fragments map[string]any) ([]byte, error)
}
```

Chaque adaptateur reçoit la même `AgentDefinition` / `CommandDefinition` normalisée et produit un `map[string]string` (chemin → contenu). Pour les cibles où plusieurs agents/commandes partagent un fichier unique (ex. `opencode.json`), la méthode optionnelle `MergeFile` permet au cœur d'accumuler les fragments puis de les fusionner dans le fichier existant en préservant les clés hors-scope. Le CLI se charge de l'écriture disque, du diff, et de la détection de modifications manuelles.

## Architecture

```
cmd/
  shenron/
    main.go              # Point d'entrée, construction du CLI Cobra

internal/
  pivot/
    schema.go            # Structs Go : AgentDefinition, CommandDefinition, Permissions
    parser.go            # Parse YAML → structs + validation
    discover.go          # Découverte du fichier pivot (walk-up)

  adapter/
    adapter.go           # Interface Adapter (définition)
    claude/
      adapter.go         # Implémentation Claude Code
      agent.go           # Génération agents/*.md (frontmatter YAML + Markdown)
      command.go         # Génération commands/*.md
    opencode/
      adapter.go         # Implémentation OpenCode
      agent.go           # Génération bloc agent.* dans opencode.json + prompts/*.md
      command.go         # Génération bloc command.* + command/*.md
    codex/
      adapter.go         # Implémentation Codex (v2 seulement)
      agent.go           # Génération section [agents.<name>] dans config.toml
      skill.go           # Génération [[skills.config]] + .agents/skills/

  cli/
    root.go              # Commande racine
    diff.go              # shenron diff
    push.go              # shenron push
    validate.go          # shenron validate
    init.go              # shenron init

  diff/
    differ.go            # Logique de diff unifié + détection de modifications manuelles
    state.go             # Fichier d'état (.shenron-state.json) pour tracker la dernière sortie connue

  fsutil/
    paths.go             # Résolution de chemins : ~/.claude/, ~/.config/opencode/, ~/.codex/
    write.go             # Écriture atomique (temp file + rename)
```

## Data flow

```
shenron.yaml  ──[parser]──►  []AgentDefinition  ──[adapter.GenerateAgent]──►  map[path]content
                               []CommandDefinition ──[adapter.GenerateCommand]──►  map[path]content
                                                                                      │
                                                    ┌─────────────────────────────────┘
                                                    ▼
                                              [differ]  ──►  diff affiché
                                              [writer]  ──►  fichiers écrits
```

## Target CLI formats — summary from research

### Claude Code

| Concept | Format | Emplacement |
|---|---|---|
| Agent | Markdown + YAML frontmatter : `name`, `description`, `tools`, `model`, `permissionMode` | `~/.claude/agents/<id>.md` |
| Command | Markdown + YAML frontmatter : `name`, `description` | `~/.claude/commands/<id>.md` |
| Skill | Dossier avec `SKILL.md` (standard agentskills.io) | `~/.claude/skills/<name>/SKILL.md` |

### OpenCode

| Concept | Format | Emplacement |
|---|---|---|
| Agent | Bloc `agent.<id>` dans `opencode.json` (JSON) + fichier prompt externe via `"prompt": "{file:./prompts/<id>.md}"` | `~/.config/opencode/opencode.json` + `~/.config/opencode/prompts/<id>.md` |
| Command | Bloc `command.<id>` dans `opencode.json` (JSON) + fichier template externe via `"template": "{file:./command/<id>.md}"` | `~/.config/opencode/opencode.json` + `~/.config/opencode/command/<id>.md` |
| Skill | Dossier avec `SKILL.md` (standard agentskills.io). Lit aussi `.claude/skills/` directement. | `~/.config/opencode/skills/` OU `~/.claude/skills/` |

### Codex (CLI)

| Concept | Format | Emplacement |
|---|---|---|
| Agent | Fichier TOML autonome : `name`, `description`, `developer_instructions`, modèle et sandbox | `~/.codex/agents/<name>.toml` |
| Command | Markdown avec frontmatter `description` | `~/.codex/prompts/<name>.md` |
| Skills | Dossier avec `SKILL.md` (standard agentskills.io) | `~/.agents/skills/`, `$REPO_ROOT/.agents/skills/`, `/etc/codex/skills/` |

## Testing strategy

### Tests unitaires

- **Parser YAML** : parse un `shenron.yaml` valide et vérifie les structs résultantes
- **Parser YAML** : erreurs de validation (champ obligatoire manquant, type incorrect, regex invalide sur `id`, `promptFile` introuvable)
- **Parser YAML** : références croisées (`commands[].agent` pointe vers un agent inexistant → erreur)
- **Permissions** : mapping des patterns glob de permission vers le format natif
- **Adapter OpenCode** : `GenerateAgent` produit le bloc JSON attendu + le fichier prompt
- **Adapter OpenCode** : `GenerateCommand` produit le bloc JSON + le fichier commande
- **Adapter Claude Code** : `GenerateAgent` produit le Markdown + YAML frontmatter attendu
- **Adapter Claude Code** : `GenerateCommand` produit le fichier commande Markdown

### Tests d'intégration

- **Push end-to-end OpenCode** : le fichier de sortie `opencode.json` est du JSON valide, parsable, et préserve les champs hors-scope (permissions globales, model par défaut, etc.)
- **Push end-to-end Claude Code** : les fichiers `agents/*.md` produits ont un frontmatter YAML syntaxiquement valide et respectent la spec Claude Code
- **Diff** : modification du pivot → `diff` montre le changement sans erreur
- **État** : un `.shenron-state.json` est produit après un push réussi, et un push suivant détecte les modifications manuelles si le fichier a changé sans passer par l'outil

### Golden files

Les configurations existantes de l'utilisateur servent de golden files pour les tests d'intégration :

- `~/.config/opencode/opencode.json` → test fixture OpenCode
- `~/.claude/agents/build.md` → test fixture Claude Code agent
- `~/.claude/commands/` → test fixtures Claude Code commands

### Structure de test (Go)

```
internal/pivot/parser_test.go
internal/adapter/opencode/adapter_test.go
internal/adapter/opencode/testdata/
    shenron.yaml
    expected_opencode.json
    expected_prompts_build.md
internal/adapter/claude/adapter_test.go
internal/adapter/claude/testdata/
    shenron.yaml
    expected_agents_build.md
    expected_commands_ship.md
internal/diff/differ_test.go
```

## Decisions log

Les questions ouvertes de conception ont été résolues avant la v1.

| ID | Décision | FR |
|---|---|---|
| D1 | `promptFile` est relatif au répertoire contenant le fichier pivot. Exemple : si le pivot est dans `~/projets/repo/shenron.yaml` et `promptFile: prompts/build.md`, le chemin résolu est `~/projets/repo/prompts/build.md`. | FR2 |
| D2 | Le pivot définit les permissions au niveau le plus fin (tool par tool, pattern bash). Les adaptateurs dégradent vers leur modèle natif avec le moins de perte possible. La granularité fine OpenCode (`glob`/`grep`/`list`/`lsp`) passe par `extensions.opencode.permission`. | FR3 |
| D3 | Conflit : refuser le push avec message d'erreur, suggérer `--force`. Pas de merge 3-way en v1. | FR6 |
| D4 | Fichier d'état `.shenron-state.json` stocké dans le même répertoire que le fichier pivot. Git-ignorable. | FR5, FR6 |
| D5 | Skills référencés en lecture seule dans le pivot (`skills: [{name}]`), non gérés. Les skills sont déjà standardisés via `agentskills.io` dans tous les CLI cibles. | Non-goals, FR1 |
| D6 | Walk-up CWD→root + fallback `$HOME/.shenron/shenron.yaml`. Les deux modes (projet et global) coexistent. | FR1 |
| D7 | Adaptateur pur + méthode `MergeFile` optionnelle dans l'interface `Adapter`. L'adaptateur OpenCode renvoie les fragments JSON pour les blocs `agent`/`command` ; le cœur fait le merge en préservant les champs hors-scope et l'indentation. | FR10 |
| D8 | Nom du binaire : `shenron`. Distribution : binaire statique macOS/Linux via GitHub Releases + script `curl\|sh`. Pas de package npm/pip/brew en v1. | Stack |
| D9 | Le binding skills-par-agent est désormais dans le scope v1 (amend FR2). D5 reste inchangé pour le contenu des skills, qui demeure read-only. Les adaptateurs Claude Code et Codex émettent `skills` comme metadata ; **l'adaptateur OpenCode omet la clé `skills`** parce qu'OpenCode v1.x la forward au provider LLM en tant qu'option top-level inconnue, ce que les providers stricts (schéma Pydantic `additionalProperties: false`, ex. GLM-5.2) rejettent en 400 `Extra inputs are not permitted`. Les agents OpenCode sont censés référencer leurs skills depuis leur prompt (`## Available skills`). L'existence locale d'une skill n'est pas bloquante. | FR2, FR7, FR10 |
| D10 | Le flux single-pivot de la v1 (`init` / `validate` / `diff` / `push` sur un `shenron.yaml` nu) a été retiré au profit du flux par package. Le contrat CLI actuel vit dans `docs/prd/scope-flatten-commands.md` : cinq commandes top-level (`install`, `list`, `update`, `diff`, `push`), `init` et `validate` supprimés, `--store` promu au niveau racine. FR1-FR4 (schéma pivot) restent valides ; FR5-FR8 sont remplacés par les FR1-FR7 du nouveau PRD. | FR5, FR6, FR7, FR8 |

## Acceptance criteria (v1)

> **Les critères 1-12 ci-dessous décrivent le flux single-pivot de la v1
> (commande `init` + `diff`/`push` sur un `shenron.yaml` nu), qui a été
> remplacé par le flux par package (D10).** Les critères d'acceptation
> du contrat actuel sont dans `docs/prd/scope-flatten-commands.md`. Les
> scénarios restent informatifs pour valider l'engine, mais les
> commandes exactes ont changé.

Une session de test valide le scénario suivant :

1. `shenron init` → génère un `shenron.yaml` à partir de la config OpenCode existante, avec 7 agents et 1+ commandes.
2. L'utilisateur édite le fichier : change le `systemPrompt` de l'agent `build`.
3. `shenron diff --target opencode` → montre que `opencode.json` et `prompts/build.md` vont être modifiés.
4. `shenron push --target opencode` → écrit les fichiers. Aucun autre champ de `opencode.json` n'est altéré.
5. `shenron diff --target opencode` → "No changes" (l'état est synchronisé).
6. L'utilisateur modifie `prompts/build.md` manuellement.
7. `shenron diff --target opencode` → détecte la modification manuelle, affiche un avertissement.
8. `shenron push --target opencode` → refuse d'écraser (sauf `--force`).
9. `shenron push --target claude-code` → écrit `~/.claude/agents/build.md` à partir du même pivot, avec le frontmatter YAML Claude Code.
10. Les permissions mappées (`edit: ask`, `bash` avec patterns) apparaissent correctement dans les deux fichiers natifs.
11. `shenron push` round-trip un champ `skills: [foo]` modifié : il est visible dans `~/.claude/agents/<id>.md` (frontmatter) et comme instruction Codex, mais **absent de `opencode.json`** (voir décision D9).
12. `shenron push --target codex` génère les agents TOML et prompts Markdown, puis `shenron diff --target codex` retourne "No changes".

## Stack

- **Langage** : Go 1.24+
- **CLI** : `github.com/spf13/cobra`
- **YAML** : `gopkg.in/yaml.v3`
- **TOML** (Codex adapter v2) : `github.com/pelletier/go-toml/v2`
- **Tests** : `testing` standard + golden files
- **Build** : `go build -o shenron ./cmd/shenron/`
- **Lint** : `golangci-lint run`

## Repo layout (rappel)

```
cmd/shenron/main.go
internal/
  pivot/schema.go, parser.go, discover.go
  adapter/adapter.go
  adapter/claude/{adapter,agent,command}.go
  adapter/opencode/{adapter,agent,command}.go
  adapter/codex/{adapter,agent,skill}.go       # v2
  cli/{root,diff,push,validate,init}.go
  diff/{differ,state}.go
  fsutil/{paths,write}.go
docs/prd/shenron.md                          # ce document
```
