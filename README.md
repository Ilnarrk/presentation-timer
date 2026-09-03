# Таймер докладов

<img width="940" height="708" alt="image" src="https://github.com/user-attachments/assets/b9bc3d08-dd15-4c85-89ae-802d24bbf2ae" />


Настольное Windows-приложение для контроля времени доклада и блока вопросов на мероприятиях, конференциях и видеоконференциях.

После сборки вы получаете один файл `presentation-timer.exe`. Это полноценное десктоп-приложение: его запускают двойным щелчком, оно работает в отдельном окне Windows и не требует браузера, веб-сервера или установленного Node.js.

## Возможности

- Ручная настройка длительности доклада и вопросов
- Крупный обратный отсчёт и понятные статусы этапов
- Ручные переходы: «К вопросам» и «Следующий докладчик»
- Звуковой сигнал по окончании лимита с настраиваемым интервалом повторов
- Восстановление окна поверх остальных приложений при сигнале
- Выбор звука, громкости и аудиоустройства, импорт WAV, MP3 и OGG
- Опциональные записи «Время вопросов» и «Следующий докладчик» для ВКС
- Автономный участник ВКС, передающий сигнал прямо в конференцию
- Сохранение настроек между запусками

---

## Что нужно для сборки

Сборка выполняется один раз на компьютере разработчика. На компьютере, где будет использоваться таймер, нужен только готовый `.exe`.

| Компонент | Минимальная версия | Зачем нужен |
|-----------|-------------------|-------------|
| Windows | 10 / 11 | Целевая ОС |
| Go | 1.25+ | Компиляция backend |
| Node.js | 18+ | Сборка интерфейса на этапе build |
| Wails CLI | v2 | Сборка десктопного `.exe` |
| WebView2 Runtime | обычно уже есть в Windows 11 | Отображение окна приложения |
| Google Chrome или Яндекс Браузер | актуальная версия | Автономный участник ВКС; Edge используется только как fallback |

### 1. Установить Go

