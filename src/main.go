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

const PORT = "80"
const MAX_MESSAGE_LEN = 2000

var messagesLock sync.RWMutex
var messages = []string{"<p>[SYSTEM] Welcome to my chat room!</p>"}

var chattersLock sync.RWMutex
var chatters = map[string]Chatter{}

type Chatter struct {
	ip    string
	name  string
	color string
}

func getClientIp(r *http.Request) string {
	cf_ip := r.Header.Get("CF-Connecting-IP")
	if len(cf_ip) > 0 {
		return cf_ip
	}

	return r.RemoteAddr
}

// "Middleware" functions
func logRequest(r *http.Request) {
	log.Printf("INFO: %s %s - %s", r.Method, r.URL.Path, getClientIp(r))
}

func setCorsHeaders(w http.ResponseWriter, r *http.Request) {
	origin := r.Header.Get("Origin")
	headers := w.Header()
	headers.Add("Access-Control-Allow-Origin", origin)
	headers.Add("Access-Control-Allow-Headers", "Accept, Accept-Language, Content-Language, Content-Type, hx-target, hx-current-url, hx-trigger, hx-request")
	headers.Add("Access-Control-Allow-Methods", "*")
	w.WriteHeader(http.StatusOK)
}

// Wrapper function to execute custom "middleware"
func handleRoute(pattern string, handler func(http.ResponseWriter, *http.Request)) {
	http.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		logRequest(r)

		setCorsHeaders(w, r)
		if r.Method != "OPTIONS" {
			handler(w, r)
		}
	})
}

// TODO: Explore not using a polling architecture for messages
// TODO: Can we persist messages or give them a TTL with Redis or something! + Docker Compose?!
func main() {
	http.HandleFunc("/static/", handleStatic)
	http.HandleFunc("/", handleIndex)

	// These handlers need middleware
	handleRoute("/send", handleSend)
	handleRoute("/messages", handleMessages)

	fmt.Printf("Listening on port %s...\n", PORT)
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%s", PORT), nil))
}

func handleMessages(w http.ResponseWriter, r *http.Request) {
	messagesLock.RLock()
	defer messagesLock.RUnlock()

	fmt.Fprint(w, strings.Join(messages, ""))
}

func handleSend(w http.ResponseWriter, r *http.Request) {
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

	chattersLock.Lock()
	chatter, exists := chatters[getClientIp(r)]

	if !exists {
		chatter = Chatter{getClientIp(r), getClientIp(r), "green"}
		chatters[getClientIp(r)] = chatter
	}
	chattersLock.Unlock()

	if message[0] == '/' {
		handleSlash(message, chatter)
		return
	}

	message = fmt.Sprintf(`<p style="color: %s;">[%s] %s: %s</p>`, chatter.color, time.Format("2006-01-02 15:04:05"), chatter.name, template.HTMLEscapeString(message))

	messagesLock.Lock()
	messages = append(messages, message)
	messagesLock.Unlock()

	fmt.Fprint(w, message)
}

func handleSlash(message string, c Chatter) {
	if message == "/wipe" || message == "/clear" {
		messages = messages[:1]
	} else if message[:5] == "/nick" {
		c.name = template.HTMLEscapeString(message[5:])
		chatters[c.ip] = c
	} else if message[:6] == "/color" {
		c.color = template.HTMLEscapeString(message[6:])
		chatters[c.ip] = c
	}
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	logRequest(r)
	http.ServeFile(w, r, "src/index.html")
}

func handleStatic(w http.ResponseWriter, r *http.Request) {
	logRequest(r)
	http.ServeFile(w, r, "."+r.URL.Path)
}
