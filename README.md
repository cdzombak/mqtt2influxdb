# `mqtt2influxdb` 

An opinionated and intentionally scope-limited MQTT to InfluxDB bridge.

## Goals & Features

- Get simple JSON data from MQTT to InfluxDB
- Straightforward; easy to learn about, configure, and run
- Take advantage of orchestration features from e.g. systemd or Docker Compose
- Allow health monitoring via an outgoing heartbeat, HTTP GET, and/or Docker's health check

## Non-goals

### Multiple topics/subscriptions

Each `mqtt2influxdb` instance subscribes to a single MQTT topic (or, in ESPHome mode, a set of topics derived from a single prefix). To bridge multiple topics, run multiple instances of `mqtt2influxdb` and use your orchestration tool (systemd, Docker Compose, etc.) to manage them.

### Infinitely flexible schema/transformations

`mqtt2influxdb` can't necessarily work with every message format. It does not support non-JSON payloads in JSON mode, and it may not handle unusual key names or deeply nested structures gracefully. For more complex transformation needs, consider a more full-featured ETL tool.

## Usage

```text
mqtt2influxdb [options]
```

### Arguments

- `-env-files`: Comma-separated list of `.env` files to load. Earlier files take precedence over later ones. No `.env` file values will overwrite currently-set environment variables. See [godotenv's precedence documentation](https://github.com/joho/godotenv?tab=readme-ov-file#precedence--conventions) for details.
- `-strict`: Exit on invalid messages, unexpected topics, casting failures, or other unexpected conditions. This overrides `FIELDTAG_DETERMINATION_FAILURE` and `CAST_FAILURE` policies, setting both to `fatal`.
- `-version`: Print version and exit.
- `-help`: Print usage information and exit.

### Docker

A Docker image is available. All configuration is done through environment variables (and optionally `.env` files via `-env-files`).

```yaml
# docker-compose.yml example:
services:
  mqtt2influxdb:
    image: ghcr.io/cdzombak/mqtt2influxdb:latest
    environment:
      MQTT_SERVER: mqtt://my-broker:1883
      MQTT_TOPIC: sensors/temperature
      INFLUX_SERVER: http://influxdb:8086
      INFLUX_BUCKET: my-db/autogen
      INFLUX_MEASUREMENT_NAME: temperature
      M2I_MODE: json
      HEARTBEAT_INTERVAL_S: 30
      HEARTBEAT_THRESHOLD_S: 120
      HEARTBEAT_HEALTH_PORT: 8080
      # optionally, ping a remote monitor like Uptime Kuma:
      # HEARTBEAT_GET_URL: https://uptimekuma.example.com:9001/api/push/abcdabcd?status=up&msg=OK&ping=
    healthcheck:
      test: ["CMD", "curl", "-sf", "http://localhost:8080/"]
      interval: 60s
      timeout: 5s
      retries: 3
      start_period: 10s
```

### Message Modes

`mqtt2influxdb` supports three message modes, configured via the `M2I_MODE` environment variable. The default mode is `json`.

#### JSON Mode (`M2I_MODE=json`)

The default mode. Each MQTT message payload is expected to be a JSON object. Top-level keys are parsed into InfluxDB fields and tags according to the field/tag mapping and type hint rules described below.

#### Single-Value Mode (`M2I_MODE=single`)

In single-value mode, each MQTT message payload is treated as a single value (not parsed as JSON). This is useful for topics that publish a plain numeric or string value.

**Required:** `M2I_SINGLE_FIELDNAME` must be set to specify the InfluxDB field name for the value.

Type hints and renames work the same as in JSON mode, keyed on the canonical form of the field name given by `M2I_SINGLE_FIELDNAME`.

> [!NOTE]
> `DEDUPE_ON` is not supported in single-value mode.

#### ESPHome Mode (`M2I_MODE=esphome`)

ESPHome mode is designed to work with [ESPHome](https://esphome.io) devices publishing via MQTT. Instead of subscribing to a single topic, `mqtt2influxdb` subscribes to the following topic patterns derived from `MQTT_TOPIC` (used as the device's topic prefix):

- `<MQTT_TOPIC>/status` — device online/offline status (parsed as bool)
- `<MQTT_TOPIC>/binary_sensor/+/state` — binary sensor states (parsed as bool)
- `<MQTT_TOPIC>/sensor/+/state` — sensor values (parsed using default rules/hints)
- `<MQTT_TOPIC>/switch/+/state` — switch states (parsed as bool)

The sensor/switch name from the topic path is used as the InfluxDB field name. Type hints and renames apply based on this field name.

**Optional:** `M2I_ESPHOME_TRIM_SENSOR_PREFIX` can be set to trim a common prefix from sensor names. For example, if your ESPHome device publishes to `prefix/sensor/livingroom_temperature/state`, setting `M2I_ESPHOME_TRIM_SENSOR_PREFIX=livingroom_` would use `temperature` as the field name.

> [!NOTE]
> `DEDUPE_ON` is not supported in ESPHome mode.

### Timestamps

The timestamp for each InfluxDB point defaults to the time the message was processed.

If the message contains a top-level entry named `at`, `ts`, or `time`, that value will be parsed and used as the timestamp.

#### rtl_433 compatibility

To produce compatible timestamps from [rtl_433](https://github.com/merbanan/rtl_433), use the flag `-M time:iso:utc:tz` to output ISO 8601/RFC 3339 timestamps in UTC.

### Key Names, Canonicalization, and Nesting

Key names from JSON messages are canonicalized (lowercased) before being used as InfluxDB field or tag names. When referenced in environment variable names (for ISA mappings, type hints, and renames), key names are uppercased.

The following characters are replaced with `_` during canonicalization:
- ASCII control characters (code 0-31)
- `=`
- `#`

The following names are disallowed as keys: `time`, `_measurement`, `_field`.

Nested JSON objects and arrays are supported. Nested keys are joined with `.` (e.g., a key `temp` inside an object `sensor` becomes `sensor.temp`). Array elements are indexed numerically (e.g., `values.0`, `values.1`).

> [!NOTE]
> Avoid using periods (`.`) or mixed case in key names, as canonicalization could cause collisions.

#### Special JSON keys

In JSON mode, certain top-level keys receive special handling:

- `t` or `tags`: All entries within this object are treated as tags.
- `f` or `fields`: All entries within this object are treated as fields.
- Keys prefixed with `t_`: The remainder of the key (after `t_`) is treated as a tag name.
- Keys prefixed with `f_`: The remainder of the key (after `f_`) is treated as a field name.
- `at`, `ts`, or `time`: Parsed as the point's timestamp (see [Timestamps](#timestamps)).

## Configuration

All configuration is done through environment variables. These can be set directly or loaded from `.env` files via the `-env-files` argument.

### MQTT

| Variable | Description | Required | Default |
|---|---|---|---|
| `MQTT_SERVER` | MQTT broker URL. The `mqtt://` scheme prefix is added automatically if not present. | **Yes** | — |
| `MQTT_TOPIC` | MQTT topic to subscribe to (or topic prefix in ESPHome mode). | **Yes** | — |
| `MQTT_USER` | MQTT username for authentication. | No | — |
| `MQTT_PASS` | MQTT password for authentication. Also accepted as `MQTT_PASSWORD`. | No | — |
| `MQTT_QOS` | MQTT QoS level (0, 1, or 2). | No | `0` |
| `MQTT_CLIENT_ID` | MQTT client ID. If unset, a random ID is generated based on hostname. | No | Auto-generated |
| `MQTT_CLEAN_START` | Whether to start with a clean MQTT session (`true`/`false`). | No | `false` |
| `MQTT_KEEP_ALIVE_S` | MQTT keep-alive interval in seconds. | No | `20` |
| `MQTT_SESSION_EXPIRY_S` | MQTT session expiry interval in seconds. | No | `0` |

### Influx

| Variable | Description | Required | Default |
|---|---|---|---|
| `INFLUX_SERVER` | InfluxDB server URL. `http://` is added automatically if no scheme is present. | **Yes** | — |
| `INFLUX_USER` | InfluxDB username. | No | — |
| `INFLUX_PASS` | InfluxDB password. Also accepted as `INFLUX_PASSWORD`. | No | — |
| `INFLUX_BUCKET` | InfluxDB bucket (in InfluxDB 1.x, use `database/retention_policy` format). | **Yes** | — |
| `INFLUX_MEASUREMENT_NAME` | InfluxDB measurement name for all written points. | **Yes** | — |
| `INFLUX_TIMEOUT_S` | Timeout for InfluxDB write operations, in seconds. | No | `5` |
| `INFLUX_RETRIES` | Number of retry attempts for failed InfluxDB writes. | No | `2` |
| `INFLUX_TAGS` | Comma-separated `key=value` pairs of tags to add to every point (e.g. `host=server1,env=prod`). | No | — |

### Field/Tag Mapping

By default, `mqtt2influxdb` needs to determine whether each key in the message is an InfluxDB field or a tag. You can control this using environment variables of the form:

```text
M2I_<CANONICALIZED_NAME>_ISA=<field|tag|drop>
```

- `field`: Treat this key as an InfluxDB field.
- `tag`: Treat this key as an InfluxDB tag (value is always stored as a string).
- `drop`: Ignore this key entirely.

The canonicalized name is the uppercased, canonicalized version of the key. For nested keys, use `.` as the separator (e.g. `M2I_SENSOR.TEMP_ISA=field`).

If a key's field/tag status cannot be determined (no ISA hint and no prefix like `t_` or `f_`), the behavior is controlled by:

| Variable | Description | Default |
|---|---|---|
| `FIELDTAG_DETERMINATION_FAILURE` | Policy when a key's field/tag status is unknown. One of `ignore`, `log`, or `fatal`. | `log` |

### Data Type Hints

By default, values are written to InfluxDB with their JSON type. You can override this with environment variables of the form:

```text
M2I_<CANONICALIZED_FIELD_NAME>_TYPE=<int|float|double|string|bool>
```

- `int`: Parse or convert the value to an integer. Strings are parsed; floats are rounded; booleans become 0/1.
- `float` or `double`: Parse or convert the value to a 64-bit float. Strings are parsed; booleans become 0.0/1.0.
- `string`: Convert the value to its string representation.
- `bool`: Parse the value as a boolean. Accepted string values: `1`, `t`, `true`, `on`, `online` (true) and `0`, `f`, `false`, `off`, `offline` (false). Non-negative integers and floats are also accepted (0 is false, nonzero is true).

| Variable | Description | Default |
|---|---|---|
| `CAST_FAILURE` | Policy when a type cast fails. One of `ignore`, `log`, or `fatal`. | `log` |
| `DEFAULT_NUMBERS_TO_FLOAT` | When `true`, untyped numeric values from JSON are written as floats instead of their default JSON number type. | `false` |

### Renaming Fields and Tags

You can rename any field or tag after canonicalization using environment variables of the form:

```text
M2I_<CANONICALIZED_NAME>_RENAME=<new_name>
```

The new name is always lowercased. This applies in all modes (JSON, single-value, and ESPHome).

For example, to rename a field `temp_f` to `temperature`:

```text
M2I_TEMP_F_RENAME=temperature
```

### Heartbeat

Heartbeat support allows health monitoring via an outgoing HTTP GET heartbeat and/or an HTTP health check server. The heartbeat is triggered each time an MQTT message is received. If no message has been received within the liveness threshold, the health endpoint reports unhealthy and outgoing heartbeats are paused.

This is implemented using [`github.com/cdzombak/heartbeat`](https://github.com/cdzombak/heartbeat).

| Variable | Description |
|---|---|
| `HEARTBEAT_GET_URL` | URL to send periodic GET requests to as a heartbeat. Works well with [Uptime Kuma](https://github.com/louislam/uptime-kuma) push monitors. |
| `HEARTBEAT_INTERVAL_S` | Interval between heartbeat GET requests, in seconds. Required if `HEARTBEAT_GET_URL` is set. |
| `HEARTBEAT_THRESHOLD_S` | Maximum time since the last received MQTT message before the health check reports unhealthy and outgoing heartbeats are paused, in seconds. Required if any heartbeat feature is enabled. |
| `HEARTBEAT_HEALTH_PORT` | Port to serve a health check HTTP endpoint on. A GET to `/` returns `{"ok":true}` with HTTP 200 when healthy, or `{"ok":false}` with HTTP 503 when unhealthy. |

At least one of `HEARTBEAT_GET_URL` or `HEARTBEAT_HEALTH_PORT` must be set for heartbeat features to be enabled.

### Timestamp Parsing

If a message contains a timestamp field, it must be a string and will be parsed as an RFC 3339 timestamp.

> [!NOTE]  
> Timestamps from an Apple platform formatted as `NSISO8601DateFormatWithInternetDateTime` **will** be parsed correctly by this program.

In the future, multiple timestamp formats may be supported; see [#3](https://github.com/cdzombak/mqtt2influxdb/issues/3).

### Deduplication

In JSON mode, you can enable deduplication to skip messages that have already been processed. Set `DEDUPE_ON` to the name of a JSON key whose value should be used for deduplication. Any message whose value for this key has already been seen within the deduplication period will be skipped.

`DEDUPE_PERIOD_S` controls how long (in seconds) a seen value is remembered for deduplication. The default is `300` (5 minutes). After this period, a previously-seen value will be processed again.

> [!NOTE]
> `DEDUPE_ON` is only supported in JSON mode.

### Multiple .env files

You can load configuration from multiple `.env` files using the `-env-files` argument with a comma-separated list of file paths. Files are processed in order, with **earlier** files taking precedence over later ones. No `.env` file values will overwrite environment variables that are already set.

See [godotenv's precedence documentation](https://github.com/joho/godotenv?tab=readme-ov-file#precedence--conventions) for details on precedence behavior.

## License

This software is licensed under the LGPL-3.0 license. See [LICENSE](LICENSE) in this repo.

## Author

Chris Dzombak
- [dzombak.com](https://dzombak.com)
- [GitHub @cdzombak](https://www.github.com/cdzombak)
