package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"maps"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/avast/retry-go"
	"github.com/eclipse/paho.golang/autopaho"
	"github.com/eclipse/paho.golang/paho"
	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	"github.com/influxdata/influxdb-client-go/v2/api"
	"github.com/joho/godotenv"
)

type MsgMode int

const (
	MsgModeJSON MsgMode = iota
	MsgModeSingle
	MsgModeESPHome
)

type MqttConfig struct {
	Server               *url.URL
	Topic                string
	User                 string
	Password             string
	ClientID             string
	QoS                  byte
	SessionExpirySeconds uint32
	KeepAliveSeconds     uint16
	CleanStart           bool
}

type InfluxConfig struct {
	Server          *url.URL
	User            string
	Password        string
	Bucket          string
	MeasurementName string
	Timeout         time.Duration
	Retries         uint
	Tags            map[string]string
}

type HeartbeatConfig struct {
	GetURL     string
	Interval   time.Duration
	Threshold  time.Duration
	HealthPort uint16
}

type Config struct {
	Mode      MsgMode
	Mqtt      *MqttConfig
	Influx    *InfluxConfig
	Heartbeat *HeartbeatConfig
}

var version = "<dev>"

//goland:noinspection GoUnhandledErrorResult
func usage() {
	fmt.Fprintf(os.Stderr, "mqtt2influxdb %s\n", version)
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "An opinionated and intentionally scope-limited MQTT to InfluxDB bridge.")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Usage:")
	fmt.Fprintln(os.Stderr, "  mqtt2influxdb [options]")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Options:")
	flag.PrintDefaults()
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Files given in -env-files are processed in order, with *earlier* files taking "+
		"precedence over later ones. No .env files will overwrite currently-set environment variables. "+
		"For details on precedence, see:")
	fmt.Fprintln(os.Stderr, "  https://github.com/joho/godotenv?tab=readme-ov-file#precedence--conventions")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Note that configuration is achieved entirely through environment variables. "+
		"See the README for details:")
	fmt.Fprintln(os.Stderr, "  https://www.github.com/cdzombak/mqtt2influxdb/blob/main/README.md")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Available environment variables:")
	fmt.Fprintln(os.Stderr, "  MQTT_SERVER")
	fmt.Fprintln(os.Stderr, "  MQTT_TOPIC")
	fmt.Fprintln(os.Stderr, "  MQTT_USER")
	fmt.Fprintln(os.Stderr, "  MQTT_PASS")
	fmt.Fprintln(os.Stderr, "  MQTT_QOS")
	fmt.Fprintln(os.Stderr, "  MQTT_CLIENT_ID")
	fmt.Fprintln(os.Stderr, "  MQTT_CLEAN_START")
	fmt.Fprintln(os.Stderr, "  MQTT_KEEP_ALIVE_S")
	fmt.Fprintln(os.Stderr, "  MQTT_SESSION_EXPIRY_S")
	fmt.Fprintln(os.Stderr, "  --")
	fmt.Fprintln(os.Stderr, "  INFLUX_SERVER")
	fmt.Fprintln(os.Stderr, "  INFLUX_USER")
	fmt.Fprintln(os.Stderr, "  INFLUX_PASS")
	fmt.Fprintln(os.Stderr, "  INFLUX_BUCKET")
	fmt.Fprintln(os.Stderr, "  INFLUX_MEASUREMENT_NAME")
	fmt.Fprintln(os.Stderr, "  INFLUX_TIMEOUT_S")
	fmt.Fprintln(os.Stderr, "  INFLUX_RETRIES")
	fmt.Fprintln(os.Stderr, "  INFLUX_TAGS")
	fmt.Fprintln(os.Stderr, "  --")
	fmt.Fprintln(os.Stderr, "  M2I_MODE=<json|single|esphome>")
	fmt.Fprintln(os.Stderr, "  M2I_<canonicalized-name>_ISA=<field|tag|drop>")
	fmt.Fprintln(os.Stderr, "  M2I_SINGLE_FIELDNAME")
	fmt.Fprintln(os.Stderr, "  M2I_<canonicalized-field-name>_TYPE=<int|float|double|string|bool>")
	fmt.Fprintln(os.Stderr, "  DEFAULT_NUMBERS_TO_FLOAT=<true|false>")
	fmt.Fprintln(os.Stderr, "  FIELDTAG_DETERMINATION_FAILURE=<ignore|log|fatal>")
	fmt.Fprintln(os.Stderr, "  CAST_FAILURE=<ignore|log|fatal>")
	fmt.Fprintln(os.Stderr, "  --")
	fmt.Fprintln(os.Stderr, "  HEARTBEAT_GET_URL")
	fmt.Fprintln(os.Stderr, "  HEARTBEAT_INTERVAL_S")
	fmt.Fprintln(os.Stderr, "  HEARTBEAT_THRESHOLD_S")
	fmt.Fprintln(os.Stderr, "  HEALTH_PORT")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "mqtt2influxdb is written by Chris Dzombak <https://www.dzombak.com> and licensed under the LGPL-3.0 license.")
	fmt.Fprintln(os.Stderr, "🌐 https://www.github.com/cdzombak/mqtt2influxdb")
}

