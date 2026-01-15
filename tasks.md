# hledger-lsp: План доработок

## Цель

Закрыть отложенные функции и расширить LSP‑возможности: go to definition, find references, completion (теги/даты/шаблоны), диагностика дублей, workspace‑индексация и ограничения производительности.

## План работ (чек‑лист)

### 1) Анализ требований и дизайн

- [x] Зафиксировать критерии готовности для каждой фичи
- [x] Описать модель индекса символов и источники данных
- [x] Согласовать минимальный набор конфигураций (лимиты/флаги)

#### Критерии готовности (разделы 2–10)

**2) Индексация и workspace**

- Workspace‑индекс (accounts/payees/commodities/tags) строится по include‑дереву, обновляется инкрементально на didChange/didSave, есть unit‑тесты в `internal/workspace` или `internal/analyzer`, подтверждающие актуальность индекса.
- Индекс транзакций добавлен и используется для references/duplicates, формат ключей документирован, есть unit‑тесты на выборку.
- Include‑дерево интегрировано в индекс, есть тесты на мультифайловые сценарии и циклы include.
- Cache invalidation обновляет только затронутые файлы/узлы, есть тесты на корректность и бенчмарк на инкрементальные обновления (< 50ms, NFR‑1.3).

**3) Go to Definition**

- Handler `textDocument/definition` зарегистрирован в capabilities.
- Определения работают для accounts/commodities/payees, возвращают корректный `Location`/`Range`, неизвестные символы дают пустой ответ без ошибок.
- Тесты: один файл + include‑сценарии, проверка точных диапазонов.

**4) Find References**

- Handler `textDocument/references` зарегистрирован в capabilities.
- Поиск ссылок по account/commodity/payee в пределах workspace, результаты уникальны и стабильно отсортированы (uri+range).
- Тесты: один файл + include‑сценарии, проверка сортировки/уникальности.

**5) Completion: теги, даты, шаблоны**

- Tag name completion строится по тегам из комментариев/AST, tag value completion учитывает контекст (ключ тега).
- Date completion включает today/relative/history, контекстом определяется допустимый формат.
- Template completion использует исторические транзакции и корректно формирует шаблон с postings.
- Unit‑тесты на каждый тип completion и контекст.

**6) Completion: качество выдачи**

- Приоритизация по частоте/релевантности реализована и подтверждена тестами на ранжирование.
- Лимит `hledger.completion.maxResults` применяется ко всем completion‑источникам, есть тесты на лимит и стабильность выдачи.
- Workspace‑wide completion использует индекс include‑дерева, есть тесты на кросс‑файловые данные.
- Бенчмарк подтверждает latency completion < 100ms (NFR‑1.1).

**7) Diagnostics: дубликаты транзакций**

- Нормализованный хэш транзакции определен (дата/плательщик/postings/amount/commodity/теги), порядок и пробелы не влияют.
- Диагностика сообщает понятное сообщение, корректный range указывает на строку транзакции.
- Тесты на истинные и ложные срабатывания.

**8) Производительность и лимиты**

- Конфиг‑лимиты (file size, include depth, max results) описаны в конфигурации и документации, применяются в коде.
- Бенчмарки для completion и index‑операций добавлены; цели: parsing < 500ms на 10k строк, incremental < 50ms, memory < 200MB (NFR‑1.2/1.3/1.4).
- Профилирование подтверждено отчётом/комментарием в PRD или docs, оптимизации задокументированы.

**9) Тесты и тестовые данные**

- Unit‑тесты для tag/date/template completion в `internal/providers`.
- Интеграционные тесты definition/references в `internal/integration`.
- Добавлены testdata‑файлы: include‑дерево, дубликаты, large‑файлы.

**10) Документация**

- `README.md` обновлен: таблица фичей и статус.
- `docs/` содержит описание новых возможностей и ключей конфигурации.

### 2) Индексация и workspace

- [x] Реализовать workspace‑индекс: accounts, payees, commodities, tags
- [x] Добавить индекс транзакций (для references/duplicates)
- [x] Интегрировать include‑дерево в workspace‑индексацию
- [x] Обновить cache invalidation при изменениях файлов

### 3) Go to Definition

- [x] Добавить LSP handler `textDocument/definition`
- [x] Разрешение определения для accounts/commodities/payees
- [x] Тесты на определения в одном файле
- [x] Тесты на определения через include

### 4) Find References

- [x] Добавить LSP handler `textDocument/references`
- [x] Поиск ссылок на account/commodity/payee
- [x] Тесты на references в одном файле
- [x] Тесты на references через include

### 5) Completion: теги, даты, шаблоны

- [x] Tag name completion из тегов в комментариях/AST
- [x] Tag value completion по контексту
- [x] Date completion (today/relative/история)
- [x] Template completion из исторических транзакций

### 6) Completion: качество выдачи

- [x] Приоритизация по частоте/релевантности
- [x] Лимит maxResults через конфиг
- [x] Workspace‑wide completion на основе индекса

### 7) Diagnostics: дубликаты транзакций

- [ ] Детектор дублей по нормализованному хэшу транзакций
- [ ] Диагностика с понятным сообщением и range
- [ ] Тесты на ложные/истинные срабатывания

### 8) Производительность и лимиты

- [ ] Конфигурируемые лимиты: file size, include depth, max results
- [ ] Бенчмарки для новых completion и index‑операций
- [ ] Профилирование горячих путей и оптимизация

### 9) Тесты и тестовые данные

- [ ] Unit‑тесты для tag/date/template completion
- [ ] Интеграционные тесты definition/references
- [ ] Добавить testdata для новых сценариев

### 10) Документация

- [ ] Обновить `README.md` (таблица фичей)
- [ ] Обновить `docs/` с описанием новых возможностей
