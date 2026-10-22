# huandu/v3 — простые фильтры по полям

Отдельный модуль `example.com/huandu-v3`, библиотека `github.com/huandu/go-sqlbuilder v1.44.0`.
Один переиспользуемый пакет `query`, без дерева условий, Plan и отдельных
JSON/URL-адаптеров. Соседние реализации не меняются.

Вход — `query.Filters`: имя поля → список структур `Condition`.
**Между полями всегда AND, внутри одного поля всегда OR.** Выбирать логику не нужно.
Поддерживаются операторы из таблицы: `eq`, `ne`, `in`, `not_in`, `contains`,
`not_contains`, `range`, `nrange`, `empty`, `nempty`, включая обычные массивы
PostgreSQL. Существующие сравнения GTE/LTE и операторы LIKE/BETWEEN также доступны.

```go
filters := query.Filters{
    "score": {
        {Op: query.Between, Value: query.Range{From: 7.0, To: 9.0}},
        {Op: query.GTE, Value: 10.0},
    },
    "status": {
        {Op: query.EQ, Value: "active"},
        {Op: query.EQ, Value: "pending"},
    },
}
```

Это означает:

```sql
(score BETWEEN 7 AND 9 OR score >= 10)
AND (status = 'active' OR status = 'pending')
```

В настоящем SQL значения передаются параметрами. Порядок полей в SQL
алфавитный, порядок условий поля совпадает с порядком в списке.

## Использование в своём репозитории

```go
import (
    "example.com/huandu-v3/query"
    sqlbuilder "github.com/huandu/go-sqlbuilder"
)

// Серверная конфигурация. Имена и таблица здесь иллюстративные.
fields := query.Registry{
    "score": {
        Column: "a.score",
        Type: query.Number,
        Operators: []query.Operator{query.Between, query.GTE, query.LTE},
    },
    "status": {
        Column: "a.status",
        Type: query.String,
        Operators: []query.Operator{query.EQ, query.In},
    },
}

base := sqlbuilder.PostgreSQL.NewSelectBuilder()
base.Select("a.id", "a.score", "a.status").From("assets AS a")
base.Where(base.Equal("a.tenant_id", tenantID))

sb, err := query.WithFilters(base, fields, filters)
if err != nil {
    return err
}
sb.OrderByDesc("a.score").OrderByAsc("a.id")
sql, args := sb.Limit(20).Offset(40).Build()
// rows, err := db.QueryContext(ctx, sql, args...)
```

Для MariaDB создавайте `sqlbuilder.MySQL.NewSelectBuilder()`.
Диалект берётся из базового builder, отдельной настройки в обёртке нет.
После применения фильтров не переключайте flavor: имена колонок уже процитированы.

`WithFilters` клонирует базовый запрос через встроенный `Clone()` и возвращает
`*sqlbuilder.SelectBuilder`. JOIN, WHERE, GROUP BY, HAVING, ORDER BY и пагинация
сохраняются; новые поля фильтра добавляются через AND к обязательным условиям.
При повторных вызовах фильтры в `base` не накапливаются. При ошибке возвращается
`nil, error`, исходный builder также сохраняется. Не меняйте базовый запрос,
registry или входные данные одновременно с их использованием.

Для подключения к другому модулю можно перенести папку `query` в свой проект
или использовать локальный `replace` для `example.com/huandu-v3`.
Зависимости от соседнего `v2` нет.

## Типы значений

`Field.Type` обязателен для каждого поля, которое используется в фильтрах.
Пустой или неизвестный тип возвращает ошибку, даже для сравнения с NULL.
Обёртка сама подготавливает значения по типу поля и оператору перед сборкой SQL.
Внешний слой преобразований для поддерживаемых представлений не требуется.
Для `contains` отдельно строится экранированный LIKE-шаблон; строковые сравнения
без учёта регистра выполняются средствами БД.

