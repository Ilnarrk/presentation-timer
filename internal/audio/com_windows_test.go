//go:build windows

package audio

import (
	"sync"
	"testing"
)

func TestConcurrentListDevicesAndWarmup(t *testing.T) {
	player := NewPlayer(NewMemoryCatalog(nil))

	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := ListDevices()
			if err != nil {
				errs <- err
			}
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := player.Warmup(); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
}
