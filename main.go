package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"runtime"
	"strconv"
	"time"

	"github.com/miekg/dns"
)

const (
	DefaultMaxIdleConnsPerHost       int    = 2
	DefaultIdleConnTimeout           int    = 10
	DefaultDisableKeepAlives         bool   = false
	DefaultMaxWorkers                int    = 500
	DefaultMaxJobsInQueue            int    = 500
	DefaultMaxLength                 int64  = 1048576
	DefaultBusinessGoInputValidation string = "http://blackzone.fritz.box:1234"
)

var (
	MaxIdleConnsPerHost       int
	IdleConnTimeout           int
	DisableKeepAlives         bool
	MaxWorker                 int
	MaxQueue                  int
	MaxLength                 int64
	BusinessGoInputValidation string
)

type PayloadCollection struct {
	Token    string  `json:"token"`
	Payloads Payload `json:"person"`
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
var ResponseError chan int
var ErrorEncode chan error

type TTLCache struct {
	Hostname *string
	TTL      *uint32
	Now      *uint32
}

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
					log.Println(err)
					ErrorEncode <- err
					return
				}

				resp, err := http.Post(fmt.Sprintf("http://%s:%s", *Cache.Hostname, "1234"), "application/json", b)
				//request, err := http.NewRequest("POST", BusinessGoInputValidation, b)
				if err != nil {
					log.Println(err)
					ResponseError <- resp.StatusCode
					return
				}

				//resp, _ := (&http.Client{}).Do(request)
				//rr, _ := httputil.DumpResponse(resp, true)
				//fmt.Printf("%q", string(rr))

				body, err := ioutil.ReadAll(resp.Body)
				if err != nil {
					log.Println(err)
					ErrorEncode <- err
					return
				}

				defer resp.Body.Close()
				ResponseQueue <- []byte(body)

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

	content := &PayloadCollection{}
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")

	err := json.NewDecoder(io.LimitReader(r.Body, MaxLength)).Decode(content)
	if err != nil {
		http.Error(w, "*** invalid payload", http.StatusBadRequest)
		return
	}

	// let's create a job with the payload
	job := Job{Payload: content.Payloads}

	// Push the work onto the queue.
	JobQueue <- job

	for {
		select {
		case response := <-ResponseQueue:
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(response))
			return
		case responseError := <-ResponseError:
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(strconv.Itoa(responseError)))
			return
		case errorEncode := <-ErrorEncode:
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(errorEncode.Error()))
			return
		}
	}

}

var Cache *TTLCache

func setDNS() *TTLCache {
	config, _ := dns.ClientConfigFromFile("/etc/resolv.conf")
	c := new(dns.Client)
	m := new(dns.Msg)

	u, _ := url.Parse(BusinessGoInputValidation)
	host, _, _ := net.SplitHostPort(u.Host)

	m.SetQuestion(dns.Fqdn(host), dns.TypeA)
	m.RecursionDesired = true
	r, _, err := c.Exchange(m, net.JoinHostPort(config.Servers[0], config.Port))
	if r == nil {
		log.Fatalf("*** %s\n", err.Error())
	}
	if r.Rcode != dns.RcodeSuccess {
		log.Fatalf("*** invalid answer name for %s\n", host)
	}

	now := uint32(time.Now().Unix())

	ip, _ := net.LookupIP(host)
	fmt.Println(ip)

	var hostname string
	var ttl uint32

	// net.LookupIP(host string) (ips []IP, err error)
	s := regexp.MustCompile("(?:[0-9]{1,3}.){3}[0-9]{1,3}")
	ipaddress := s.FindString(fmt.Sprintf("%v", r.Answer))

	for _, a := range r.Answer {
		hostname = ipaddress
		ttl = now + uint32(a.Header().Ttl)
	}

	return &TTLCache{
		Now:      &now,
		Hostname: &hostname,
		TTL:      &ttl,
	}
}

func checkDNS() {
	for {
		if *Cache.Now > *Cache.TTL {
			// TTL has been expired, send another DNS request
			Cache = setDNS()
		} else {
			*Cache.Now = uint32(time.Now().Unix())
		}
		time.Sleep(time.Second)
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

	if MaxIdleConnsPerHost, err = strconv.Atoi(os.Getenv("MaxIdleConnsPerHost")); err != nil {
		MaxIdleConnsPerHost = DefaultMaxIdleConnsPerHost
	}
	if IdleConnTimeout, err = strconv.Atoi(os.Getenv("IdleConnTimeout")); err != nil {
		IdleConnTimeout = DefaultIdleConnTimeout
	}
	if DisableKeepAlives, err = strconv.ParseBool(os.Getenv("DisableKeepAlives")); err != nil {
		DisableKeepAlives = DefaultDisableKeepAlives
	}

	JobQueue = make(chan Job, MaxQueue)
	ResponseQueue = make(chan []byte, MaxQueue)

}

func main() {
	runtime.GOMAXPROCS(4)

	initialize()
	Cache = setDNS()
	go checkDNS()

	dispatcher := NewDispatcher(MaxWorker)
	dispatcher.Run()

	http.HandleFunc("/", payloadHandler)
	log.Println("listening on localhost:8080")

	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "HEALTHY")
	})

	err := http.ListenAndServe(":8080", nil)
	fmt.Println(err)

}
