// matrix-websockets-proxy provides a websockets interface to a Matrix
// homeserver implementing the REST API.
//
// It listens on a TCP port (localhost:8009 by default), and turns any
// incoming connections into REST requests to the configured homeserver.
//
// It exposes only the one HTTP endpoint '/stream'. It is intended
// that an SSL-aware reverse proxy (such as Apache or nginx) be used in front
// of it, to direct most requests to the homeserver, but websockets requests
// to this proxy.
//
// You can also visit http://localhost:8009/test/test.html, which is a very
// simple client for testing the websocket interface.
//
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"runtime"

	"github.com/gorilla/websocket"
	"github.com/krombel/matrix-websockets-proxy/proxy"
)

var compress = flag.Bool("compress", false, "Enable compression of the WebSocket-Connection with per-message-deflate")
var port = flag.Int("port", 8009, "TCP port to listen on")
var upstreamURL = flag.String("upstream", "http://localhost:8008/", "URL of upstream server")
var testHTML *string

func init() {
	_, srcfile, _, _ := runtime.Caller(0)
	def := filepath.Join(filepath.Dir(srcfile), "test")
	testHTML = flag.String("testdir", def, "Path to the HTML test resources")
}

func main() {
	flag.Parse()
	fmt.Println("Starting websocket/EventSource server on port", *port)
	http.Handle("/test/", http.StripPrefix("/test/", http.FileServer(http.Dir(*testHTML))))
	http.HandleFunc("/stream", serveStream)
	http.HandleFunc("/events", serveEventSource)
	err := http.ListenAndServe(fmt.Sprintf(":%d", *port), nil)
	log.Fatal("ListenAndServe: ", err)
}

// Handle a request to /events
//
func serveEventSource(rw http.ResponseWriter, req *http.Request) {
	log.Println("Got new EventSource request")

	if req.Method != "GET" {
		log.Println("Invalid method", req.Method)
		httpError(rw, http.StatusMethodNotAllowed)
		return
	}

	// Make sure that the writer supports flushing.
	flusher, ok := rw.(http.Flusher)

	if !ok {
		http.Error(rw, "Streaming unsupported!", http.StatusInternalServerError)
		return
	}

	// Listen to connection close and un-register messageChan
	notify := rw.(http.CloseNotifier).CloseNotify()
	connectionExists := true
	go func() {
		<-notify
		log.Println("Remote closed connection")
		connectionExists = false
	}()

	accessToken := req.Header.Get("Authorization")
	if accessToken != "" {
		if accessToken[0:6] == "Bearer " {
			accessToken = accessToken[7:len(accessToken)]
		}
	} else {
		accessToken = req.URL.Query().Get("access_token")
	}
	//log.Println("Recognized AccessToken: " + accessToken)

	client := proxy.NewClient(*upstreamURL, accessToken)
	client.Filter = req.URL.Query().Get("filter")
	client.Presence = req.URL.Query().Get("presence")

	client.NextSyncBatch = req.Header.Get("last-event-id")
	if client.NextSyncBatch == "" {
		// only use since when we are sure that there is no EventSource
		// implementation is currently reconnecting
		client.NextSyncBatch = req.URL.Query().Get("since")
	}
	log.Println("identified following sync token:", client.NextSyncBatch)

	rw.Header().Set("Content-Type", "text/event-stream")
	rw.Header().Set("Cache-Control", "no-cache")
	rw.Header().Set("Connection", "keep-alive")
	rw.Header().Set("Access-Control-Allow-Origin", "*")

	content, err := client.Sync(false)
	for {
		if err != nil {
			log.Println("Error in sync", err)
			switch err.(type) {
			case *proxy.MatrixError:
				handleHttpError(rw, err.(*proxy.MatrixError).HttpError)
			case *proxy.HttpError:
				handleHttpError(rw, *(err.(*proxy.HttpError)))
			default:
				httpError(rw, http.StatusInternalServerError)
			}
			return
		}

		// Write to the ResponseWriter
		// Server Sent Events compatible
		fmt.Fprintf(rw, "id: %s\n", client.NextSyncBatch)
		fmt.Fprintf(rw, "event: sync\n")
		content, err = PopNextBatch(content)
		fmt.Fprintf(rw, "data: %s\n\n", content)

		// Flush the data immediatly instead of buffering it for later.
		flusher.Flush()

		if !connectionExists {
			return
		}
		content, err = client.Sync(true)
	}

}

// Handle a request to /events
//
func serveStream(w http.ResponseWriter, r *http.Request) {
	log.Println("Got websocket request to", r.URL)

	if r.Method != "GET" {
		log.Println("Invalid method", r.Method)
		httpError(w, http.StatusMethodNotAllowed)
		return
	}

	accessToken := r.Header.Get("Authorization")
	if accessToken != "" {
		if accessToken[0:6] == "Bearer " {
			accessToken = accessToken[7:len(accessToken)]
		}
	} else {
		accessToken = r.URL.Query().Get("access_token")
	}
	log.Println("Recognized AccessToken: " + accessToken)

	client := proxy.NewClient(*upstreamURL, accessToken)
	client.NextSyncBatch = r.URL.Query().Get("since")
	client.Filter = r.URL.Query().Get("filter")
	client.UpdatePresence(r.URL.Query().Get("presence"))

	msg, err := client.Sync(false)
	if err != nil {
		log.Println("Error in sync", err)
		switch err.(type) {
		case *proxy.MatrixError:
			handleHttpError(w, err.(*proxy.MatrixError).HttpError)
		case *proxy.HttpError:
			handleHttpError(w, *(err.(*proxy.HttpError)))
		default:
			httpError(w, http.StatusInternalServerError)
		}
		return
	}

	upgrader := websocket.Upgrader{
		EnableCompression: *compress,
		Subprotocols:      []string{"m.json"},
	}
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
		return
	}
	ws.EnableWriteCompression(*compress)

	c := proxy.New(client, ws)
	c.SendMessage(msg)
	c.Start()
}

func httpError(w http.ResponseWriter, status int) {
	http.Error(w, http.StatusText(status), status)
}

func handleHttpError(w http.ResponseWriter, errp proxy.HttpError) {
	w.Header().Set("Content-Type", errp.ContentType)
	w.WriteHeader(errp.StatusCode)
	w.Write(errp.Body)
}

// PopNextBatch pops NextBatch from a /sync response
func PopNextBatch(in []byte) (out []byte, err error) {
	type SyncResponse struct {
		AccountData    interface{} `json:"account_data",omit`
		ToDevice       interface{} `json:"to_device",omit`
		DeviceLists    interface{} `json:"device_lists",omit`
		Presence       interface{} `json:"presence",omit`
		Rooms          interface{} `json:"rooms",omit`
		Groups         interface{} `json:"groups",omit`
		DeviceOTKCount int         `json:"device_one_time_keys_count",omit`
		NextBatch      string      `json:"next_batch"`
	}

	var jsr SyncResponse
	if err = json.Unmarshal(in, &jsr); err != nil {
		return
	}
	jsr.NextBatch = ""

	out, err = json.Marshal(jsr)
	return
}