func main() {
	envFiles := flag.String("env-files", "", "Comma-separated list of .env files to load. Earlier envs take precedence over later ones.")
	strict := flag.Bool("strict", false, "Exit on invalid messages, unexpected topics, casting failures, or other unexpected conditions.")
	printVersion := flag.Bool("version", false, "Print version and exit.")
	flag.Usage = usage
	flag.Parse()

	if *printVersion {
		fmt.Printf("mqtt2influxdb %s\n", version)
		os.Exit(0)
	}

	if *envFiles != "" {
		envFileList := strings.Split(*envFiles, ",")
		err := godotenv.Load(envFileList...)
		if err != nil {
			log.Fatalf("error processing -env-files: %s", err)
		}
	}
	SetStrictEnvPolicies(*strict)

	cfg := Config{
		Mode: MsgModeJSON,
		Mqtt: &MqttConfig{
			Topic:            os.Getenv("MQTT_TOPIC"),
			User:             os.Getenv("MQTT_USER"),
			Password:         os.Getenv("MQTT_PASS"),
			ClientID:         os.Getenv("MQTT_CLIENT_ID"),
			KeepAliveSeconds: 20,
		},
		Influx: &InfluxConfig{
			User:            os.Getenv("INFLUX_USER"),
			Password:        os.Getenv("INFLUX_PASS"),
			Bucket:          os.Getenv("INFLUX_BUCKET"),
			MeasurementName: os.Getenv("INFLUX_MEASUREMENT_NAME"),
			Timeout:         5 * time.Second,
			Retries:         2,
			Tags:            make(map[string]string),
		},
		Heartbeat: &HeartbeatConfig{
			GetURL: os.Getenv("HEARTBEAT_GET_URL"),
		},
	}

	if os.Getenv("M2I_MODE") != "" {
		mode := strings.ToLower(os.Getenv("M2I_MODE"))
		if mode == "json" {
			cfg.Mode = MsgModeJSON
		} else if mode == "single" {
			cfg.Mode = MsgModeSingle
		} else if mode == "esphome" {
			cfg.Mode = MsgModeESPHome
		} else {
			log.Fatalf("invalid M2I_MODE '%s'; must be 'json' or 'single'", mode)
		}
	}
	if cfg.Mode == MsgModeSingle && os.Getenv("M2I_SINGLE_FIELDNAME") == "" {
		log.Fatalf("M2I_SINGLE_FIELDNAME must be set when M2I_MODE=single")
	}

	if cfg.Mqtt.ClientID == "" {
		hostname, err := os.Hostname()
		if err != nil {
			log.Fatalf("failed to get hostname: %s", err)
		}
		clientID := fmt.Sprintf("%s-%s", hostname, RandAlnumString(8))
		log.Printf("generated client ID: %s", clientID)
	}
	// check for client ID conflicts with other instances on this network
	// ( tracked at https://github.com/cdzombak/mqtt2influxdb/issues/4 )

	if cfg.Mqtt.Topic == "" {
		log.Fatalf("MQTT_TOPIC must be set")
	}
	if cfg.Mqtt.Password == "" {
		cfg.Mqtt.Password = os.Getenv("MQTT_PASSWORD")
	}
	if os.Getenv("MQTT_SERVER") == "" {
		log.Fatalf("MQTT_SERVER must be set")
	} else {
		urlString := os.Getenv("MQTT_SERVER")
		if !strings.HasPrefix(strings.ToLower(urlString), "mqtt://") {
			urlString = "mqtt://" + urlString
		}
		serverURL, err := url.Parse(urlString)
		if err != nil {
			log.Fatalf("failed to parse MQTT server URL '%s': %s", urlString, err)
		}
		cfg.Mqtt.Server = serverURL
	}
	if os.Getenv("MQTT_QOS") != "" {
		qos, err := strconv.ParseUint(os.Getenv("MQTT_QOS"), 10, 8)
		if err != nil {
			log.Fatalf("failed to parse MQTT_QOS '%s': %s", os.Getenv("MQTT_QOS"), err)
		}
		cfg.Mqtt.QoS = byte(qos)
	}
	if os.Getenv("MQTT_SESSION_EXPIRY_S") != "" {
		sessionExpiry, err := strconv.ParseUint(os.Getenv("MQTT_SESSION_EXPIRY_S"), 10, 32)
		if err != nil {
			log.Fatalf("failed to parse MQTT_SESSION_EXPIRY_S '%s': %s", os.Getenv("MQTT_SESSION_EXPIRY_S"), err)
		}
		cfg.Mqtt.SessionExpirySeconds = uint32(sessionExpiry)
	}
	if os.Getenv("MQTT_KEEP_ALIVE_S") != "" {
		keepalive, err := strconv.ParseUint(os.Getenv("MQTT_KEEP_ALIVE_S"), 10, 16)
		if err != nil {
			log.Fatalf("failed to parse MQTT_KEEP_ALIVE_S '%s': %s", os.Getenv("MQTT_KEEP_ALIVE_S"), err)
		}
		cfg.Mqtt.KeepAliveSeconds = uint16(keepalive)
	}
	if os.Getenv("MQTT_CLEAN_START") != "" {
		cleanStart, err := strconv.ParseBool(os.Getenv("MQTT_CLEAN_START"))
		if err != nil {
			log.Fatalf("failed to parse MQTT_CLEAN_START '%s': %s", os.Getenv("MQTT_CLEAN_START"), err)
		}
		cfg.Mqtt.CleanStart = cleanStart
	}

	if cfg.Influx.Password == "" {
		cfg.Influx.Password = os.Getenv("INFLUX_PASSWORD")
	}
	if os.Getenv("INFLUX_SERVER") == "" {
		log.Fatalf("INFLUX_SERVER must be set")
	} else {
		urlString := os.Getenv("INFLUX_SERVER")
		if !strings.HasPrefix(strings.ToLower(urlString), "http") {
			//goland:noinspection HttpUrlsUsage
			urlString = "http://" + urlString
		}
		serverURL, err := url.Parse(urlString)
		if err != nil {
			log.Fatalf("failed to parse Influx server URL '%s': %s", serverURL, err)
		}
		cfg.Influx.Server = serverURL
	}
	if cfg.Influx.Bucket == "" {
		log.Fatalf("INFLUX_BUCKET must be set")
	}
	if cfg.Influx.MeasurementName == "" {
		log.Fatalf("INFLUX_MEASUREMENT_NAME must be set")
	}
	if os.Getenv("INFLUX_TIMEOUT_S") != "" {
		timeout, err := strconv.ParseUint(os.Getenv("INFLUX_TIMEOUT_S"), 10, 64)
		if err != nil {
			log.Fatalf("failed to parse INFLUX_TIMEOUT_S '%s': %s", os.Getenv("INFLUX_TIMEOUT_S"), err)
		}
		cfg.Influx.Timeout = time.Duration(timeout) * time.Second
	}
	if os.Getenv("INFLUX_RETRIES") != "" {
		retries, err := strconv.ParseUint(os.Getenv("INFLUX_RETRIES"), 10, 64)
		if err != nil {
			log.Fatalf("failed to parse INFLUX_RETRIES '%s': %s", os.Getenv("INFLUX_RETRIES"), err)
		}
		cfg.Influx.Retries = uint(retries)
	}
	if os.Getenv("INFLUX_TAGS") != "" {
		tags := strings.Split(os.Getenv("INFLUX_TAGS"), ",")
		for _, tag := range tags {
			parts := strings.SplitN(tag, "=", 2)
			if len(parts) != 2 {
				log.Fatalf("failed to parse Influx tag '%s'", tag)
			}
			cfg.Influx.Tags[parts[0]] = parts[1]
		}
	}

	if os.Getenv("HEARTBEAT_INTERVAL_S") != "" {
		interval, err := strconv.ParseUint(os.Getenv("HEARTBEAT_INTERVAL_S"), 10, 64)
		if err != nil {
			log.Fatalf("failed to parse HEARTBEAT_INTERVAL_S '%s': %s", os.Getenv("HEARTBEAT_INTERVAL_S"), err)
		}
		cfg.Heartbeat.Interval = time.Duration(interval) * time.Second
	}
	if os.Getenv("HEARTBEAT_THRESHOLD_S") != "" {
		threshold, err := strconv.ParseUint(os.Getenv("HEARTBEAT_THRESHOLD_S"), 10, 64)
		if err != nil {
			log.Fatalf("failed to parse HEARTBEAT_THRESHOLD_S '%s': %s", os.Getenv("HEARTBEAT_THRESHOLD_S"), err)
		}
		cfg.Heartbeat.Threshold = time.Duration(threshold) * time.Second
	}
	if os.Getenv("HEARTBEAT_HEALTH_PORT") != "" {
		port, err := strconv.ParseUint(os.Getenv("HEARTBEAT_HEALTH_PORT"), 10, 16)
		if err != nil {
			log.Fatalf("failed to parse HEARTBEAT_HEALTH_PORT '%s': %s", os.Getenv("HEARTBEAT_HEALTH_PORT"), err)
		}
		cfg.Heartbeat.HealthPort = uint16(port)
	}

	ctx := context.Background()
	ctx = WithStrictLogger(ctx, *strict)

	if err := Main(ctx, cfg); err != nil {
		log.Fatalf("error: %s", err)
	}
}

