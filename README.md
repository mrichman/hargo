# <img src="./img/hargo-logo.png" height="40"> Hargo ![Hargo Build Status](https://travis-ci.org/mrichman/hargo.svg?branch=master)&nbsp;[![GoDoc](https://godoc.org/github.com/mrichman/hargo?status.svg)](https://godoc.org/github.com/mrichman/hargo)

Hargo parses HAR files, can convert to curl format, and serve as a load test driver.

```
NAME:
   hargo - work with HTTP Archive (.har) files

USAGE:
   hargo <command> [arguments] <.har file>

VERSION:
   0.1.0

AUTHOR(S):
   Mark A. Richman <mark@markrichman.com>

COMMANDS:
     fetch, f     Fetch URLs in .har
     curl, c      Convert .har to curl
     run, r       Run .har file
     validate, v  Validate .har file
     dump, d      Dump .har file
     load, l      Load test .har file
     help, h      Shows a list of commands or help for one command

GLOBAL OPTIONS:
   --debug        enable verbose (debug) logging
   --help, -h     show help
   --version, -v  print the version

COPYRIGHT:
   (c) 2016 Mark A. Richman
```

## Running Hargo

```
go get github.com/mrichman/hargo
cd $GOPATH/src/github.com/mrichman/hargo
go run cmd/hargo/main.go validate test/golang.org.har
```

## About HAR Files

If you use Google Chrome, you can record these files by following the steps below:

1. Right-click anywhere on that page and click on Inspect Element to open Chrome's Developer Tools
2. The Developer Tools will open as a panel at the bottom of the page. Click on the Network tab.
3. Click the Record button, which is the solid black circle at the bottom of the Network tab, and you'll start recording activity in your browser.
4. Refresh the page and start working normally
5. Right-click within the Network tab and click Save as HAR with Content to save a copy of the activity that you recorded.
6. Within the file window, save the HAR file.

## Commands

### Fetch

The `fetch` command downloads all resources references in .har file:

`hargo fetch foo.har`

This will produce a directory named `hargo-fetch-yyyymmddhhmmss` containing all assets references by the .har file. This is similar to what you'd see when invoking `wget` on a particular URL.

### Curl

The `curl` command will output a [curl](https://curl.haxx.se/) command line for each entry in the .har file.

`hargo curl foo.har`

### Run

The `run` command executes each HTTP request in .har file:

`hargo run foo.har`

This is similar to `fetch` but will not save any output.

### Validate

The `validate` command will report any errors in the format of a .har file.

`hargo validate foo.har`

HAR file format is defined here: https://w3c.github.io/web-performance/specs/HAR/Overview.html

### Dump

Dump prints information about all HTTP requests in .har file

`hargo dump foo.har`

### Load

Hargo can act as a load test agent. Given a .har file, hargo can spawn a number of concurrent workers to repeat each HTTP request in order. By default, hargo will spawn 10 workers and run for a duration of 60 seconds.

Hargo will also save its results to InfluxDB, if available. Each HTTP response is stored as a point of time-series data, which can be graphed by [Chronograf](https://www.influxdata.com/time-series-platform/chronograf/), [Grafana](http://grafana.org/), or similar visualization tool for analysis.