| Type | Допустимый вход → тип SQL-параметра |
| --- | --- |
| `query.String` | `string` |
| `query.Enum` | `string` — метка enum, не числовой код |
| `query.UUID` | `string` — идентификатор |
| `query.Integer` | Готовые Go-числа без преобразования; десятичная строка или `json.Number` → `int64` |
| `query.Counter` | То же, что Integer |
| `query.Number` | Готовые Go-числа без преобразования; строка или `json.Number` → конечный `float64` |
| `query.Boolean` | `bool` или строка по правилам `strconv.ParseBool` → `bool` |
| `query.Date` | `time.Time` или строка `YYYY-MM-DD` → `time.Time` |
| `query.DateTime` | `time.Time` или строка RFC3339 с необязательными наносекундами → `time.Time` |
| `query.Bytes` | Ненулевой `[]byte` (пустой допустим) |

Подготавливается каждый скаляр, каждый элемент `IN/NOT IN` и каждая указанная
граница `Between/RangeOp/NotRange`. Для `Contains` нужна одна строка поиска,
даже если колонка имеет тип StringArray. Только обычный `nil` означает SQL NULL,
он разрешён только для EQ/NE. Указатели (включая nil-указатели), `sql.Null*`,
другие `driver.Valuer` и nil-срез байтов не принимаются.
String/Enum/UUID принимают именно строки: числа, объекты и списки автоматически
в текст не превращаются. Пользовательские именованные типы нужно явно привести
к поддерживаемым Go-типам. Пробелы вокруг строк не удаляются.

`prepareCondition` проверяет форму значения оператора и вызывает `prepareValue`
для отдельных значений. `compile` затем выбирает метод go-sqlbuilder.
Исходные Filters, списки и Range при подготовке не изменяются, включая ошибки.

```go
fields := query.Registry{
    "age": {Column: "age", Type: query.Integer, Operators: []query.Operator{query.EQ}},
}

// Успех: строка разбирается в int64(25) внутри обёртки.
sb, err := query.WithFilters(base, fields, query.Filters{
    "age": {{Op: query.EQ, Value: "25"}},
})

// Ошибка: строка для Integer должна содержать десятичное целое.
_, err = query.WithFilters(base, fields, query.Filters{
    "age": {{Op: query.EQ, Value: "25.5"}},
})
```

Integer, Counter и Number одинаково принимают готовые `int`, `int8/16/32/64`,
`uint`, `uint8/16/32/64`, конечные `float32/64` и передают их в args без изменения
типа и значения. Например, `int16(25)` остаётся int16, а `float32(25.5)` — float32,
в том числе для поля Integer. NaN и бесконечность возвращают ошибку.
Целочисленность float и диапазон конкретной колонки здесь не проверяются;
совместимость параметра с драйвером и колонкой определяется при выполнении SQL.

Строки и json.Number по-прежнему разбираются по зарегистрированному типу:
для Integer/Counter — `strconv.ParseInt` в int64 (десятичное целое без дробной
части и экспоненты), для Number — `strconv.ParseFloat` в float64 с обычным
округлением. Ошибки разбора, переполнение и нечисловые значения запрещены.

Обычные JSON-числа, декодированные как float64, передаются дальше как float64.
Чтобы сохранить точность больших целых при чтении JSON, используйте
`decoder.UseNumber()`: для Integer/Counter полученный json.Number разбирается
непосредственно в int64, без промежуточного float64.

Форма значений не угадывается: для IN нужен непустой `[]any`, для диапазонов —
`query.Range`, для скалярных операторов — одно значение. Строка не превращается
в список и не разбивается по запятым. Диапазон из JSON-объекта при необходимости
сопоставьте с query.Range во входном коде; его границы обёртка подготовит сама.

Готовый time.Time передаётся как есть. Строка Date разбирается как дата в UTC,
строка DateTime сохраняет заданное смещение часового пояса. Приведение готового
time.Time к UTC или полуночи для Date при необходимости сделайте сами.

Дополнительно проверяется совместимость оператора с типом поля:
`Like/ILike` доступны только для String; сравнения GT/GTE/LT/LTE — для
String, Integer, Number, Counter, Date, DateTime; диапазоны — для Integer, Number,
Counter, Date, DateTime. EQ/NE и операции принадлежности списку доступны всем
скалярным типам. Для массивов набор операций описан ниже.
Оператор также должен быть явно разрешён в `Field.Operators`.

