package main

import (
	"bufio"
	"crypto"
	_ "embed"
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

//go:embed assets/ui.html
var uiHTML []byte

//go:embed assets/ui.js
var uiJS []byte

var cursor int
var wd string

type Connection struct {
	m      *sync.Mutex
	Cursor int
	Conn   *websocket.Conn
	db     *bbolt.DB
}

func (c *Connection) Listen() {
	go func() {
		for _, msg, err := c.Conn.ReadMessage(); err == nil; {
			if err != nil {
				c.WriteTextMessage([]byte(fmt.Sprintf("error:reading message:%v", err)))
				c.Conn.Close()
				return
			}
			nextCursor, err := strconv.Atoi(string(msg))
			if err != nil {
				c.WriteTextMessage([]byte(fmt.Sprintf("error:converting message to int:%s", err)))
				c.Conn.Close()
				return
			}
			c.Cursor = nextCursor
		}
	}()
}

func (c *Connection) UpdateClient() error {
	return c.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket(logsBucket)
		curs := b.Cursor()
		k, v := curs.Seek([]byte(strconv.Itoa(c.Cursor - 10)))
		var batchMsg []byte
		nl := []byte("\n")
		for i := 0; i < 20; i++ {
			if k == nil {
				break
			}
			msg := []byte(fmt.Sprintf("%s:%s", k, v))
			batchMsg = append(batchMsg, msg...)

			k, v = curs.Next()
			if k != nil {
				batchMsg = append(batchMsg, nl...)
			}
		}
		return c.WriteTextMessage(batchMsg)
	})
}

func (c *Connection) WriteTextMessage(msg []byte) error {
	c.m.Lock()
	defer c.m.Unlock()
	return c.Conn.WriteMessage(websocket.TextMessage, msg)
}

func (c *Connection) KeepAlive() {
	for {
		// ping every one second and give the client two seconds to read.
		// otherwise the connection is dead and we can remove it
		time.Sleep(time.Second * 1)
		c.m.Lock()
		err := c.Conn.SetReadDeadline(time.Now().Add(time.Second * 1))
		if err != nil {
			break
		}
		err = c.Conn.WriteMessage(websocket.PingMessage, []byte{})
		c.m.Unlock()
		if err != nil {
			break
		}
	}
}

func NewConnection(conn *websocket.Conn, db *bbolt.DB) *Connection {
	var m sync.Mutex
	return &Connection{
		m:      &m,
		Cursor: cursor,
		Conn:   conn,
		db:     db,
	}
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

	wsConns := make(map[int]*Connection)

	go func() {
		for {
			err := <-writes
			if err != nil {
				fmt.Printf("Error in writes: %v\n", err)
				continue
			}
			for _, wsConn := range wsConns {
				wsConn.UpdateClient()
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

		m.Lock()
		connID++
		myID := connID
		wsConn := NewConnection(conn, db)
		wsConns[myID] = wsConn
		m.Unlock()

		wsConn.Listen()

		wsConn.KeepAlive()

		delete(wsConns, myID)
		conn.Close()
	})

	mux.HandleFunc("/ui.js", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Header().Set("Content-Type", "application/javascript")
		w.Write(uiJS)
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Header().Add("Content-Type", "text/html")
		w.Write(uiHTML)
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
	go runScanner(stdin, db, errChan)
	go runScanner(stderr, db, errChan)

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

func runScanner(s *bufio.Scanner, db *bbolt.DB, c chan<- error) {
	for s.Scan() {
		var js interface{}
		var jsSlice []interface{}
		input := s.Bytes()
		if json.Unmarshal(input, &js) == nil || json.Unmarshal(input, &jsSlice) == nil {
			input = append([]byte("json:"), input...)
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