func Main(ctx context.Context, cfg Config) error {
	strictLog := StrictLoggerFromContext(ctx)
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	influxWriter := newInfluxWriter(ctx, cfg.Influx)

	// heartbeat tracked at: https://github.com/cdzombak/mqtt2influxdb/issues/2
	if cfg.Heartbeat.GetURL != "" || cfg.Heartbeat.HealthPort != 0 {
		log.Fatalf("heartbeat is not yet implemented")
	}

	receivedMessages := make(chan paho.PublishReceived)
	go func(ctx context.Context) {
		for {
			select {
			case <-ctx.Done():
			case rm := <-receivedMessages:
				if cfg.Mode == MsgModeJSON {
					var msg map[string]any
					if err := json.Unmarshal(rm.Packet.Payload, &msg); err != nil {
						strictLog(fmt.Sprintf("failed to unmarshal message: %s\n(content: '%s')", err, rm.Packet.Payload))
						continue
					}
					go handleMessage(ctx, cfg, influxWriter, msg)
				} else if cfg.Mode == MsgModeESPHome {
					go handleESPHomeMsg(ctx, cfg, influxWriter, rm)
				} else if cfg.Mode == MsgModeSingle {
					go handleSinglePayload(ctx, cfg, influxWriter, rm.Packet.Payload)
				} else {
					panic(fmt.Sprintf("unknown message mode: %d", cfg.Mode))
				}
			}
		}
	}(ctx)

	cliCfg := autopaho.ClientConfig{
		ServerUrls:                    []*url.URL{cfg.Mqtt.Server},
		ConnectUsername:               cfg.Mqtt.User,
		ConnectPassword:               []byte(cfg.Mqtt.Password),
		KeepAlive:                     cfg.Mqtt.KeepAliveSeconds,
		CleanStartOnInitialConnection: cfg.Mqtt.CleanStart,
		SessionExpiryInterval:         cfg.Mqtt.SessionExpirySeconds,
		OnConnectionUp: func(cm *autopaho.ConnectionManager, connAck *paho.Connack) {
			// Subscribing in the OnConnectionUp callback is recommended (ensures the subscription is reestablished if the connection drops)
			log.Printf("connected to '%s'", cfg.Mqtt.Server)

			subs := []paho.SubscribeOptions{{Topic: cfg.Mqtt.Topic, QoS: cfg.Mqtt.QoS}}
			if cfg.Mode == MsgModeESPHome {
				subs = []paho.SubscribeOptions{
					{Topic: cfg.Mqtt.Topic + "/binary_sensor/+/state", QoS: cfg.Mqtt.QoS},
					{Topic: cfg.Mqtt.Topic + "/sensor/+/state", QoS: cfg.Mqtt.QoS},
					{Topic: cfg.Mqtt.Topic + "/switch/+/state", QoS: cfg.Mqtt.QoS},
					{Topic: cfg.Mqtt.Topic + "/status", QoS: cfg.Mqtt.QoS},
				}
			}

			if _, err := cm.Subscribe(ctx, &paho.Subscribe{Subscriptions: subs}); err != nil {
				log.Fatalf("failed to subscribe to topic '%s': %s", cfg.Mqtt.Topic, err)
			}
			log.Printf("subscribed to '%s'", cfg.Mqtt.Topic)
		},
		OnConnectError: func(err error) {
			log.Printf("error while attempting MQTT connection: %s", err)
		},
		// eclipse/paho.golang/paho provides base mqtt functionality, the below config will be passed in for each connection
		ClientConfig: paho.ClientConfig{
			ClientID: cfg.Mqtt.ClientID,
			OnPublishReceived: []func(paho.PublishReceived) (bool, error){
				func(pr paho.PublishReceived) (bool, error) {
					receivedMessages <- pr
					return true, nil
				}},
			OnClientError: func(err error) {
				log.Fatalf("MQTT client error: %s", err)
			},
			OnServerDisconnect: func(d *paho.Disconnect) {
				if d.Properties != nil {
					log.Fatalf("MQTT server requested disconnect: %s\n", d.Properties.ReasonString)
				} else {
					log.Fatalf("MQTT server requested disconnect; reason code: %d\n", d.ReasonCode)
				}
			},
		},
	}
	c, err := autopaho.NewConnection(ctx, cliCfg)
	if err != nil {
		log.Fatalf("failed to start MQTT connection: %s", err)
	}

	<-c.Done()
	log.Println("signal caught - exiting")
	return nil
}

