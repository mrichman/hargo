package main

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/mrichman/hargo"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli"
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

// openHar opens the .har file named by the first CLI argument.
func openHar(c *cli.Context) (*os.File, error) {
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
	app.Copyright = "(c) 2022 Mark A. Richman"
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
			ArgsUsage:   "<.har file> [output dir]",
			Action: func(c *cli.Context) error {
				file, err := openHar(c)
				if err != nil {
					return err
				}
				defer func() { _ = file.Close() }()

				log.Infof("fetch .har file: %s", c.Args().First())
				r := hargo.NewReader(file)

				// An explicit output directory is optional; without one a
				// timestamped directory is created in the working directory.
				if outdir := c.Args().Get(1); outdir != "" {
					return hargo.FetchTo(r, outdir)
				}
				return hargo.Fetch(r)
			},
		},
		{
			Name:        "curl",
			Aliases:     []string{"c"},
			Usage:       "Convert .har to curl",
			UsageText:   "curl - convert .har file to curl format",
			Description: "convert all .har file entries to curl commands",
			ArgsUsage:   "<.har file>",
			Action: func(c *cli.Context) error {
				file, err := openHar(c)
				if err != nil {
					return err
				}
				defer func() { _ = file.Close() }()

				log.Infof("curl .har file: %s", c.Args().First())
				cmd, err := hargo.ToCurl(hargo.NewReader(file))
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
				cli.BoolFlag{
					Name:  "ignore-har-cookies",
					Usage: "Ignore the cookies provided by the HAR entries"},
				cli.BoolFlag{
					Name:  "insecure-skip-verify",
					Usage: "Skips the TLS security checks"},
			},
			Action: func(c *cli.Context) error {
				ignoreHarCookies := c.Bool("ignore-har-cookies")
				insecureSkipVerify := c.Bool("insecure-skip-verify")

				file, err := openHar(c)
				if err != nil {
					return err
				}
				defer func() { _ = file.Close() }()

				log.Info("run .har file: ", c.Args().First())
				return hargo.Run(hargo.NewReader(file), ignoreHarCookies, insecureSkipVerify)
			},
		},
		{
			Name:        "validate",
			Aliases:     []string{"v"},
			Usage:       "Validate .har file",
			UsageText:   "validate - validates the format of a .har file",
			Description: "validates the format of a .har file",
			ArgsUsage:   "<.har file>",
			Action: func(c *cli.Context) error {
				file, err := openHar(c)
				if err != nil {
					return err
				}
				defer func() { _ = file.Close() }()

				log.Info("validate .har file: ", c.Args().First())
				if _, err := hargo.Validate(hargo.NewReader(file)); err != nil {
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
			Action: func(c *cli.Context) error {
				file, err := openHar(c)
				if err != nil {
					return err
				}
				defer func() { _ = file.Close() }()

				log.Info("dump .har file: ", c.Args().First())
				return hargo.DumpTo(os.Stdout, hargo.NewReader(file))
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
			Action: func(c *cli.Context) error {

				if c.GlobalBool("debug") {
					log.Info("Setting debug log level")
					log.SetLevel(log.DebugLevel)
				}

				file, err := openHar(c)
				if err != nil {
					return err
				}
				defer func() { _ = file.Close() }()

				harFile := c.Args().First()
				log.Info("load test .har file: ", harFile)

				workers := c.Int("w")
				duration := c.Int("d")
				ignoreHarCookies := c.Bool("ignore-har-cookies")
				insecureSkipVerify := c.Bool("insecure-skip-verify")

				u, err := url.Parse(c.String("u"))
				if err != nil {
					return cli.NewExitError(fmt.Sprintf("invalid InfluxDB URL %q: %v", c.String("u"), err), 1)
				}

				return hargo.LoadTest(filepath.Base(harFile), file, workers,
					time.Duration(duration)*time.Second, *u, ignoreHarCookies, insecureSkipVerify)
			},
		},
	}

	if err := app.Run(os.Args); err != nil {
		// cli.ExitCoder values carry their own exit status; anything else is a
		// plain failure. errors.As so a wrapped ExitCoder is still honoured.
		var ec cli.ExitCoder
		if errors.As(err, &ec) {
			log.Error(ec.Error())
			os.Exit(ec.ExitCode())
		}
		log.Error(err)
		os.Exit(1)
	}
}
