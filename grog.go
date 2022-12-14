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

var cursor int64
var wd string

type Connection struct {
	m      *sync.Mutex
	Cursor *int64
	Conn   *websocket.Conn
	db     *bbolt.DB
}

func (c *Connection) Listen() {
	go func() {
		for {
			_, msg, err := c.Conn.ReadMessage()
			if err != nil {
				c.WriteTextMessage([]byte(fmt.Sprintf("error:reading message:%v", err)))
				c.Conn.Close()
				return
			}
			nextCursor, err := strconv.ParseInt(string(msg), 10, 64)
			if err != nil {
				c.WriteTextMessage([]byte(fmt.Sprintf("error:converting message to int:%s", err)))
				c.Conn.Close()
				return
			}
			*c.Cursor = nextCursor
		}
	}()
}

func (c *Connection) UpdateClient() error {
	return c.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket(logsBucket)
		curs := b.Cursor()
		start := *c.Cursor - 40
		if start < 1 {
			start = 1
		}
		k, v := curs.Seek(formatCursor(start))
		batchMsg := []string{}
		for i := 0; i < 80; i++ {
			if k == nil {
				break
			}
			kval, err := strconv.ParseInt(string(k), 10, 64)
			if err != nil {
				return fmt.Errorf("Found invalid key when updating a client: %w", err)
			}
			msg := fmt.Sprintf("%d:%s", kval, v)
			batchMsg = append(batchMsg, msg)

			k, v = curs.Next()
		}
		js, err := json.Marshal(batchMsg)
		if err != nil {
			return fmt.Errorf("Error marshaling batch message: %w", err)
		}
		// ignore errors on write here, we have handling on dead connections
		// and we should only send an error for invalid keys/messages instead
		c.WriteTextMessage(js)
		return nil
	})
}

func (c *Connection) WriteTextMessage(msg []byte) error {
	c.m.Lock()
	defer c.m.Unlock()
	return c.Conn.WriteMessage(websocket.TextMessage, msg)
}

func (c *Connection) KeepAlive() {
	for {
		// ping a connection every 10 seconds with a read deadline of half a
		// second to read a ping message
		time.Sleep(time.Second * 10)
		c.m.Lock()
		c.Conn.SetReadDeadline(time.Now().Add(time.Millisecond * 500))
		err := c.Conn.WriteMessage(websocket.PingMessage, []byte{})
		c.Conn.SetReadDeadline(time.Time{})
		c.m.Unlock()
		if err != nil {
			break
		}
	}
}

func NewConnection(conn *websocket.Conn, db *bbolt.DB) *Connection {
	var m sync.Mutex
	myCursor := cursor
	return &Connection{
		m:      &m,
		Cursor: &myCursor,
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
				err = wsConn.UpdateClient()
				if err != nil {
					fmt.Printf("Error in UpdateClient, check for bad data: %v\n", err)
					// TODO: I think more likely we should gracefully shut down here
					continue
				}
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
		w.Header().Set("Content-Type", "application/javascript")
		w.Write(uiJS)
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
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

	// redundant removal just in case previous cleanup failed somehow
	os.Remove(dbName())

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
		b.Put(formatCursor(cursor), input)
		c <- tx.Commit()
	}
}

func dbName() string {
	h := crypto.MD5.New()
	io.WriteString(h, wd)
	io.WriteString(h, os.Getenv("PORT"))
	return fmt.Sprintf("tmp_grog_db_%x", h.Sum(nil))
}

func formatCursor(c int64) []byte {
	s := strconv.FormatInt(c, 10)

	for len(s) < 20 {
		s = fmt.Sprintf("0%s", s)
	}
	return []byte(s)
}
