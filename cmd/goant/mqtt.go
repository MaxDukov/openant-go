package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"

	"github.com/maxdukov/openant-go/devices"
)

const mqttUsage = `Usage: goant mqtt [arguments] <device name>

Stream device data to an MQTT broker. Every device data event is
published as JSON on <topic>/<device type>/<device id>; with
-topic-per-field each field gets its own subtopic instead.

Arguments:
`

// mqttArgs are the mqtt specific flags (openant subparsers/mqtt.py).
type mqttArgs struct {
	datatargetArgs
	host          string
	port          int
	user          string
	password      string
	clientID      string
	topic         string
	topicPerField bool
	deviceTopics  deviceTopicList
}

func runMqtt(args []string) error {
	var a mqttArgs
	fs := flag.NewFlagSet("mqtt", flag.ExitOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, mqttUsage)
		fs.PrintDefaults()
	}
	addDatatargetFlags(fs, &a.datatargetArgs)
	fs.StringVar(&a.host, "host", "localhost", "MQTT broker host")
	fs.IntVar(&a.port, "port", 1883, "MQTT broker port")
	fs.StringVar(&a.user, "user", "", "MQTT username")
	fs.StringVar(&a.password, "password", "", "MQTT password")
	fs.StringVar(&a.clientID, "client-id", "", "MQTT client id; default goant-<pid>")
	fs.StringVar(&a.topic, "topic", "openant", "base topic; events publish to <topic>/<device type>/<device id>")
	fs.BoolVar(&a.topicPerField, "topic-per-field", false, "publish every field on <topic>/<device>/<id>/<field>")
	fs.Var(&a.deviceTopics, "device-topic", "per-device base topic as type:id:topic (repeatable)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return fmt.Errorf("exactly one device name is required")
	}
	a.deviceType = fs.Arg(0)

	specs, err := deviceSpecs(&a.datatargetArgs)
	if err != nil {
		return err
	}
	p, err := newMqttPublisher(&a)
	if err != nil {
		return err
	}
	launchers, err := resolveDatatargetSticks(&a.datatargetArgs)
	if err != nil {
		return err
	}
	run, err := attachDevices(launchers, specs, p.publish)
	if err != nil {
		return err
	}
	defer p.close()
	return run()
}

// mqttPublisher publishes device data events as JSON payloads.
type mqttPublisher struct {
	client        mqtt.Client
	base          string
	topicPerField bool
	deviceTopics  deviceTopicList
	verbose       bool
}

func newMqttPublisher(a *mqttArgs) (*mqttPublisher, error) {
	clientID := a.clientID
	if clientID == "" {
		clientID = fmt.Sprintf("goant-%d", os.Getpid())
	}
	opts := mqtt.NewClientOptions().
		AddBroker(fmt.Sprintf("tcp://%s:%d", a.host, a.port)).
		SetClientID(clientID).
		SetAutoReconnect(true).
		SetConnectRetry(true).
		SetConnectRetryInterval(5 * time.Second)
	if a.user != "" || a.password != "" {
		opts.SetUsername(a.user)
		opts.SetPassword(a.password)
	}
	client := mqtt.NewClient(opts)
	if token := client.Connect(); !token.WaitTimeout(15 * time.Second) {
		return nil, fmt.Errorf("timeout connecting to %s:%d", a.host, a.port)
	} else if err := token.Error(); err != nil {
		return nil, fmt.Errorf("connect to %s:%d: %w", a.host, a.port, err)
	}
	return &mqttPublisher{
		client:        client,
		base:          strings.Trim(a.topic, "/"),
		topicPerField: a.topicPerField,
		deviceTopics:  a.deviceTopics,
		verbose:       a.verbose,
	}, nil
}

// publish serialises and sends one device data event.
func (p *mqttPublisher) publish(spec deviceSpec, page int, name string, data devices.DeviceData) {
	base := p.deviceTopics.baseFor(spec.dtype, spec.id)
	if base == "" {
		base = fmt.Sprintf("%s/%s/%d", p.base, escapeTopic(spec.name), spec.id)
	}
	if p.topicPerField {
		// openant publishes each field value on its own subtopic.
		for _, f := range devices.InfluxFields(data) {
			payload, err := json.Marshal(f.Value)
			if err != nil {
				continue
			}
			p.client.Publish(base+"/"+escapeTopic(f.Key), 0, false, string(payload))
		}
		return
	}
	payload, err := json.Marshal(dataPayload(data))
	if err != nil {
		return
	}
	p.client.Publish(base, 0, false, string(payload))
	if p.verbose {
		fmt.Printf("[mqtt] %s page %d %s -> %s: %s\n", spec.name, page, name, base, payload)
	}
}

// dataPayload builds the JSON document of one event (the "_type" field
// mirrors openant's payload marker).
func dataPayload(data devices.DeviceData) map[string]any {
	out := map[string]any{"_type": devices.InfluxMeasurement(data)}
	for _, f := range devices.InfluxFields(data) {
		out[f.Key] = f.Value
	}
	return out
}

func (p *mqttPublisher) close() { p.client.Disconnect(250) }

// escapeTopic keeps topic levels slash-free.
func escapeTopic(s string) string { return strings.ReplaceAll(s, "/", "-") }

// deviceTopicList is a repeatable -device-topic flag value.
type deviceTopicList []deviceTopic

type deviceTopic struct {
	typeName string
	id       int
	topic    string
}

// String implements flag.Value.
func (l *deviceTopicList) String() string {
	if l == nil {
		return ""
	}
	parts := make([]string, len(*l))
	for i, dt := range *l {
		parts[i] = fmt.Sprintf("%s:%d:%s", dt.typeName, dt.id, dt.topic)
	}
	return strings.Join(parts, ", ")
}

// Set parses type:id:topic.
func (l *deviceTopicList) Set(v string) error {
	typeName, rest, ok := strings.Cut(v, ":")
	if !ok {
		return fmt.Errorf("-device-topic wants type:id:topic, got %q", v)
	}
	idStr, topic, ok := strings.Cut(rest, ":")
	if !ok {
		return fmt.Errorf("-device-topic wants type:id:topic, got %q", v)
	}
	var id int
	if _, err := fmt.Sscanf(idStr, "%d", &id); err != nil {
		return fmt.Errorf("-device-topic: bad id in %q", v)
	}
	*l = append(*l, deviceTopic{typeName: typeName, id: id, topic: strings.Trim(topic, "/")})
	return nil
}

// baseFor returns the override topic for a device, if any.
func (l deviceTopicList) baseFor(dtype devices.DeviceType, id int) string {
	for _, dt := range l {
		if dt.id == id && (dt.typeName == dtype.String() || dt.typeName == fmt.Sprint(int(dtype))) {
			return dt.topic
		}
	}
	return ""
}
