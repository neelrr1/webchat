package main

import (
	"fmt"
	"html/template"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const PORT = "8080"
const MAX_MESSAGE_LEN = 2000

var messagesLock sync.RWMutex
var messages = []string{"<p>[SYSTEM] Welcome to my chat room!</p>"}

func logRequest(r *http.Request) {
	log.Printf("INFO: %s %s - %s", r.Method, r.URL.Path, r.RemoteAddr)
}

// TODO: Explore not using a polling architecture for messages
// TODO: Can we persist messages or give them a TTL with Redis or something!
func main() {
	http.HandleFunc("/static/", handleStatic)
	http.HandleFunc("/", handleIndex)
	http.HandleFunc("/send", handleSend)
	http.HandleFunc("/messages", handleMessages)

	fmt.Printf("Listening on port %s...\n", PORT)
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%s", PORT), nil))
}

func handleMessages(w http.ResponseWriter, r *http.Request) {
	logRequest(r)
	messagesLock.RLock()
	defer messagesLock.RUnlock()

	fmt.Fprint(w, strings.Join(messages, ""))
}

func handleSend(w http.ResponseWriter, r *http.Request) {
	logRequest(r)
	time := time.Now()

	bytes, err := ioutil.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Error reading request body", http.StatusInternalServerError)
		return
	}
	defer r.Body.Close()

	body := string(bytes)
	body, err = url.QueryUnescape(body)
	if err != nil {
		http.Error(w, "Error parsing request body", http.StatusBadRequest)
	}

	message, found := strings.CutPrefix(body, "message=")
	if !found {
		http.Error(w, "Error parsing request body", http.StatusBadRequest)
	}

	if len(message) == 0 {
		return
	}
	if len(message) > MAX_MESSAGE_LEN {
		message = message[:MAX_MESSAGE_LEN]
	}
	log.Printf("Received %s\n", message)
	message = fmt.Sprintf("<p>[%s] %s: %s</p>", time.Format("2006-01-02 15:04:05"), r.RemoteAddr, template.HTMLEscapeString(message))

	messagesLock.Lock()
	messages = append(messages, message)
	messagesLock.Unlock()

	fmt.Fprint(w, message)
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	logRequest(r)
	http.ServeFile(w, r, "src/index.html")
}

func handleStatic(w http.ResponseWriter, r *http.Request) {
	logRequest(r)
	http.ServeFile(w, r, "."+r.URL.Path)
}
