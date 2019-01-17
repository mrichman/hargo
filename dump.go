package hargo

import (
	"bufio"
	"encoding/json"

	"fmt"

	log "github.com/sirupsen/logrus"
)

// Dump prints all HTTP requests in .har file
func Dump(r *bufio.Reader) {
	//_, err := Validate(r)

	dec := json.NewDecoder(r)
	var har Har
	err := dec.Decode(&har)

	if err != nil {
		log.Error(err)
	}

	fmt.Println("HAR Version: " + har.Log.Version)
	fmt.Println("Creator: ", har.Log.Creator.Name+" "+har.Log.Creator.Version)

	for _, entry := range har.Log.Entries {
		fmt.Println("----------------------------------------------------------------------")
		fmt.Println("Timestamp: ", entry.StartedDateTime)
		fmt.Println("Request URL: ", entry.Request.URL)
		fmt.Println("Request Method: ", entry.Request.Method)
		fmt.Println("HTTP Version: ", entry.Request.HTTPVersion)
		fmt.Println("Status Code: ", entry.Response.Status)
		fmt.Println("Server IP Address: ", entry.ServerIPAddress)

		fmt.Println("Request Headers:")

		for _, req := range entry.Request.Headers {
			fmt.Println("\t" + req.Name + ": " + req.Value)
		}

		fmt.Println("Querystring Parameters:")

		for _, qs := range entry.Request.QueryString {
			fmt.Println("\t" + qs.Name + ": " + qs.Value)
		}

		fmt.Println("Cookies:")

		for _, cookie := range entry.Request.Cookies {
			fmt.Println("\t🍪 " + cookie.Name + "=" + cookie.Value)
		}

		fmt.Println("POST Data:")

		fmt.Println("\tMIME Type: " + entry.Request.PostData.MimeType)

		for _, params := range entry.Request.PostData.Params {
			fmt.Println("\t" + params.Name + ": " + params.Value)
		}

		fmt.Println("Response Headers:")

		for _, res := range entry.Response.Headers {
			fmt.Println("\t" + res.Name + ": " + res.Value)
		}

	}
}
