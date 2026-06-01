package store

import (
	"sync"
	"time"
)

type Generator struct {
	lock      sync.Mutex
	timestamp int64
	counter   uint32
}

const maxCounterBits = 19
const MaxCounter uint32 = 1<<maxCounterBits - 1

func NewGenerator() *Generator {
	return &Generator{}
}

func (g *Generator) Gen() uint64 {
	g.lock.Lock()
	defer g.lock.Unlock()
	return g.gen()
}

func (g *Generator) gen() uint64 {
	now := time.Now().UnixMilli()
	if now == g.timestamp {
		if g.counter == MaxCounter {
			time.Sleep(time.Millisecond)
			return g.gen()
		}
		g.counter++
	} else {
		g.counter = 0
		g.timestamp = now
	}
	return initUID(now, g.counter)
}

func initUID(timestamp int64, counter uint32) uint64 {
	var uid uint64 = 0
	uid += uint64(timestamp) << maxCounterBits
	uid += uint64(counter)
	return uid
}

func ParseUID(uid uint64) (time.Time, uint32) {
	return time.UnixMilli(int64(uid >> maxCounterBits)), uint32(uid) & MaxCounter
}
