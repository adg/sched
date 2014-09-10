// Package example is a demonstration usage of the sched package.
package example

import (
	"net/http"
	"time"

	"appengine"
	"appengine/memcache"

	"github.com/adg/sched"
)

func init() {
	// Set this threshold low for demonstration purposes.
	sched.OversleepThreshold = time.Microsecond
	http.HandleFunc("/", handler)
}

func handler(w http.ResponseWriter, r *http.Request) {
	ctx := appengine.NewContext(r)
	sched.Check(ctx)

	// Do a couple of RPCs (why not?)
	const tv = "some test value"
	err := memcache.Set(ctx, &memcache.Item{
		Key:   "test",
		Value: []byte(tv),
	})
	if err != nil {
		ctx.Errorf("set: %v", err)
		return
	}
	item, err := memcache.Get(ctx, "test")
	if err != nil {
		ctx.Errorf("get: %v", err)
		return
	}
	if v := string(item.Value); v != tv {
		ctx.Errorf("got %q, want %q", v, tv)
	}
}
