package hargo

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
)

// Fetch downloads all resources references in .har file
func Fetch(r *bufio.Reader) error {
	har, err := Decode(r)

	check(err)

	datestring := time.Now().Format("20060102150405")
	outdir := "." + string(filepath.Separator) + "hargo-fetch-" + datestring

	err = os.Mkdir(outdir, 0777)

	check(err)

	for _, entry := range har.Log.Entries {

		//TODO create goroutine here to parallelize requests

		fmt.Println("URL: " + entry.Request.URL)

		req, _ := http.NewRequest(entry.Request.Method, entry.Request.URL, nil)

		for _, h := range entry.Request.Headers {
			if !strings.HasPrefix(h.Name, ":") {
				req.Header.Add(h.Name, h.Value)
			}
		}

		for _, c := range entry.Request.Cookies {
			cookie := &http.Cookie{Name: c.Name, Value: c.Value, HttpOnly: false, Domain: c.Domain}
			req.AddCookie(cookie)
		}

		//cookie := &http.Cookie{Name: "_hargo", Value: "true", HttpOnly: false}
		//req.AddCookie(cookie)

		err = downloadFile(req, outdir)

		if err != nil {
			log.Error(err)
			return err
		}
	}

	return nil
}

func downloadFile(req *http.Request, outdir string) error {

	fileName := path.Base(req.URL.Path)

	if fileName == "/" || fileName == "" {
		fileName = "index.html"
	}

	fileName = outdir + string(filepath.Separator) + fileName

	if len(fileName) == 0 {
		return nil
	}

	file, err := os.Create(fileName)

	if err != nil {
		log.Error(err)
		return err
	}
	defer file.Close()

	jar, _ := cookiejar.New(nil)

	jar.SetCookies(req.URL, req.Cookies())

	client := http.Client{
		CheckRedirect: func(r *http.Request, via []*http.Request) error {
			r.URL.Opaque = r.URL.Path
			return nil
		},
		Jar: jar,
	}

	// spew.Dump(client)
	// spew.Dump(req)

	resp, err := client.Do(req) //.Get(rawURL) // add a filter to check redirect

	if err != nil {
		log.Error(err)
		return err
	}
	defer resp.Body.Close()

	size, err := io.Copy(file, resp.Body)

	if err != nil {
		log.Error(err)
		return err
	}

	fmt.Printf("Downloaded %s [%v bytes]\n", fileName, size)
	return nil
}