Registry задаёт сервер, без чтения схемы БД. Его Type должен соответствовать
реальной колонке. Ограничения конкретного SQL-типа — например, диапазон SMALLINT,
точность DECIMAL, длина VARCHAR или формат UUID — проверяйте отдельно.
Проверку допустимых enum-меток, UTF-8 и trim строк также оставьте входному слою.

## Обычные массивы PostgreSQL

В registry указывается тип **колонки-массива**:

| Type | Колонка БД | Значение для In/NotIn |
| --- | --- | --- |
| `query.StringArray` | `text[]`, `varchar[]` | Непустой `[]any` со строками |
| `query.EnumArray` | Нативный enum[] или текстовый массив меток | Непустой `[]any` со строками |
| `query.UUIDArray` | `uuid[]` | Непустой `[]any` со строками UUID |
| `query.IntegerArray` | `smallint[]`, `integer[]`, `bigint[]` | Непустой `[]any`, каждый элемент подготавливается как Integer |

```go
fields := query.Registry{
    "ip": {
        Column: "a.ips", Type: query.StringArray,
        Operators: []query.Operator{query.Contains, query.NotContains, query.In, query.NotIn},
    },
    "ports": {
        Column: "a.ports", Type: query.IntegerArray,
        Operators: []query.Operator{query.In, query.NotIn},
    },
}
filters := query.Filters{
    "ip":    {{Op: query.Contains, Value: "192.168."}},
    "ports": {{Op: query.In, Value: []any{int64(22), int64(443)}}},
}
sb, err := query.WithFilters(base, fields, filters)
```

Это означает: хотя бы один IP содержит `192.168.`, **AND** хотя бы один порт
равен 22 или 443. `In` для массивов — пересечение, а не равенство массивов.
Строковые и enum-элементы сравниваются без учёта регистра. `Contains` доступен
только для StringArray и ищет подстроку в каждом элементе.

Компилятор строит `EXISTS (SELECT 1 FROM unnest(column) ... WHERE ...)` через
sqlbuilder; отрицательные операции используют `NOT EXISTS`. Значения остаются
отдельными скалярными параметрами, поэтому `pq.Array` и другой драйверный
сериализатор массива здесь не нужны. `&&` и оптимизация под GIN в этой
реализации не используются.

NULL-элементы не дают совпадения. Пустой и NULL-массив считаются не имеющими
совпадений: In/Contains дают false, NotIn/NotContains — true.
Пустой входной список и NULL во входном списке запрещены.
В MariaDB эти типы возвращают ошибку: SQL для нативных массивов PostgreSQL
не подменяется операциями над JSON.

## Пагинация, разные сортировки, группировка и JOIN

Эти операции уже есть в go-sqlbuilder: обёртка их не дублирует.
Их можно вызывать до или после `WithFilters`.

```go
// LIMIT/OFFSET заменяют соответствующие настройки builder.
// LIMIT 0 — ноль строк; отсутствие вызова Limit сохраняет исходный лимит.
sb.Limit(20).Offset(40)

// У каждой колонки своё направление. Не смешивайте эти методы с глобальными
// устаревшими Asc()/Desc(), которые задают направление всему ORDER BY.
sb.OrderByDesc("a.score").OrderByAsc("a.id")

// Агрегатный запрос: заменяем SELECT и добавляем GROUP BY/HAVING.
sb.Select("a.status", "COUNT(*) AS total").GroupBy("a.status")
sb.Having(sb.GreaterThan("COUNT(*)", 2))

// JOIN обычно задаётся в base; registry может ссылаться на алиас o.name.
base.JoinWithOption(sqlbuilder.LeftJoin, "owners AS o",
    base.Equal("a.owner_id", sqlbuilder.Raw("o.id")),
)
```

Примеры выше показывают отдельные возможности: набор SELECT, GROUP BY
и ORDER BY должен быть согласован в конкретном запросе.
`GroupBy`, `Having` и `OrderByAsc/Desc` **добавляют** элементы; обёртка
не сбрасывает существующие настройки. Для другого порядка сортировки собирайте
базовый запрос без ORDER BY. Для сброса старого offset используйте `Offset(0)`.
Для переносимой пагинации MySQL/PostgreSQL задавайте OFFSET вместе с LIMIT.
Проверку допустимого лимита и неотрицательных значений делайте в своём DTO.

