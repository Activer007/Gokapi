//go:build test && awsmock && !noaws

package aws

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"io"
	"sync"
	"testing"

	"github.com/forceu/gokapi/internal/models"
)

func sec030AWSMockContextSetup(t *testing.T) {
	t.Helper()
	for name, value := range map[string]string{
		"GOKAPI_AWS_BUCKET": bucketName, "GOKAPI_AWS_REGION": region,
		"GOKAPI_AWS_KEY": accessId, "GOKAPI_AWS_KEY_SECRET": accessKey,
	} {
		t.Setenv(name, value)
	}
	mockStateMutex.RLock()
	saved := append([]models.File(nil), uploadedFiles...)
	savedLogin := isCorrectLogin
	mockStateMutex.RUnlock()
	t.Cleanup(func() {
		mockStateMutex.Lock()
		uploadedFiles, isCorrectLogin = saved, savedLogin
		mockStateMutex.Unlock()
	})
}

type sec030AWSBrokenReader struct{}

func (sec030AWSBrokenReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestSEC030AWSMockContextConsumesBody(t *testing.T) {
	sec030AWSMockContextSetup(t)
	file := models.File{AwsBucket: bucketName, SHA1: rand.Text(), SizeBytes: 999}
	reader := bytes.NewReader([]byte("synthetic-object"))
	if _, err := UploadContext(context.Background(), reader, file); err != nil || reader.Len() != 0 {
		t.Fatal("context upload did not consume the synthetic body")
	}
	if exists, size, err := FileExistsContext(context.Background(), file); err != nil || !exists || size != 16 {
		t.Fatal("context lookup did not report the actual uploaded length")
	}
	if exists, size, err := FileExists(file); err != nil || !exists || size != 10000 {
		t.Fatal("new context methods changed the legacy mock lookup contract")
	}
	failed := models.File{AwsBucket: bucketName, SHA1: rand.Text()}
	if _, err := UploadContext(context.Background(), sec030AWSBrokenReader{}, failed); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatal("context upload ignored a source read failure")
	}
	if exists, _, err := FileExistsContext(context.Background(), failed); err != nil || exists {
		t.Fatal("failed context upload registered a stored object")
	}
}

func TestSEC030AWSMockContextConcurrentUploads(t *testing.T) {
	sec030AWSMockContextSetup(t)
	const count = 10
	files := make([]models.File, count)
	for i := range files {
		files[i] = models.File{AwsBucket: bucketName, SHA1: rand.Text()}
	}
	errorsFound := make(chan error, count)
	var workers sync.WaitGroup
	for _, file := range files {
		workers.Add(1)
		go func(file models.File) {
			defer workers.Done()
			_, err := UploadContext(context.Background(), bytes.NewReader([]byte("abc")), file)
			errorsFound <- err
		}(file)
	}
	workers.Wait()
	close(errorsFound)
	for err := range errorsFound {
		if err != nil {
			t.Error("concurrent synthetic upload failed")
		}
	}
	for _, file := range files {
		if exists, size, err := FileExistsContext(context.Background(), file); err != nil || !exists || size != 3 {
			t.Error("concurrent synthetic upload was lost or mismeasured")
		}
	}
}

func TestSEC030AWSMockBucketIsolation(t *testing.T) {
	sec030AWSMockContextSetup(t)
	ctx := context.Background()
	first := models.File{AwsBucket: bucketName, SHA1: rand.Text()}
	second := models.File{AwsBucket: bucketName + "-other", SHA1: first.SHA1}
	firstBody, secondBody := []byte("abc"), []byte("different synthetic length")
	for _, candidate := range []struct {
		file models.File
		body []byte
	}{{first, firstBody}, {second, secondBody}} {
		if _, err := UploadContext(ctx, bytes.NewReader(candidate.body), candidate.file); err != nil {
			t.Fatal("synthetic bucket-scoped upload failed")
		}
	}
	for _, candidate := range []struct {
		file models.File
		size int64
	}{{first, int64(len(firstBody))}, {second, int64(len(secondBody))}} {
		if exists, size, err := FileExistsContext(ctx, candidate.file); err != nil || !exists || size != candidate.size {
			t.Fatal("same-hash objects in distinct buckets did not retain their own lengths")
		}
	}
	if deleted, err := DeleteObject(first); err != nil || !deleted {
		t.Fatal("synthetic bucket-scoped deletion failed")
	}
	if exists, size, err := FileExistsContext(ctx, first); err != nil || exists || size != 0 {
		t.Fatal("deleted bucket still reports the object")
	}
	if exists, size, err := FileExistsContext(ctx, second); err != nil || !exists || size != int64(len(secondBody)) {
		t.Fatal("deleting one bucket removed or changed the same-hash object in another bucket")
	}
}
