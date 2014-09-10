// Package shed provides a basic mechanism to test the latency of the Go
// runtime scheduler. When imported, it periodically performs a series of
// short benchmarks and records the timings. These include:
// 	- An unbuffered channel send. ("ChanSend")
// 	- Sending a value from one goroutine to another and back. ("PingPong")
// 	- How much longer a goroutine takes to wake after its sleep period.
// 	  ("Oversleep")
// 	- How long it takes to create 20 goroutines and pass a message through
// 	  all of them. ("Chain")
package sched

import (
	"bytes"
	"fmt"
	"sync"
	"time"
)

// These values may be changed to configure the thresholds observed by Check.
var (
	OversleepThreshold = 3 * time.Millisecond
	ChanSendThreshold  = 10 * time.Microsecond
	PingPongThreshold  = 20 * time.Microsecond
	ChainThreshold     = 400 * time.Microsecond
)

// Warner is anything that can log warnings.
// This is usually appengine.Context.
type Warner interface {
	Warningf(string, ...interface{})
}

// Check tests whether we recently observed samples that exceeded the
// thresholds and, if so, uses the provided Warner to log a warning message
// containing a table of the most recent samples.
//
// For example:
// 	func handler(w http.ResponseWriter, r *http.Request) {
// 		ctx := appengine.NewContext(r)
// 		sched.Check(ctx)
// 		// the rest of your code as usual
// 	}
func Check(w Warner) {
	checkChan <- w
}

const (
	sampleInterval   = 1 * time.Second
	testSleep        = 50 * time.Millisecond
	historySize      = 100
	numChainRoutines = 20
)

var (
	mu        sync.Mutex
	nextIndex int
	samples   [historySize]sample
)

type sample struct {
	start     time.Time
	oversleep time.Duration // undesired extra sleep latency
	bufSend   time.Duration // send on a buffered channel
	pingPong  time.Duration // ping-pong with goroutine on buffered channel
	chain     time.Duration
}

func init() {
	go channelHelper()
	go collectSampleLoop()
}

var (
	unbufc = make(chan bool)
	bufc   = make(chan bool, 1)
)

func collectSampleLoop() {
	ticker := time.NewTicker(sampleInterval - testSleep)
	var bad *sample
	for {
		select {
		case <-ticker.C:
			s := collectSample()
			if overThreshold(s) {
				bad = &s
			}
		case w := <-checkChan:
			if bad != nil {
				w.Warningf("Recent sample exceeded threshold.\nLast %v samples:\n%s", historySize, highlightSample(*bad))
				bad = nil
			}
		}
	}
}

func overThreshold(s sample) bool {
	return s.oversleep > OversleepThreshold ||
		s.bufSend > ChanSendThreshold ||
		s.pingPong > PingPongThreshold ||
		s.chain > ChainThreshold
}

var checkChan = make(chan Warner)

func channelHelper() {
	for {
		unbufc <- <-bufc
	}
}

func collectSample() sample {
	var s sample

	s.start = time.Now()
	time.Sleep(testSleep)
	t1 := time.Now()
	s.oversleep = t1.Sub(s.start) - testSleep

	bufc <- true
	t2 := time.Now()
	s.bufSend = t2.Sub(t1)
	<-unbufc
	t3 := time.Now()
	s.pingPong = t3.Sub(t2)

	head := make(chan bool)
	tail := head
	for i := 0; i < numChainRoutines; i++ {
		ch := make(chan bool)
		go func(a, b chan bool) {
			b <- <-a
		}(tail, ch)
		tail = ch
	}
	head <- true
	<-tail
	s.chain = time.Now().Sub(t3)

	mu.Lock()
	defer mu.Unlock()
	idx := nextIndex
	nextIndex = (nextIndex + 1) % historySize
	samples[idx] = s

	return s
}

const header = "| " +
	"Sampled at   | " +
	"Oversleep  | " +
	"Chan send  | " +
	"Ping-pong  | " +
	"Chain      |"

// Samples returns a text table of the last 100 samples.
func Samples() string {
	return highlightSample(sample{})
}

func highlightSample(hl sample) string {
	defer mu.Unlock()
	mu.Lock()
	var buf bytes.Buffer

	fmt.Fprintln(&buf, header)
	idx := nextIndex
	now := time.Now()
	for n := 0; n < historySize; n++ {
		idx--
		if idx < 0 {
			idx = historySize - 1
		}
		s := &samples[idx]
		if s.start.IsZero() {
			break
		}
		hls := ""
		if *s == hl {
			hls = " <---"
		}
		fmt.Fprintf(&buf, "| %7.1fs ago | %10v | %10v | %10v | %10v |%s\n",
			now.Sub(s.start).Seconds(),
			s.oversleep, s.bufSend, s.pingPong, s.chain,
			hls)
	}
	return buf.String()
}
