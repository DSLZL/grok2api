# Grok2API

[中文](../readme.md) | **English**

> [!NOTE]
> This project is for learning and research only. You must comply with Grok **Terms of Use** and **local laws and regulations**. Do not use for illegal purposes.

Grok2API rebuilt with **FastAPI**, fully aligned with the latest web call format. Supports streaming/non-streaming chat, image generation/editing, deep reasoning, token pool concurrency, and automatic load balancing.

### NOTE: The project is no longer accepting PRs and feature updates; this is the last structure optimization.

<img width="2562" height="1280" alt="image" src="https://github.com/user-attachments/assets/356d772a-65e1-47bd-abc8-c00bb0e2c9cc" />

<br>

## Usage

### How to Start

- Local development

```
uv sync

uv run main.py
```

### How to Deploy

#### docker compose
```
git clone https://github.com/chenyme/grok2api

docker compose up -d
```

#### Vercel

[![Deploy with Vercel](https://vercel.com/button)](https://vercel.com/new/clone?repository-url=https://github.com/chenyme/grok2api&env=LOG_LEVEL,LOG_FILE_ENABLED,DATA_DIR,SERVER_STORAGE_TYPE,SERVER_STORAGE_URL&envDefaults=%7B%22DATA_DIR%22%3A%22/tmp/data%22%2C%22LOG_FILE_ENABLED%22%3A%22false%22%2C%22LOG_LEVEL%22%3A%22INFO%22%2C%22SERVER_STORAGE_TYPE%22%3A%22local%22%2C%22SERVER_STORAGE_URL%22%3A%22%22%7D)

> Make sure to set `DATA_DIR=/tmp/data` and disable file logging with `LOG_FILE_ENABLED=false`.
>
> For persistence, use MySQL / Redis / PostgreSQL and set `SERVER_STORAGE_TYPE` (mysql/redis/pgsql) and `SERVER_STORAGE_URL` in Vercel env vars.

#### Render

[![Deploy to Render](https://render.com/images/deploy-to-render-button.svg)](https://render.com/deploy?repo=https://github.com/chenyme/grok2api)

> Render free instances sleep after 15 minutes of inactivity; restart/redeploy will lose data.
>
> For persistence, use MySQL / Redis / PostgreSQL and set `SERVER_STORAGE_TYPE` (mysql/redis/pgsql) and `SERVER_STORAGE_URL` in Render env vars.

### Admin Panel

Access: `http://<host>:8000/admin`
Default password: `grok2api` (config `app.app_key`, recommended to change).

**Features**:

- **Token Management**: import/add/delete tokens, view status and quota
- **Status Filter**: filter by status (active/limited/expired) or NSFW status
- **Batch Ops**: batch refresh/export/delete/enable NSFW
- **NSFW Enable**: one-click Unhinged for tokens (proxy or cf_clearance required)
- **Config Management**: update system config online
- **Cache Management**: view and clear media cache

### Environment Variables

> Configure `.env`

| Name                   | Description                                         | Default     | Example                                               |
| :--------------------- | :-------------------------------------------------- | :---------- | :---------------------------------------------------- |
| `LOG_LEVEL`            | Log level                                           | `INFO`      | `DEBUG`                                               |
| `LOG_FILE_ENABLED`     | Enable file logging                                 | `true`      | `false`                                               |
| `DATA_DIR`             | Data dir (config/tokens/locks)                      | `./data`    | `/data`                                               |
| `SERVER_HOST`          | Bind address                                        | `0.0.0.0`   | `0.0.0.0`                                             |
| `SERVER_PORT`          | Server port                                         | `8000`      | `8000`                                                |
| `SERVER_WORKERS`       | Uvicorn worker count                                | `1`         | `2`                                                   |
| `SERVER_STORAGE_TYPE`  | Storage type (`local`/`redis`/`mysql`/`pgsql`)      | `local`     | `pgsql`                                               |
| `SERVER_STORAGE_URL`   | Storage DSN (optional for local)                    | `""`        | `postgresql+asyncpg://user:password@host:5432/db`     |

> MySQL example: `mysql+aiomysql://user:password@host:3306/db` (if you provide `mysql://`, it will be converted to `mysql+aiomysql://`)

### Quotas

- Basic account: 80 requests / 20h
- Super account: 140 requests / 2h

### Models

| Model                   | Cost | Account     | Chat | Image | Video |
| :---------------------- | :--: | :---------- | :--: | :---: | :---: |
| `grok-3`                |  1   | Basic/Super | Yes  | Yes   |  -    |
| `grok-3-fast`           |  1   | Basic/Super | Yes  | Yes   |  -    |
| `grok-4`                |  1   | Basic/Super | Yes  | Yes   |  -    |
| `grok-4-mini`           |  1   | Basic/Super | Yes  | Yes   |  -    |
| `grok-4-fast`           |  1   | Basic/Super | Yes  | Yes   |  -    |
| `grok-4-heavy`          |  4   | Super       | Yes  | Yes   |  -    |
| `grok-4.1`              |  1   | Basic/Super | Yes  | Yes   |  -    |
| `grok-4.1-thinking`     |  4   | Basic/Super | Yes  | Yes   |  -    |
| `grok-imagine-1.0`      |  4   | Basic/Super |  -   | Yes   |  -    |
| `grok-imagine-1.0-edit` |  4   | Basic/Super |  -   | Yes   |  -    |
| `grok-imagine-1.0-video`|  -   | Basic/Super |  -   |  -    | Yes   |

<br>

## API

### `POST /v1/chat/completions`

> Generic endpoint: chat, image generation, image editing, video generation, video upscaling

```bash
curl http://localhost:8000/v1/chat/completions   -H "Content-Type: application/json"   -H "Authorization: Bearer $GROK2API_API_KEY"   -d '{
    "model": "grok-4",
    "messages": [{"role":"user","content":"Hello"}]
  }'
```

<details>
<summary>Supported request parameters</summary>

<br>

| Field                  | Type    | Description                 | Allowed values                                                                                                   |
| :--------------------- | :------ | :-------------------------- | :--------------------------------------------------------------------------------------------------------------- |
| `model`                | string  | Model ID                    | See model list above                                                                                             |
| `messages`             | array   | Message list                | See message format below                                                                                         |
| `stream`               | boolean | Enable streaming            | `true`, `false`                                                                                                  |
| `reasoning_effort`     | string  | Reasoning effort            | `none`, `minimal`, `low`, `medium`, `high`, `xhigh`                                                              |
| `temperature`          | number  | Sampling temperature        | `0` ~ `2`                                                                                                        |
| `top_p`                | number  | Nucleus sampling            | `0` ~ `1`                                                                                                        |
| `video_config`         | object  | **Video model only**        | Supported: `grok-imagine-1.0-video`                                                                              |
| └─ `aspect_ratio`      | string  | Video aspect ratio          | `16:9`, `9:16`, `1:1`, `2:3`, `3:2`, `1280x720`, `720x1280`, `1792x1024`, `1024x1792`, `1024x1024`               |
| └─ `video_length`      | integer | Video length (seconds)      | `6`, `10`, `15`                                                                                                  |
| └─ `resolution_name`   | string  | Resolution                  | `480p`, `720p`                                                                                                   |
| └─ `preset`            | string  | Style preset                | `fun`, `normal`, `spicy`, `custom`                                                                               |
| `image_config`         | object  | **Image models only**       | Supported: `grok-imagine-1.0` / `grok-imagine-1.0-edit`                                                          |
| └─ `n`                 | integer | Number of images            | `1` ~ `10`                                                                                                       |
| └─ `size`              | string  | Image size                  | `1280x720`, `720x1280`, `1792x1024`, `1024x1792`, `1024x1024`                                                    |
| └─ `response_format`   | string  | Response format             | `url`, `b64_json`, `base64`                                                                                      |

**Message format (messages)**:

| Field     | Type         | Description                                         |
| :-------- | :----------- | :-------------------------------------------------- |
| `role`    | string       | `developer`, `system`, `user`, `assistant`          |
| `content` | string/array | Message content (plain text or multimodal array)    |

**Multimodal content block types (content array)**:

| type          | Description | Example                                                                  |
| :------------ | :---------- | :----------------------------------------------------------------------- |
| `text`        | Text        | `{"type": "text", "text": "Describe this image"}`                        |
| `image_url`   | Image URL   | `{"type": "image_url", "image_url": {"url": "https://..."}}`             |
| `input_audio` | Audio       | `{"type": "input_audio", "input_audio": {"data": "https://..."}}`        |
| `file`        | File        | `{"type": "file", "file": {"file_data": "https://..."}}`                 |

**Notes**:
- `image_url/input_audio/file` only supports URL or Data URI (`data:<mime>;base64,...`); raw base64 will be rejected.
- `reasoning_effort`: `none` disables thinking output; any other value enables it.
- `grok-imagine-1.0-edit` requires an image; if multiple are provided, the last image and last text are used.
- Any other parameters will be discarded and ignored.

<br>

</details>

<br>

### `POST /v1/images/generations`

> Image generation endpoint

```bash
curl http://localhost:8000/v1/images/generations   -H "Content-Type: application/json"   -H "Authorization: Bearer $GROK2API_API_KEY"   -d '{
    "model": "grok-imagine-1.0",
    "prompt": "A cat floating in space",
    "n": 1
  }'
```

<details>
<summary>Supported request parameters</summary>

<br>

| Field              | Type    | Description      | Allowed values                                                     |
| :----------------- | :------ | :--------------- | :----------------------------------------------------------------- |
| `model`            | string  | Image model ID   | `grok-imagine-1.0`                                                 |
| `prompt`           | string  | Prompt           | -                                                                  |
| `n`                | integer | Number of images | `1` - `10` (streaming: `1` or `2` only)                            |
| `stream`           | boolean | Enable streaming | `true`, `false`                                                    |
| `size`             | string  | Image size       | `1280x720`, `720x1280`, `1792x1024`, `1024x1792`, `1024x1024`      |
| `quality`          | string  | Image quality    | - (not supported)                                                  |
| `response_format`  | string  | Response format  | `url`, `b64_json`, `base64`                                        |
| `style`            | string  | Style            | -                                                                  |

**Notes**:
- `quality` and `style` are OpenAI compatibility placeholders and are not customizable yet.

<br>

</details>

<br>

### `POST /v1/images/edits`

> Image edit endpoint (multipart/form-data)

```bash
curl http://localhost:8000/v1/images/edits   -H "Authorization: Bearer $GROK2API_API_KEY"   -F "model=grok-imagine-1.0-edit"   -F "prompt=Make it sharper"   -F "image=@/path/to/image.png"   -F "n=1"
```

<details>
<summary>Supported request parameters</summary>

<br>

| Field              | Type    | Description      | Allowed values                                                     |
| :----------------- | :------ | :--------------- | :----------------------------------------------------------------- |
| `model`            | string  | Image model ID   | `grok-imagine-1.0-edit`                                            |
| `prompt`           | string  | Edit prompt      | -                                                                  |
| `image`            | file    | Image file       | `png`, `jpg`, `webp`                                               |
| `n`                | integer | Number of images | `1` - `10` (streaming: `1` or `2` only)                            |
| `stream`           | boolean | Enable streaming | `true`, `false`                                                    |
| `size`             | string  | Image size       | `1280x720`, `720x1280`, `1792x1024`, `1024x1792`, `1024x1024`      |
| `quality`          | string  | Image quality    | - (not supported)                                                  |
| `response_format`  | string  | Response format  | `url`, `b64_json`, `base64`                                        |
| `style`            | string  | Style            | - (not supported)                                                  |

**Notes**:
- `quality` and `style` are OpenAI compatibility placeholders and are not customizable yet.

<br>

</details>

<br>

## Configuration

Config file: `data/config.toml`

> [!NOTE]
> In production or reverse proxy environments, set `app.app_url` to a publicly accessible URL,
> otherwise file links may be incorrect or return 403.

> [!TIP]
> **v2.0 config structure upgrade**: legacy config will be **automatically migrated** to the new structure.
> Custom values under the old `[grok]` section are mapped to the new sections.

| Module               | Field                          | Name                   | Description                                                        | Default                                                     |
| :------------------- | :----------------------------- | :--------------------- | :----------------------------------------------------------------- | :---------------------------------------------------------- |
| **app**              | `app_url`                      | App URL                | External access URL for Grok2API (used for file links).            | `http://127.0.0.1:8000`                                     |
|                      | `app_key`                      | Admin password         | Password for Grok2API admin panel (required).                      | `grok2api`                                                  |
|                      | `api_key`                      | API key                | Token for calling Grok2API (optional).                             | `""`                                                        |
|                      | `image_format`                 | Image format           | Output image format (url or base64).                               | `url`                                                       |
|                      | `video_format`                 | Video format           | Output video format (html or url, url is processed).               | `html`                                                      |
|                      | `temporary`                    | Temporary chat         | Enable temporary conversation mode.                                | `true`                                                      |
|                      | `disable_memory`               | Disable memory         | Disable Grok memory to prevent irrelevant context.                 | `true`                                                      |
|                      | `stream`                       | Streaming              | Enable streaming by default.                                       | `true`                                                      |
|                      | `thinking`                     | Thinking chain         | Enable model thinking output.                                      | `true`                                                      |
|                      | `dynamic_statsig`              | Dynamic fingerprint    | Enable dynamic Statsig generation.                                 | `true`                                                      |
|                      | `filter_tags`                  | Filter tags            | Auto-filter special tags in Grok responses.                        | `["xaiartifact", "xai:tool_usage_card", "grok:render"]`     |
| **proxy**            | `base_proxy_url`               | Base proxy URL         | Base service address proxying Grok official site.                  | `""`                                                        |
|                      | `asset_proxy_url`              | Asset proxy URL        | Proxy URL for Grok static assets (images/videos).                  | `""`                                                        |
|                      | `cf_clearance`                 | CF Clearance           | Cloudflare clearance cookie for anti-bot.                          | `""`                                                        |
|                      | `browser`                      | Browser fingerprint    | curl_cffi browser fingerprint (e.g. chrome136).                    | `chrome136`                                                 |
|                      | `user_agent`                   | User-Agent             | HTTP User-Agent string.                                            | `Mozilla/5.0 (Macintosh; ...)`                              |
| **voice**            | `timeout`                      | Request timeout        | Voice request timeout (seconds).                                   | `120`                                                       |
| **chat**             | `concurrent`                   | Concurrency            | Reverse interface concurrency limit.                               | `10`                                                        |
|                      | `timeout`                      | Request timeout        | Reverse interface timeout (seconds).                               | `60`                                                        |
|                      | `stream_timeout`               | Stream idle timeout    | Stream idle timeout (seconds).                                     | `60`                                                        |
| **video**            | `concurrent`                   | Concurrency            | Reverse interface concurrency limit.                               | `10`                                                        |
|                      | `timeout`                      | Request timeout        | Reverse interface timeout (seconds).                               | `60`                                                        |
|                      | `stream_timeout`               | Stream idle timeout    | Stream idle timeout (seconds).                                     | `60`                                                        |
| **retry**            | `max_retry`                    | Max retries            | Max retries on Grok request failure.                               | `3`                                                         |
|                      | `retry_status_codes`           | Retry status codes     | HTTP status codes that trigger retry.                              | `[401, 429, 403]`                                           |
|                      | `retry_backoff_base`           | Backoff base           | Base delay for retry backoff (seconds).                            | `0.5`                                                       |
|                      | `retry_backoff_factor`         | Backoff factor         | Exponential multiplier for retry backoff.                          | `2.0`                                                       |
|                      | `retry_backoff_max`            | Backoff max            | Max wait per retry (seconds).                                      | `30.0`                                                      |
|                      | `retry_budget`                 | Backoff budget         | Max total retry time per request (seconds).                        | `90.0`                                                      |
| **image**            | `timeout`                      | Request timeout        | WebSocket request timeout (seconds).                               | `120`                                                       |
|                      | `stream_timeout`               | Stream idle timeout    | WebSocket stream idle timeout (seconds).                           | `120`                                                       |
|                      | `final_timeout`                | Final image timeout    | Timeout after medium image before final (seconds).                 | `15`                                                        |
|                      | `nsfw`                         | NSFW mode              | Enable NSFW in WebSocket requests.                                 | `true`                                                      |
|                      | `medium_min_bytes`             | Medium min bytes       | Minimum bytes for medium quality image.                            | `30000`                                                     |
|                      | `final_min_bytes`              | Final min bytes        | Minimum bytes to treat an image as final (JPG usually > 100KB).    | `100000`                                                    |
| **token**            | `auto_refresh`                 | Auto refresh           | Enable automatic token refresh.                                    | `true`                                                      |
|                      | `refresh_interval_hours`       | Refresh interval       | Regular token refresh interval (hours).                            | `8`                                                         |
|                      | `super_refresh_interval_hours` | Super refresh interval | Super token refresh interval (hours).                              | `2`                                                         |
|                      | `fail_threshold`               | Failure threshold      | Consecutive failures before a token is disabled.                   | `5`                                                         |
|                      | `save_delay_ms`                | Save delay             | Debounced save delay for token changes (ms).                       | `500`                                                       |
|                      | `reload_interval_sec`          | Sync interval          | Token state refresh interval in multi-worker setups (sec).         | `30`                                                        |
| **cache**            | `enable_auto_clean`            | Auto clean             | Enable cache auto clean; cleanup when exceeding limit.             | `true`                                                      |
|                      | `limit_mb`                     | Cleanup threshold      | Cache size threshold (MB) that triggers cleanup.                   | `1024`                                                      |
| **asset**            | `upload_concurrent`            | Upload concurrency     | Max concurrency for upload. Recommended 30.                        | `30`                                                        |
|                      | `upload_timeout`               | Upload timeout         | Upload timeout (seconds). Recommended 60.                          | `60`                                                        |
|                      | `download_concurrent`          | Download concurrency   | Max concurrency for download. Recommended 30.                      | `30`                                                        |
|                      | `download_timeout`             | Download timeout       | Download timeout (seconds). Recommended 60.                        | `60`                                                        |
|                      | `list_concurrent`              | List concurrency       | Max concurrency for asset listing. Recommended 10.                 | `10`                                                        |
|                      | `list_timeout`                 | List timeout           | List timeout (seconds). Recommended 60.                            | `60`                                                        |
|                      | `list_batch_size`              | List batch size        | Batch size per list request. Recommended 10.                       | `10`                                                        |
|                      | `delete_concurrent`            | Delete concurrency     | Max concurrency for asset delete. Recommended 10.                  | `10`                                                        |
|                      | `delete_timeout`               | Delete timeout         | Delete timeout (seconds). Recommended 60.                          | `60`                                                        |
|                      | `delete_batch_size`            | Delete batch size      | Batch size per delete request. Recommended 10.                     | `10`                                                        |
| **nsfw**             | `concurrent`                   | Concurrency            | Max concurrency for enabling NSFW. Recommended 10.                 | `10`                                                        |
|                      | `batch_size`                   | Batch size             | Batch size for enabling NSFW. Recommended 50.                      | `50`                                                        |
|                      | `timeout`                      | Request timeout        | NSFW enable request timeout (seconds). Recommended 60.             | `60`                                                        |
| **usage**            | `concurrent`                   | Concurrency            | Max concurrency for usage refresh. Recommended 10.                 | `10`                                                        |
|                      | `batch_size`                   | Batch size             | Batch size for usage refresh. Recommended 50.                      | `50`                                                        |
|                      | `timeout`                      | Request timeout        | Usage query timeout (seconds). Recommended 60.                     | `60`                                                        |

<br>

## Star History

[![Star History Chart](https://api.star-history.com/svg?repos=Chenyme/grok2api&type=Timeline)](https://star-history.com/#Chenyme/grok2api&Timeline)
