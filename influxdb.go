package hargo

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	client "github.com/influxdata/influxdb1-client/v2"
	log "github.com/sirupsen/logrus"
)

var db string

// newInfluxDBClient returns a new InfluxDB client
func newInfluxDBClient(u url.URL) (client.Client, error) {

	addr := fmt.Sprintf("%s://%s:%s", u.Scheme, u.Hostname(), u.Port())
	log.Print("Connecting to InfluxDB: ", addr)

	c, err := client.NewHTTPClient(client.HTTPConfig{
		Addr: addr,
	})

	if err != nil {
		log.Error("Error: ", err)
		return nil, err
	}

	retry := 1

	for retry < 3 {
		_, resp, e := c.Ping(2 * time.Second)
		if e != nil {
			retry++
			time.Sleep(10 * time.Second)
		} else if len(resp) > 0 {
			log.Println("Version InfluxDB: " + resp)
			break
		}
	}

	db = strings.ReplaceAll(u.Path, "/", "")

	log.Info("DB: ", db)

	cmd := fmt.Sprintf("CREATE DATABASE %s", db)

	log.Debug("Query: ", cmd)

	_, err = queryDB(c, cmd)
	if err != nil {
		log.Warn("Could not connect to InfluxDB: ", err)
		return nil, err
	}
	return c, nil
}

// WritePoint inserts data to InfluxDB
func WritePoint(u url.URL, results chan TestResult) {
	c, err := newInfluxDBClient(u)

	if err != nil || c == nil {
		// Without a client there is nowhere to write. Drain results so the
		// workers producing them are never blocked.
		log.Warn("No test results will be recorded to InfluxDB")
		for range results {
		}
		return
	}

	log.Info("Recording results to InfluxDB: ", u.String())

	for result := range results {
		bp, err := client.NewBatchPoints(client.BatchPointsConfig{
			Database:  db,
			Precision: "ms",
		})
		if err != nil {
			log.Error("Error: ", err)
			continue
		}

		fields := map[string]interface{}{
			"URL":       result.URL,
			"Status":    result.Status,
			"StartTime": result.StartTime,
			"EndTime":   result.EndTime,
			"Latency":   result.Latency,
			"Method":    result.Method,
			"HarFile":   result.HarFile}

		pt, err := client.NewPoint("test_result", nil, fields, time.Now())
		if err != nil {
			log.Error("Error: ", err)
			continue
		}

		bp.AddPoint(pt)

		if err := c.Write(bp); err != nil {
			log.Error("Error: ", err)
		}
	}
}

// queryDB convenience function to query the database
func queryDB(clnt client.Client, cmd string) (res []client.Result, _ error) {
	q := client.Query{
		Command:  cmd,
		Database: db,
	}
	if response, err := clnt.Query(q); err == nil {
		if response.Error() != nil {
			return res, response.Error()
		}
		res = response.Results
	} else {
		return res, err
	}
	return res, nil
}
