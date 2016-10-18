package hargo

import "time"

/*
HTTP Archive (HAR) format
https://w3c.github.io/web-performance/specs/HAR/Overview.html
*/

// Har is a container type for deserialization
type Har struct {
	Log Log
}

// Log represents the root of the exported data. This object MUST be present and its name MUST be "log".
type Log struct {
	// The object contains the following name/value pairs:

	// Required. Version number of the format.
	Version string
	// Required. An object of type creator that contains the name and version
	// information of the log creator application.
	Creator Creator
	// Optional. An object of type browser that contains the name and version
	// information of the user agent.
	Browser Browser
	// Optional. An array of objects of type page, each representing one exported
	// (tracked) page. Leave out this field if the application does not support
	// grouping by pages.
	Pages []Page `json:"omitempty"`
	// Required. An array of objects of type entry, each representing one
	// exported (tracked) HTTP request.
	Entries []Entry
	// Optional. A comment provided by the user or the application. Sorting
	// entries by startedDateTime (starting from the oldest) is preferred way how
	// to export data since it can make importing faster. However the reader
	// application should always make sure the array is sorted (if required for
	// the import).
	Comment string
}

// Creator contains information about the log creator application
type Creator struct {
	// Required. The name of the application that created the log.
	Name string
	// Required. The version number of the application that created the log.
	Version string
	// Optional. A comment provided by the user or the application.
	Comment string `json:"omitempty"`
}

// Browser that created the log
type Browser struct {
	// Required. The name of the browser that created the log.
	Name string
	// Required. The version number of the browser that created the log.
	Version string
	// Optional. A comment provided by the user or the browser.
	Comment string
}

// Page object for every exported web page and one <entry> object for every HTTP request.
// In case when an HTTP trace tool isn't able to group requests by a page,
// the <pages> object is empty and individual requests doesn't have a parent page.
type Page struct {
	/* There is one <page> object for every exported web page and one <entry>
	object for every HTTP request. In case when an HTTP trace tool isn't able to
	group requests by a page, the <pages> object is empty and individual
	requests doesn't have a parent page.
	*/

	// Date and time stamp for the beginning of the page load
	// (ISO 8601 YYYY-MM-DDThh:mm:ss.sTZD, e.g. 2009-07-24T19:20:30.45+01:00).
	StartedDateTime string
	// Unique identifier of a page within the . Entries use it to refer the parent page.
	ID string
	// Page title.
	Title string
	// Detailed timing info about page load.
	PageTiming PageTiming
	// (new in 1.2) A comment provided by the user or the application.
	Comment string `json:"omitempty"`
}

// PageTiming describes timings for various events (states) fired during the page load.
// All times are specified in milliseconds. If a time info is not available appropriate field is set to -1.
type PageTiming struct {
	// Content of the page loaded. Number of milliseconds since page load started
	// (page.startedDateTime). Use -1 if the timing does not apply to the current
	// request.
	// Depeding on the browser, onContentLoad property represents DOMContentLoad
	// event or document.readyState == interactive.
	OnContentLoad int
	// Page is loaded (onLoad event fired). Number of milliseconds since page
	// load started (page.startedDateTime). Use -1 if the timing does not apply
	// to the current request.
	OnLoad int
	// (new in 1.2) A comment provided by the user or the application.
	Comment string
}

// Entry is a unique, optional Reference to the parent page.
// Leave out this field if the application does not support grouping by pages.
type Entry struct {
	Pageref string `json:"omitempty"`
	// Date and time stamp of the request start
	// (ISO 8601 YYYY-MM-DDThh:mm:ss.sTZD).
	StartedDateTime string
	// Total elapsed time of the request in milliseconds. This is the sum of all
	// timings available in the timings object (i.e. not including -1 values) .
	Time float32
	// Detailed info about the request.
	Request Request
	// Detailed info about the response.
	Response Response
	// Info about cache usage.
	Cache Cache
	// Detailed timing info about request/response round trip.
	PageTimings PageTimings
	// optional (new in 1.2) IP address of the server that was connected
	// (result of DNS resolution).
	ServerIPAddress string `json:"omitempty"`
	// optional (new in 1.2) Unique ID of the parent TCP/IP connection, can be
	// the client port number. Note that a port number doesn't have to be unique
	// identifier in cases where the port is shared for more connections. If the
	// port isn't available for the application, any other unique connection ID
	// can be used instead (e.g. connection index). Leave out this field if the
	// application doesn't support this info.
	Connection string `json:"omitempty"`
	// (new in 1.2) A comment provided by the user or the application.
	Comment string `json:"omitempty"`
}

// Request contains detailed info about performed request.
type Request struct {
	// Request method (GET, POST, ...).
	Method string
	// Absolute URL of the request (fragments are not included).
	URL string
	// Request HTTP Version.
	HTTPVersion string
	// List of cookie objects.
	Cookies []Cookie
	// List of header objects.
	Headers []NVP
	// List of query parameter objects.
	QueryString []NVP
	// Posted data.
	PostData PostData
	// Total number of bytes from the start of the HTTP request message until
	// (and including) the double CRLF before the body. Set to -1 if the info
	// is not available.
	HeaderSize int
	// Size of the request body (POST data payload) in bytes. Set to -1 if the
	// info is not available.
	BodySize int
	// (new in 1.2) A comment provided by the user or the application.
	Comment string
}

