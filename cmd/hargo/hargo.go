package main

import (
	"bufio"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/mrichman/hargo"
	log "github.com/sirupsen/logrus"
	"gopkg.in/urfave/cli.v1"
)

var (
	// Version is the version number or commit hash
	// These variables should be set by the linker when compiling
	Version = "Unknown"
	// CommitHash is the commit this version was built on
	CommitHash = "Unknown"
	// CompileDate is the date this binary was compiled on
	CompileDate = "Unknown"
)

const usage = "work with HTTP Archive (.har) files"

func init() {
	log.SetLevel(log.InfoLevel)
}

func main() {

	log.Debug("hargo started in debug mode")

	app := cli.NewApp()
	app.Name = "hargo"
	app.Version = Version + " (" + CommitHash + ")"
	app.Compiled, _ = time.Parse("January 02, 2006", CompileDate)
	app.Authors = []cli.Author{
		{
			Name:  "Mark A. Richman",
			Email: "mark@markrichman.com",
		},
	}
	app.Copyright = "(c) 2019 Mark A. Richman"
	app.HelpName = "hargo"
	app.Usage = usage
	app.UsageText = "hargo <command> [arguments] <.har file>"
	app.ArgsUsage = "[args and such]"

	app.Flags = []cli.Flag{
		cli.BoolFlag{
			Name:  "debug",
			Usage: "Show debug output"},
	}

	app.Commands = []cli.Command{
		{
			Name:        "fetch",
			Aliases:     []string{"f"},
			Usage:       "Fetch URLs in .har",
			UsageText:   "fetch - fetch all URLs",
			Description: "fetch all URLs found in HAR file, saving all objects in an output directory",
			ArgsUsage:   "<.har file> <output dir>",
			Action: func(c *cli.Context) {
				harFile := c.Args().First()
				log.Infof("fetch .har file: %s", harFile)
				file, err := os.Open(harFile)
				if err == nil {
					r := newReader(file)
					hargo.Fetch(r)
				} else {
					log.Fatal("Cannot open file: ", harFile)
					os.Exit(-1)
				}
			},
		},
		{
			Name:        "curl",
			Aliases:     []string{"c"},
			Usage:       "Convert .har to curl",
			UsageText:   "curl - convert .har file to curl format",
			Description: "convert all .har file entries to curl commands",
			ArgsUsage:   "<.har file>",
			Action: func(c *cli.Context) {
				harFile := c.Args().First()
				log.Infof("curl .har file: %s", harFile)
				file, err := os.Open(harFile)
				if err == nil {
					r := newReader(file)
					cmd, err := hargo.ToCurl(r)

					if err != nil {
						log.Error(err)
					}

					fmt.Println(cmd)
				} else {
					log.Fatal("Cannot open file: ", harFile)
					os.Exit(-1)
				}
			},
		},
		{
			Name:        "run",
			Aliases:     []string{"r"},
			Usage:       "Run .har file",
			UsageText:   "run - execute all requests in .har file",
			Description: "execute all requests in .har file",
			ArgsUsage:   "<.har file>",
			Flags: []cli.Flag{
				cli.BoolFlag{
					Name:  "ignore-har-cookies",
					Usage: "Ignore the cookies provided by the HAR entries"},
				cli.BoolFlag{
					Name:  "insecure-skip-verify",
					Usage: "Skips the TLS security checks"},
			},
			Action: func(c *cli.Context) {
				ignoreHarCookies := c.Bool("ignore-har-cookies")
				insecureSkipVerify := c.Bool("insecure-skip-verify")
				harFile := c.Args().First()
				log.Info("run .har file: ", harFile)
				file, err := os.Open(harFile)
				if err == nil {
					r := newReader(file)
					hargo.Run(r, ignoreHarCookies, insecureSkipVerify)
				} else {
					log.Fatal("Cannot open file: ", harFile)
					os.Exit(-1)
				}
			},
		},
		{
			Name:        "validate",
			Aliases:     []string{"v"},
			Usage:       "Validate .har file",
			UsageText:   "validate - validates the format of a .har file",
			Description: "validates the format of a .har file",
			ArgsUsage:   "<.har file>",
			Action: func(c *cli.Context) {
				harFile := c.Args().First()
				log.Info("validate .har file: ", harFile)
				file, err := os.Open(harFile)
				if err == nil {
					r := newReader(file)
					hargo.Validate(r)
				} else {
					log.Fatal("Cannot open file: ", harFile)
					os.Exit(-1)
				}
			},
		},
		{
			Name:        "dump",
			Aliases:     []string{"d"},
			Usage:       "Dump .har file",
			UsageText:   "dump - print all HTTP requests in .har file",
			Description: "print all HTTP requests in .har file",
			ArgsUsage:   "<.har file>",
			Action: func(c *cli.Context) {
				harFile := c.Args().First()
				log.Info("dump .har file: ", harFile)
				file, err := os.Open(harFile)
				if err == nil {
					r := newReader(file)
					hargo.Dump(r)
				} else {
					log.Fatal("Cannot open file: ", harFile)
					os.Exit(-1)
				}
			},
		},
		{
			Name:        "load",
			Aliases:     []string{"l"},
			Usage:       "Load test .har file",
			UsageText:   "load - runs all requests in sequence, concurrently",
			Description: "runs all requests in sequence, concurrently",
			ArgsUsage:   "<.har file>",
			Flags: []cli.Flag{
				cli.IntFlag{
					Name:  "workers, w",
					Value: 10,
					Usage: "Number of workers (default 10)"},
				cli.IntFlag{
					Name:  "duration, d",
					Value: 60,
					Usage: "Test duration in seconds (default 60)"},
				cli.StringFlag{
					Name:  "influxurl, u",
					Usage: "InfluxDB URL"},
				cli.BoolFlag{
					Name:  "ignore-har-cookies",
					Usage: "Ignore the cookies provided by the HAR entries"},
				cli.BoolFlag{
					Name:  "insecure-skip-verify",
					Usage: "Skips the TLS security checks"},
			},
			Action: func(c *cli.Context) {

				if c.GlobalBool("debug") {
					log.Info("Setting debug log level")
					log.SetLevel(log.DebugLevel)
				}

				harFile := c.Args().First()

				if len(harFile) == 0 {
					log.Fatal("Must supply a .har file")
					os.Exit(-1)
				}

				log.Info("load test .har file: ", harFile)
				file, err := os.Open(harFile)
				if err == nil {
					r := newReader(file)
					workers := c.Int("w")
					duration := c.Int("d")
					u, err := url.Parse(c.String("u"))
					ignoreHarCookies := c.Bool("ignore-har-cookies")
					insecureSkipVerify := c.Bool("insecure-skip-verify")

					if err != nil {
						log.Fatal("Invalid InfluxDB URL: ", c.String("u"))
						os.Exit(-1)
					}

					hargo.LoadTest(filepath.Base(harFile), r, workers, time.Duration(duration)*time.Second, *u, ignoreHarCookies, insecureSkipVerify)
				} else {
					log.Fatal("Cannot open file: ", harFile)
					os.Exit(-1)
				}
			},
		},
	}

	app.Run(os.Args)
}

// newReader returns a bufio.Reader that will skip over initial UTF-8 byte order marks.
// https://tools.ietf.org/html/rfc7159#section-8.1
func newReader(r io.Reader) *bufio.Reader {

	buf := bufio.NewReader(r)
	b, err := buf.Peek(3)
	if err != nil {
		// not enough bytes
		return buf
	}
	if b[0] == 0xef && b[1] == 0xbb && b[2] == 0xbf {
		log.Warn("BOM detected. Skipping first 3 bytes of file. Consider removing the BOM from this file. " +
			"See https://tools.ietf.org/html/rfc7159#section-8.1 for details.")
		buf.Discard(3)
	}
	return buf
}
