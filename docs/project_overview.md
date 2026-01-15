# Обзор проекта hledger-lsp

## Назначение и контекст

hledger-lsp — LSP-сервер для journal-файлов hledger на Go. Он дает редакторам функции IDE (completion, diagnostics, formatting и т.д.) и устраняет зависимость от VS Code‑расширения, делая hledger удобным в любых LSP‑совместимых редакторах.

Целевая аудитория:

- пользователи hledger в Neovim/Emacs/Helix и др.;
- команды, которым нужны единые правила проверки и форматирования.

## Реализованные функции (по README/tasks)

- Completion: счета, плательщики, валюты.
- Diagnostics: баланс, синтаксис, базовые проверки.
- Formatting: выравнивание сумм и отступы.
- Hover: балансы и детали транзакций.
- Semantic tokens: подсветка синтаксиса.
- Document symbols: транзакции, директивы, include.
- Include: разрешение путей, детекция циклов.
- CLI code actions: запуск отчетов hledger.

## Архитектура и модули

Ключевые папки:

- `cmd/` — entrypoint сервера.
- `internal/parser` — лексер/парсер и AST.
- `internal/analyzer` — семантика, баланс, индексация.
- `internal/server` — LSP handlers.
- `internal/formatter` — форматирование.
- `internal/include` — include resolution.
- `internal/cli` — обертка для hledger CLI.
- `internal/workspace` — агрегация данных по файлам.

Цепочка обработки: парсинг → анализ → диагностика/подсветка/hover/completion.

## Модель индекса символов и источники данных

Сейчас индексируются:

- accounts: `AccountIndex` с `All` и `ByPrefix` для автодополнения по префиксу;
- payees: уникальные значения из транзакций;
- commodities: директивы и суммы/стоимости в postings;
- tags: теги из AST (теги из комментариев пока не извлекаются).

Источники данных:

- AST из `internal/parser` (директивы account/commodity, транзакции и postings);
- include-дерево через `internal/include.Loader` и `ResolvedJournal` (Primary + Files).

Разделение анализов:

- `Analyze` обрабатывает один `Journal`;
- `AnalyzeResolved` агрегирует символы по include-дереву, объединяя данные всех файлов.

Кэши workspace:

- declared accounts/commodities и форматы чисел из commodity‑директив;
- используются для быстрых проверок и форматирования без полного пересчета.

Ограничения:

- нет индекса транзакций и связей для references/duplicates (задел для задач 2–4).

## Пробелы и незавершенные функции

- Go to Definition — нет.
- Find References — нет.
- Completion для тегов — отложено.
- Completion для дат — отложено.
- Диагностика дублей транзакций — отложено.
- FR из PRD: template completion, приоритизация по частоте, лимит результатов, workspace-wide completions — не подтверждены как реализованные.

## Риски и текущие mitigations (по PRD)

Риски:

- производительность на больших файлах;
- сложность формата hledger;
- совместимость с CLI.

Mitigations, уже отмеченные в проекте:

- бенчмарки и инкрементальные обновления;
- hand-written parser и тесты;
- include cycle detection;
- lint/CI и защитные проверки.

## Предложения по доработке

- Go to Definition/Find References: индекс символов в `internal/analyzer`/`internal/workspace` + новые LSP handlers в `internal/server`.
- Completion tags/dates/templates: расширить completion provider и контекстные правила.
- Дубликаты транзакций: детекция по хэшу транзакций в анализаторе.
- Workspace-wide completion: общий индекс по include‑дереву.
- Производительность: расширить бенчмарки, ввести конфиг‑лимиты (max results, file size, include depth).
