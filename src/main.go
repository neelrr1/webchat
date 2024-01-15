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

var newMessage = sync.NewCond(&messagesLock)

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
	headers.Add("Access-Control-Allow-Headers", "Accept, Accept-Language, Content-Language, Connection, Content-Type, Cache-Control, hx-target, hx-current-url, hx-trigger, hx-request")
	headers.Add("Access-Control-Allow-Methods", "*")
}

type Header struct {
	key   string
	value string
}

// Wrapper function to execute custom "middleware"
func handleRoute(pattern string, handler func(http.ResponseWriter, *http.Request, *int), headers []Header) {
	http.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		logRequest(r)

		setCorsHeaders(w, r)
		for _, header := range headers {
			w.Header().Add(header.key, header.value)
		}

		status := http.StatusOK
		if r.Method != "OPTIONS" {
			handler(w, r, &status)
		}
		w.WriteHeader(status)
	})
}

// TODO: Can we persist messages with Redis?! + implement connection pooling!
func main() {
	http.HandleFunc("/static/", handleStatic)
	http.HandleFunc("/", handleIndex)

	// These handlers need middleware
	handleRoute("/send", handleSend, nil)
	handleRoute("/messages", handleMessages, nil)
	handleRoute("/subscribe", handleSubscribe, []Header{{"Content-Type", "text/event-stream"}, {"Cache-Control", "no-store"}, {"Connection", "keep-alive"}})

	fmt.Printf("Listening on port %s...\n", PORT)
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%s", PORT), nil))
}

func handleSubscribe(w http.ResponseWriter, r *http.Request, status *int) {
	rc := http.NewResponseController(w)

	newMessage.L.Lock()
	for {
		// Avoid holding locks across IO calls
		formattedData := strings.ReplaceAll(strings.Join(messages, ""), "\n", " ")
		newMessage.L.Unlock()

		fmt.Fprintf(w, "data: %s\n\n", formattedData)

		err := rc.Flush()
		if err != nil {
			log.Println("Unable to flush http data!")
			return
		}

		newMessage.L.Lock()
		newMessage.Wait()
	}

	newMessage.L.Unlock()
}

func handleMessages(w http.ResponseWriter, r *http.Request, status *int) {
	// Avoid holding locks across IO calls
	messagesLock.RLock()
	out := strings.Join(messages, "")
	messagesLock.RUnlock()

	fmt.Fprint(w, out)
}

func handleSend(w http.ResponseWriter, r *http.Request, status *int) {
	time := time.Now()
	*status = http.StatusNoContent

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
	log.Printf("Received \"%s\"\n", message)

	chattersLock.Lock()
	chatter, exists := chatters[getClientIp(r)]

	if !exists {
		chatter = Chatter{getClientIp(r), getClientIp(r), "green"}
		chatters[getClientIp(r)] = chatter
	}
	chattersLock.Unlock()

	if message[0] == '/' {
		messagesLock.Lock()
		handleSlash(message, chatter)
		messagesLock.Unlock()
		return
	}

	message = fmt.Sprintf(`<p style="color: %s;">[%s] %s: %s</p>`, chatter.color, time.Format("2006-01-02 15:04:05"), chatter.name, template.HTMLEscapeString(message))

	messagesLock.Lock()
	messages = append(messages, message)
	newMessage.Broadcast()
	messagesLock.Unlock()
}

func handleSlash(message string, c Chatter) {
	if message == "/wipe" || message == "/clear" {
		messages = messages[:1]
		newMessage.Broadcast()
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
