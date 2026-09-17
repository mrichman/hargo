// Command hargo is a command line tool for working with HTTP Archive (.har)
// files: validating them, dumping their contents, converting them to curl
// commands, replaying them, fetching the resources they reference, and driving
// load tests from them.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/mrichman/hargo/v2"
	"github.com/urfave/cli"
)

var (
	// Version is the version number or commit hash.
	// These variables should be set by the linker when compiling.
	Version = "Unknown"
	// CommitHash is the commit this version was built on.
	CommitHash = "Unknown"
	// CompileDate is the date this binary was compiled on.
	CompileDate = "Unknown"
)

const usage = "work with HTTP Archive (.har) files"

// debugFlag is registered both globally and on every command.
//
// urfave/cli v1 only reorders a command's own flags, so a global flag placed after
// the subcommand is not recognised: "hargo validate --debug f.har" failed with
// "flag provided but not defined", and "hargo fetch f.har --debug" was worse, since
// --debug then landed in the positional arguments and became the output directory.
func debugFlag() cli.Flag {
	return cli.BoolFlag{
		Name:  "debug",
		Usage: "Show debug output",
	}
}

// logger builds the logger for this run. Diagnostics go to stderr so that
// stdout stays usable for piping curl and dump output.
//
// The flag is accepted before or after the subcommand.
func logger(c *cli.Context) *slog.Logger {
	level := slog.LevelInfo
	if c.GlobalBool("debug") || c.Bool("debug") {
		level = slog.LevelDebug
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
}

// openHAR opens the .har file named by the first CLI argument.
func openHAR(c *cli.Context) (*os.File, error) {
	harFile := c.Args().First()
	if harFile == "" {
		return nil, cli.NewExitError("must supply a .har file", 1)
	}
	file, err := os.Open(harFile)
	if err != nil {
		return nil, cli.NewExitError(fmt.Sprintf("cannot open file %s: %v", harFile, err), 1)
	}
	return file, nil
}

func main() {
	// os.Exit skips deferred calls, so the real work happens in run and the
	// process exits only after its defers have unwound.
	os.Exit(run())
}

func run() int {
	// Ctrl-C and SIGTERM cancel the context, which unwinds an in-flight replay,
	// fetch, or load test cleanly rather than killing it mid-request.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

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
	app.Copyright = "(c) 2022 Mark A. Richman"
	app.HelpName = "hargo"
	app.Usage = usage
	app.UsageText = "hargo <command> [arguments] <.har file>"
	app.ArgsUsage = "[args and such]"

	app.Flags = []cli.Flag{debugFlag()}

	app.Commands = []cli.Command{
		{
			Name:        "fetch",
			Aliases:     []string{"f"},
			Usage:       "Fetch URLs in .har",
			UsageText:   "fetch - fetch all URLs",
			Description: "fetch all URLs found in HAR file, saving all objects in an output directory",
			ArgsUsage:   "<.har file> [output dir]",
			Flags:       []cli.Flag{debugFlag()},
			Action: func(c *cli.Context) error {
				file, err := openHAR(c)
				if err != nil {
					return err
				}
				defer func() { _ = file.Close() }()

				log := logger(c)
				log.Debug("fetching .har file", "file", c.Args().First())

				// An empty output directory is optional; without one a
				// timestamped directory is created in the working directory.
				return hargo.Fetch(ctx, file, hargo.FetchOptions{
					OutDir:   c.Args().Get(1),
					Logger:   log,
					Progress: os.Stdout,
				})
			},
		},
		{
			Name:        "curl",
			Aliases:     []string{"c"},
			Usage:       "Convert .har to curl",
			UsageText:   "curl - convert .har file to curl format",
			Description: "convert all .har file entries to curl commands",
			ArgsUsage:   "<.har file>",
			Flags:       []cli.Flag{debugFlag()},
			Action: func(c *cli.Context) error {
				file, err := openHAR(c)
				if err != nil {
					return err
				}
				defer func() { _ = file.Close() }()

				logger(c).Debug("converting .har file to curl", "file", c.Args().First())

				cmd, err := hargo.ToCurl(file)
				if err != nil {
					return err
				}

				fmt.Println(cmd)
				return nil
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
				debugFlag(),
				cli.BoolFlag{
					Name:  "ignore-har-cookies",
					Usage: "Ignore the cookies provided by the HAR entries"},
				cli.BoolFlag{
					Name:  "insecure-skip-verify",
					Usage: "Skips the TLS security checks"},
				cli.Float64Flag{
					Name:  "speed",
					Value: 1,
					Usage: "Replay speed multiplier for the recorded delays (2 = twice as fast)"},
				cli.BoolFlag{
					Name:  "no-wait",
					Usage: "Ignore the recorded delays and issue requests back to back"},
				cli.DurationFlag{
					Name:  "max-delay",
					Usage: "Cap the wait before any single entry, e.g. 2s (0 = no cap)"},
			},
			Action: func(c *cli.Context) error {
				file, err := openHAR(c)
				if err != nil {
					return err
				}
				defer func() { _ = file.Close() }()

				log := logger(c)
				log.Debug("running .har file", "file", c.Args().First())

				return hargo.Run(ctx, file, hargo.RunOptions{
					IgnoreHARCookies:   c.Bool("ignore-har-cookies"),
					InsecureSkipVerify: c.Bool("insecure-skip-verify"),
					Speed:              c.Float64("speed"),
					NoWait:             c.Bool("no-wait"),
					MaxDelay:           c.Duration("max-delay"),
					Logger:             log,
					Progress:           os.Stdout,
				})
			},
		},
		{
			Name:        "validate",
			Aliases:     []string{"v"},
			Usage:       "Validate .har file",
			UsageText:   "validate - validates the format of a .har file",
			Description: "validates the format of a .har file",
			ArgsUsage:   "<.har file>",
			Flags:       []cli.Flag{debugFlag()},
			Action: func(c *cli.Context) error {
				file, err := openHAR(c)
				if err != nil {
					return err
				}
				defer func() { _ = file.Close() }()

				logger(c).Debug("validating .har file", "file", c.Args().First())

				if err := hargo.Validate(file); err != nil {
					// -2 is preserved for compatibility: it surfaces as exit 254.
					return cli.NewExitError(err.Error(), -2)
				}
				fmt.Println("Valid HAR file! 😊")
				return nil
			},
		},
		{
			Name:        "dump",
			Aliases:     []string{"d"},
			Usage:       "Dump .har file",
			UsageText:   "dump - print all HTTP requests in .har file",
			Description: "print all HTTP requests in .har file",
			ArgsUsage:   "<.har file>",
			Flags:       []cli.Flag{debugFlag()},
			Action: func(c *cli.Context) error {
				file, err := openHAR(c)
				if err != nil {
					return err
				}
				defer func() { _ = file.Close() }()

				logger(c).Debug("dumping .har file", "file", c.Args().First())

				return hargo.DumpTo(os.Stdout, file)
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
				debugFlag(),
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
			Action: func(c *cli.Context) error {
				file, err := openHAR(c)
				if err != nil {
					return err
				}
				defer func() { _ = file.Close() }()

				harFile := c.Args().First()

				log := logger(c)
				log.Debug("load testing .har file", "file", harFile)

				// An empty flag must stay nil: url.Parse("") succeeds and yields
				// an empty URL, which would look like a real InfluxDB target.
				var influxURL *url.URL
				if raw := c.String("u"); raw != "" {
					u, err := url.Parse(raw)
					if err != nil {
						return cli.NewExitError(fmt.Sprintf("invalid InfluxDB URL %q: %v", raw, err), 1)
					}
					influxURL = u
				}

				return hargo.LoadTest(ctx, file, hargo.LoadTestOptions{
					HARFile:            filepath.Base(harFile),
					Workers:            c.Int("w"),
					Duration:           time.Duration(c.Int("d")) * time.Second,
					InfluxDBURL:        influxURL,
					IgnoreHARCookies:   c.Bool("ignore-har-cookies"),
					InsecureSkipVerify: c.Bool("insecure-skip-verify"),
					Logger:             log,
					Progress:           os.Stdout,
				})
			},
		},
	}

	if err := app.Run(os.Args); err != nil {
		log := slog.New(slog.NewTextHandler(os.Stderr, nil))

		// cli.ExitCoder values carry their own exit status; anything else is a
		// plain failure. errors.As so a wrapped ExitCoder is still honoured.
		var ec cli.ExitCoder
		if errors.As(err, &ec) {
			log.Error(ec.Error())
			return ec.ExitCode()
		}
		log.Error(err.Error())
		return 1
	}

	return 0
}
