package hargo

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
	"time"

	client "github.com/influxdata/influxdb1-client/v2"
)

// influxPingAttempts is how many times to probe a new InfluxDB connection
// before giving up on it.
const influxPingAttempts = 3

// influxHTTPTimeout bounds every InfluxDB request. The client defaults to no
// timeout at all, and Ping's argument is a wait_for_leader query parameter rather
// than a client timeout, so without this a stalled server blocks forever. That
// mattered most in the write path: it runs on the consumer goroutine that LoadTest
// joins on, so a stall there hung the whole test after its workers had stopped.
const influxHTTPTimeout = 30 * time.Second

// influxWriter records test results in an InfluxDB database.
type influxWriter struct {
	client client.Client
	db     string
	logger *slog.Logger
}

// newInfluxWriter connects to the InfluxDB instance described by u and creates
// the database named by its path if it does not already exist.
//
// Any user info in u is used to authenticate.
func newInfluxWriter(ctx context.Context, u url.URL, logger *slog.Logger) (*influxWriter, error) {
	log := loggerOrDiscard(logger)

	addr := fmt.Sprintf("%s://%s:%s", u.Scheme, u.Hostname(), u.Port())
	log.Info("connecting to InfluxDB", "addr", addr)

	cfg := client.HTTPConfig{Addr: addr, Timeout: influxHTTPTimeout}
	// Credentials in the URL would otherwise be dropped, and the connection would
	// fail as an unauthenticated one with a confusing error.
	if u.User != nil {
		cfg.Username = u.User.Username()
		cfg.Password, _ = u.User.Password()
	}

	c, err := client.NewHTTPClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("creating InfluxDB client for %s: %w", addr, err)
	}

	// Probe the connection, tolerating a server that is still starting up. The
	// loop counts every attempt, including one that answers without reporting a
	// version, so an unresponsive server cannot spin here forever.
	var pingErr error
	for attempt := 1; attempt <= influxPingAttempts; attempt++ {
		_, version, err := c.Ping(2 * time.Second)
		if err == nil {
			log.Info("connected to InfluxDB", "version", version)
			pingErr = nil
			break
		}

		pingErr = err
		log.Debug("InfluxDB ping failed", "attempt", attempt, "err", err)

		if attempt == influxPingAttempts {
			break
		}
		select {
		case <-ctx.Done():
			_ = c.Close()
			return nil, ctx.Err()
		case <-time.After(10 * time.Second):
		}
	}
	if pingErr != nil {
		_ = c.Close()
		return nil, fmt.Errorf("pinging InfluxDB at %s: %w", addr, pingErr)
	}

	w := &influxWriter{
		client: c,
		db:     strings.ReplaceAll(u.Path, "/", ""),
		logger: log,
	}

	// The database name is interpolated into InfluxQL, which accepts
	// semicolon-separated statements, so anything but an identifier is rejected.
	if err := validateDatabaseName(w.db); err != nil {
		_ = c.Close()
		return nil, err
	}

	log.Debug("ensuring InfluxDB database exists", "db", w.db)
	if _, err := w.query(fmt.Sprintf("CREATE DATABASE %q", w.db)); err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("creating InfluxDB database %s: %w", w.db, err)
	}

	return w, nil
}

// validateDatabaseName reports whether name is a usable InfluxDB identifier.
func validateDatabaseName(name string) error {
	if name == "" {
		return errors.New("InfluxDB URL has no database in its path")
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
		default:
			return fmt.Errorf("invalid InfluxDB database name %q: unexpected character %q", name, r)
		}
	}
	return nil
}

// start announces that results are about to be recorded.
func (w *influxWriter) start() {
	w.logger.Info("recording results to InfluxDB", "db", w.db)
}

// writeOne records a single result.
//
// This takes one result rather than ranging a channel so that a single consumer
// can both accumulate a summary and forward to InfluxDB; ranging the channel here
// would take exclusive ownership of it.
func (w *influxWriter) writeOne(result TestResult) {
	bp, err := client.NewBatchPoints(client.BatchPointsConfig{
		Database: w.db,
		// Nanosecond precision keeps concurrent results distinct. Millisecond
		// precision truncated the timestamp, and since a point's identity is
		// measurement plus tags plus timestamp, results landing in the same
		// millisecond silently overwrote one another.
		Precision: "ns",
	})
	if err != nil {
		w.logger.Error("cannot create InfluxDB batch", "err", err)
		return
	}

	// Tags are indexed and form part of a point's identity, so they both make
	// the series queryable and stop distinct results from colliding.
	tags := map[string]string{
		"method":  result.Method,
		"status":  strconv.Itoa(result.Status),
		"har":     result.HARFile,
		"outcome": outcome(result.Status),
	}

	// Times are stored as Unix nanoseconds rather than time.Time: the client
	// formats an unrecognised field type with %v, which for a time.Time
	// includes the monotonic clock reading and is not queryable as a time.
	fields := map[string]any{
		"URL":       result.URL,
		"Status":    result.Status,
		"StartTime": result.StartTime.UnixNano(),
		"EndTime":   result.EndTime.UnixNano(),
		"Latency":   result.Latency,
		"Method":    result.Method,
		"HARFile":   result.HARFile,
	}

	// The point is timestamped when the request started, not when it happened
	// to be recorded.
	ts := result.StartTime
	if ts.IsZero() {
		ts = time.Now()
	}

	pt, err := client.NewPoint("test_result", tags, fields, ts)
	if err != nil {
		w.logger.Error("cannot create InfluxDB point", "err", err)
		return
	}

	bp.AddPoint(pt)

	if err := w.client.Write(bp); err != nil {
		w.logger.Error("cannot write to InfluxDB", "err", err)
	}
}

// outcome classifies a status code for grouping in queries. A zero status means
// the request never completed.
func outcome(status int) string {
	switch {
	case status == 0:
		return "error"
	case status < 400:
		return "ok"
	default:
		return "failed"
	}
}

// query runs cmd against the writer's database.
func (w *influxWriter) query(cmd string) ([]client.Result, error) {
	q := client.Query{
		Command:  cmd,
		Database: w.db,
	}

	response, err := w.client.Query(q)
	if err != nil {
		return nil, err
	}
	if response.Error() != nil {
		return nil, response.Error()
	}
	return response.Results, nil
}

// close releases the underlying client.
func (w *influxWriter) close() error {
	return w.client.Close()
}