func handleMessage(ctx context.Context, cfg Config, influxWriter api.WriteAPIBlocking, msg map[string]any) {
	strictLog := StrictLoggerFromContext(ctx)

	parsed, err := MsgParse(msg)
	if err != nil {
		ForEachError(err, func(err error) {
			if errors.Is(err, ErrCastFailure) {
				OnCastFailure(err)
			} else if errors.Is(err, ErrFieldTagDeterminationFailure) {
				OnFieldTagDeterminationFailure(err)
			} else {
				strictLog(fmt.Sprintf("failed to parse message: %s", err))
			}
		})
		if !IsPartialFailure(err) {
			return
		}
	}
	maps.Copy(parsed.Tags, cfg.Influx.Tags)

	writeParseResult(ctx, cfg, influxWriter, parsed)
}

func handleSinglePayload(ctx context.Context, cfg Config, influxWriter api.WriteAPIBlocking, payload []byte) {
	strictLog := StrictLoggerFromContext(ctx)

	parsed, err := SinglePayloadParse(os.Getenv("M2I_SINGLE_FIELDNAME"), string(payload))
	if err != nil {
		ForEachError(err, func(err error) {
			if errors.Is(err, ErrCastFailure) {
				OnCastFailure(err)
			} else if errors.Is(err, ErrFieldTagDeterminationFailure) {
				OnFieldTagDeterminationFailure(err)
			} else {
				strictLog(fmt.Sprintf("failed to parse message: %s", err))
			}
		})
		return
	}
	maps.Copy(parsed.Tags, cfg.Influx.Tags)

	writeParseResult(ctx, cfg, influxWriter, parsed)
}

