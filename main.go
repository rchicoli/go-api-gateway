/*
Inspired by
    http://marcio.io/2015/07/handling-1-million-requests-per-minute-with-golang/

*/

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
)

const (
	DEFAULT_MAX_WORKERS       int   = 500
	DEFAULT_MAX_JOBS_IN_QUEUE int   = 500
	DEFAULT_MAX_LENGTH        int64 = 1048576
)

var (
	MaxWorker int
	MaxQueue  int
	MaxLength int64
)

type PayloadCollection struct {
	Token    string    `json:"token"`
	Payloads []Payload `json:"person"`
}

type Payload struct {
	Email string `json:"email"`
}

type Job struct {
	Payload Payload
}

type Dispatcher struct {
	WorkerPool chan chan Job
}

var JobQueue chan Job

type Worker struct {
	WorkerPool chan chan Job
	JobChannel chan Job
	quit       chan bool
}

func (p *Payload) EmailCheck() error {
	b := new(bytes.Buffer)
	err := json.NewEncoder(b).Encode(p)
	if err != nil {
		return err
	}
	r, err := http.Post("http://127.0.0.1:1234", "application/json", b)
	if err != nil {
		return err
	}
	fmt.Println(r.StatusCode)

	defer r.Body.Close()

	//client := &http.Client{}
	//r, _ := http.NewRequest("POST", "http://127.0.0.1:1234", p)
	//w, _ := client.Do(r)
	//fmt.Println(w.Status)
	//fmt.Println(w.Header)
	return nil
}

func NewWorker(workerPool chan chan Job) Worker {
	return Worker{
		WorkerPool: workerPool,
		JobChannel: make(chan Job),
		quit:       make(chan bool)}
}

func (w Worker) Start() {
	go func() {
		for {
			// register the current worker into the worker queue.
			w.WorkerPool <- w.JobChannel

			select {
			case job := <-w.JobChannel:
				if err := job.Payload.EmailCheck(); err != nil {
					log.Printf("Error Post to Backend: %s", err.Error())
				}
			case <-w.quit:
				return
			}
		}
	}()
}

func (w Worker) Stop() {
	go func() {
		w.quit <- true
	}()
}

func NewDispatcher(maxWorkers int) *Dispatcher {
	pool := make(chan chan Job, maxWorkers)
	return &Dispatcher{WorkerPool: pool}
}

func (d *Dispatcher) Run() {
	for i := 0; i < MaxWorker; i++ {
		worker := NewWorker(d.WorkerPool)
		worker.Start()
	}
	go d.dispatch()
}

func (d *Dispatcher) dispatch() {
	for {
		select {
		case job := <-JobQueue:
			go func(job Job) {
				// try to obtain a worker job channel that is available.
				// this will block until a worker is idle
				jobChannel := <-d.WorkerPool

				jobChannel <- job
			}(job)
		}
	}
}

func payloadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var content = &PayloadCollection{}

	err := json.NewDecoder(io.LimitReader(r.Body, MaxLength)).Decode(&content)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(fmt.Sprintf("%v", err)))
		return
	}

	for _, payload := range content.Payloads {
		work := Job{Payload: payload}
		JobQueue <- work
	}

	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	w.WriteHeader(http.StatusOK)
}

func initialize() {
	var err error
	if MaxWorker, err = strconv.Atoi(os.Getenv("MAX_WORKERS")); err != nil {
		MaxWorker = DEFAULT_MAX_WORKERS
	}

	if MaxQueue, err = strconv.Atoi(os.Getenv("MAX_QUEUES")); err != nil {
		MaxQueue = DEFAULT_MAX_JOBS_IN_QUEUE
	}

	if MaxLength, err = strconv.ParseInt(os.Getenv("MAX_LENGTH"), 10, 64); err != nil {
		MaxLength = DEFAULT_MAX_LENGTH
	}

	JobQueue = make(chan Job, MaxQueue)
}

func main() {
	initialize()
	dispatcher := NewDispatcher(MaxWorker)
	dispatcher.Run()

	http.HandleFunc("/", payloadHandler)
	err := http.ListenAndServe(":8080", nil)
	if err != nil {
		fmt.Println(err)
	}
}