Методы sqlbuilder принимают SQL-выражения напрямую. Таблицы, JOIN, SELECT,
сортировки и группировки задавайте в репозитории; пользовательские имена для
сортировки/группировки сопоставляйте с разрешёнными серверными колонками.

## Операторы

| Константа | SQL | Value |
| --- | --- | --- |
| `EQ`, `NE` | `=`, `<>`; String/Enum без учёта регистра | Скаляр; `nil` даёт `IS NULL` / `IS NOT NULL` |
| `GT`, `GTE`, `LT`, `LTE` | `>`, `>=`, `<`, `<=` | Ненулевой скаляр |
| `In`, `NotIn` | `IN`, `NOT IN`; для массивов наличие/отсутствие пересечения | Непустой `[]any` без NULL |
| `Contains`, `NotContains` | Поиск/отсутствие буквальной подстроки без учёта регистра | `string`, поле String или StringArray |
| `Like`, `ILike` | `LIKE`, регистронезависимый LIKE | Строка с SQL-шаблоном |
| `Between` | `BETWEEN ... AND ...` | `Range{From: ..., To: ...}` |
| `RangeOp` | `BETWEEN`, `>=` или `<=` | `Range` с одной или двумя границами |
| `NotRange` | `NOT BETWEEN`, `<` или `>` | `Range` с одной или двумя границами |
| `Empty`, `NotEmpty` | `IS NULL`, `IS NOT NULL` | Без Value; поле Date/DateTime |

`BETWEEN` включает обе границы. Нулевое значение границы допустимо:
`Range{From: 0.0, To: 10.0}` для Number. Пустой диапазон запрещён.
**Для интервала используйте `Between` или `RangeOp`: отдельные GTE/LTE
одного поля соединятся через OR**, как и любые другие условия этого поля.
Несколько диапазонов одного поля тоже объединяются OR.

Для NotRange с двумя границами подходит значение строго вне интервала:
`x < from OR x > to`. Только From означает `x < from`, только To — `x > to`.
Пустой Range запрещён; порядок границ проверяет вызывающий код.

String/Enum для EQ/NE и IN/NOT IN сравниваются через `LOWER` с обеих сторон.
Нативный PostgreSQL enum сначала приводится к тексту в SQL. Числа, bool,
UUID и даты сравниваются обычными SQL-операторами. Для даты EQ — точное сравнение.
Регистр зависит от правил locale/collation БД.

Empty/NotEmpty принимают Condition без Value, например `{Op: query.Empty}`.
Они проверяют именно SQL NULL: нулевая дата или другой специальный маркер
«не задано» не интерпретируется. Отрицательные скалярные сравнения с NULL-колонкой
дают SQL UNKNOWN и не включают такую строку в WHERE.

В PostgreSQL `ILike` использует `ILIKE`, в MySQL/MariaDB — встроенный
в библиотеку вариант `LOWER(column) LIKE LOWER(?)`.
В Like/ILike `%` и `_` остаются символами SQL-шаблона; для поиска подстроки
передайте, например, `%team%`. В Contains/NotContains передавайте просто `team`:
компилятор сам добавляет `%` по краям и экранирует `%`, `_`, `!` через `ESCAPE '!'`.

Изменение относительно первоначальной v3: Contains/NotContains больше не алиасы
IN/NOT IN. Вместо `{Op: query.Contains, Value: []any{"a", "b"}}` используйте
`{Op: query.In, Value: []any{"a", "b"}}`.
EQ/NE для строк теперь регистронезависимы, как в таблице требований.

## Проверки и отличия от v2

- Неизвестные поля и неразрешённые операторы возвращают ошибку.
  Пустой список `Operators` не разрешает никакие операции.
- Колонки из registry проверяются как обычные имена (`score`, `a.score`)
  и цитируются по сегментам. SQL-выражения в registry не принимаются.