func handleESPHomeMsg(ctx context.Context, cfg Config, influxWriter api.WriteAPIBlocking, msg paho.PublishReceived) {
	strictLog := StrictLoggerFromContext(ctx)

	// this will be a single-style message on one of these topics:
	// PREFIX/status (bool)
	// PREFIX/binary_sensor/SENSOR_NAME/state (bool)
	// PREFIX/switch/SWITCH_NAME/state (bool)
	// PREFIX/sensor/SENSOR_NAME/state (use default parsing rules/hints)
	parts := strings.Split(strings.TrimPrefix(msg.Packet.Topic, cfg.Mqtt.Topic+"/"), "/")
	espTrimSensorPrefix := os.Getenv("M2I_ESPHOME_TRIM_SENSOR_PREFIX")

	var (
		parsed ParseResult
		err    error
	)

	if parts[0] == "status" {
		if len(parts) != 1 {
			strictLog(fmt.Sprintf("/status topic has wrong number of parts: %s", msg.Packet.Topic))
			return
		}
		if err := os.Setenv("M2I_STATUS_TYPE", "bool"); err != nil {
			panic(fmt.Sprintf("os.Setenv failed: %s", err.Error()))
		}
		parsed, err = SinglePayloadParse("status", string(msg.Packet.Payload))
	} else if parts[0] == "binary_sensor" || parts[0] == "sensor" || parts[0] == "switch" {
		if len(parts) != 3 {
			strictLog(fmt.Sprintf("topic has wrong number of parts: %s", msg.Packet.Topic))
			return
		}
		fName := parts[1]
		if espTrimSensorPrefix != "" {
			fName = strings.TrimPrefix(fName, espTrimSensorPrefix)
		}
		if parts[0] == "binary_sensor" || parts[0] == "switch" {
			if err := os.Setenv(
				fmt.Sprintf("M2I_%s_TYPE", strings.ToUpper(fName)), "bool"); err != nil {
				panic(fmt.Sprintf("os.Setenv failed: %s", err.Error()))
			}
		}
		parsed, err = SinglePayloadParse(fName, string(msg.Packet.Payload))
	} else {
		strictLog(fmt.Sprintf("unexpected topic: %s", msg.Packet.Topic))
		return
	}

	if err != nil {
		ForEachError(err, func(err error) {
			if errors.Is(err, ErrCastFailure) {
				OnCastFailure(err)
			} else if errors.Is(err, ErrFieldTagDeterminationFailure) {
				OnFieldTagDeterminationFailure(err)
			} else {
				strictLog(fmt.Sprintf("failed to parse message: %s", err))
			}
		})
		return
	}
	maps.Copy(parsed.Tags, cfg.Influx.Tags)
	writeParseResult(ctx, cfg, influxWriter, parsed)
}

