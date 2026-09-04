package main

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/maxdukov/openant-go/devices"
)

const influxUsage = `Usage: goant influx [arguments] <device name>

Stream device data to an InfluxDB instance using the line protocol.

With a -token the v2 API is used (POST <url>/api/v2/write with org,
bucket and precision=ns query parameters); otherwise the v1 API is used
(POST <url>/write with db, u and p query parameters).

Arguments:
`

// influxArgs are the influx specific flags (openant subparsers/influx.py).
type influxArgs struct {
	datatargetArgs
	url      string
	host     string
	port     int
	token    string
	org      string
	bucket   string
	db       string
	username string
	password string
	interval time.Duration
}

func runInflux(args []string) error {
	var a influxArgs
	fs := flag.NewFlagSet("influx", flag.ExitOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, influxUsage)
		fs.PrintDefaults()
	}
	addDatatargetFlags(fs, &a.datatargetArgs)
	fs.StringVar(&a.url, "url", "", "InfluxDB base URL (e.g. http://localhost:8086); default http://<host>:<port>")
	fs.StringVar(&a.host, "host", "localhost", "InfluxDB host (ignored with -url)")
	fs.IntVar(&a.port, "port", 8086, "InfluxDB port (ignored with -url)")
	fs.StringVar(&a.token, "token", "", "InfluxDB v2 API token; empty selects the v1 API")
	fs.StringVar(&a.org, "org", "my-org", "InfluxDB v2 organisation")
	fs.StringVar(&a.bucket, "bucket", "my-bucket", "InfluxDB v2 bucket")
	fs.StringVar(&a.db, "db", "", "InfluxDB v1 database")
	fs.StringVar(&a.username, "username", "", "InfluxDB v1 username")
	fs.StringVar(&a.password, "password", "", "InfluxDB v1 password")
	fs.DurationVar(&a.interval, "interval", 0, "batch flush interval (default 0: write every point immediately)")
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
	w, err := newInfluxWriter(&a)
	if err != nil {
		return err
	}
	launchers, err := resolveDatatargetSticks(&a.datatargetArgs)
	if err != nil {
		return err
	}
	run, err := attachDevices(launchers, specs, w.write)
	if err != nil {
		return err
	}
	defer w.close()
	return run()
}

// influxWriter buffers line protocol points and flushes them over HTTP.
type influxWriter struct {
	client   *http.Client
	writeURL string
	token    string
	auth     string // v1 basic auth header value
	buf      bytes.Buffer
	mu       sync.Mutex
	flush    time.Duration
	stop     chan struct{}
	verbose  bool
	tags     string // static tag portion of every line
}

func newInfluxWriter(a *influxArgs) (*influxWriter, error) {
	base := a.url
	if base == "" {
		base = fmt.Sprintf("http://%s:%d", a.host, a.port)
	}
	base = strings.TrimRight(base, "/")
	w := &influxWriter{
		client:  &http.Client{Timeout: 10 * time.Second},
		token:   a.token,
		flush:   a.interval,
		stop:    make(chan struct{}),
		verbose: a.verbose,
	}
	host, _ := os.Hostname()
	var tags []string
	if host != "" {
		tags = append(tags, "host="+escapeTag(host))
	}
	tags = append(tags, "uuid="+escapeTag(sessionUUID()))
	w.tags = strings.Join(tags, ",")
	if a.token != "" {
		w.writeURL = fmt.Sprintf("%s/api/v2/write?org=%s&bucket=%s&precision=ns", base, escapeQuery(a.org), escapeQuery(a.bucket))
	} else {
		if a.db == "" {
			return nil, fmt.Errorf("-db is required without -token (v1 API)")
		}
		w.writeURL = fmt.Sprintf("%s/write?db=%s&precision=ns", base, escapeQuery(a.db))
		if a.username != "" || a.password != "" {
			w.auth = "Basic " + basicAuth(a.username, a.password)
		}
	}
	if w.flush > 0 {
		go w.flushLoop()
	}
	return w, nil
}

// flushLoop periodically pushes the buffered points.
func (w *influxWriter) flushLoop() {
	t := time.NewTicker(w.flush)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			w.mu.Lock()
			payload := append([]byte(nil), w.buf.Bytes()...)
			w.buf.Reset()
			w.mu.Unlock()
			if len(payload) > 0 {
				w.post(payload)
			}
		case <-w.stop:
			return
		}
	}
}

