package expirecache

import (
	"math/rand"
	"sync"
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	_ = New[string, []byte](1024)
	_ = New[string, string](1024)
}

func TestCacheExpire(t *testing.T) {

	c := &Cache[string, string]{cache: make(map[string]element[string])}

	sleep := make(chan bool)
	cleanerSleep = func(_ time.Duration) { <-sleep }
	done := make(chan bool)
	cleanerDone = func() { <-done }

	defer func() {
		cleanerSleep = time.Sleep
		cleanerDone = func() {}
		timeNow = time.Now
	}()

	go c.Cleaner(5 * time.Minute)
	t0 := time.Now()

	timeNow = func() time.Time { return t0 }

	c.Set("foo", "bar", 3, 30)
	c.Set("baz", "qux", 3, 60)
	c.Set("zot", "bork", 4, 120)

	type expireTest struct {
		key string
		ok  bool
	}

	// test expiration logic in get()

	present := []expireTest{
		{"foo", true},
		{"baz", true},
		{"zot", true},
	}

	// unexpired
	for _, p := range present {

		b, ok := c.Get(p.key)

		if ok != p.ok || (ok != (b != "")) {
			t.Errorf("expireCache: bad unexpired cache.Get(%v)=(%v,%v), want %v", p.key, b, ok, p.ok)
		}
	}

	if len(c.keys) != 3 {
		t.Errorf("unexpired keys array length mismatch: got %d, want %d", len(c.keys), 3)
	}

	if c.totalSize != 3+3+4 {
		t.Errorf("unexpired cache size mismatch: got %d, want %d", c.totalSize, 3+3+4)
	}

	c.Set("baz", "snork", 5, 60)

	if len(c.keys) != 3 {
		t.Errorf("unexpired extra keys array length mismatch: got %d, want %d", len(c.keys), 3)
	}

	if c.totalSize != 3+5+4 {
		t.Errorf("unexpired extra cache size mismatch: got %d, want %d", c.totalSize, 3+3+4)
	}

	// expire key `foo`
	timeNow = func() time.Time { return t0.Add(45 * time.Second) }

	present = []expireTest{
		{"foo", false},
		{"baz", true},
		{"zot", true},
	}

	for _, p := range present {
		b, ok := c.Get(p.key)
		if ok != p.ok || (ok != (b != "")) {
			t.Errorf("expireCache: bad partial expire cache.Get(%v)=(%v,%v), want %v", p.key, b, ok, p.ok)
		}
	}

	// let the cleaner run
	timeNow = func() time.Time { return t0.Add(75 * time.Second) }
	sleep <- true
	done <- true

	present = []expireTest{
		{"foo", false},
		{"baz", false},
		{"zot", true},
	}

	for _, p := range present {
		b, ok := c.Get(p.key)
		if ok != p.ok || (ok != (b != "")) {
			t.Errorf("expireCache: bad partial expire cache.Get(%v)=(%v,%v), want %v", p.key, b, ok, p.ok)
		}
	}

	if len(c.keys) != 1 {
		t.Errorf("unexpired keys array length mismatch: got %d, want %d", len(c.keys), 3)
	}

	if c.totalSize != 4 {
		t.Errorf("unexpired cache size mismatch: got %d, want %d", c.totalSize, 3+3+4)
	}

	// getOrSet test
	d := "bar"
	b := c.GetOrSet("bork", d, 3, 30)
	if b != d {
		t.Errorf("GetOrSet should return the same object if key doesn't exist")
	}

	d2 := "baz"
	b = c.GetOrSet("bork", d2, 3, 30)
	if b != d {
		t.Errorf("GetOrSet should return existing key if it already exist")
	}

}

func random(min, max int) int {
	return rand.Intn(max-min) + min
}

type kv struct {
	key   string
	value string
}

func Benchmark(b *testing.B) {
	c := &Cache[string, string]{cache: make(map[string]element[string])}
	vals := []kv{
		{"1", "string 1"}, {"2", "string 2"}, {"3", "string 3"}, {"4", "string 4"},
		{"10", "string 10"}, {"100", "string 100"}, {"1000", "string 1000"}, {"10000", "string 10000"},
	}
	if len(vals) == 0 {
		b.Fatal("vals is empty")
	}
	b.Run("Set", func(b *testing.B) {
		for n := 0; n < b.N; n++ {
			j := random(0, len(vals))
			c.Set(vals[j].key, vals[j].value, uint64(len(vals[j].value)), 60)
		}
	})
	b.Run("Get", func(b *testing.B) {
		for n := 0; n < b.N; n++ {
			j := random(0, len(vals))
			if s, ok := c.Get(vals[j].key); ok {
				_ = s
			}
		}
	})
}

func benchmarkPCache(b *testing.B, readers, writers uint, vals []kv) {
	if len(vals) == 0 {
		b.Fatal("vals is empty")
	}
	var wg, wgStart sync.WaitGroup

	c := New[string, string](0)

	wgStart.Add(int(readers+writers) + 1)
	for i := 0; i < int(readers); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			wgStart.Done()
			wgStart.Wait()
			// Test routine
			for n := 0; n < b.N; n++ {
				j := random(0, len(vals))
				c.Set(vals[j].key, vals[j].value, uint64(len(vals[j].value)), 60)
			}
			// End test routine
		}()
	}

	for i := 0; i < int(writers); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			wgStart.Done()
			wgStart.Wait()
			// Test routine
			for n := 0; n < b.N; n++ {
				j := random(0, len(vals))
				if s, ok := c.Get(vals[j].key); ok {
					_ = s
				}
			}
			// End test routine
		}()
	}

	wgStart.Done()
	wg.Wait()
}

func BenchmarkCache_R10_W2(b *testing.B) {
	benchmarkPCache(b, 10, 2, []kv{
		{"1", "string 1"}, {"2", "string 2"}, {"3", "string 3"}, {"4", "string 4"},
		{"10", "string 10"}, {"100", "string 100"}, {"1000", "string 1000"}, {"10000", "string 10000"},
	})
}
