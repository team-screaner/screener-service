# Screener Service

Go API для матриц компетенций, подтверждающих фактов, самооценки и планов развития. PostgreSQL хранит связи и неизменяемые снимки; контракт API — [`api/openapi.yaml`](api/openapi.yaml). Фронтенда в этом репозитории нет.

## Быстрый локальный запуск

Нужен Docker с Compose. Все опубликованные порты привязаны к `127.0.0.1`.

```sh
cp .env.example .env
docker compose up --build -d
curl --fail http://127.0.0.1:8080/readyz
```

Compose запускает PostgreSQL 17 на порту `15432`, применяет миграции, загружает каталог и поднимает API на `8080`. Каталог: 236 навыков, 24 типа фактов и восемь шаблонов профессий. Повторная загрузка не дублирует системные данные. Шаблоны требуют адаптации под организацию; подробности — [`docs/catalog.md`](docs/catalog.md).

Если `8080` уже занят, задайте `HTTP_PORT=18081 docker compose up --build -d`; контейнер слушает прежний внутренний порт.

Остановка с сохранением данных: `docker compose down`. Том PostgreSQL удаляется только при явном `docker compose down -v`.

Для запуска Go без контейнера приложения:

```sh
docker compose up -d postgres
set -a
. ./.env
set +a
make migrate
make seed
make run
```

Нужен Go версии из `go.mod`. Сервис проверяет доступность БД и версию схемы при запуске. Миграции и загрузка каталога выполняются отдельным шагом, а не автоматически при старте приложения.

## Первые запросы

Примеры ниже используют `jq`. Пароль примера подходит только для локальной проверки; замените его для своего пользователя.

```sh
BASE=http://127.0.0.1:8080
SESSION=$(curl --fail --silent "$BASE/api/v1/auth/register" \
  -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: local-register-001' \
  -d '{"email":"demo@example.com","password":"local-demo-password-123","name":"Demo"}' \
  | jq -r '.token')

curl --fail --silent "$BASE/api/v1/matrices?limit=10" \
  -H "Authorization: Bearer $SESSION" | jq

AGENT_TOKEN=$(curl --fail --silent "$BASE/api/v1/tokens" \
  -H "Authorization: Bearer $SESSION" \
  -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: local-agent-001' \
  -d '{"name":"Local agent","scopes":["matrix:read","evidence:read","evidence:write","assessment:read"],"expires_in_days":30}' \
  | jq -r '.token')

curl --fail --silent "$BASE/api/v1/evidence/batch" \
  -H "Authorization: Bearer $AGENT_TOKEN" \
  -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: local-evidence-001' \
  -d '{"items":[{"external_id":"demo-pr-1","title":"Исправлена гонка данных","description":"Воспроизвел конкурентный сбой, добавил регрессионный тест и проверил исправление с race detector.","fact_type":"feature-delivery","source":"api","source_url":"","matches":[]}]}' | jq

curl --fail --silent "$BASE/api/v1/evidence?limit=20" \
  -H "Authorization: Bearer $SESSION" | jq
```

Агент присылает предлагаемые факты. Пользователь проверяет и принимает их; агент не выставляет самооценку от имени пользователя. Личные данные не становятся доступны организации из-за членства. Положительная оценка требует принятого собственного факта, связанного с этим требованием; N/A требует причины и score 0. Сопоставления фактов с навыками, оценка, readiness и план развития используют отдельные операции из OpenAPI. Для повторной отправки одной операции используйте тот же `Idempotency-Key` и тот же запрос; для нового действия нужен новый ключ.

## Проверки и разработка

```sh
make generate          # типы и HTTP-контракт из OpenAPI
make test              # unit: race detector + случайный порядок
make test-integration  # настоящая PostgreSQL через Testcontainers; нужен Docker
make vet
make lint
make build
make smoke             # /healthz и /readyz уже запущенного сервиса
```

`make lint` дополнительно требует установленный `golangci-lint`. CI проверяет генерацию, тесты, `go vet`, стандарты через `golangci-lint v2.13.2` и сборку. Интеграционные тесты создают собственные временные контейнеры и не используют локальную БД Compose.

Миграции: `go run ./cmd/migrate up`, `status`, `down`; каталог: `go run ./cmd/migrate seed`. `down` откатывает одну миграцию и может удалить данные — перед ним нужен проверенный план восстановления.

## Конфигурация

| Переменная | Назначение |
|---|---|
| `DATABASE_URL` | Обязательное подключение PostgreSQL |
| `RESPONSE_ENCRYPTION_KEY` | Обязательный base64-ключ из 32 байт для сохраненных ответов идемпотентности |
| `HTTP_ADDR` | Адрес HTTP, по умолчанию `:8080` |
| `DB_MAX_OPEN_CONNS` | Максимум соединений, по умолчанию `20` |
| `DB_MAX_IDLE_CONNS` | Неактивные соединения, по умолчанию `10`, не больше максимума |
| `MIGRATIONS_DIR` | Каталог миграций, по умолчанию `migrations` |

Значения пароля и ключа в `.env.example` и Compose предназначены для локальной разработки. Для окружения с настоящими данными задайте собственный пароль, защищенное подключение к БД и постоянный случайный ключ, например результат `openssl rand -base64 32`. Ключ нужен для расшифровки сохраненных ответов, включая выданные токены. Сохраняйте его отдельно от БД и включайте в план восстановления. Онлайн-ротация ключа в MVP не реализована: старые ответы требуют прежний ключ, поэтому простая замена делает их недоступными.

API предназначен для работы за TLS-прокси. Журналы приложения имеют JSON-формат; завершение по SIGTERM прекращает прием соединений и дает текущим запросам до 20 секунд. `/healthz` проверяет процесс, `/readyz` — подключение к БД. Реальные интеграции с GitHub/Jira, автоматическое получение фактов и отправка уведомлений не настроены: внешние агенты работают через API.

Отдельные HTTP acceptance tests: [screener-service-autotests](https://github.com/team-screaner/screener-service-autotests). Они используют сгенерированный клиент и не импортируют internal-пакеты сервиса.

Проверки и ограничения: [verification](docs/verification.md). Сканер зависимостей сохраняет известное предупреждение Excelize; входная проверка и регрессии описаны в [security](docs/security.md).
