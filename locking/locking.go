package locking

import (
	"time"

	"github.com/PlakarLabs/plakar/logger"
	"github.com/PlakarLabs/plakar/profiler"
	"github.com/vmihailenco/msgpack/v5"
)

type Lock struct {
	Timestamp time.Time
}

func New() *Lock {
	return &Lock{
		Timestamp: time.Now(),
	}
}

func NewFromBytes(serialized []byte) (*Lock, error) {
	t0 := time.Now()
	defer func() {
		profiler.RecordEvent("locking.NewFromBytes", time.Since(t0))
		logger.Trace("locking", "NewFromBytes(...): %s", time.Since(t0))
	}()

	var lock Lock
	if err := msgpack.Unmarshal(serialized, &lock); err != nil {
		return nil, err
	}
	return &lock, nil
}

func (lock *Lock) Serialize() ([]byte, error) {
	t0 := time.Now()
	defer func() {
		profiler.RecordEvent("locking.Serialize", time.Since(t0))
		logger.Trace("locking", "Serialize(): %s", time.Since(t0))
	}()

	serialized, err := msgpack.Marshal(lock)
	if err != nil {
		return nil, err
	}
	return serialized, nil
}
