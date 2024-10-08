# `mqtt2influxdb` 

An opinionated and intentionally scope-limited MQTT to InfluxDB bridge.

TODO(cdzombak): finish readme

## Goals & Features

- Get simple JSON data from MQTT to InfluxDB
- Straightforward; easy to learn about, configure, and run
- Take advantage of orchestration features from e.g. systemd or Docker Compose
- Allow health monitoring via an outgoing heartbeat, HTTP GET, and/or Docker's health check

## Non-goals

### Multiple topics/subscriptions

tk
orchestration, retries, etc

### Infinitely flexible schema/transformations

tk
can't necessarily work with every message format
can't necessarily work with weird key names or most nested structures
doesn't support non-JSON

## Usage

tk

### Arguments

tk

- `-env-files`
- `-strict`
- `-help`
- `-version`

note, strict overrides `FIELDTAG_DETERMINATION_FAILURE` and `CAST_FAILURE`

### Docker

tk

### Timestamps

The timestamp for each InfluxDB point defaults to the time the message was processed.

If the message contains a top-level entry named `at`, `ts`, or `time`, that value will be parsed and used as the timestamp.

### Key Names, Canonicalization, and Nesting

tk

per The Open Group Base Specifications https://pubs.opengroup.org/onlinepubs/000095399/basedefs/xbd_chap08.html , any char other than NUL or `=` is allowed in an env var name.

Influx disallows(ish) ( https://docs.influxdata.com/influxdb/v1/write_protocols/line_protocol_reference/#special-characters ): #, and the names time, _field, _measurement.

For sanity I also disallow ASCII control chars (character code 0-31). And I recommend you don't use 
periods (.) or mixed case as that could cause collisions.

lowercased except in env vars, when they're all uppercased.

## Configuration

tk

### MQTT

tk

- `MQTT_SERVER`
- `MQTT_TOPIC`
- `MQTT_USER`
- `MQTT_PASS`
- `MQTT_QOS`
- `MQTT_CLIENT_ID`
- `MQTT_CLEAN_START`
- `MQTT_KEEP_ALIVE_S`
- `MQTT_SESSION_EXPIRY_S`

### Influx

tk

- `INFLUX_SERVER`
- `INFLUX_USER`
- `INFLUX_PASS`
- `INFLUX_BUCKET`
- `INFLUX_MEASUREMENT_NAME`
- `INFLUX_TIMEOUT_S`
- `INFLUX_RETRIES`
- `INFLUX_TAGS`

### Field/Tag Mapping

tk

Typically, we auto TK

Format: `M2I_<canonicalized-name>_ISA=<field|tag>`

- `FIELDTAG_DETERMINATION_FAILURE`: `ignore`, `log`, or `fatal`

### Data Type Hints

tk

Format: `M2I_<canonicalized-field-name>_TYPE=<int|float|double|string|bool>`

- `CAST_FAILURE`: `ignore`, `log`, or `fatal`
- `DEFAULT_NUMBERS_TO_FLOAT`: `true` or `false`

### Heartbeat

tk

- `HEARTBEAT_GET_URL`
- `HEARTBEAT_INTERVAL_S`
- `HEARTBEAT_THRESHOLD_S`
- `HEALTH_PORT`

### Timestamp Parsing

If a message contains a timestamp field, it must be a string and will be parsed as an RFC3339 timestamp.

> [!NOTE]  
> Timestamps from an Apple platform formatted as `NSISO8601DateFormatWithInternetDateTime` **will** be parsed correctly by this program.

In the future, multiple timestamp formats may be supported; see #TK.

### Multiple .env files

tk

## License

This software is licensed under the LGPL-3.0 license. See [LICENSE](LICENSE) in this repo.

## Author

Chris Dzombak
- [dzombak.com](https://dzombak.com)
- [GitHub @cdzombak](https://www.github.com/cdzombak)
