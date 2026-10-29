package presign

import (
	"crypto/rand"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Activer007/Gokapi/internal/models"
)

func TestSEC021PresignSingleConsumer(t *testing.T) {
	id := rand.Text()
	Save(models.Presign{Id: id, Expiry: time.Now().Add(time.Minute).Unix(), FileIds: []string{"synthetic"}})
	t.Cleanup(func() { Delete(id) })
	start := make(chan struct{})
	var wg sync.WaitGroup
	var winners atomic.Int32
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if Consume(id) {
				winners.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()
	if winners.Load() != 1 {
		t.Fatalf("consumers=%d want=1", winners.Load())
	}
	if _, ok := Get(id); ok || Consume(id) {
		t.Fatal("consumed capability retained")
	}
}

func TestSEC021PresignExpiry(t *testing.T) {
	for _, expiry := range []int64{time.Now().Add(-time.Minute).Unix(), time.Now().Unix()} {
		id := rand.Text()
		Save(models.Presign{Id: id, Expiry: expiry})
		t.Cleanup(func() { Delete(id) })
		if _, ok := Get(id); ok || Consume(id) {
			t.Fatal("expired capability accepted")
		}
	}
	if Consume(rand.Text()) {
		t.Fatal("missing capability accepted")
	}
}
