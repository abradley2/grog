package main

import (
	"bufio"
	"bytes"
	"crypto"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/websocket"

	"go.etcd.io/bbolt"
)

var cursor int
var wd string

type connection struct {
	m      sync.Mutex
	Cursor *int
	Conn   *websocket.Conn
}

type app struct {
	mux *http.ServeMux
}

func NewApp(db *bbolt.DB, m *sync.Mutex, writes <-chan error) *app {
	var connID int
	wsUpgrader := websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
	}

	wsConns := make(map[int]*connection)

	go func() {
		for {
			err := <-writes
			if err != nil {
				fmt.Printf("Error in writes: %v\n", err)
				continue
			}
			for _, wsConn := range wsConns {
				db.View(func(tx *bbolt.Tx) error {
					b := tx.Bucket(logsBucket)
					c := b.Cursor()
					startRead := *wsConn.Cursor - 20
					if startRead < 0 {
						startRead = 0
					}
					endRead := startRead + 20
					min := []byte(strconv.Itoa(startRead))
					max := []byte(strconv.Itoa(endRead))

					for k, v := c.Seek(min); k != nil && bytes.Compare(k, max) <= 0; k, v = c.Next() {
						prefix := append(k, []byte(":")...)
						wsConn.Conn.WriteMessage(websocket.TextMessage, append(prefix, v...))
					}
					return nil
				})
			}
		}
	}()

	mux := http.NewServeMux()

	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := wsUpgrader.Upgrade(w, r, nil)

		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("Unabled to open websocket connection with this request"))
			return
		}

		var myID int
		var myCursor *int
		*myCursor = cursor

		m.Lock()
		connID++
		myID = connID
		wsConns[myID] = &connection{
			Cursor: myCursor,
			Conn:   conn,
		}
		m.Unlock()

		go func() {
			for _, msg, err := conn.ReadMessage(); err == nil; {
				if err != nil {
					conn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("Error reading message: %s", err)))
					conn.Close()
					return
				}
				nextCursor, err := strconv.Atoi(string(msg))
				if err != nil {
					conn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("Error converting message to int: %s", err)))
					conn.Close()
					return
				}
				*myCursor = nextCursor
			}
		}()

		for {
			// ping every one second and give the client two seconds to read.
			// otherwise the connection is dead and we can remove it
			time.Sleep(time.Second * 1)
			err = conn.SetReadDeadline(time.Now().Add(time.Second * 1))
			if err != nil {
				break
			}
			err = conn.WriteMessage(websocket.PingMessage, []byte{})
			if err != nil {
				break
			}
		}

		defer func() {
			delete(wsConns, myID)
			conn.Close()
		}()
	})

	return &app{
		mux: mux,
	}
}

func (h *app) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mux.ServeHTTP(w, r)
}

func serveDb(db *bbolt.DB, port int, writes <-chan error) error {
	var m sync.Mutex
	// everything in NewApp should actually just be here lol, turns out I didn't need that
	// but I'm keeping it as is for now
	s := http.NewServeMux()
	s.Handle("/", NewApp(db, &m, writes))
	return http.ListenAndServe(fmt.Sprintf(":%d", port), s)
}

var logsBucket []byte

func main() {
	writes := make(chan error)
	logsBucket = []byte("logsBucket")
	wd, err := os.Getwd()

	if err != nil {
		fmt.Printf("Could not get working directory to join with DB_PATH: %v", err)
		return
	}

	dbPath := path.Join(wd, dbName())

	// handle gracefull shutdown
	sthaaaap := make(chan os.Signal, 1)
	signal.Notify(sthaaaap, syscall.SIGTERM)
	signal.Notify(sthaaaap, syscall.SIGINT)

	go func() {
		<-sthaaaap
		os.Remove(dbPath)
		os.Exit(0)
	}()

	// handle shutdown on error
	defer func() {
		os.Remove(dbPath)
		os.Exit(1)
	}()

	defer os.Remove(dbPath)

	db, err := bbolt.Open(dbPath, 0600, nil)

	if err != nil {
		fmt.Printf("Error opening database: %v\n", err)
		return
	}

	err = db.Update(func(tx *bbolt.Tx) error {
		_, err := tx.CreateBucket(logsBucket)

		return err
	})

	if err != nil {
		fmt.Printf("Error creating log bucket: %v\n", err)
		return
	}

	if err != nil {
		fmt.Printf("Error opening bboltdb: %v\n", err)
		return
	}

	portArg := os.Getenv("PORT")

	if portArg == "" {
		fmt.Println("PORT cannot be empty")
		return
	}

	port, err := strconv.Atoi(portArg)

	if err != nil {
		fmt.Println("PORT must be an integer")
		return
	}

	go serveDb(db, port, writes)

	errChan := make(chan error)

	stdin := bufio.NewScanner(os.Stdin)
	stderr := bufio.NewScanner(os.Stderr)
	go runScanner(stdin, db, errChan, os.Stdin)
	go runScanner(stderr, db, errChan, os.Stderr)

	for {
		err = <-errChan
		writes <- err
		if err != nil {
			break
		}
	}
	err = <-errChan
	fmt.Printf("Error in scanner: %v", err)
}

// TODO: this currently only works for ndjson, should just allow it to work with whatever probably
func runScanner(s *bufio.Scanner, db *bbolt.DB, c chan<- error, pipeOut io.Writer) {
	for s.Scan() {
		// just check if this is valid json or a valid primitive
		var js interface{}
		var jsSlice []interface{}
		input := s.Bytes()
		pipeOut.Write(input)
		if json.Unmarshal(input, &js) != nil && json.Unmarshal(input, &jsSlice) != nil {
			continue
		}
		tx, err := db.Begin(true)
		if err != nil {
			c <- fmt.Errorf("Error beginning transaction: %w", err)
		}
		cursor += 1
		b := tx.Bucket(logsBucket)
		b.Put([]byte(strconv.Itoa(cursor)), input)
		c <- tx.Commit()
	}
}

func dbName() string {
	h := crypto.MD5.New()
	io.WriteString(h, wd)
	io.WriteString(h, time.Now().String())
	return fmt.Sprintf("tmp_grog_db_%x", h.Sum(nil))
}
