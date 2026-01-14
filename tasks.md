# hledger-lsp: План разработки

## Статус проекта

**Фаза 1: Основа** — ✅ Завершена
**Фаза 2: Основные функции LSP** — ✅ Завершена

| Компонент | Статус | Покрытие |
|-----------|--------|----------|
| Структура проекта | ✅ | - |
| Lexer | ✅ | 89.3% |
| Parser | ✅ | есть тесты |
| AST типы | ✅ | - |
| Базовый LSP сервер | ✅ | - |
| Синхронизация документов | ✅ | - |
| Базовая диагностика | ✅ | - |

---

## Фаза 2: Основные функции LSP

- [x] **2.1 Analyzer модуль** — валидация баланса, семантический анализ
  - ✅ Создан `internal/analyzer/` (analyzer.go, types.go, indexer.go, balance.go)
  - ✅ Проверка баланса транзакций (включая multi-commodity и cost)
  - ✅ Сбор информации о счетах, плательщиках, валютах
  - ✅ Покрытие тестами: 87.6%

- [x] **2.2 Completion Provider** — автодополнение
  - ✅ Счета (из директив и использования)
  - ✅ Плательщики (payee из транзакций)
  - ✅ Валюты/commodities
  - Теги (ожидает 3.4 — теги из комментариев)
  - Даты (отложено)

- [x] **2.3 Diagnostics Provider** — расширенная диагностика
  - ✅ Несбалансированные транзакции (UNBALANCED, MULTIPLE_INFERRED)
  - ✅ Неизвестные счета (UNDECLARED_ACCOUNT, если есть директивы account)
  - Дублирующиеся транзакции (отложено)

- [x] **2.4 Formatting Provider** — форматирование документов
  - ✅ Выравнивание сумм
  - ✅ Нормализация отступов
  - ✅ Поддержка статусов и virtual postings

- [x] **2.5 Semantic Tokens** — подсветка синтаксиса
  - ✅ Даты, счета, суммы, комментарии
  - ✅ Директивы, статусы, операторы

---

## Фаза 3: Расширенные функции

- [x] **3.1 Hover Provider** — информация при наведении
  - ✅ Баланс счёта (по валютам)
  - ✅ Информация о транзакции (дата, плательщик, постинги)
  - ✅ Информация о сумме (количество, валюта, cost)
  - ✅ Информация о плательщике (количество транзакций)

- [x] **3.2 Document Symbols** — структура документа
  - ✅ Транзакции (дата + описание)
  - ✅ Директивы (account, commodity)
  - ✅ Include файлы

- [x] **3.3 Include file resolution** — обработка include
  - ✅ Разрешение путей (относительные, абсолютные, ~)
  - ✅ Обнаружение циклов (set-based)
  - ✅ Агрегация данных из всех файлов
  - ✅ Интеграция с Completion/Hover

- [x] **3.4 Parser extensions** — расширение парсера
  - ✅ Virtual postings (скобки `[account]` и `(account)`)
  - ✅ Теги из комментариев (`tag:value`)
  - ✅ Date2 (вторичная дата `2024-01-15=2024-01-20`)
  - ✅ Price directive (`P 2024-01-15 EUR $1.08`)

- [x] **3.5 CLI Integration** — интеграция с hledger
  - ✅ CLI клиент (`internal/cli/client.go`) — обёртка для вызова hledger
  - ✅ Code Actions для запуска команд (bal, reg, is, bs, cf)
  - ✅ Результат вставляется как комментарии на позицию курсора
  - ✅ Graceful degradation — работает без hledger

---

## Фаза 4: Тестирование

- [x] **4.1 Server tests** — тесты LSP хендлеров
  - ✅ Initialize/Shutdown/Exit
  - ✅ TextDocumentSync (DidOpen, DidChange, DidClose, DidSave)
  - ✅ Completion requests (были ранее)
  - ✅ Diagnostics publishing (с mock client)
  - ✅ Helper functions (applyChange, splitLines, isFullChange)
  - ✅ Покрытие: 83.1%

- [x] **4.2 Benchmark tests** — производительность
  - ✅ Парсинг больших файлов (Lexer, Parser: small/medium/large)
  - ✅ Инкрементальные обновления (applyChange)
  - ✅ Время отклика completion (Account/Payee/Commodity)
  - ✅ Analyzer benchmarks (Analyze, CheckBalance, Collect*)

- [x] **4.3 Integration tests** — интеграционные тесты
  - ✅ 17 тестов покрывающих полные user flows
  - ✅ Open → Edit → Diagnostics workflow
  - ✅ Completion в разных контекстах
  - ✅ Hover с обновлением баланса
  - ✅ Include файлы (вложенные, циклы, относительные пути)
  - ✅ Error recovery (completion работает при ошибках парсинга)
  - ✅ Channel-based синхронизация (без time.Sleep)

---

## Фаза 5: Финализация

- [x] **5.1 Code review** — ревью кода
  - ✅ Автоматический анализ (golangci-lint, race detector)
  - ✅ Исправлены critical issues: path traversal, cache race condition, UTF-8 column tracking
  - ✅ Исправлены high issues: type assertions, bounds checking, empty function removal
  - ✅ Добавлены: file size limits, include depth limits, mutex protection
- [x] **5.2 CI/CD pipeline** — автоматизация сборки и релизов
  - ✅ CI workflow (lint, test, build)
  - ✅ Release workflow (goreleaser, cross-platform)
  - ✅ Version info в binary (--version flag)
  - ✅ Coverage reporting (codecov)
- [x] **5.3 Документация для редакторов** — VS Code, Neovim, Emacs
  - ✅ LICENSE (MIT)
  - ✅ README.md (badges, features, installation, editor links)
  - ✅ docs/vscode.md — setup guide
  - ✅ docs/neovim.md — nvim-lspconfig, lazy.nvim
  - ✅ docs/emacs.md — eglot, lsp-mode, use-package

---

## Порядок выполнения

```
Analyzer (2.1)
     │
     ▼
┌────┴────┬────────┬────────┬────────┐
│         │        │        │        │
▼         ▼        ▼        ▼        ▼
2.2      2.3      2.4      2.5      3.x
Completion Diagnostics Formatting Semantic
     │         │        │        │
     └────┬────┴────────┴────────┘
          │
          ▼
    Тестирование (4.x)
          │
          ▼
    Финализация (5.x)
```

**Критический путь**: 2.1 → (2.2-2.5 параллельно) → 4.x → 5.x
