package screeps

import (
	"sync"
	"testing"
)

func TestSnapshotDeepCopiesObjectMaps(t *testing.T) {
	c := NewClient("http://localhost:21025", "", "", nil)
	c.mu.Lock()
	c.room = "W1N1"
	c.objects["c1"] = map[string]any{
		"_id":  "c1",
		"type": "creep",
		"x":    float64(10),
		"y":    float64(12),
		"store": map[string]any{
			"energy": float64(50),
		},
		"spawning": map[string]any{
			"name": "W1",
		},
	}
	c.mu.Unlock()

	var wg sync.WaitGroup
	wg.Add(2)

	// Writer simulates handleEvent mutating c.objects in place under c.mu.
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			c.mu.Lock()
			c.objects["c1"]["x"] = float64(i % 50)
			c.objects["c1"]["store"].(map[string]any)["energy"] = float64(i)
			c.mu.Unlock()
		}
	}()

	// Reader takes Snapshots and reads nested maps without holding c.mu.
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			snap := c.Snapshot()
			for _, obj := range snap.Objects {
				_ = obj.Store["energy"]
				_ = obj.Raw["x"]
				if obj.Spawning != nil {
					_ = obj.Spawning["name"]
				}
			}
		}
	}()

	wg.Wait()
}