func newInfluxWriter(ctx context.Context, cfg *InfluxConfig) api.WriteAPIBlocking {
	influxAuthStr := ""
	if cfg.User != "" || cfg.Password != "" {
		influxAuthStr = fmt.Sprintf("%s:%s", cfg.User, cfg.Password)
	}
	influxClient := influxdb2.NewClient(cfg.Server.String(), influxAuthStr)
	ctx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()
	health, err := influxClient.Health(ctx)
	if err != nil {
		log.Fatalf("failed to check InfluxDB health: %v", err)
	}
	if health.Status != "pass" {
		log.Fatalf("InfluxDB did not pass health check: status %s; message '%s'", health.Status, *health.Message)
	}
	return influxClient.WriteAPIBlocking("", cfg.Bucket)
}

func writeParseResult(ctx context.Context, cfg Config, influxWriter api.WriteAPIBlocking, parsed ParseResult) {
	point := influxdb2.NewPoint(
		cfg.Influx.MeasurementName,
		parsed.Tags,
		parsed.Fields,
		parsed.Timestamp,
	)

	if err := retry.Do(
		func() error {
			ctx, cancel := context.WithTimeout(ctx, cfg.Influx.Timeout)
			defer cancel()
			return influxWriter.WritePoint(ctx, point)
		},
		retry.Attempts(cfg.Influx.Retries),
	); err != nil {
		log.Printf("failed to write to Influx: %s", err.Error())
	}
}
