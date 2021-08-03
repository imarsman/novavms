package main

import (
	"context"
	"encoding/json"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"regexp"
	"sync/atomic"
	"text/template"
	"time"

	"github.com/google/uuid"
	cache "github.com/patrickmn/go-cache"
	"google.golang.org/grpc"

	// "github.com/imarsman/nanovms/app"

	"github.com/imarsman/nanovms/app/grpcpass"
	"github.com/imarsman/nanovms/app/tweets"
)

var templates *template.Template // templates for dynamic pages
var routeMatch *regexp.Regexp    // template route regex
var count uint64                 // page hit counter
var startTime *time.Time         // start time of server running
var csrfCache *cache.Cache

const ( // various content types
	jsonContentType = "application/json; charset=utf-8"
	textContentType = "text/plain; charset=utf-8"
	htmlContentType = "text/html; charset=utf-8"
)

// PageData data for a page's templates
// Capitalized because it is used in templates and needs to be public
type PageData struct {
	Timestamp     time.Time
	LoadStart     time.Time
	LoadTime      time.Duration
	PageLoads     uint64
	Uptime        time.Duration
	CsrfToken     string
	IPAddress     string
	ServerAddress string
}

// uniqueToken get a random string that can be used as a CSRF header and later to
// fetch the server-stored JSR token string.
func uniqueToken() (token string, err error) {
	id, err := uuid.NewRandom()
	if err != nil {
		return "", err
	}
	token = id.String()

	return token, nil
}

// newPageData create a pointer to a new PageData struct instance
func newPageData() *PageData {
	pd := PageData{}
	pd.LoadStart = time.Now()

	return &pd
}

func getServerAddress(r *http.Request) string {
	ctx := r.Context()

	srvAddr := ctx.Value(http.LocalAddrContextKey).(net.Addr)

	return srvAddr.String()
}

func (pd *PageData) setServerAddress(address string) {
	pd.ServerAddress = address
}

func (pd *PageData) setToken(token string) {
	if token != "" {
		pd.CsrfToken = token
	}
}

// finalize finish off page info that is time specific
func (pd *PageData) finalize() {
	if pd.CsrfToken == "" {
		token, err := uniqueToken()
		if err != nil {
			token = ""
		} else {
			pd.setToken(token)
			err := csrfCache.Add(token, "", cache.DefaultExpiration)
			if err != nil {
				token = ""
			}
		}
		pd.CsrfToken = token
	}

	pd.LoadTime = time.Since(pd.LoadStart)
	pd.PageLoads = counterIncrement()
	pd.Uptime = time.Since(*startTime).Round(time.Second)
}

func findTokenFromRequest(r *http.Request) string {
	// This is not meant to be definitive but rather to avoid doing work for
	// free. The csrf token will be renewed frequently and will expire quickly.
	token := r.URL.Query().Get("csrf")
	if token == "" {
		return ""
	}
	_, ok := csrfCache.Get(token)
	if ok == false {
		return ""
	}
	// Renew cache
	csrfCache.Set(token, "", cache.DefaultExpiration)

	return token
}

// counterIncrement a simple increment of page hit count
func counterIncrement() uint64 {
	return atomic.AddUint64(&count, 1)
}

// init initialize counter and parse templates.
func init() {
	csrfCache = cache.New(5*time.Minute, 2*time.Minute)

	t := time.Now()
	startTime = &t

	// We need to convert the embed FS to an io.FS in order to work with it
	fsys := fs.FS(dynamic)
	contentDynamic, _ := fs.Sub(fsys, "dynamic")

	// Load templates by pattern into a structure for later use
	var err error
	templates, err = template.ParseFS(contentDynamic, "templates/*.html")
	if err != nil {
		log.Println("Cannot parse templates:", err)
		os.Exit(-1)
	}
	// Set up our route matching pattern
	routeMatch, err = regexp.Compile(`^\/(\w+)`)
	if err != nil {
		log.Println("Problems with regular expression:", err)
		os.Exit(-1)
	}
}

func xkcdNoGRPCHandler(w http.ResponseWriter, r *http.Request) {
	bytes, err := grpcpass.FetchRandomXKCD()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	xkcd, err := grpcpass.ParseXKCDJSON(bytes)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	json, err := json.MarshalIndent(&xkcd, "", "  ")
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Header().Add("Content-Type", jsonContentType)
	w.Write(json)

}

func xkcdHandler(w http.ResponseWriter, r *http.Request) {
	// serverAddr := "localhost:9000"
	serverAddr := "[::1]:5222"

	var opts []grpc.DialOption

	opts = append(opts, grpc.WithInsecure())
	opts = append(opts, grpc.WithBlock())

	// Connect with credentials
	// Currently trying only to use TLS to allow GCP to permit the connection
	conn, err := grpc.Dial(serverAddr, grpc.WithTransportCredentials(*ClientCredentials()))
	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}
	client := grpcpass.NewXKCDServiceClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	number := grpcpass.MessageNumber{}
	number.Number = 0

	callOption := grpc.MaxCallRecvMsgSize(5000)
	message, err := client.GetXKCD(ctx, &number, callOption)
	if err != nil {
		log.Fatalf("%v.GetXKCD(_) = _, %v: ", client, err)
	}

	xkcd := grpcpass.NewXKCD()
	xkcd.Number = int(message.GetNumber())
	xkcd.Img = message.GetImg()
	xkcd.Date = message.Date
	xkcd.Title = message.GetTitle()
	xkcd.AltText = message.Alt

	json, err := json.MarshalIndent(&xkcd, "", "  ")
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Header().Add("Content-Type", jsonContentType)
	w.Write(json)
}

// twitterHandler get an id for a tweet
func twitterHandler(w http.ResponseWriter, r *http.Request) {
	findTokenFromRequest(r)

	td, err := tweets.GetTweetData()
	if err != nil { // simulate error getting data
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// fmt.Println("tweet data", td)
	payload, err := json.MarshalIndent(td, "", "  ")
	if err != nil { // simulate error getting data
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Header().Add("Content-Type", jsonContentType)
	w.Write(payload)
}

// templatePageHandler use template collection to produce output
func templatePageHandler(w http.ResponseWriter, r *http.Request) {
	pd := newPageData()

	token := findTokenFromRequest(r)
	address := getServerAddress(r)
	pd.setServerAddress(address)

	matches := routeMatch.FindStringSubmatch(r.URL.Path)
	if len(matches) >= 1 {
		page := matches[1] + ".html"
		if templates.Lookup(page) != nil {
			w.Header().Add("Content-Type", htmlContentType)
			w.WriteHeader(http.StatusOK)
			pd.setToken(token)
			pd.finalize()
			templates.ExecuteTemplate(w, page, pd)
			return
		}
	} else if r.URL.Path == "/" {
		w.Header().Add("Content-Type", htmlContentType)
		w.WriteHeader(http.StatusOK)
		pd.setToken(token)
		pd.finalize()
		templates.ExecuteTemplate(w, "index.html", pd)
		return
	}

	w.WriteHeader(http.StatusNotFound)
	w.Header().Add("Content-Type", textContentType)
	w.Write([]byte("NOT FOUND"))
}

// getTransactionsHandler get list of transactions
func getTransactionsHandler(w http.ResponseWriter, r *http.Request) {

	w.Header().Set("Content-Type", jsonContentType)
	transactionList, err := readTransactions()
	if err != nil { // simulate error getting data
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	sortDescendingPostTimestamp(&transactionList)

	// obscured, err := obscured(transactions)
	transactionList, err = obscureTransactionID(transactionList) // allow for error
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	json, err := toJSON(transactionList)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(json))
}
