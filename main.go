package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strconv"
)

const (
	DefaultMaxWorkers                int    = 500
	DefaultMaxJobsInQueue            int    = 500
	DefaultMaxLength                 int64  = 1048576
	DefaultBusinessGoInputValidation string = "http://127.0.0.1:1234"
)

var (
	MaxWorker                 int
	MaxQueue                  int
	MaxLength                 int64
	BusinessGoInputValidation string
)

type PayloadCollection struct {
	Token    string    `json:"token"`
	Payloads []Payload `json:"person"`
}

type Payload struct {
	Email string `json:"email"`
}

// Job represents the job to be run
type Job struct {
	Payload Payload
}

// A buffered channel that we can send work requests on.
var JobQueue chan Job
var ResponseQueue chan []byte

// Worker represents the worker that executes the job
type Worker struct {
	WorkerPool chan chan Job
	JobChannel chan Job
	quit       chan bool
}

func NewWorker(workerPool chan chan Job) Worker {
	return Worker{
		WorkerPool: workerPool,
		JobChannel: make(chan Job),
		quit:       make(chan bool)}
}

// Start method starts the run loop for the worker, listening for a quit channel in
// case we need to stop it
func (w Worker) Start() {
	go func() {
		for {
			// register the current worker into the worker queue.
			w.WorkerPool <- w.JobChannel

			select {
			case job := <-w.JobChannel:
				// we have received a work request.
				b := new(bytes.Buffer)
				err := json.NewEncoder(b).Encode(job.Payload)
				if err != nil {
					//w.WriteHeader(http.StatusBadRequest)
					//w.Write([]byte(fmt.Sprintf("%v", err)))
					ResponseQueue <- []byte(err.Error())
					return
				}
				resp, err := http.Post(BusinessGoInputValidation, "application/json", b)
				if err != nil {
					//w.WriteHeader(http.StatusBadRequest)
					//w.Write([]byte(fmt.Sprintf("%v", err)))
					ResponseQueue <- []byte(err.Error())
					return
				}
				body, _ := ioutil.ReadAll(resp.Body)

				defer resp.Body.Close()
				ResponseQueue <- body

			case <-w.quit:
				// we have received a signal to stop
				return
			}
		}
	}()
}

// Stop signals the worker to stop listening for work requests.
func (w Worker) Stop() {
	go func() {
		w.quit <- true
	}()
}

type Dispatcher struct {
	// A pool of workers channels that are registered with the dispatcher
	WorkerPool chan chan Job
}

func NewDispatcher(maxWorkers int) *Dispatcher {
	pool := make(chan chan Job, maxWorkers)
	return &Dispatcher{WorkerPool: pool}
}

func (d *Dispatcher) Run() {
	// starting n number of workers
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
			// a job request has been received
			go func(job Job) {
				// try to obtain a worker job channel that is available.
				// this will block until a worker is idle
				jobChannel := <-d.WorkerPool

				// dispatch the job to the worker job channel
				jobChannel <- job
			}(job)
		}
	}
}

func payloadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("HEALTHY"))
		return
	}

	// Read the body into a string for json decoding
	var content = &PayloadCollection{}

	err := json.NewDecoder(io.LimitReader(r.Body, MaxLength)).Decode(&content)
	if err != nil {
		w.Header().Set("Content-Type", "application/json; charset=UTF-8")
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if content.Payloads == nil {
		w.Header().Set("Content-Type", "application/json; charset=UTF-8")
		http.Error(w, "ERROR: Payload error", http.StatusBadRequest)
		return
	}

	// Go through each payload and queue items individually to be posted to S3
	for _, payload := range content.Payloads {

		if payload.Email == "" {
			w.Header().Set("Content-Type", "application/json; charset=UTF-8")
			http.Error(w, "ERROR: Email field is missing", http.StatusBadRequest)
			return
		}
		// let's create a job with the payload
		job := Job{Payload: payload}

		// Push the work onto the queue.
		JobQueue <- job

	}

	for {
		select {
		case response := <-ResponseQueue:
			w.Header().Set("Content-Type", "application/json; charset=UTF-8")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(response))
			return
		}
	}

}

func initialize() {
	var err error
	if MaxWorker, err = strconv.Atoi(os.Getenv("MAX_WORKERS")); err != nil {
		MaxWorker = DefaultMaxWorkers
	}

	if MaxQueue, err = strconv.Atoi(os.Getenv("MAX_QUEUES")); err != nil {
		MaxQueue = DefaultMaxJobsInQueue
	}

	if MaxLength, err = strconv.ParseInt(os.Getenv("MAX_LENGTH"), 10, 64); err != nil {
		MaxLength = DefaultMaxLength
	}

	BusinessGoInputValidation = os.Getenv("BUSINESS_GO_INPUT_VALIDATION_URL")
	if BusinessGoInputValidation == "" {
		BusinessGoInputValidation = DefaultBusinessGoInputValidation
	}

	JobQueue = make(chan Job, MaxQueue)
	ResponseQueue = make(chan []byte, MaxQueue)

}

func main() {
	initialize()
	dispatcher := NewDispatcher(MaxWorker)
	dispatcher.Run()

	http.HandleFunc("/", payloadHandler)
	log.Println("listening on localhost:8080")
	err := http.ListenAndServe(":8080", nil)
	fmt.Println(err)

}
