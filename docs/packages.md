# Packages

Packages are installable directories that contribute resources to go-pi.

They are intentionally simple: no daemon, no custom host process model, no heavyweight plugin API.

## Supported resource types

A package can contain any combination of:

```text
my-package/
├── extensions/
├── skills/
├── prompts/
├── themes/
└── models/
```

## Commands

```bash
pi package install <path-or-git-url>
pi package install --project <path-or-git-url>
pi package list
pi package update <name>
pi package remove <name>
```

## Scopes

- **global:** `~/.pi-go/packages/<name>/`
- **project:** `.pi-go/packages/<name>/`

Project packages override global packages when resource names collide.

## Why packages exist

Packages give go-pi a shareable customization unit that still fits the minimal-core philosophy.

Use packages when you want to distribute:

- a reusable prompt set
- a themed environment
- extension manifests
- skill libraries
- compatible provider/model aliases

## Relationship to extensions

Packages are distribution containers.
Extensions are one possible resource type inside a package.

A package might contain:

- only `models/`
- only `themes/`
- an extension plus prompts and skills
- any other lightweight combination of supported resources