Скачайте и установите Go с [https://go.dev/dl/](https://go.dev/dl/) 

Проверка:

```powershell
go version
```

### 2. Установить Node.js

Скачайте и установите LTS-версию с [https://nodejs.org/](https://nodejs.org/) 

Проверка:

```powershell
node --version
npm --version
```

### 3. Установить Wails CLI

```powershell
go install github.com/wailsapp/wails/v2/cmd/wails@latest
```

Убедитесь, что `%USERPROFILE%\go\bin` есть в `PATH`.

Проверка:

```powershell
wails version
```

### 4. Проверить окружение Wails

```powershell
wails doctor
```

Если `wails doctor` сообщает об отсутствии WebView2 Runtime, установите его с сайта Microsoft. На Windows 11 runtime обычно уже присутствует.

---

## Полная сборка приложения

Откройте PowerShell, склонируйте репозиторий и перейдите в папку проекта:

```powershell
git clone https://github.com/Ilnarrk/presentation-timer.git
cd presentation-timer
```

Если репозиторий уже скачан, достаточно перейти в его папку:

```powershell
cd presentation-timer
```

### Шаг 1. Установить зависимости интерфейса

```powershell
cd frontend
npm install
cd ..
```

### Шаг 2. Собрать десктопное приложение

При необходимости сначала положите свои WAV, MP3 или OGG в папку `sounds` в
корне проекта. Они будут встроены непосредственно в `.exe`.

```powershell
wails build
```

Для инсталлятора (нужен NSIS в PATH):

```powershell
wails build
powershell -ExecutionPolicy Bypass -File build\windows\sign-binaries.ps1 -PfxPath build\windows\codesign.pfx -Password "ваш-пароль" -ExeOnly
makensis -DARG_WAILS_AMD64_BINARY=..\..\bin\presentation-timer.exe project.nsi
powershell -ExecutionPolicy Bypass -File build\windows\sign-binaries.ps1 -PfxPath build\windows\codesign.pfx -Password "ваш-пароль" -InstallerOnly
```

(команда `makensis` — из каталога `build\windows\installer`)

Wails автоматически:

1. соберёт интерфейс;
2. скомпилирует Go-код;
3. упакует всё в один Windows-файл;
4. подставит иконку из `build/windows/icon.ico`.

### Шаг 3. Найти готовый файл

После успешной сборки файлы будут здесь:

```text
build\bin\presentation-timer.exe
build\bin\presentation-timer-amd64-installer.exe
```

Инсталлятор — основной способ установки на другой ПК: копирует приложение в Program Files и импортирует публичный сертификат (запрос UAC). Portable `.exe` можно запускать без установки, но сертификат тогда нужно импортировать вручную.

Для работы на другом ПК **не нужны** Go, Node.js, npm или исходники проекта.

Перед `wails build --nsis` должен существовать `build\windows\codesign.cer` (его создаёт скрипт сертификата или CI из `.pfx`). Без этого файла инсталлятор соберётся, но не сможет поставить сертификат.

### Подпись сборки

Самозаверяющий сертификат не убирает SmartScreen «из коробки», но инсталлятор может доверить издателя на этом ПК.

Один раз создайте сертификат (нужны права на запись в хранилище текущего пользователя):

```powershell
powershell -ExecutionPolicy Bypass -File build\windows\create-codesign-cert.ps1
```

Будут созданы `build\windows\codesign.pfx` (закрытый ключ, **не коммитить**) и `build\windows\codesign.cer` (публичный). Порядок сборки с подписью:

```powershell
powershell -ExecutionPolicy Bypass -File build\windows\sign-binaries.ps1 -PfxPath build\windows\codesign.pfx -Password "ваш-пароль" -ExportOnly
wails build
powershell -ExecutionPolicy Bypass -File build\windows\sign-binaries.ps1 -PfxPath build\windows\codesign.pfx -Password "ваш-пароль" -ExeOnly
cd build\windows\installer
makensis -DARG_WAILS_AMD64_BINARY=..\..\bin\presentation-timer.exe project.nsi
cd ..\..\..
powershell -ExecutionPolicy Bypass -File build\windows\sign-binaries.ps1 -PfxPath build\windows\codesign.pfx -Password "ваш-пароль" -InstallerOnly
```

Нужен [Windows SDK](https://developer.microsoft.com/windows/downloads/windows-sdk/) (`signtool`). Для релизов из GitHub Actions задайте секреты `CODE_SIGN_PFX_BASE64` (Base64 содержимого `.pfx`) и `CODE_SIGN_PFX_PASSWORD`.

---

## Windows SmartScreen и ложные срабатывания

Windows может показать SmartScreen или Defender для неподписанного либо **самоподписанного** `.exe`: нет репутации у центра сертификации, а приложение запускает браузер для ВКС.

- Скачивайте файлы **только** с [GitHub Releases](https://github.com/Ilnarrk/presentation-timer/releases) и сверяйте SHA256 из описания релиза.
- **Рекомендуется инсталлятор** `presentation-timer-*-installer.exe`. Он запросит права администратора, установит приложение и сам импортирует публичный `codesign.cer` в хранилища «Доверенные лица» и «Доверенные издатели» локального компьютера (`certutil -addstore`). Закрытый ключ (`.pfx`) в инсталлятор не входит.
- Portable `presentation-timer.exe` сертификат сам не ставит. При необходимости импортируйте `codesign.cer` вручную:

```powershell
Import-Certificate -FilePath .\codesign.cer -CertStoreLocation Cert:\LocalMachine\TrustedPeople
Import-Certificate -FilePath .\codesign.cer -CertStoreLocation Cert:\LocalMachine\TrustedPublisher
```

Нужны права администратора. Подробнее: [создание сертификата для подписи пакета](https://learn.microsoft.com/ru-ru/windows/msix/package/create-certificate-package-signing).

Самоподпись **не равна** сертификату от доверенного CA. «Зелёный» SmartScreen без предупреждений возможен только с платным OV/EV Code Signing.

Если предупреждение всё равно появилось и вы доверяете источнику релиза: в SmartScreen — «Подробнее» → «Выполнить в любом случае»; в Defender — разрешить файл на устройстве.

---

## Запуск приложения

### Обычный запуск

```powershell
.\build\bin\presentation-timer.exe
```

Или дважды щёлкните по `presentation-timer.exe` в Проводнике.

### Первый запуск

1. В стартовом окне укажите ссылку ВКС или нажмите «Пропустить».
2. Откройте настройки, укажите длительность доклада, вопросов и интервал повторного сигнала.
3. Выберите звук, при необходимости импортируйте свою запись и выберите аудиоустройство.
4. Для подключённой ВКС запустите проверку звука зелёной кнопкой Play.
5. Нажмите зелёную кнопку запуска таймера.

### Рабочий сценарий

1. Докладчик начинает выступление — запускается таймер доклада.
2. Когда доклад завершён, нажмите «К вопросам».
3. После блока вопросов нажмите «Следующий докладчик».
4. Приложение переключается на таймер следующего выступающего.

Если время вышло:

- прозвучит выбранный сигнал;
- окно развернётся и поднимется поверх остальных;
- при дальнейшей просрочке сигнал повторится через заданный в настройках интервал.

Настройки сохраняются автоматически в:

```text
%AppData%\presentation-timer\settings.json
```

Импортированные записи хранятся в:

```text
%AppData%\presentation-timer\sounds
```

### Пользовательские аудиозаписи

В меню настроек нажмите «Добавить аудио» и выберите WAV, MP3 или OGG. Файл
нормализуется в PCM WAV и после этого доступен во всех списках сигналов.
Максимальный размер исходного файла — 20 МБ, продолжительность — 5 минут.

Сигналы «Время вопросов» и «Следующий докладчик» по умолчанию выключены. Если
их включить, они проигрываются только через подключённого участника ВКС.

Чтобы включить записи в готовый `.exe`, положите файлы в корневую папку
`sounds` перед `wails build`. Имя без учёта регистра, пробелов, дефисов и
подчёркиваний определяет назначение:

- `alert`, `overtime` или `просрочка` — основной сигнал окончания времени;
- `questions` или `время вопросов` — переход к вопросам;
- `next` или `следующий докладчик` — переход к следующему докладчику;
- остальные имена — дополнительные варианты в общем списке звуков.

Например: `Время вопросов.mp3`, `next.ogg`, `просрочка.wav`. При наличии
такого файла он автоматически становится сборочным значением по умолчанию.
Без matching-файлов сигналы переходов остаются выключенными.

---

## Режим разработки

Если вы меняете код и хотите быстро проверять изменения:

```powershell
wails dev
```

Откроется окно приложения с автоперезагрузкой интерфейса. Этот режим нужен только разработчикам.

---

## Иконка приложения

Иконка для `.exe` лежит здесь:

```text
build/windows/icon.ico
```

Исходник для пересборки иконки:

```text
build/appicon.png
```

Если нужно обновить иконку:

1. измените `build/generate_icon.py` или замените `build/appicon.png`;
2. при ручной замене PNG пересоберите ICO:

```powershell
python build/generate_icon.py
```

3. пересоберите приложение:

```powershell
wails build
```

---

## Автономный участник ВКС

Таймер может открыть встречу во вкладке Google Chrome или Яндекс Браузера, войти как участник
`Таймер` и передать сигнал через синтетический WebRTC-микрофон. VB-CABLE и
VoiceMeeter для этого не нужны.

Поддерживаются:

- SaluteJazz (`salutejazz.ru`, `sberjazz.ru`, `jazz.sber.ru`);
- Яндекс Телемост (`telemost.yandex.ru`, `telemost.yandex.com`);
- Контур.Толк (`ktalk.ru`, `talk.kontur.ru`);
- МТС Линк (`mts-link.ru`, `webinar.ru`);
- MINT (`mintconf.ru`, корпоративные инсталляции вроде `mint.tatneft.tatar`).

Любая другая HTTPS-ссылка на ВКС (on-prem с кастомным доменом или внутренним
IP-адресом) принимается через универсальный адаптер. Автовход может потребовать
ручного завершения входа.

### Подключение

1. В окне «Подключение к ВКС» вставьте полную HTTPS-ссылку и укажите имя.
2. Нажмите «Подключиться». Откроется вкладка браузера. Если браузер таймера уже
   запущен, приложение использует его повторно.
3. Если встреча защищена SSO, CAPTCHA или комнатой ожидания, завершите вход вручную.
4. Дождитесь статуса «Подключён». Если вы вошли вручную, а статус не изменился,
   нажмите «Я уже подключён».
5. Нажмите зелёную кнопку Play для проверки звука.
6. Убедитесь вместе с одним из участников, что тестовый сигнал слышен.
7. Запустите таймер. При таймауте тот же сигнал прозвучит локально и в ВКС.

Открыть это окно повторно можно нажатием на статус ВКС над таймером. Красная
кнопка в окне отключает участника от встречи.

Камера участника всегда подменяется пустым видеопотоком и остаётся выключенной.
Между сигналами синтетический микрофон передаёт тишину.

### Ограничения

- Организатор должен допустить участника и разрешить ему включить микрофон.
- В закрытой встрече Контур.Толк должен быть разрешён вход внешних участников.
- В вебинаре МТС Линк гость передаст звук только после разрешения организатора говорить.
- Автоматизация зависит от интерфейса сайтов. После обновления ВКС может потребоваться
  обновить адаптер приложения.
- Подключиться к обычному пользовательскому Chromium-браузеру можно только если он был
  запущен с `--remote-debugging-port`. Иначе таймер запускает собственный профиль
  браузера в `%AppData%\presentation-timer\conference-browser`.
- Браузеры выбираются в порядке: Google Chrome, Яндекс Браузер, затем Edge как
  fallback для совместимых платформ. SaluteJazz в Edge не запускается.
- Firefox поддерживается многими ВКС, но не подходит для текущего синтетического
  микрофона таймера: его протокол автоматизации несовместим с используемым CDP.
- Таймер подтверждает отправку данных в браузер, но ВКС не возвращают надёжную
  квитанцию о фактической слышимости. Поэтому тест звука обязателен. В `wails dev`
  кнопка «Диагностика» в окне ВКС копирует снимок медиа-моста в буфер обмена;
  в прод-сборке её нет.
- HTTP-ссылки и адреса с данными авторизации в URL отклоняются. Параметры и токены
  ссылки не показываются в интерфейсе и не сохраняются в `settings.json`.
- На внутренних серверах с самоподписанным сертификатом Chromium может отказать
  открыть страницу — это ограничение браузера, не приложения.

---

## Проверка и диагностика

### Тесты backend

```powershell
go test ./...
```

### Проверка сборки интерфейса

```powershell
cd frontend
npm run build
cd ..
```

### Типичные проблемы

| Проблема | Решение |
|---------|---------|
| `wails: command not found` | Добавьте `%USERPROFILE%\go\bin` в `PATH` и переоткройте PowerShell |
| Ошибка WebView2 | Установите [Microsoft Edge WebView2 Runtime](https://developer.microsoft.com/microsoft-edge/webview2/) |
| Нет звука | Проверьте выбранное устройство и громкость в приложении |
| Не открывается участник ВКС | Установите или обновите Google Chrome / Яндекс Браузер |
| Участник ждёт допуска | Организатор должен принять гостя в конференцию |
| Сигнал не слышен в ВКС | Разрешите участнику микрофон и повторите «Тест звука» |
| SmartScreen / Defender считает файл опасным | Установите через инсталлятор из GitHub Releases (он импортирует сертификат). Для portable сверьте SHA256 и при необходимости импортируйте `codesign.cer` |

---

## Структура проекта

```text
presentation-timer/
├── app.go                  # API приложения, окно, сигналы
├── main.go                 # Точка входа десктопного приложения
├── internal/timer/         # Логика таймера
├── internal/audio/         # Звук и аудиоустройства Windows
├── internal/settings/      # Локальные настройки
├── sounds/                  # Аудиозаписи, встраиваемые при сборке
├── frontend/               # Исходники интерфейса (нужны только при сборке)
├── build/
│   ├── appicon.png         # Исходная иконка
│   └── windows/
│       ├── icon.ico        # Иконка для .exe
│       ├── create-codesign-cert.ps1
│       └── sign-binaries.ps1
└── build/bin/
    └── presentation-timer.exe
```

---

## Краткая шпаргалка

```powershell
# Клонирование (один раз)
git clone https://github.com/Ilnarrk/presentation-timer.git
cd presentation-timer

# Сборка и подпись (нужны codesign.pfx / .cer, NSIS в PATH)
powershell -ExecutionPolicy Bypass -File build\windows\sign-binaries.ps1 -PfxPath build\windows\codesign.pfx -Password "ваш-пароль" -ExportOnly
wails build
powershell -ExecutionPolicy Bypass -File build\windows\sign-binaries.ps1 -PfxPath build\windows\codesign.pfx -Password "ваш-пароль" -ExeOnly
cd build\windows\installer && makensis -DARG_WAILS_AMD64_BINARY=..\..\bin\presentation-timer.exe project.nsi && cd ..\..\..

# Portable без установки
.\build\bin\presentation-timer.exe
```