// Response contains detailed info about the response.
type Response struct {
	// Response status.
	Status int
	// Response status description.
	StatusText string
	// Response HTTP Version.
	HTTPVersion string
	// List of cookie objects.
	Cookies []Cookie
	// List of header objects.
	Headers []NVP
	// Details about the response body.
	Content Content
	// Redirection target URL from the Location response header.
	RedirectURL string
	// Total number of bytes from the start of the HTTP response message until
	// (and including) the double CRLF before the body. Set to -1 if the info is
	// not available.
	// The size of received response-headers is computed only from headers that
	// are really received from the server. Additional headers appended by the
	// browser are not included in this number, but they appear in the list of
	// header objects.
	HeadersSize int
	// Size of the received response body in bytes. Set to zero in case of
	// responses coming from the cache (304). Set to -1 if the info is not
	// available.
	BodySize int
	// optional (new in 1.2) A comment provided by the user or the application.
	Comment string `json:"omitempty"`
}

// Cookie contains list of all cookies (used in <request> and <response> objects).
type Cookie struct {
	// The name of the cookie.
	Name string
	// The cookie value.
	Value string
	// optional The path pertaining to the cookie.
	Path string `json:"omitempty"`
	// optional The host of the cookie.
	Domain string `json:"omitempty"`
	// optional Cookie expiration time.
	// (ISO 8601 YYYY-MM-DDThh:mm:ss.sTZD, e.g. 2009-07-24T19:20:30.123+02:00).
	Expires string `json:"omitempty"`
	// optional Set to true if the cookie is HTTP only, false otherwise.
	HTTPOnly string `json:"omitempty"`
	// optional (new in 1.2) True if the cookie was transmitted over ssl, false
	// otherwise.
	Secure bool `json:"omitempty"`
	// optional (new in 1.2) A comment provided by the user or the application.
	Comment bool `json:"omitempty"`
}

// NVP is simply a name/value pair with a comment
type NVP struct {
	Name    string
	Value   string
	Comment string `json:"omitempty"`
}

// PostData describes posted data, if any (embedded in <request> object).
type PostData struct {
	//  Mime type of posted data.
	MimeType string
	//  List of posted parameters (in case of URL encoded parameters).
	Params []PostParam
	//  Plain text posted data
	Text string
	// optional (new in 1.2) A comment provided by the user or the
	// application.
	Comment string `json:"omitempty"`
}

// PostParam is a list of posted parameters, if any (embedded in <postData> object).
type PostParam struct {
	// name of a posted parameter.
	Name string
	// optional value of a posted parameter or content of a posted file.
	Value string `json:"omitempty"`
	// optional name of a posted file.
	FileName string `json:"omitempty"`
	// optional content type of a posted file.
	ContentType string `json:"omitempty"`
	// optional (new in 1.2) A comment provided by the user or the application.
	Comment string `json:"omitempty"`
}

// Content describes details about response content (embedded in <response> object).
type Content struct {
	// Length of the returned content in bytes. Should be equal to
	// response.bodySize if there is no compression and bigger when the content
	// has been compressed.
	Size int
	// optional Number of bytes saved. Leave out this field if the information
	// is not available.
	Compression int `json:"omitempty"`
	// MIME type of the response text (value of the Content-Type response
	// header). The charset attribute of the MIME type is included (if
	// available).
	MimeType string
	// optional Response body sent from the server or loaded from the browser
	// cache. This field is populated with textual content only. The text field
	// is either HTTP decoded text or a encoded (e.g. "base64") representation of
	// the response body. Leave out this field if the information is not
	// available.
	Text string `json:"omitempty"`
	// optional (new in 1.2) Encoding used for response text field e.g
	// "base64". Leave out this field if the text field is HTTP decoded
	// (decompressed & unchunked), than trans-coded from its original character
	// set into UTF-8.
	Encoding string `json:"omitempty"`
	// optional (new in 1.2) A comment provided by the user or the application.
	Comment string `json:"omitempty"`
}

// Cache contains info about a request coming from browser cache.
type Cache struct {
	// optional State of a cache entry before the request. Leave out this field
	// if the information is not available.
	BeforeRequest CacheObject `json:"omitempty"`
	// optional State of a cache entry after the request. Leave out this field if
	// the information is not available.
	AfterRequest CacheObject `json:"omitempty"`
	// optional (new in 1.2) A comment provided by the user or the application.
	Comment string `json:"omitempty"`
}

// CacheObject is used by both beforeRequest and afterRequest
type CacheObject struct {
	// optional - Expiration time of the cache entry.
	Expires string `json:"omitempty"`
	// The last time the cache entry was opened.
	LastAccess string
	// Etag
	ETag string
	// The number of times the cache entry has been opened.
	HitCount int
	// optional (new in 1.2) A comment provided by the user or the application.
	Comment string `json:"omitempty"`
}

// PageTimings describes various phases within request-response round trip.
// All times are specified in milliseconds.
type PageTimings struct {
	Blocked int `json:"omitempty"`
	// optional - Time spent in a queue waiting for a network connection. Use -1
	// if the timing does not apply to the current request.
	DNS int `json:"omitempty"`
	// optional - DNS resolution time. The time required to resolve a host name.
	// Use -1 if the timing does not apply to the current request.
	Connect int `json:"omitempty"`
	// optional - Time required to create TCP connection. Use -1 if the timing
	// does not apply to the current request.
	Send int
	// Time required to send HTTP request to the server.
	Wait int
	// Waiting for a response from the server.
	Receive int
	// Time required to read entire response from the server (or cache).
	Ssl int `json:"omitempty"`
	// optional (new in 1.2) - Time required for SSL/TLS negotiation. If this
	// field is defined then the time is also included in the connect field (to
	// ensure backward compatibility with HAR 1.1). Use -1 if the timing does not
	// apply to the current request.
	Comment string `json:"omitempty"`
	// optional (new in 1.2) - A comment provided by the user or the application.
}

// TestResult contains results for an individual HTTP request
type TestResult struct {
	URL       string
	Status    int // 200, 500, etc.
	StartTime time.Time
	EndTime   time.Time
	Latency   int // milliseconds
}