- Значения проверяются по обязательному `Field.Type`; готовые Go-числа
  сохраняют тип и значение. Числовые строки и json.Number разбираются по типу
  поля, строки Boolean — в bool, Date/DateTime — в time.Time.
  String/Enum/UUID принимают строки, Bytes — ненулевой []byte;
  для IN и диапазонов подготавливается каждый элемент или граница.
  SQL builder, `sqlbuilder.Raw`, произвольные структуры и карты в Value запрещены.
- `nil` означает NULL, а не пропущенное значение. NULL допустим только для EQ/NE.
  Для списков нужен именно `[]any`, а не `[]string` или `[]int`.
- Скалярный оператор принимает одно значение. Для нескольких равенств используйте
  несколько `Condition` или одно `In`, вместо `EQ` со списком из v2.
- Пустой `Filters` сохраняет исходный запрос; поле с пустым списком — ошибка.
- Здесь нет чтения схемы БД, JSON/URL-адаптеров, search, лимитов размера
  входа, проверки бизнес-ограничений или порядка границ диапазона.
  Поддерживаемые числовые, логические и строковые представления дат разбираются
  внутри обёртки; дополнительные ограничения и доступные операции задаёт сервер.
- Скаляры поддерживают PostgreSQL и MySQL/MariaDB; массивы — только PostgreSQL.
  Операции над JSONB и пользовательские SQL-операторы в обёртку не входят.

Вся реализация — пять файлов: `types.go` (модель), `query.go`
(копирование и группировка по полям), `conditions.go` (операторы и вызовы методов
sqlbuilder), `arrays.go` (фильтры нативных массивов), `values.go` (подготовка значений).
Нет промежуточного дерева и собственного сборщика SQL.

## Запуск

Из папки `huandu/v3`:

```sh
go test ./...
go run ./cmd/demo
go run ./cmd/array-demo
```

Это отдельный модуль, поэтому `go test ./...` из корня проекта его не обходит.
Демо печатает запросы со связанной таблицей, фильтрами, двумя направлениями
сортировки и пагинацией, а также отдельный агрегатный запрос, для обоих диалектов.
База данных для запуска не нужна.

`array-demo` показывает PostgreSQL-запрос с массивами, enum и счётчиком.
Опциональные интеграционные тесты выполняют параметризованные запросы на
настоящей PostgreSQL через `psql`, используя только временные объекты сессии:

```sh
V3_TEST_POSTGRES_DSN='postgresql://localhost/filter_test' go test ./query -run TestPostgreSQLFilterSemantics -v
```

Без этой переменной интеграционный тест пропускается; обычные тесты сборки SQL
выполняются всегда. Проверяются все операторы, границы интервалов, регистр,
литеральные символы LIKE, enum/uuid/int/string-массивы, NULL, пустые массивы
и сохранение обязательного tenant-условия.

Для фильтров из первого примера и базового условия `tenant_id = 42`:

PostgreSQL:

```sql
SELECT a.id FROM assets AS a
WHERE a.tenant_id = $1
  AND ("a"."score" BETWEEN $2 AND $3 OR "a"."score" >= $4)
  AND (LOWER("a"."status") = LOWER($5) OR LOWER("a"."status") = LOWER($6))
ORDER BY a.score DESC, a.id ASC LIMIT $7 OFFSET $8
```

MariaDB:

```sql
SELECT a.id FROM assets AS a
WHERE a.tenant_id = ?
  AND (`a`.`score` BETWEEN ? AND ? OR `a`.`score` >= ?)
  AND (LOWER(`a`.`status`) = LOWER(?) OR LOWER(`a`.`status`) = LOWER(?))
ORDER BY a.score DESC, a.id ASC LIMIT ? OFFSET ?
```

Параметры в обоих случаях: `[42, 7, 9, 10, "active", "pending", 20, 40]`.

Семантика PostgreSQL: [unnest и массивы](https://www.postgresql.org/docs/current/functions-array.html),
[EXISTS](https://www.postgresql.org/docs/current/functions-subquery.html),
[LIKE/ILIKE и ESCAPE](https://www.postgresql.org/docs/current/functions-matching.html).