// write serialises one device data event into the buffer.
func (w *influxWriter) write(spec deviceSpec, page int, name string, data devices.DeviceData) {
	line := buildLine(devices.InfluxMeasurement(data), w.tags, specFields(spec), devices.InfluxFields(data), time.Now())
	if line == "" {
		return
	}
	w.mu.Lock()
	w.buf.WriteString(line)
	w.buf.WriteByte('\n')
	full := w.flush == 0 || w.buf.Len() > 8<<10
	var payload []byte
	if full {
		payload = append(payload, w.buf.Bytes()...)
		w.buf.Reset()
	}
	w.mu.Unlock()
	if full {
		w.post(payload)
	}
	if w.verbose {
		fmt.Printf("[influx] %s page %d %s: %s\n", spec.name, page, name, strings.TrimRight(line, "\n"))
	}
}

// specFields renders the per-device tags.
func specFields(spec deviceSpec) string {
	return "device=" + escapeTag(spec.name) + ",id=" + fmt.Sprint(spec.id)
}

// close stops the flush loop and pushes whatever is buffered.
func (w *influxWriter) close() {
	if w.stop != nil {
		select {
		case <-w.stop:
		default:
			close(w.stop)
		}
	}
	w.mu.Lock()
	payload := append([]byte(nil), w.buf.Bytes()...)
	w.buf.Reset()
	w.mu.Unlock()
	if len(payload) > 0 {
		w.post(payload)
	}
}

// sessionUUID returns a random session uuid (openant tags every write
// with one, so points of a session can be filtered).
func sessionUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "goant"
	}
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// basicAuth renders the v1 API authorization header value.
func basicAuth(user, pass string) string {
	return base64.StdEncoding.EncodeToString([]byte(user + ":" + pass))
}

// post sends one batch to InfluxDB.
func (w *influxWriter) post(payload []byte) {
	req, err := http.NewRequest(http.MethodPost, w.writeURL, bytes.NewReader(payload))
	if err != nil {
		fmt.Fprintln(os.Stderr, "influx:", err)
		return
	}
	req.Header.Set("Content-Type", "text/plain; charset=utf-8")
	if w.token != "" {
		req.Header.Set("Authorization", "Token "+w.token)
	} else if w.auth != "" {
		req.Header.Set("Authorization", w.auth)
	}
	resp, err := w.client.Do(req)
	if err != nil {
		fmt.Fprintln(os.Stderr, "influx: write failed:", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		fmt.Fprintf(os.Stderr, "influx: write failed: %s %s\n", resp.Status, strings.TrimSpace(string(body)))
	} else if w.verbose {
		fmt.Printf("[influx] flushed %d bytes\n", len(payload))
	}
}

// buildLine renders one line protocol point; empty fields produce "".
func buildLine(measurement, staticTags, deviceTags string, fields []devices.InfluxField, ts time.Time) string {
	if len(fields) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(escapeMeasurement(measurement))
	tags := staticTags
	if deviceTags != "" {
		if tags != "" {
			tags += ","
		}
		tags += deviceTags
	}
	if tags != "" {
		b.WriteByte(',')
		b.WriteString(tags)
	}
	b.WriteByte(' ')
	first := true
	for _, f := range fields {
		fv, ok := formatFieldValue(f.Value)
		if !ok {
			continue
		}
		if !first {
			b.WriteByte(',')
		}
		first = false
		b.WriteString(escapeFieldKey(f.Key))
		b.WriteByte('=')
		b.WriteString(fv)
	}
	if first {
		return ""
	}
	b.WriteByte(' ')
	b.WriteString(fmt.Sprint(ts.UnixNano()))
	return b.String()
}

// formatFieldValue renders an influx field value.
func formatFieldValue(v any) (string, bool) {
	switch x := v.(type) {
	case float64:
		// Integers stay integer-typed; everything else is a float.
		if x == float64(int64(x)) {
			return fmt.Sprint(int64(x)) + "i", true
		}
		return fmt.Sprint(x), true
	case int64:
		return fmt.Sprint(x) + "i", true
	case string:
		return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(x) + `"`, true
	case bool:
		return fmt.Sprint(x), true
	}
	return "", false
}

var (
	tagEscaper   = strings.NewReplacer(",", `\,`, " ", `\ `, "=", `\=`)
	keyEscaper   = strings.NewReplacer(",", `\,`, " ", `\ `, "=", `\=`)
	measureEsc   = strings.NewReplacer(",", `\,`, " ", `\ `)
	queryEscaper = strings.NewReplacer(" ", "%20", "&", "%26", "=", "%3D")
)

func escapeTag(s string) string      { return tagEscaper.Replace(s) }
func escapeFieldKey(s string) string { return keyEscaper.Replace(s) }
func escapeMeasurement(s string) string {
	return measureEsc.Replace(s)
}
func escapeQuery(s string) string { return queryEscaper.Replace(s) }
